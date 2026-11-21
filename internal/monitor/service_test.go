package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"qbit_limit/internal/config"
	"qbit_limit/internal/db"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMonitorSyncRestartProtectionAndLimit(t *testing.T) {
	var mu sync.Mutex
	upData := int64(1000)
	dlData := int64(500)
	upLimit := int64(0)

	mockTransport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}
		switch r.URL.Path {
		case "/api/v2/auth/login":
			resp.Body = io.NopCloser(bytes.NewBufferString("Ok."))
		case "/api/v2/app/version":
			resp.Body = io.NopCloser(bytes.NewBufferString("v4.6.5"))
		case "/api/v2/app/webapiVersion":
			resp.Body = io.NopCloser(bytes.NewBufferString("2.9.3"))
		case "/api/v2/transfer/info":
			b, _ := json.Marshal(map[string]any{
				"up_info_data":  upData,
				"dl_info_data":  dlData,
				"up_info_speed": 102400,
				"dl_info_speed": 204800,
				"up_rate_limit": upLimit,
			})
			resp.Body = io.NopCloser(bytes.NewReader(b))
		case "/api/v2/transfer/setUploadLimit":
			_ = r.ParseForm()
			v, _ := strconv.ParseInt(r.FormValue("limit"), 10, 64)
			upLimit = v
		default:
			resp.StatusCode = http.StatusNotFound
		}
		return resp, nil
	})

	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	cfg := config.DefaultSettings()
	cfg.QBHost = "http://mock-qb.local"
	cfg.DailyUploadLimit = 10000
	cfg.MonthlyUploadLimit = 50000
	cfg.NormalUploadSpeed = 2 * config.MiB
	cfg.DenyUploadSpeed = 5 * config.KiB
	cfg.RestartProtect = true

	svc := NewService(store, cfg)
	svc.client.SetTransport(mockTransport)

	// 1st sync: initializes today row and sets normal upload speed
	if err := svc.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	if upLimit != cfg.NormalUploadSpeed {
		t.Fatalf("expected normal limit %d, got %d", cfg.NormalUploadSpeed, upLimit)
	}

	// 2nd sync: upload increases by 6000 (total 7000) -> still under 10000
	mu.Lock()
	upData = 7000
	mu.Unlock()
	if err := svc.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync 2: %v", err)
	}
	rec, _ := store.GetLogByDay(time.Now().Format("2006-01-02"))
	if rec.Upload != 6000 || rec.DenyLimit != 0 {
		t.Fatalf("expected upload=6000 deny=0, got %+v", rec)
	}

	// 3rd sync: qBittorrent restarts! counter resets to 5000 (< total_upload_first=1000? no, make it 500 < 1000)
	// First push to 500 (<1000) so restart triggers: upload should become 6000 + 500 = 6500
	mu.Lock()
	upData = 500
	mu.Unlock()
	if err := svc.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync 3 (restart): %v", err)
	}
	rec, _ = store.GetLogByDay(time.Now().Format("2006-01-02"))
	if rec.Upload != 6500 || rec.DenyLimit != 0 {
		t.Fatalf("restart protection failed, expected upload=6500, got %+v", rec)
	}

	// 4th sync: upload reaches 4500 in new session -> total today = 6000 + 4500 = 10500 >= 10000 -> trigger deny limit!
	mu.Lock()
	upData = 4500
	mu.Unlock()
	if err := svc.SyncOnce(context.Background()); err != nil {
		t.Fatalf("sync 4 (over limit): %v", err)
	}
	rec, _ = store.GetLogByDay(time.Now().Format("2006-01-02"))
	if rec.Upload != 10500 || rec.DenyLimit != 1 {
		t.Fatalf("expected deny_limit=1, got %+v", rec)
	}
	if upLimit != cfg.DenyUploadSpeed {
		t.Fatalf("expected deny upload speed %d, got %d", cfg.DenyUploadSpeed, upLimit)
	}
}

func TestBillingWindow(t *testing.T) {
	loc := time.UTC
	s, e, rem := BillingWindow(time.Date(2026, 11, 21, 10, 0, 0, 0, loc), 1)
	if s != "2026-11-01" || e != "2026-11-30" || rem != 10 {
		t.Fatalf("resetDay=1 unexpected: %s..%s rem=%d", s, e, rem)
	}
	s2, e2, _ := BillingWindow(time.Date(2026, 11, 10, 10, 0, 0, 0, loc), 15)
	if s2 != "2026-10-15" || e2 != "2026-11-14" {
		t.Fatalf("resetDay=15 unexpected: %s..%s", s2, e2)
	}
}
