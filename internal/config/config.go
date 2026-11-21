package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	KiB int64 = 1024
	MiB int64 = 1024 * 1024
	GiB int64 = 1024 * 1024 * 1024
)

// Settings holds all persistent application settings stored in SQLite.
type Settings struct {
	QBHost             string `json:"qb_host"`
	QBUsername         string `json:"qb_username"`
	QBPassword         string `json:"qb_password"`
	DailyUploadLimit   int64  `json:"daily_upload_limit"`   // bytes
	MonthlyUploadLimit int64  `json:"monthly_upload_limit"` // bytes
	NormalUploadSpeed  int64  `json:"normal_upload_speed"`  // bytes/sec (0 = unlimited)
	DenyUploadSpeed    int64  `json:"deny_upload_speed"`    // bytes/sec (default 5 KiB/s)
	BillingResetDay    int    `json:"billing_reset_day"`    // 1..28
	MonitorEnabled     bool   `json:"monitor_enabled"`
	PollIntervalSec    int    `json:"poll_interval_sec"`
	LogRetentionDays   int    `json:"log_retention_days"`
	RestartProtect     bool   `json:"restart_protect"`
	AuthEnabled        bool   `json:"auth_enabled"`
	AuthPasswordHash   string `json:"-"`
	AuthSalt           string `json:"-"`
	SessionSecret      string `json:"-"`
}

// PublicSettings is returned to the frontend (masks sensitive passwords/secrets).
type PublicSettings struct {
	QBHost             string `json:"qb_host"`
	QBUsername         string `json:"qb_username"`
	QBPasswordSet      bool   `json:"qb_password_set"`
	DailyUploadLimit   int64  `json:"daily_upload_limit"`
	MonthlyUploadLimit int64  `json:"monthly_upload_limit"`
	NormalUploadSpeed  int64  `json:"normal_upload_speed"`
	DenyUploadSpeed    int64  `json:"deny_upload_speed"`
	BillingResetDay    int    `json:"billing_reset_day"`
	MonitorEnabled     bool   `json:"monitor_enabled"`
	PollIntervalSec    int    `json:"poll_interval_sec"`
	LogRetentionDays   int    `json:"log_retention_days"`
	RestartProtect     bool   `json:"restart_protect"`
	AuthEnabled        bool   `json:"auth_enabled"`
	AuthPasswordSet    bool   `json:"auth_password_set"`
}

// UpdateSettingsRequest represents the JSON payload when saving settings from UI.
type UpdateSettingsRequest struct {
	QBHost             string  `json:"qb_host"`
	QBUsername         string  `json:"qb_username"`
	QBPassword         *string `json:"qb_password,omitempty"`
	DailyUploadLimit   int64   `json:"daily_upload_limit"`
	MonthlyUploadLimit int64   `json:"monthly_upload_limit"`
	NormalUploadSpeed  int64   `json:"normal_upload_speed"`
	DenyUploadSpeed    int64   `json:"deny_upload_speed"`
	BillingResetDay    int     `json:"billing_reset_day"`
	MonitorEnabled     bool    `json:"monitor_enabled"`
	PollIntervalSec    int     `json:"poll_interval_sec"`
	LogRetentionDays   int     `json:"log_retention_days"`
	RestartProtect     bool    `json:"restart_protect"`
	AuthEnabled        bool    `json:"auth_enabled"`
	AuthPassword       *string `json:"auth_password,omitempty"`
}

// DefaultSettings returns sensible initial defaults matching the original project.
func DefaultSettings() Settings {
	return Settings{
		QBHost:             "http://127.0.0.1:8080",
		QBUsername:         "admin",
		QBPassword:         "",
		DailyUploadLimit:   20 * GiB,
		MonthlyUploadLimit: 600 * GiB,
		NormalUploadSpeed:  1 * MiB,
		DenyUploadSpeed:    5 * KiB,
		BillingResetDay:    1,
		MonitorEnabled:     true,
		PollIntervalSec:    10,
		LogRetentionDays:   90,
		RestartProtect:     true,
		AuthEnabled:        false, // 默认关闭，局域网直接打开可用
		SessionSecret:      RandomHex(32),
	}
}

func (s Settings) ToPublic() PublicSettings {
	return PublicSettings{
		QBHost:             s.QBHost,
		QBUsername:         s.QBUsername,
		QBPasswordSet:      s.QBPassword != "",
		DailyUploadLimit:   s.DailyUploadLimit,
		MonthlyUploadLimit: s.MonthlyUploadLimit,
		NormalUploadSpeed:  s.NormalUploadSpeed,
		DenyUploadSpeed:    s.DenyUploadSpeed,
		BillingResetDay:    s.BillingResetDay,
		MonitorEnabled:     s.MonitorEnabled,
		PollIntervalSec:    s.PollIntervalSec,
		LogRetentionDays:   s.LogRetentionDays,
		RestartProtect:     s.RestartProtect,
		AuthEnabled:        s.AuthEnabled,
		AuthPasswordSet:    s.AuthPasswordHash != "",
	}
}

// NormalizeHost ensures host has a scheme and no trailing slash.
func NormalizeHost(raw string) string {
	h := strings.TrimSpace(raw)
	if h == "" {
		return ""
	}
	if !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
		h = "http://" + h
	}
	return strings.TrimRight(h, "/")
}

// Validate checks bounds on user-supplied settings.
func (s *Settings) Validate() error {
	s.QBHost = NormalizeHost(s.QBHost)
	s.QBUsername = strings.TrimSpace(s.QBUsername)

	if s.DailyUploadLimit < 0 {
		return fmt.Errorf("每日上行上限不能为负数")
	}
	if s.MonthlyUploadLimit < 0 {
		return fmt.Errorf("每月上行上限不能为负数")
	}
	if s.NormalUploadSpeed < 0 {
		return fmt.Errorf("正常上行速率限制不能为负数")
	}
	if s.DenyUploadSpeed < 1024 {
		return fmt.Errorf("超限熔断速率至少为 1 KiB/s (1024 B/s)")
	}
	if s.BillingResetDay < 1 || s.BillingResetDay > 28 {
		return fmt.Errorf("月度统计重置日必须在 1 到 28 之间")
	}
	if s.PollIntervalSec < 3 || s.PollIntervalSec > 3600 {
		return fmt.Errorf("轮询检测间隔必须在 3 到 3600 秒之间")
	}
	if s.LogRetentionDays < 7 || s.LogRetentionDays > 3650 {
		return fmt.Errorf("历史日志保留天数必须在 7 到 3650 天之间")
	}
	if s.AuthEnabled && s.AuthPasswordHash == "" {
		return fmt.Errorf("开启后台管理密码保护时必须设置访问密码")
	}
	if s.SessionSecret == "" {
		s.SessionSecret = RandomHex(32)
	}
	return nil
}

// HashPassword hashes a plaintext password with a salt using stretched SHA-256.
func HashPassword(password, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + password))
	for i := 0; i < 10000; i++ {
		h = sha256.Sum256(append([]byte(salt), h[:]...))
	}
	return hex.EncodeToString(h[:])
}

// RandomHex generates n random bytes encoded as hex.
func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FormatBytes formats bytes in IEC binary units (KiB, MiB, GiB, TiB), matching humanize.naturalsize(..., binary=True).
func FormatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.2f %s", float64(b)/float64(div), units[exp])
}

// FormatSpeed formats bytes/sec as human readable speed.
func FormatSpeed(bps int64) string {
	if bps <= 0 {
		return "0 B/s"
	}
	return FormatBytes(bps) + "/s"
}
