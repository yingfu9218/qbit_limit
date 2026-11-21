package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLegacyQBDBCompatibility(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_qb.db")

	// Create exact legacy Python qb.db schema and sample rows
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	_, err = rawDB.Exec(`
CREATE TABLE "log" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "day_time" DATE,
  "total_upload_first" integer,
  "total_download_first" integer,
  "upload" integer,
  "down" integer,
  "deny_limit" integer DEFAULT 0,
  "created_at" DATE,
  "updated_at" DATE
);
INSERT INTO "log" ("day_time","total_upload_first","total_download_first","upload","down","deny_limit","created_at","updated_at")
VALUES
  ('2026-11-20', 5436861995, 0, 7098077005, 1024000, 0, '2026-11-20 00:00:02', '2026-11-20 23:59:52'),
  ('2026-11-21', 12534939000, 0, 15389215165, 4157732751, 1, '2026-11-21 00:00:04', '2026-11-21 16:50:02');
`)
	if err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}
	_ = rawDB.Close()

	// Now open with Store (should migrate non-destructively and read seamlessly)
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Store.Open failed on legacy qb.db: %v", err)
	}
	defer store.Close()

	rec, err := store.GetLogByDay("2026-11-21")
	if err != nil || rec == nil {
		t.Fatalf("GetLogByDay failed: rec=%v err=%v", rec, err)
	}
	if rec.Upload != 15389215165 || rec.Down != 4157732751 || rec.DenyLimit != 1 {
		t.Fatalf("unexpected migrated row: %+v", rec)
	}

	sum, err := store.GetPeriodSummary("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatalf("GetPeriodSummary: %v", err)
	}
	if sum.TotalUpload != 7098077005+15389215165 || sum.LimitedDays != 1 || sum.RecordDays != 2 {
		t.Fatalf("unexpected period summary: %+v", sum)
	}

	months, err := store.ListAvailableMonths()
	if err != nil || len(months) != 1 || months[0] != "2026-11" {
		t.Fatalf("unexpected months: %v, err=%v", months, err)
	}
}
