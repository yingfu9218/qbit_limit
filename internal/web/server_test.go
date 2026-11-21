package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"qbit_limit/internal/config"
	"qbit_limit/internal/db"
	"qbit_limit/internal/monitor"
)

func TestWebServerAssetsSettingsAndAuth(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	cfg, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	mon := monitor.NewService(store, cfg)
	srv := NewServer(store, mon)

	// 1. Embedded HTML served on /
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "QBit Limit") {
		t.Fatalf("expected embedded HTML on /, got status=%d", w.Code)
	}

	// 2. Default auth_enabled is false -> GET /api/settings works without login
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/settings when auth disabled, got %d", w.Code)
	}

	// 3. Enable auth password via PUT /api/settings
	pass := "secret123"
	body, _ := json.Marshal(config.UpdateSettingsRequest{
		QBHost:             "http://127.0.0.1:8080",
		QBUsername:         "admin",
		DailyUploadLimit:   20 * config.GiB,
		MonthlyUploadLimit: 600 * config.GiB,
		NormalUploadSpeed:  2 * config.MiB,
		DenyUploadSpeed:    5 * config.KiB,
		BillingResetDay:    1,
		MonitorEnabled:     true,
		PollIntervalSec:    10,
		LogRetentionDays:   90,
		RestartProtect:     true,
		AuthEnabled:        true,
		AuthPassword:       &pass,
	})
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable auth failed: %d %s", w.Code, w.Body.String())
	}

	// 4. Unauthenticated GET /api/settings now returns 401
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when auth enabled, got %d", w.Code)
	}

	// 5. Login with password succeeds and grants cookie
	loginBody, _ := json.Marshal(map[string]string{"password": pass})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie after login")
	}

	// 6. Authenticated GET /api/settings succeeds
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with session cookie, got %d", w.Code)
	}

	// 7. Legacy /refresh_data compatibility works
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/refresh_data", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "month_upload") {
		t.Fatalf("legacy /refresh_data failed: %d %s", w.Code, w.Body.String())
	}
}
