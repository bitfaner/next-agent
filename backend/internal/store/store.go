package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"nextx/backend/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	return store, store.migrate(context.Background())
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS workspaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  codex_thread_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  request_id TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  action TEXT NOT NULL,
  resolved INTEGER NOT NULL DEFAULT 0,
  approved INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, request_id)
);
`)
	if err != nil {
		return err
	}
	return s.ensureSessionThreadIDColumn(ctx)
}

func (s *Store) ensureSessionThreadIDColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return err
	}
	hasThreadID := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "codex_thread_id" {
			hasThreadID = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if hasThreadID {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN codex_thread_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *Store) CreateWorkspace(ctx context.Context, path string) (model.Workspace, error) {
	if strings.TrimSpace(path) == "" {
		return model.Workspace{}, errors.New("workspace path is required")
	}
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == string(filepath.Separator) {
		name = path
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO workspaces(name, path, created_at)
VALUES(?, ?, ?)
ON CONFLICT(path) DO UPDATE SET name = excluded.name
`, name, path, now.Format(time.RFC3339Nano))
	if err != nil {
		return model.Workspace{}, err
	}
	return s.workspaceByPath(ctx, path)
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, created_at FROM workspaces ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Workspace{}
	for rows.Next() {
		item, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) workspaceByPath(ctx context.Context, path string) (model.Workspace, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, path, created_at FROM workspaces WHERE path = ?`, path)
	return scanWorkspace(row)
}

func (s *Store) Workspace(ctx context.Context, id int64) (model.Workspace, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, path, created_at FROM workspaces WHERE id = ?`, id)
	return scanWorkspace(row)
}

func (s *Store) DeleteWorkspace(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, workspaceID int64) (model.Session, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(workspace_id, title, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?)
`, workspaceID, "New session", "starting", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return model.Session{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.Session{}, err
	}
	return s.Session(ctx, id)
}

func (s *Store) UpdateSessionCodexThreadID(ctx context.Context, sessionID int64, threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return errors.New("codex thread id is required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET codex_thread_id = ?, updated_at = ? WHERE id = ?`, threadID, time.Now().UTC().Format(time.RFC3339Nano), sessionID)
	return err
}

func (s *Store) UpdateSessionStatus(ctx context.Context, sessionID int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().UTC().Format(time.RFC3339Nano), sessionID)
	return err
}

func (s *Store) UpdateSessionTitle(ctx context.Context, sessionID int64, title string) error {
	if strings.TrimSpace(title) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`, title, time.Now().UTC().Format(time.RFC3339Nano), sessionID)
	return err
}

func (s *Store) UpdateSessionTitleIfDefault(ctx context.Context, sessionID int64, title string) error {
	if strings.TrimSpace(title) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ? AND (title = '' OR title = 'New session')`, title, time.Now().UTC().Format(time.RFC3339Nano), sessionID)
	return err
}

func (s *Store) ListSessions(ctx context.Context, workspaceID int64) ([]model.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace_id, codex_thread_id, title, status, created_at, updated_at FROM sessions WHERE workspace_id = ? ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Session{}
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Session(ctx context.Context, id int64) (model.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, codex_thread_id, title, status, created_at, updated_at FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (s *Store) DeleteSession(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AddMessage(ctx context.Context, msg model.Message) (model.Message, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO messages(session_id, role, kind, content, metadata, created_at)
VALUES(?, ?, ?, ?, ?, ?)
`, msg.SessionID, msg.Role, msg.Kind, msg.Content, msg.Metadata, now.Format(time.RFC3339Nano))
	if err != nil {
		return model.Message{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.Message{}, err
	}
	msg.ID = id
	msg.CreatedAt = now
	return msg, nil
}

func (s *Store) ListMessages(ctx context.Context, sessionID int64) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, role, kind, content, metadata, created_at FROM messages WHERE session_id = ? ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Message{}
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateAuthRequest(ctx context.Context, auth model.AuthRequest) (model.AuthRequest, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_requests(session_id, request_id, title, body, action, resolved, approved, created_at)
VALUES(?, ?, ?, ?, ?, 0, 0, ?)
ON CONFLICT(session_id, request_id) DO NOTHING
`, auth.SessionID, auth.RequestID, auth.Title, auth.Body, auth.Action, now.Format(time.RFC3339Nano))
	if err != nil {
		return model.AuthRequest{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, request_id, title, body, action, resolved, approved, created_at FROM auth_requests WHERE session_id = ? AND request_id = ?`, auth.SessionID, auth.RequestID)
	return scanAuth(row)
}

func (s *Store) ResolveAuthRequest(ctx context.Context, sessionID int64, requestID string, approved bool) error {
	approvedInt := 0
	if approved {
		approvedInt = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE auth_requests SET resolved = 1, approved = ? WHERE session_id = ? AND request_id = ?`, approvedInt, sessionID, requestID)
	return err
}

func (s *Store) PendingAuthRequests(ctx context.Context, sessionID int64) ([]model.AuthRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, request_id, title, body, action, resolved, approved, created_at FROM auth_requests WHERE session_id = ? AND resolved = 0 ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.AuthRequest{}
	for rows.Next() {
		item, err := scanAuth(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(row scanner) (model.Workspace, error) {
	var item model.Workspace
	var created string
	if err := row.Scan(&item.ID, &item.Name, &item.Path, &created); err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}

func scanSession(row scanner) (model.Session, error) {
	var item model.Session
	var created, updated string
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.CodexThreadID, &item.Title, &item.Status, &created, &updated); err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func scanMessage(row scanner) (model.Message, error) {
	var item model.Message
	var created string
	if err := row.Scan(&item.ID, &item.SessionID, &item.Role, &item.Kind, &item.Content, &item.Metadata, &created); err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}

func scanAuth(row scanner) (model.AuthRequest, error) {
	var item model.AuthRequest
	var created string
	if err := row.Scan(&item.ID, &item.SessionID, &item.RequestID, &item.Title, &item.Body, &item.Action, &item.Resolved, &item.Approved, &created); err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}
