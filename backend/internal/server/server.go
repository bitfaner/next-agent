package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"nextx/backend/internal/codex"
	"nextx/backend/internal/model"
	"nextx/backend/internal/rpc"
	"nextx/backend/internal/store"
)

type Server struct {
	store *store.Store
	codex *codex.Manager
}

func New(store *store.Store, codex *codex.Manager) *Server {
	s := &Server{store: store, codex: codex}
	codex.SetSink(s.persistCodexEvent)
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	rpcServer := rpc.NewServer()
	rpcServer.Register("workspace.create", s.createWorkspace)
	rpcServer.Register("workspace.list", s.listWorkspaces)
	rpcServer.Register("workspace.delete", s.deleteWorkspace)
	rpcServer.Register("session.start", s.startSession)
	rpcServer.Register("session.list", s.listSessions)
	rpcServer.Register("session.get", s.getSession)
	rpcServer.Register("session.delete", s.deleteSession)
	rpcServer.Register("session.send", s.sendMessage)
	rpcServer.Register("session.authorize", s.authorize)
	mux.Handle("/rpc", cors(rpcServer))
	mux.HandleFunc("/events/", s.events)
	return mux
}

func (s *Server) createWorkspace(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		Path string `json:"path"`
	}](params)
	if err != nil {
		return nil, err
	}
	return s.store.CreateWorkspace(r.Context(), req.Path)
}

func (s *Server) listWorkspaces(r *http.Request, _ json.RawMessage) (any, error) {
	return s.store.ListWorkspaces(r.Context())
}

func (s *Server) deleteWorkspace(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		WorkspaceID int64 `json:"workspaceId"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.WorkspaceID, "workspaceId"); err != nil {
		return nil, err
	}
	sessions, err := s.store.ListSessions(r.Context(), req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		s.codex.Stop(session.ID)
	}
	if err := s.store.DeleteWorkspace(r.Context(), req.WorkspaceID); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (s *Server) startSession(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		WorkspaceID int64 `json:"workspaceId"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.WorkspaceID, "workspaceId"); err != nil {
		return nil, err
	}
	workspace, err := s.store.Workspace(r.Context(), req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	session, err := s.store.CreateSession(r.Context(), req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	slog.Info("session created",
		"session_id", session.ID,
		"workspace_id", workspace.ID,
		"workspace_path", workspace.Path,
	)
	threadID, err := s.codex.Start(r.Context(), session.ID, workspace.Path, "")
	if err != nil {
		slog.Error("codex session start failed", "session_id", session.ID, "error", err)
		_ = s.store.UpdateSessionStatus(r.Context(), session.ID, "failed")
		return nil, fmt.Errorf("start codex: %w", err)
	}
	if threadID != "" {
		if err := s.store.UpdateSessionCodexThreadID(r.Context(), session.ID, threadID); err != nil {
			s.codex.Stop(session.ID)
			_ = s.store.UpdateSessionStatus(r.Context(), session.ID, "failed")
			return nil, fmt.Errorf("save codex thread: %w", err)
		}
	}
	slog.Info("codex session started",
		"session_id", session.ID,
		"codex_thread_id", threadID,
	)
	_ = s.store.UpdateSessionStatus(r.Context(), session.ID, "running")
	session, _ = s.store.Session(r.Context(), session.ID)
	return model.Conversation{Session: session, Messages: []model.Message{}, AuthRequests: []model.AuthRequest{}}, nil
}

func (s *Server) listSessions(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		WorkspaceID int64 `json:"workspaceId"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.WorkspaceID, "workspaceId"); err != nil {
		return nil, err
	}
	return s.store.ListSessions(r.Context(), req.WorkspaceID)
}

func (s *Server) getSession(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		SessionID int64 `json:"sessionId"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.SessionID, "sessionId"); err != nil {
		return nil, err
	}
	session, err := s.store.Session(r.Context(), req.SessionID)
	if err != nil {
		return nil, err
	}
	messages, err := s.store.ListMessages(r.Context(), req.SessionID)
	if err != nil {
		return nil, err
	}
	resumeError := ""
	if session.CodexThreadID != "" {
		workspace, workspaceErr := s.store.Workspace(r.Context(), session.WorkspaceID)
		if workspaceErr != nil {
			resumeError = workspaceErr.Error()
		} else if _, startErr := s.codex.Start(r.Context(), session.ID, workspace.Path, session.CodexThreadID); startErr != nil {
			resumeError = fmt.Errorf("resume codex: %w", startErr).Error()
			_ = s.store.UpdateSessionStatus(r.Context(), session.ID, "failed")
		} else {
			_ = s.store.UpdateSessionStatus(r.Context(), session.ID, "running")
			session, _ = s.store.Session(r.Context(), req.SessionID)
		}
	}
	auths, err := s.store.PendingAuthRequests(r.Context(), req.SessionID)
	if err != nil {
		return nil, err
	}
	return model.Conversation{Session: session, Messages: messages, AuthRequests: auths, ResumeError: resumeError}, nil
}

func (s *Server) deleteSession(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		SessionID int64 `json:"sessionId"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.SessionID, "sessionId"); err != nil {
		return nil, err
	}
	s.codex.Stop(req.SessionID)
	if err := s.store.DeleteSession(r.Context(), req.SessionID); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (s *Server) sendMessage(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		SessionID int64    `json:"sessionId"`
		Text      string   `json:"text"`
		Images    []string `json:"images"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.SessionID, "sessionId"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Text) == "" && len(req.Images) == 0 {
		return nil, errors.New("message text or image is required")
	}
	session, err := s.store.Session(r.Context(), req.SessionID)
	if err != nil {
		return nil, err
	}
	if session.CodexThreadID == "" {
		return nil, errors.New("this session was created before conversation resume was supported; start a new session to continue with Codex")
	}
	workspace, err := s.store.Workspace(r.Context(), session.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := s.codex.Start(r.Context(), session.ID, workspace.Path, session.CodexThreadID); err != nil {
		return nil, fmt.Errorf("resume codex: %w", err)
	}
	slog.Info("user message sent to codex",
		"session_id", req.SessionID,
		"codex_thread_id", session.CodexThreadID,
		"message", req.Text,
		"image_count", len(req.Images),
	)
	if strings.TrimSpace(req.Text) != "" {
		msg, err := s.store.AddMessage(r.Context(), model.Message{SessionID: req.SessionID, Role: "user", Kind: "text", Content: req.Text})
		if err != nil {
			return nil, err
		}
		s.codex.Publish(req.SessionID, codex.Event{Type: "message", Message: &msg})
		if err := s.store.UpdateSessionTitleIfDefault(r.Context(), req.SessionID, firstLine(req.Text)); err != nil {
			return nil, err
		}
	}
	for _, image := range req.Images {
		msg, err := s.store.AddMessage(r.Context(), model.Message{SessionID: req.SessionID, Role: "user", Kind: "image", Content: image})
		if err != nil {
			return nil, err
		}
		s.codex.Publish(req.SessionID, codex.Event{Type: "message", Message: &msg})
	}
	if err := s.codex.Send(r.Context(), req.SessionID, req.Text, req.Images); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (s *Server) authorize(r *http.Request, params json.RawMessage) (any, error) {
	req, err := rpc.Decode[struct {
		SessionID int64  `json:"sessionId"`
		RequestID string `json:"requestId"`
		Approved  bool   `json:"approved"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := rpc.RequiredID(req.SessionID, "sessionId"); err != nil {
		return nil, err
	}
	if req.RequestID == "" {
		return nil, errors.New("requestId is required")
	}
	session, err := s.store.Session(r.Context(), req.SessionID)
	if err != nil {
		return nil, err
	}
	decision := "denied"
	if req.Approved {
		decision = "approved"
	}
	slog.Info("user authorization decision",
		"session_id", req.SessionID,
		"codex_thread_id", session.CodexThreadID,
		"request_id", req.RequestID,
		"decision", decision,
		"approved", req.Approved,
	)
	if err := s.store.ResolveAuthRequest(r.Context(), req.SessionID, req.RequestID, req.Approved); err != nil {
		return nil, err
	}
	if err := s.codex.Authorize(r.Context(), req.SessionID, req.RequestID, req.Approved); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("access-control-allow-origin", "*")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	w.Header().Set("content-type", "text/event-stream")

	idText := strings.TrimPrefix(r.URL.Path, "/events/")
	sessionID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || sessionID <= 0 {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	events, unsubscribe := s.codex.Subscribe(sessionID)
	defer unsubscribe()
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			writeSSE(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) persistCodexEvent(sessionID int64, event codex.Event) codex.Event {
	if event.Type == "message" && event.Message != nil && event.Message.ID == 0 {
		msg, err := s.store.AddMessage(context.Background(), *event.Message)
		if err == nil {
			event.Message = &msg
		}
	}
	if event.Auth != nil && event.Auth.ID == 0 {
		auth, err := s.store.CreateAuthRequest(context.Background(), *event.Auth)
		if err == nil {
			event.Auth = &auth
		}
	}
	if event.Status != "" {
		_ = s.store.UpdateSessionStatus(context.Background(), sessionID, event.Status)
	}
	return event
}

func writeSSE(w http.ResponseWriter, event codex.Event) {
	data, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("access-control-allow-origin", "*")
		w.Header().Set("access-control-allow-headers", "content-type")
		w.Header().Set("access-control-allow-methods", "POST, OPTIONS")
		next.ServeHTTP(w, r)
	})
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = text[:idx]
	}
	if len(text) > 80 {
		return text[:80]
	}
	return text
}
