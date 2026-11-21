package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"qbit_limit/internal/config"

	_ "modernc.org/sqlite"
)

// DailyLog maps directly to the legacy `log` table plus optional non-destructive extension columns.
type DailyLog struct {
	ID                 int64  `json:"id"`
	DayTime            string `json:"date"`
	TotalUploadFirst   int64  `json:"total_upload_first"`
	TotalDownloadFirst int64  `json:"total_download_first"`
	UploadBase         int64  `json:"upload_base"`
	DownBase           int64  `json:"down_base"`
	Upload             int64  `json:"upload_bytes"`
	Down               int64  `json:"download_bytes"`
	DenyLimit          int    `json:"deny_limit"`
	LimitReason        string `json:"limit_reason"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// EventLog represents a runtime event / rate-limit action log entry.
type EventLog struct {
	ID        int64  `json:"id"`
	Level     string `json:"level"` // INFO, WARN, ERROR
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// PeriodSummary holds aggregated upload/download totals for a date range.
type PeriodSummary struct {
	TotalUpload   int64 `json:"total_upload"`
	TotalDownload int64 `json:"total_download"`
	LimitedDays   int   `json:"limited_days"`
	RecordDays    int   `json:"record_days"`
}

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// Open initializes the SQLite database (pure-Go modernc.org/sqlite) and runs non-destructive migrations
// that are 100% compatible with existing Python qb.db files.
func Open(dbPath string) (*Store, error) {
	dsn := dbPath
	if !strings.Contains(dsn, "?") {
		dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// Single writer connection keeps SQLite completely lock-free under concurrent Gin + scheduler access.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: sqlDB}
	if err := s.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate() error {
	// 1. Preserve exact legacy `log` table schema so existing qb.db opens seamlessly.
	createLogSQL := `
CREATE TABLE IF NOT EXISTS "log" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "day_time" DATE,
  "total_upload_first" integer,
  "total_download_first" integer,
  "upload" integer,
  "down" integer,
  "deny_limit" integer DEFAULT 0,
  "created_at" DATE,
  "updated_at" DATE
);`
	if _, err := s.db.Exec(createLogSQL); err != nil {
		return fmt.Errorf("create log table: %w", err)
	}

	// Non-destructively add helper columns if missing (for restart counter protection & limit reason).
	existingCols, err := s.tableColumns("log")
	if err != nil {
		return err
	}
	addCols := []struct {
		name string
		ddl  string
	}{
		{"upload_base", `ALTER TABLE "log" ADD COLUMN "upload_base" integer DEFAULT 0`},
		{"down_base", `ALTER TABLE "log" ADD COLUMN "down_base" integer DEFAULT 0`},
		{"limit_reason", `ALTER TABLE "log" ADD COLUMN "limit_reason" text DEFAULT ''`},
	}
	for _, col := range addCols {
		if !existingCols[col.name] {
			if _, err := s.db.Exec(col.ddl); err != nil {
				return fmt.Errorf("migrate column %s: %w", col.name, err)
			}
		}
	}

	// Index on day_time for fast daily/monthly queries.
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_log_day_time ON "log"("day_time");`)

	// 2. Settings table for backend general configuration.
	createSettingsSQL := `
CREATE TABLE IF NOT EXISTS "settings" (
  "key" text NOT NULL PRIMARY KEY,
  "value" text NOT NULL,
  "updated_at" text NOT NULL
);`
	if _, err := s.db.Exec(createSettingsSQL); err != nil {
		return fmt.Errorf("create settings table: %w", err)
	}

	// 3. Runtime events table for Web UI event stream.
	createEventsSQL := `
CREATE TABLE IF NOT EXISTS "events" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "level" text NOT NULL,
  "message" text NOT NULL,
  "created_at" text NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON "events"("created_at");`
	if _, err := s.db.Exec(createEventsSQL); err != nil {
		return fmt.Errorf("create events table: %w", err)
	}

	return nil
}

func (s *Store) tableColumns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// LoadSettings reads settings from SQLite or initializes defaults on first run.
func (s *Store) LoadSettings() (config.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'app_config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		def := config.DefaultSettings()
		if saveErr := s.saveSettingsLocked(def); saveErr != nil {
			return def, saveErr
		}
		return def, nil
	}
	if err != nil {
		return config.DefaultSettings(), err
	}

	// Use persisted struct wrapper so secret fields are also stored in SQLite.
	type persistedSettings struct {
		config.Settings
		AuthPasswordHash string `json:"auth_password_hash"`
		AuthSalt         string `json:"auth_salt"`
		SessionSecret    string `json:"session_secret"`
	}
	def := config.DefaultSettings()
	ps := persistedSettings{Settings: def}
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		return def, fmt.Errorf("unmarshal settings: %w", err)
	}
	res := ps.Settings
	res.AuthPasswordHash = ps.AuthPasswordHash
	res.AuthSalt = ps.AuthSalt
	res.SessionSecret = ps.SessionSecret
	if res.SessionSecret == "" {
		res.SessionSecret = config.RandomHex(32)
		_ = s.saveSettingsLocked(res)
	}
	if res.BillingResetDay < 1 || res.BillingResetDay > 28 {
		res.BillingResetDay = 1
	}
	if res.PollIntervalSec < 3 {
		res.PollIntervalSec = 10
	}
	if res.LogRetentionDays < 7 {
		res.LogRetentionDays = 90
	}
	return res, nil
}

// SaveSettings persists all configuration fields to SQLite.
func (s *Store) SaveSettings(cfg config.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSettingsLocked(cfg)
}

func (s *Store) saveSettingsLocked(cfg config.Settings) error {
	type persistedSettings struct {
		config.Settings
		AuthPasswordHash string `json:"auth_password_hash"`
		AuthSalt         string `json:"auth_salt"`
		SessionSecret    string `json:"session_secret"`
	}
	ps := persistedSettings{
		Settings:         cfg,
		AuthPasswordHash: cfg.AuthPasswordHash,
		AuthSalt:         cfg.AuthSalt,
		SessionSecret:    cfg.SessionSecret,
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = s.db.Exec(
		`INSERT INTO settings(key, value, updated_at) VALUES('app_config', ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		string(b), now,
	)
	return err
}

// GetLogByDay fetches the row for a specific YYYY-MM-DD day, or returns nil if not found.
func (s *Store) GetLogByDay(day string) (*DailyLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`
		SELECT id,
		       COALESCE(CAST(day_time AS TEXT), ''),
		       COALESCE(total_upload_first, 0),
		       COALESCE(total_download_first, 0),
		       COALESCE(upload_base, 0),
		       COALESCE(down_base, 0),
		       COALESCE(upload, 0),
		       COALESCE(down, 0),
		       COALESCE(deny_limit, 0),
		       COALESCE(limit_reason, ''),
		       COALESCE(CAST(created_at AS TEXT), ''),
		       COALESCE(CAST(updated_at AS TEXT), '')
		FROM "log"
		WHERE CAST(day_time AS TEXT) = ?
		ORDER BY id DESC
		LIMIT 1`, day)

	var l DailyLog
	err := row.Scan(
		&l.ID, &l.DayTime, &l.TotalUploadFirst, &l.TotalDownloadFirst,
		&l.UploadBase, &l.DownBase, &l.Upload, &l.Down, &l.DenyLimit,
		&l.LimitReason, &l.CreatedAt, &l.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// InsertDailyLog creates a new daily row.
func (s *Store) InsertDailyLog(l DailyLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO "log"
		("day_time", "total_upload_first", "total_download_first", "upload_base", "down_base", "upload", "down", "deny_limit", "limit_reason", "created_at", "updated_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.DayTime, l.TotalUploadFirst, l.TotalDownloadFirst,
		l.UploadBase, l.DownBase, l.Upload, l.Down,
		l.DenyLimit, l.LimitReason, l.CreatedAt, l.UpdatedAt,
	)
	return err
}

// UpdateDailyLog updates an existing daily record by ID.
func (s *Store) UpdateDailyLog(l DailyLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE "log"
		SET total_upload_first = ?,
		    total_download_first = ?,
		    upload_base = ?,
		    down_base = ?,
		    upload = ?,
		    down = ?,
		    deny_limit = ?,
		    limit_reason = ?,
		    updated_at = ?
		WHERE id = ?`,
		l.TotalUploadFirst, l.TotalDownloadFirst,
		l.UploadBase, l.DownBase, l.Upload, l.Down,
		l.DenyLimit, l.LimitReason, l.UpdatedAt, l.ID,
	)
	return err
}

// GetPeriodSummary calculates total upload, download, and limited day counts between startDay and endDay (inclusive).
func (s *Store) GetPeriodSummary(startDay, endDay string) (PeriodSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var sum PeriodSummary
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(upload), 0),
		       COALESCE(SUM(down), 0),
		       COALESCE(SUM(CASE WHEN deny_limit = 1 THEN 1 ELSE 0 END), 0),
		       COUNT(*)
		FROM "log"
		WHERE CAST(day_time AS TEXT) BETWEEN ? AND ?`,
		startDay, endDay,
	).Scan(&sum.TotalUpload, &sum.TotalDownload, &sum.LimitedDays, &sum.RecordDays)
	return sum, err
}

// ListRecentLogs returns up to `limit` most recent daily records ordered newest-first.
func (s *Store) ListRecentLogs(limit int, onlyLimited bool) ([]DailyLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 30
	}
	query := `
		SELECT id,
		       COALESCE(CAST(day_time AS TEXT), ''),
		       COALESCE(total_upload_first, 0),
		       COALESCE(total_download_first, 0),
		       COALESCE(upload_base, 0),
		       COALESCE(down_base, 0),
		       COALESCE(upload, 0),
		       COALESCE(down, 0),
		       COALESCE(deny_limit, 0),
		       COALESCE(limit_reason, ''),
		       COALESCE(CAST(created_at AS TEXT), ''),
		       COALESCE(CAST(updated_at AS TEXT), '')
		FROM "log"`
	if onlyLimited {
		query += ` WHERE deny_limit = 1`
	}
	query += ` ORDER BY CAST(day_time AS TEXT) DESC, id DESC LIMIT ?`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DailyLog
	for rows.Next() {
		var l DailyLog
		if err := rows.Scan(
			&l.ID, &l.DayTime, &l.TotalUploadFirst, &l.TotalDownloadFirst,
			&l.UploadBase, &l.DownBase, &l.Upload, &l.Down, &l.DenyLimit,
			&l.LimitReason, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []DailyLog{}
	}
	return out, rows.Err()
}

// ListLogsByMonth returns logs matching a YYYY-MM prefix (or all logs if month == ""), plus summary.
func (s *Store) ListLogsByMonth(month string, onlyLimited bool) ([]DailyLog, PeriodSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	where := []string{"1=1"}
	args := []any{}
	if month != "" {
		where = append(where, `CAST(day_time AS TEXT) LIKE ?`)
		args = append(args, month+"%")
	}
	if onlyLimited {
		where = append(where, `deny_limit = 1`)
	}
	whereClause := strings.Join(where, " AND ")

	query := fmt.Sprintf(`
		SELECT id,
		       COALESCE(CAST(day_time AS TEXT), ''),
		       COALESCE(total_upload_first, 0),
		       COALESCE(total_download_first, 0),
		       COALESCE(upload_base, 0),
		       COALESCE(down_base, 0),
		       COALESCE(upload, 0),
		       COALESCE(down, 0),
		       COALESCE(deny_limit, 0),
		       COALESCE(limit_reason, ''),
		       COALESCE(CAST(created_at AS TEXT), ''),
		       COALESCE(CAST(updated_at AS TEXT), '')
		FROM "log"
		WHERE %s
		ORDER BY CAST(day_time AS TEXT) DESC, id DESC`, whereClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, PeriodSummary{}, err
	}
	defer rows.Close()

	var list []DailyLog
	for rows.Next() {
		var l DailyLog
		if err := rows.Scan(
			&l.ID, &l.DayTime, &l.TotalUploadFirst, &l.TotalDownloadFirst,
			&l.UploadBase, &l.DownBase, &l.Upload, &l.Down, &l.DenyLimit,
			&l.LimitReason, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, PeriodSummary{}, err
		}
		list = append(list, l)
	}
	if list == nil {
		list = []DailyLog{}
	}

	// Summary for the entire month (ignoring onlyLimited filter so month totals stay accurate).
	var sum PeriodSummary
	sumArgs := []any{}
	sumWhere := "1=1"
	if month != "" {
		sumWhere = `CAST(day_time AS TEXT) LIKE ?`
		sumArgs = append(sumArgs, month+"%")
	}
	err = s.db.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(upload), 0),
		       COALESCE(SUM(down), 0),
		       COALESCE(SUM(CASE WHEN deny_limit = 1 THEN 1 ELSE 0 END), 0),
		       COUNT(*)
		FROM "log"
		WHERE %s`, sumWhere), sumArgs...,
	).Scan(&sum.TotalUpload, &sum.TotalDownload, &sum.LimitedDays, &sum.RecordDays)
	return list, sum, err
}

// ListAvailableMonths returns distinct YYYY-MM values in descending order.
func (s *Store) ListAvailableMonths() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT DISTINCT substr(CAST(day_time AS TEXT), 1, 7) AS ym
		FROM "log"
		WHERE length(CAST(day_time AS TEXT)) >= 7
		ORDER BY ym DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var months []string
	for rows.Next() {
		var ym string
		if err := rows.Scan(&ym); err != nil {
			return nil, err
		}
		months = append(months, ym)
	}
	if months == nil {
		months = []string{}
	}
	return months, rows.Err()
}

// AddEvent appends a runtime event log entry.
func (s *Store) AddEvent(level, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`INSERT INTO events(level, message, created_at) VALUES(?, ?, ?)`, level, message, now)
	return err
}

// ListEvents retrieves recent runtime events, optionally filtered by level.
func (s *Store) ListEvents(limit int, level string) ([]EventLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if level != "" && level != "ALL" {
		if level == "ALERT" {
			rows, err = s.db.Query(`SELECT id, level, message, created_at FROM events WHERE level IN ('WARN', 'ERROR') ORDER BY id DESC LIMIT ?`, limit)
		} else {
			rows, err = s.db.Query(`SELECT id, level, message, created_at FROM events WHERE level = ? ORDER BY id DESC LIMIT ?`, level, limit)
		}
	} else {
		rows, err = s.db.Query(`SELECT id, level, message, created_at FROM events ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EventLog
	for rows.Next() {
		var e EventLog
		if err := rows.Scan(&e.ID, &e.Level, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []EventLog{}
	}
	return out, rows.Err()
}

// PruneOldEvents removes event logs older than retentionDays (never deletes traffic history automatically unless configured).
func (s *Store) PruneOldEvents(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02 15:04:05")
	_, err := s.db.Exec(`DELETE FROM events WHERE created_at < ?`, cutoff)
	return err
}
