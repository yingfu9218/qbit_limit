package monitor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"qbit_limit/internal/config"
	"qbit_limit/internal/db"
	"qbit_limit/internal/qbit"
)

// LiveStatus exposes real-time runtime telemetry for the Web UI dashboard.
type LiveStatus struct {
	Connected       bool   `json:"connected"`
	LastError       string `json:"last_error,omitempty"`
	AppVersion      string `json:"app_version"`
	LatencyMs       int64  `json:"latency_ms"`
	LastSyncAt      string `json:"last_sync_at"`
	UpSpeed         int64  `json:"up_speed"`
	DownSpeed       int64  `json:"down_speed"`
	CurrentUpLimit  int64  `json:"current_up_limit"`
	IsLimited       bool   `json:"is_limited"`
	LimitMode       string `json:"limit_mode"` // "normal" | "daily_over" | "monthly_over" | "manual_deny" | "paused"
	ManualDeny      bool   `json:"manual_deny"`
	MonitorEnabled  bool   `json:"monitor_enabled"`
	BillingStartDay string `json:"billing_start_day"`
	BillingEndDay   string `json:"billing_end_day"`
	DaysRemaining   int    `json:"days_remaining"`
}

type Service struct {
	store  *db.Store
	client *qbit.Client

	mu           sync.RWMutex
	cfg          config.Settings
	status       LiveStatus
	manualDeny   bool
	lastLimitLog string // avoid spamming identical events every 10s
	lastErrLog   string

	reloadCh chan struct{}
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewService(store *db.Store, cfg config.Settings) *Service {
	s := &Service{
		store:    store,
		client:   qbit.NewClient(cfg.QBHost, cfg.QBUsername, cfg.QBPassword),
		cfg:      cfg,
		reloadCh: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
	s.status.MonitorEnabled = cfg.MonitorEnabled
	s.status.LimitMode = "normal"
	s.updateBillingWindowLocked(time.Now())
	return s
}

func (s *Service) Start() {
	s.wg.Add(1)
	go s.loop()
}

func (s *Service) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Service) GetSettings() config.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Service) GetStatus() LiveStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.status
	st.ManualDeny = s.manualDeny
	st.MonitorEnabled = s.cfg.MonitorEnabled
	return st
}

// ApplySettings hot-reloads settings into the running monitor and triggers an immediate sync.
func (s *Service) ApplySettings(newCfg config.Settings) error {
	if err := newCfg.Validate(); err != nil {
		return err
	}
	if err := s.store.SaveSettings(newCfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = newCfg
	s.client.Configure(newCfg.QBHost, newCfg.QBUsername, newCfg.QBPassword)
	s.status.MonitorEnabled = newCfg.MonitorEnabled
	s.status.AppVersion = "" // refresh version on next poll
	s.updateBillingWindowLocked(time.Now())
	s.mu.Unlock()

	_ = s.store.AddEvent("INFO", fmt.Sprintf("配置已更新并热生效（日限额 %s / 月限额 %s / 常规限速 %s / 熔断速率 %s）",
		config.FormatBytes(newCfg.DailyUploadLimit),
		config.FormatBytes(newCfg.MonthlyUploadLimit),
		formatLimitOrUnlimited(newCfg.NormalUploadSpeed),
		config.FormatSpeed(newCfg.DenyUploadSpeed),
	))

	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
	return nil
}

// SetManualDeny toggles manual circuit-breaker throttling and syncs immediately.
func (s *Service) SetManualDeny(deny bool) error {
	s.mu.Lock()
	s.manualDeny = deny
	s.mu.Unlock()

	if deny {
		_ = s.store.AddEvent("WARN", "已手动开启熔断限速，立即下调至保活上传速率")
	} else {
		_ = s.store.AddEvent("INFO", "已解除手动熔断限速，恢复自动配额策略")
	}
	return s.SyncOnce(context.Background())
}

func (s *Service) loop() {
	defer s.wg.Done()

	// Initial sync immediately on boot.
	_ = s.SyncOnce(context.Background())
	lastPruneDay := ""

	for {
		interval := time.Duration(s.GetSettings().PollIntervalSec) * time.Second
		if interval < 3*time.Second {
			interval = 10 * time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-s.stopCh:
			timer.Stop()
			return
		case <-s.reloadCh:
			timer.Stop()
			_ = s.SyncOnce(context.Background())
		case <-timer.C:
			_ = s.SyncOnce(context.Background())
			today := time.Now().Format("2006-01-02")
			if today != lastPruneDay {
				lastPruneDay = today
				_ = s.store.PruneOldEvents(s.GetSettings().LogRetentionDays)
			}
		}
	}
}

// BillingWindow returns the inclusive start and end YYYY-MM-DD of the current billing cycle and remaining days.
func BillingWindow(now time.Time, resetDay int) (startDay, endDay string, daysRemaining int) {
	if resetDay < 1 || resetDay > 28 {
		resetDay = 1
	}
	loc := now.Location()
	y, m, d := now.Date()
	var start, nextStart time.Time
	if d >= resetDay {
		start = time.Date(y, m, resetDay, 0, 0, 0, 0, loc)
		nextStart = start.AddDate(0, 1, 0)
	} else {
		start = time.Date(y, m-1, resetDay, 0, 0, 0, 0, loc)
		nextStart = time.Date(y, m, resetDay, 0, 0, 0, 0, loc)
	}
	end := nextStart.AddDate(0, 0, -1)
	todayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)
	rem := int(nextStart.Sub(todayStart).Hours() / 24)
	if rem < 0 {
		rem = 0
	}
	return start.Format("2006-01-02"), end.Format("2006-01-02"), rem
}

func (s *Service) updateBillingWindowLocked(now time.Time) {
	start, end, rem := BillingWindow(now, s.cfg.BillingResetDay)
	s.status.BillingStartDay = start
	s.status.BillingEndDay = end
	s.status.DaysRemaining = rem
}

// SyncOnce polls qBittorrent, updates SQLite daily log, and enforces upload speed limits.
func (s *Service) SyncOnce(ctx context.Context) error {
	s.mu.RLock()
	cfg := s.cfg
	manualDeny := s.manualDeny
	needVer := s.status.AppVersion == ""
	s.mu.RUnlock()

	now := time.Now()
	today := now.Format("2006-01-02")
	nowStr := now.Format("2006-01-02 15:04:05")
	cycleStart, cycleEnd, daysRem := BillingWindow(now, cfg.BillingResetDay)

	if cfg.QBHost == "" {
		s.recordError("尚未配置 qBittorrent 服务地址，请前往「系统配置」填写", 0)
		return fmt.Errorf("qb_host not configured")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if needVer {
		if ver, _, _, err := s.client.GetVersion(reqCtx); err == nil && ver != "" {
			s.mu.Lock()
			s.status.AppVersion = ver
			s.mu.Unlock()
		}
	}

	info, latency, err := s.client.GetTransferInfo(reqCtx)
	if err != nil {
		s.recordError(err.Error(), latency)
		return err
	}

	totalUpload := info.UPInfoData
	totalDownload := info.DLInfoData
	currentUpLimit := info.UPRateLimit

	existing, err := s.store.GetLogByDay(today)
	if err != nil {
		s.recordError(fmt.Sprintf("读取当日数据库记录失败: %v", err), latency)
		return err
	}

	var todayRec db.DailyLog
	isNewDay := existing == nil
	if isNewDay {
		todayRec = db.DailyLog{
			DayTime:            today,
			TotalUploadFirst:   totalUpload,
			TotalDownloadFirst: totalDownload,
			UploadBase:         0,
			DownBase:           0,
			Upload:             0,
			Down:               0,
			DenyLimit:          0,
			LimitReason:        "",
			CreatedAt:          nowStr,
			UpdatedAt:          nowStr,
		}
	} else {
		todayRec = *existing
		todayRec.UpdatedAt = nowStr

		// Handle upload counter (with restart protection).
		if totalUpload >= todayRec.TotalUploadFirst {
			todayRec.Upload = todayRec.UploadBase + (totalUpload - todayRec.TotalUploadFirst)
		} else {
			// qBittorrent restarted and reset session counter!
			if cfg.RestartProtect {
				prevUpload := todayRec.Upload
				todayRec.UploadBase = prevUpload
				todayRec.TotalUploadFirst = 0
				todayRec.Upload = todayRec.UploadBase + totalUpload
				_ = s.store.AddEvent("INFO", fmt.Sprintf("检测到 qBittorrent 会话上行计数重置，已保留重启前当日累计 %s 继续统计", config.FormatBytes(prevUpload)))
			} else {
				todayRec.TotalUploadFirst = totalUpload
				todayRec.UploadBase = 0
				todayRec.Upload = 0
			}
		}

		// Handle download counter similarly.
		if totalDownload >= todayRec.TotalDownloadFirst {
			todayRec.Down = todayRec.DownBase + (totalDownload - todayRec.TotalDownloadFirst)
		} else {
			if cfg.RestartProtect {
				todayRec.DownBase = todayRec.Down
				todayRec.TotalDownloadFirst = 0
				todayRec.Down = todayRec.DownBase + totalDownload
			} else {
				todayRec.TotalDownloadFirst = totalDownload
				todayRec.DownBase = 0
				todayRec.Down = 0
			}
		}
	}

	// Calculate monthly cycle upload (excluding old today value and adding fresh todayRec.Upload).
	periodSum, err := s.store.GetPeriodSummary(cycleStart, today)
	if err != nil {
		s.recordError(fmt.Sprintf("汇总月度流量失败: %v", err), latency)
		return err
	}
	monthUpload := periodSum.TotalUpload
	if existing != nil {
		monthUpload = monthUpload - existing.Upload + todayRec.Upload
	} else {
		monthUpload += todayRec.Upload
	}

	// Determine limit policy.
	limitMode := "normal"
	shouldDeny := false
	limitReason := ""

	if !cfg.MonitorEnabled {
		limitMode = "paused"
	} else if manualDeny {
		shouldDeny = true
		limitMode = "manual_deny"
		limitReason = "手动熔断"
	} else if cfg.MonthlyUploadLimit > 0 && monthUpload >= cfg.MonthlyUploadLimit {
		shouldDeny = true
		limitMode = "monthly_over"
		limitReason = "月流量超限"
	} else if cfg.DailyUploadLimit > 0 && todayRec.Upload >= cfg.DailyUploadLimit {
		shouldDeny = true
		limitMode = "daily_over"
		limitReason = "日流量超限"
	}

	if shouldDeny {
		todayRec.DenyLimit = 1
		todayRec.LimitReason = limitReason
	} else if cfg.MonitorEnabled {
		todayRec.DenyLimit = 0
		todayRec.LimitReason = ""
	}

	if isNewDay {
		if err := s.store.InsertDailyLog(todayRec); err != nil {
			s.recordError(fmt.Sprintf("写入当日记录失败: %v", err), latency)
			return err
		}
		_ = s.store.AddEvent("INFO", fmt.Sprintf("初始化 %s 流量记录，当月累计上行 %s", today, config.FormatBytes(monthUpload)))
	} else {
		if err := s.store.UpdateDailyLog(todayRec); err != nil {
			s.recordError(fmt.Sprintf("更新当日记录失败: %v", err), latency)
			return err
		}
	}

	// Apply speed limit to qBittorrent if monitoring is enabled.
	appliedLimit := currentUpLimit
	if cfg.MonitorEnabled {
		targetSpeed := cfg.NormalUploadSpeed
		if shouldDeny {
			targetSpeed = cfg.DenyUploadSpeed
		}
		if currentUpLimit != targetSpeed {
			if err := s.client.SetUploadLimit(reqCtx, targetSpeed); err != nil {
				s.recordError(fmt.Sprintf("设置 qBittorrent 限速失败: %v", err), latency)
				return err
			}
			appliedLimit = targetSpeed
			if shouldDeny {
				s.logLimitTransition("WARN", limitMode, fmt.Sprintf("触发%s（今日 %s / 本月 %s），已将上行限速调整为 %s",
					limitReason, config.FormatBytes(todayRec.Upload), config.FormatBytes(monthUpload), config.FormatSpeed(targetSpeed)))
			} else {
				s.logLimitTransition("INFO", "normal", fmt.Sprintf("已将 qBittorrent 上行速率恢复为常规策略 %s", formatLimitOrUnlimited(targetSpeed)))
			}
		}
	}

	s.mu.Lock()
	s.status.Connected = true
	s.status.LastError = ""
	s.status.LatencyMs = latency
	s.status.LastSyncAt = nowStr
	s.status.UpSpeed = info.UPInfoSpeed
	s.status.DownSpeed = info.DLInfoSpeed
	s.status.CurrentUpLimit = appliedLimit
	s.status.IsLimited = shouldDeny
	s.status.LimitMode = limitMode
	s.status.MonitorEnabled = cfg.MonitorEnabled
	s.status.BillingStartDay = cycleStart
	s.status.BillingEndDay = cycleEnd
	s.status.DaysRemaining = daysRem
	s.lastErrLog = ""
	s.mu.Unlock()

	return nil
}

func (s *Service) recordError(msg string, latency int64) {
	s.mu.Lock()
	s.status.Connected = false
	s.status.LastError = msg
	s.status.LatencyMs = latency
	s.status.LastSyncAt = time.Now().Format("2006-01-02 15:04:05")
	shouldLog := s.lastErrLog != msg
	s.lastErrLog = msg
	s.mu.Unlock()

	if shouldLog {
		log.Printf("[WARN] %s", msg)
		_ = s.store.AddEvent("ERROR", msg)
	}
}

func (s *Service) logLimitTransition(level, key, msg string) {
	s.mu.Lock()
	changed := s.lastLimitLog != key
	s.lastLimitLog = key
	s.mu.Unlock()
	if changed {
		log.Printf("[%s] %s", level, msg)
		_ = s.store.AddEvent(level, msg)
	}
}

func formatLimitOrUnlimited(bps int64) string {
	if bps <= 0 {
		return "不限速"
	}
	return config.FormatSpeed(bps)
}
