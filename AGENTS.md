# AGENTS.md

Guidelines and architecture reference for coding agents working in `/Users/yingfu/project/qbit_limit`.

## Project Overview

**QBit Limit** is a Go + Gin rewrite of the Python `qbit_limit` utility. It monitors qBittorrent transfer statistics, tracks daily/monthly upload & download usage in SQLite, enforces normal and circuit-breaker upload rate limits, and serves an embedded responsive Web UI (with Light/Dark theme support and an Admin Configuration panel) from a single cross-compilable binary.

- **Language & Runtime**: Go (`go.mod` targets Go 1.24+, tested with Go 1.27)
- **Web Framework**: `github.com/gin-gonic/gin`
- **Database**: Pure-Go SQLite via `modernc.org/sqlite` (**no CGO**)
- **Frontend**: Vanilla HTML/CSS/JS embedded via Go `embed.FS` (zero external CDN dependencies)
- **Design Source**: `design.pen` (Pen.dev / Pencil design file)

---

## Repository Structure

```text
.
├── main.go                           # CLI flags (-port, -host, -db, -logs, -version), startup & graceful shutdown
├── Makefile                          # Single-platform & 6-platform CGO_ENABLED=0 cross-compilation targets
├── design.pen                        # UI design draft (4 artboards: Dashboard, Settings, History/Logs, Mobile)
├── README.md                         # User-facing documentation
├── internal/
│   ├── config/
│   │   └── config.go                 # Settings structs, defaults, validation, password hashing, byte/speed formatting
│   ├── db/
│   │   ├── sqlite.go                 # SQLite Store, legacy qb.db migration, CRUD for log/settings/events
│   │   └── sqlite_test.go            # Legacy Python qb.db compatibility & migration tests
│   ├── qbit/
│   │   └── client.go                 # qBittorrent WebUI API v2 client (cookie jar, auto re-login, TestConnection)
│   ├── monitor/
│   │   ├── service.go                # Background poller, hot-reload, billing cycle window, restart protection, manual override
│   │   └── service_test.go           # Unit tests using in-memory http.RoundTripper (no TCP socket required)
│   └── web/
│       ├── server.go                 # Gin router, optional HMAC cookie auth middleware, REST APIs, CSV export, /refresh_data
│       ├── server_test.go            # Embedded asset, settings, auth flow & legacy endpoint tests
│       └── assets/
│           ├── embed.go              # //go:embed index.html styles.css app.js
│           ├── index.html            # SPA layout (Dashboard, History, Logs, Settings + Auth modal + Mobile nav)
│           ├── styles.css            # Light & Dark themes ([data-theme="light"|"dark"]), responsive CSS
│           └── app.js                # Zero-dependency frontend logic, interactive SVG trend chart, settings form
```

---

## Key Architecture Rules & Invariants

1. **Maintain Zero-CGO Cross-Compilation**
   - Always keep `CGO_ENABLED=0` working across `linux/amd64`, `linux/arm64`, `linux/armv7`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`.
   - Do **not** switch `modernc.org/sqlite` to a CGO-dependent SQLite driver (such as `mattn/go-sqlite3`).

2. **Preserve Legacy `qb.db` Compatibility**
   - The `log` table must retain full compatibility with the original Python schema (`id`, `day_time`, `total_upload_first`, `total_download_first`, `upload`, `down`, `deny_limit`, `created_at`, `updated_at`).
   - Always migrate new columns non-destructively via `PRAGMA table_info("log")` + `ALTER TABLE`.
   - Legacy `DATE` columns (`day_time`, `created_at`, `updated_at`) must be selected with `CAST(... AS TEXT)` to prevent SQLite driver scan mismatches.
   - Keep the legacy `GET /refresh_data` endpoint compatible for existing integrations.

3. **Restart Counter Protection**
   - When qBittorrent restarts and resets `up_info_data` / `dl_info_data` below `total_upload_first` / `total_download_first`, preserve prior daily usage in `upload_base` / `down_base` when `restart_protect` is enabled instead of zeroing the day's total.

4. **Embedded Zero-CDN Frontend & Dual Theme**
   - Any new asset added under `internal/web/assets/` must be included in `embed.go`.
   - Keep frontend completely self-contained (inline SVGs, no external fonts/CDNs) so NAS/intranet deployments work offline.
   - Every UI color must use CSS variables defined under both `[data-theme="light"]` and `[data-theme="dark"]` in `styles.css`.

5. **Admin Authentication Semantics**
   - `auth_enabled` defaults to `false` so LAN users can open and configure the app immediately.
   - When enabled, sensitive routes (`GET/PUT /api/settings`, `POST /api/qbit/test`, `POST /api/monitor/sync`, `POST /api/monitor/override`) require a valid HMAC session cookie (`qbl_session`) or `X-Auth-Token`.
   - Never return plaintext `qb_password`, `auth_password_hash`, `auth_salt`, or `session_secret` in API responses.

6. **Design File (`design.pen`) Compatibility**
   - Keep `version: "2.8"` and use standard node types (`frame`, `rectangle`, `ellipse`, `text`) so Pen.dev Design Editor migrates and opens the file cleanly without legacy `icon_font` errors.

---

## Common Commands

```bash
# Run all tests (pure Go, no network or local ports needed)
go test -v ./...

# Run locally (default http://0.0.0.0:8091)
go run . -port 8091

# Print recent 30-day traffic table in CLI (replaces legacy logs.py)
go run . -db ./qb.db -logs

# Build binary for current host
make build

# Cross-compile all 6 platforms into dist/
make build-all

# Clean build artifacts
make clean
```

---

## HTTP API Reference

| Method | Path | Auth Required | Purpose |
| :--- | :--- | :---: | :--- |
| `GET` | `/` | No | Embedded SPA (`index.html`) |
| `GET` | `/refresh_data` | No | Legacy Python-compatible summary + 30-day logs |
| `GET` | `/api/auth/status` | No | Check if admin password protection is enabled & authenticated |
| `POST` | `/api/auth/login` | No | Verify admin password and set `qbl_session` cookie |
| `POST` | `/api/auth/logout` | No | Clear `qbl_session` cookie |
| `GET` | `/api/dashboard?days=14&only_limited=0` | No | Live status, today/cycle quotas, chart series & recent logs |
| `GET` | `/api/history?month=YYYY-MM&only_limited=0` | No | Monthly traffic ledger, summary & available months |
| `GET` | `/api/history/export?month=YYYY-MM` | No | UTF-8 BOM CSV export |
| `GET` | `/api/events?limit=80&level=ALL` | No | Runtime & rate-limit event log stream |
| `GET` | `/api/settings` | When enabled | Read public settings (passwords masked) |
| `PUT` | `/api/settings` | When enabled | Validate, persist to SQLite & hot-reload monitor |
| `POST` | `/api/qbit/test` | When enabled | Validate qBittorrent host/credentials and return version/latency |
| `POST` | `/api/monitor/sync` | When enabled | Trigger immediate poll & rate-limit evaluation |
| `POST` | `/api/monitor/override` | When enabled | Toggle manual circuit-breaker throttling (`{"manual_deny": bool}`) |
