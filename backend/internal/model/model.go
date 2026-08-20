package model

import "time"

type Workspace struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	ID            int64     `json:"id"`
	WorkspaceID   int64     `json:"workspaceId"`
	CodexThreadID string    `json:"codexThreadId,omitempty"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Message struct {
	ID        int64     `json:"id,omitempty"`
	SessionID int64     `json:"sessionId"`
	Role      string    `json:"role"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Metadata  string    `json:"metadata,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

type AuthRequest struct {
	ID        int64     `json:"id,omitempty"`
	SessionID int64     `json:"sessionId,omitempty"`
	RequestID string    `json:"requestId"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Action    string    `json:"action"`
	Resolved  bool      `json:"resolved"`
	Approved  bool      `json:"approved"`
	CreatedAt time.Time `json:"createdAt"`
}

type Conversation struct {
	Session      Session       `json:"session"`
	Messages     []Message     `json:"messages"`
	AuthRequests []AuthRequest `json:"authRequests"`
	ResumeError  string        `json:"resumeError,omitempty"`
}
