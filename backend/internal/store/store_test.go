package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSessionCodexThreadIDPersists(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "nextx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	workspace, err := db.CreateWorkspace(context.Background(), filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(context.Background(), workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSessionCodexThreadID(context.Background(), session.ID, "thr_test"); err != nil {
		t.Fatal(err)
	}

	got, err := db.Session(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CodexThreadID != "thr_test" {
		t.Fatalf("CodexThreadID = %q, want %q", got.CodexThreadID, "thr_test")
	}
}

func TestOpenMigratesLegacySessionsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
CREATE TABLE sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		found = found || name == "codex_thread_id"
	}
	if !found {
		t.Fatal("legacy sessions table was not migrated with codex_thread_id")
	}
}
