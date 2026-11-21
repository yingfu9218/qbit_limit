package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"qbit_limit/internal/config"
	"qbit_limit/internal/db"
	"qbit_limit/internal/monitor"
	"qbit_limit/internal/qbit"
	"qbit_limit/internal/web/assets"
)

const sessionCookieName = "qbl_session"

type Server struct {
	store   *db.Store
	monitor *monitor.Service
	router  *gin.Engine
}

func NewServer(store *db.Store, mon *monitor.Service) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{
		store:   store,
		monitor: mon,
		router:  r,
	}
	s.registerRoutes()
	return s
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) registerRoutes() {
	// CORS middleware for local dev / LAN usage
	s.router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-Auth-Token")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Legacy Python API compatibility endpoint
	s.router.GET("/refresh_data", s.handleLegacyRefreshData)

	api := s.router.Group("/api")
	{
		// Auth endpoints
		api.GET("/auth/status", s.handleAuthStatus)
		api.POST("/auth/login", s.handleAuthLogin)
		api.POST("/auth/logout", s.handleAuthLogout)

		// Read-only monitoring & history endpoints
		api.GET("/dashboard", s.handleDashboard)
		api.GET("/history", s.handleHistory)
		api.GET("/history/export", s.handleHistoryExport)
		api.GET("/events", s.handleEvents)

		// Protected management & configuration endpoints (bypassed automatically when auth_enabled == false)
		admin := api.Group("")
		admin.Use(s.requireAdminAuth())
		{
			admin.GET("/settings", s.handleGetSettings)
			admin.PUT("/settings", s.handleUpdateSettings)
			admin.POST("/qbit/test", s.handleTestQBConnection)
			admin.POST("/monitor/sync", s.handleManualSync)
			admin.POST("/monitor/override", s.handleManualOverride)
		}
	}

	// Serve embedded frontend assets
	subFS, _ := fs.Sub(assets.FS, ".")
	fileServer := http.FileServer(http.FS(subFS))

	s.router.GET("/", func(c *gin.Context) {
		data, err := assets.FS.ReadFile("index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	s.router.NoRoute(func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" || !strings.Contains(path, ".") {
			// SPA fallback
			data, err := assets.FS.ReadFile("index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
			return
		}
		if _, err := assets.FS.Open(path); err == nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Status(http.StatusNotFound)
	})
}

// --- Auth Helpers ---

func (s *Server) makeSessionToken(cfg config.Settings) string {
	mac := hmac.New(sha256.New, []byte(cfg.SessionSecret))
	mac.Write([]byte("qbl_admin:" + cfg.AuthPasswordHash))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) isAuthenticated(c *gin.Context, cfg config.Settings) bool {
	if !cfg.AuthEnabled {
		return true
	}
	expected := s.makeSessionToken(cfg)
	if tok := c.GetHeader("X-Auth-Token"); tok != "" && hmac.Equal([]byte(tok), []byte(expected)) {
		return true
	}
	if cookie, err := c.Cookie(sessionCookieName); err == nil && hmac.Equal([]byte(cookie), []byte(expected)) {
		return true
	}
	return false
}

func (s *Server) requireAdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := s.monitor.GetSettings()
		if !s.isAuthenticated(c, cfg) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":         "需要验证后台管理密码",
				"auth_required": true,
			})
			return
		}
		c.Next()
	}
}

func (s *Server) handleAuthStatus(c *gin.Context) {
	cfg := s.monitor.GetSettings()
	c.JSON(http.StatusOK, gin.H{
		"auth_enabled":  cfg.AuthEnabled,
		"authenticated": s.isAuthenticated(c, cfg),
	})
}

func (s *Server) handleAuthLogin(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	cfg := s.monitor.GetSettings()
	if !cfg.AuthEnabled {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	hashed := config.HashPassword(req.Password, cfg.AuthSalt)
	if !hmac.Equal([]byte(hashed), []byte(cfg.AuthPasswordHash)) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "管理密码不正确"})
		return
	}
	tok := s.makeSessionToken(cfg)
	c.SetCookie(sessionCookieName, tok, 86400*7, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "token": tok})
}

func (s *Server) handleAuthLogout(c *gin.Context) {
	c.SetCookie(sessionCookieName, "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- Dashboard & Monitoring Handlers ---

func (s *Server) handleDashboard(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "14"))
	if days != 7 && days != 14 && days != 30 {
		days = 14
	}
	onlyLimited := c.Query("only_limited") == "1" || c.Query("only_limited") == "true"

	cfg := s.monitor.GetSettings()
	status := s.monitor.GetStatus()

	now := time.Now()
	today := now.Format("2006-01-02")
	cycleStart, cycleEnd, daysRem := monitor.BillingWindow(now, cfg.BillingResetDay)

	todayLog, _ := s.store.GetLogByDay(today)
	var todayUpload, todayDown int64
	if todayLog != nil {
		todayUpload = todayLog.Upload
		todayDown = todayLog.Down
	}

	monthSum, _ := s.store.GetPeriodSummary(cycleStart, today)

	// Fetch last 30 days for chart & recent list
	recentAll, _ := s.store.ListRecentLogs(30, false)
	recentFiltered := recentAll
	if onlyLimited {
		recentFiltered, _ = s.store.ListRecentLogs(30, true)
	}

	// Build chart series of exact `days` calendar days (oldest -> newest)
	logByDay := make(map[string]db.DailyLog, len(recentAll))
	for _, r := range recentAll {
		logByDay[r.DayTime] = r
	}
	type chartPoint struct {
		Date        string `json:"date"`
		Label       string `json:"label"`
		Upload      int64  `json:"upload_bytes"`
		Download    int64  `json:"download_bytes"`
		DenyLimit   bool   `json:"limited"`
		LimitReason string `json:"limit_reason"`
	}
	chart := make([]chartPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		ds := d.Format("2006-01-02")
		lbl := d.Format("01-02")
		pt := chartPoint{Date: ds, Label: lbl}
		if rec, ok := logByDay[ds]; ok {
			pt.Upload = rec.Upload
			pt.Download = rec.Down
			pt.DenyLimit = rec.DenyLimit == 1
			pt.LimitReason = rec.LimitReason
		}
		chart = append(chart, pt)
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"today": gin.H{
			"date":           today,
			"upload_bytes":   todayUpload,
			"download_bytes": todayDown,
			"limit_bytes":    cfg.DailyUploadLimit,
		},
		"month": gin.H{
			"start_day":      cycleStart,
			"end_day":        cycleEnd,
			"days_remaining": daysRem,
			"upload_bytes":   monthSum.TotalUpload,
			"download_bytes": monthSum.TotalDownload,
			"limit_bytes":    cfg.MonthlyUploadLimit,
			"limited_days":   monthSum.LimitedDays,
			"record_days":    monthSum.RecordDays,
		},
		"policy": gin.H{
			"daily_upload_limit":   cfg.DailyUploadLimit,
			"monthly_upload_limit": cfg.MonthlyUploadLimit,
			"normal_upload_speed":  cfg.NormalUploadSpeed,
			"deny_upload_speed":    cfg.DenyUploadSpeed,
			"poll_interval_sec":    cfg.PollIntervalSec,
			"billing_reset_day":    cfg.BillingResetDay,
			"qb_host":              cfg.QBHost,
		},
		"chart":       chart,
		"recent_logs": recentFiltered,
		"total_count": len(recentAll),
	})
}

func (s *Server) handleHistory(c *gin.Context) {
	month := strings.TrimSpace(c.Query("month"))
	onlyLimited := c.Query("only_limited") == "1" || c.Query("only_limited") == "true"

	months, err := s.store.ListAvailableMonths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if month == "" {
		if len(months) > 0 {
			month = months[0]
		} else {
			month = time.Now().Format("2006-01")
		}
	}

	logs, summary, err := s.store.ListLogsByMonth(month, onlyLimited)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"month":   month,
		"months":  months,
		"summary": summary,
		"logs":    logs,
	})
}

func (s *Server) handleHistoryExport(c *gin.Context) {
	month := strings.TrimSpace(c.Query("month"))
	onlyLimited := c.Query("only_limited") == "1" || c.Query("only_limited") == "true"

	logs, _, err := s.store.ListLogsByMonth(month, onlyLimited)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := "qbit_limit_history.csv"
	if month != "" {
		filename = fmt.Sprintf("qbit_limit_%s.csv", month)
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	// Write UTF-8 BOM for Excel compatibility
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"日期", "当日上行(格式化)", "当日上行(Bytes)", "当日下行(格式化)", "当日下行(Bytes)", "是否触发限速", "限速原因", "首次采集时间", "最后更新时间"})
	for _, r := range logs {
		lim := "否"
		if r.DenyLimit == 1 {
			lim = "是"
		}
		_ = w.Write([]string{
			r.DayTime,
			config.FormatBytes(r.Upload),
			strconv.FormatInt(r.Upload, 10),
			config.FormatBytes(r.Down),
			strconv.FormatInt(r.Down, 10),
			lim,
			r.LimitReason,
			r.CreatedAt,
			r.UpdatedAt,
		})
	}
	w.Flush()
}

func (s *Server) handleEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "80"))
	level := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("level", "ALL")))
	events, err := s.store.ListEvents(limit, level)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// --- Admin Settings & Control Handlers ---

func (s *Server) handleGetSettings(c *gin.Context) {
	cfg := s.monitor.GetSettings()
	c.JSON(http.StatusOK, cfg.ToPublic())
}

func (s *Server) handleUpdateSettings(c *gin.Context) {
	var req config.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数解析失败: " + err.Error()})
		return
	}

	cur := s.monitor.GetSettings()
	updated := cur
	updated.QBHost = req.QBHost
	updated.QBUsername = req.QBUsername
	if req.QBPassword != nil && *req.QBPassword != "" {
		updated.QBPassword = *req.QBPassword
	}
	updated.DailyUploadLimit = req.DailyUploadLimit
	updated.MonthlyUploadLimit = req.MonthlyUploadLimit
	updated.NormalUploadSpeed = req.NormalUploadSpeed
	updated.DenyUploadSpeed = req.DenyUploadSpeed
	updated.BillingResetDay = req.BillingResetDay
	updated.MonitorEnabled = req.MonitorEnabled
	updated.PollIntervalSec = req.PollIntervalSec
	updated.LogRetentionDays = req.LogRetentionDays
	updated.RestartProtect = req.RestartProtect
	updated.AuthEnabled = req.AuthEnabled

	if req.AuthPassword != nil && *req.AuthPassword != "" {
		salt := config.RandomHex(16)
		updated.AuthSalt = salt
		updated.AuthPasswordHash = config.HashPassword(*req.AuthPassword, salt)
	}

	if err := s.monitor.ApplySettings(updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// If auth is enabled, refresh session cookie so current admin stays logged in seamlessly.
	if updated.AuthEnabled {
		tok := s.makeSessionToken(updated)
		c.SetCookie(sessionCookieName, tok, 86400*7, "/", "", false, true)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"settings": updated.ToPublic(),
	})
}

func (s *Server) handleTestQBConnection(c *gin.Context) {
	var req struct {
		QBHost     string `json:"qb_host"`
		QBUsername string `json:"qb_username"`
		QBPassword string `json:"qb_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	cur := s.monitor.GetSettings()
	pwd := req.QBPassword
	if pwd == "" {
		pwd = cur.QBPassword
	}
	res := qbit.TestConnection(c.Request.Context(), req.QBHost, req.QBUsername, pwd)
	c.JSON(http.StatusOK, res)
}

func (s *Server) handleManualSync(c *gin.Context) {
	err := s.monitor.SyncOnce(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":     false,
			"error":  err.Error(),
			"status": s.monitor.GetStatus(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"status": s.monitor.GetStatus(),
	})
}

func (s *Server) handleManualOverride(c *gin.Context) {
	var req struct {
		ManualDeny bool `json:"manual_deny"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	err := s.monitor.SetManualDeny(req.ManualDeny)
	c.JSON(http.StatusOK, gin.H{
		"ok":     err == nil,
		"error":  errString(err),
		"status": s.monitor.GetStatus(),
	})
}

// handleLegacyRefreshData preserves full compatibility with the original Python /refresh_data endpoint.
func (s *Server) handleLegacyRefreshData(c *gin.Context) {
	logs, err := s.store.ListRecentLogs(30, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	formatted := make([]gin.H, 0, len(logs))
	for _, r := range logs {
		lim := "否"
		if r.DenyLimit == 1 {
			lim = "是"
		}
		formatted = append(formatted, gin.H{
			"date":       r.DayTime,
			"upload":     config.FormatBytes(r.Upload),
			"download":   config.FormatBytes(r.Down),
			"limited":    lim,
			"created_at": r.CreatedAt,
			"updated_at": r.UpdatedAt,
		})
	}
	now := time.Now()
	monthStart := now.Format("2006-01") + "-01"
	today := now.Format("2006-01-02")
	sum, _ := s.store.GetPeriodSummary(monthStart, today)

	c.JSON(http.StatusOK, gin.H{
		"month_upload":   config.FormatBytes(sum.TotalUpload),
		"month_download": config.FormatBytes(sum.TotalDownload),
		"logs":           formatted,
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
