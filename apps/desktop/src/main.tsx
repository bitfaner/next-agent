import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { open } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import {
  ArrowUp,
  Bot,
  BrainCircuit,
  ChevronRight,
  Circle,
  Clock3,
  Command,
  Folder,
  FolderPlus,
  MessageSquare,
  MessageSquarePlus,
  Plus,
  ShieldCheck,
  ShieldX,
  Sparkles,
  TerminalSquare,
  Trash2,
  UserRound
} from "lucide-react";
import "./styles.css";

type Workspace = {
  id: number;
  name: string;
  path: string;
  createdAt: string;
};

type Session = {
  id: number;
  workspaceId: number;
  title: string;
  status: string;
  createdAt: string;
  updatedAt: string;
};

type Message = {
  id?: number;
  streamId?: string;
  sessionId: number;
  role: string;
  kind: string;
  content: string;
  metadata?: string;
  createdAt?: string;
};

type AuthRequest = {
  requestId: string;
  title: string;
  body: string;
  action: string;
  createdAt: string;
};

type EventPayload =
  | { type: "message"; streamId?: string; message: Message }
  | { type: "message_delta"; streamId: string; message: Message }
  | { type: "auth_request"; auth: AuthRequest }
  | { type: "status"; status: string };

type Conversation = {
  session: Session;
  messages: Message[];
  authRequests: AuthRequest[];
  resumeError?: string;
};

type DeleteTarget =
  | { kind: "workspace"; id: number; name: string }
  | { kind: "session"; id: number; name: string }
  | null;

let rpcId = 1;

async function baseUrl(): Promise<string> {
  if ("__TAURI_INTERNALS__" in window) {
    return invoke<string>("backend_base_url");
  }
  return import.meta.env.VITE_BACKEND_URL ?? "http://127.0.0.1:34121";
}

async function rpc<T>(method: string, params?: unknown): Promise<T> {
  const url = `${await baseUrl()}/rpc`;
  const res = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: rpcId++, method, params: params ?? {} })
  });
  const payload = await res.json();
  if (payload.error) {
    throw new Error(payload.error.message || "RPC request failed");
  }
  return payload.result as T;
}

function sessionTitle(text: string): string {
  const line = text.trim().split("\n", 1)[0].trim();
  return line.length > 80 ? line.slice(0, 80) : line;
}

function formatTime(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(date);
}

function statusLabel(status: string): string {
  if (!status) return "idle";
  return status.replace(/[_-]/g, " ");
}

function roleIcon(role: string) {
  if (role === "user") return <UserRound size={16} />;
  if (role === "system") return <TerminalSquare size={16} />;
  return <Bot size={16} />;
}

function messageKey(message: Message, index: number): string {
  return message.streamId ?? (message.id ? String(message.id) : `live-${index}`);
}

function isStreamingMessage(message: Message): boolean {
  return Boolean(message.streamId) && !message.id;
}

function MarkdownBlock({ content, cursor = false }: { content: string; cursor?: boolean }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ children, ...props }) => (
            <a {...props} target="_blank" rel="noreferrer">
              {children}
            </a>
          ),
          pre: ({ children }) => <pre className="code-block">{children}</pre>
        }}
      >
        {content}
      </ReactMarkdown>
      {cursor && <span className="typing-cursor" aria-hidden="true" />}
    </div>
  );
}

function StreamingMarkdown({ content, streaming }: { content: string; streaming: boolean }) {
  const animated = useRef(streaming);
  const [visibleLength, setVisibleLength] = useState(() => (streaming ? 0 : content.length));

  useEffect(() => {
    if (streaming) animated.current = true;
    if (!animated.current) {
      setVisibleLength(content.length);
      return;
    }
    if (visibleLength > content.length) {
      setVisibleLength(content.length);
      return;
    }
    if (visibleLength === content.length) return;

    const backlog = content.length - visibleLength;
    const step = backlog > 400 ? 14 : backlog > 160 ? 7 : backlog > 48 ? 3 : 1;
    const timeout = window.setTimeout(
      () => setVisibleLength((current) => Math.min(content.length, current + step)),
      16
    );
    return () => window.clearTimeout(timeout);
  }, [content.length, streaming, visibleLength]);

  const typing = streaming || visibleLength < content.length;
  return <MarkdownBlock content={content.slice(0, visibleLength)} cursor={typing} />;
}

function ReasoningMessage({ message, live }: { message: Message; live: boolean }) {
  const [open, setOpen] = useState(live);

  useEffect(() => {
    if (live) setOpen(true);
  }, [live]);

  return (
    <details
      className={`message reasoning${live ? " streaming" : ""}`}
      open={open}
      onToggle={(event) => setOpen(live ? true : event.currentTarget.open)}
    >
      <summary>
        <span className="avatar reasoning-avatar">
          <BrainCircuit size={16} />
        </span>
        <span className="message-meta">Thinking process</span>
        {live && <span className="live-label">Live</span>}
      </summary>
      <StreamingMarkdown content={message.content} streaming={live} />
    </details>
  );
}

function App() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeWorkspaceId, setActiveWorkspaceId] = useState<number | null>(null);
  const [activeSessionId, setActiveSessionId] = useState<number | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [authRequests, setAuthRequests] = useState<AuthRequest[]>([]);
  const [prompt, setPrompt] = useState("");
  const [images, setImages] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [awaitingResponse, setAwaitingResponse] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>(null);
  const [deleting, setDeleting] = useState(false);
  const feedEndRef = useRef<HTMLDivElement | null>(null);

  const activeWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === activeWorkspaceId) ?? null,
    [workspaces, activeWorkspaceId]
  );

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [sessions, activeSessionId]
  );

  const assistantMessages = messages.filter((message) => message.role === "assistant").length;

  useEffect(() => {
    refresh();
  }, []);

  useEffect(() => {
    if (activeWorkspaceId) {
      rpc<Session[]>("session.list", { workspaceId: activeWorkspaceId })
        .then(setSessions)
        .catch((err) => setError(err.message));
    } else {
      setSessions([]);
    }
  }, [activeWorkspaceId]);

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      feedEndRef.current?.scrollIntoView({ block: "end" });
    });
    return () => cancelAnimationFrame(frame);
  }, [messages, authRequests, activeSessionId]);

  useEffect(() => {
    if (!activeSessionId) return;
    setAwaitingResponse(false);
    let closed = false;
    let source: EventSource;

    baseUrl().then((url) => {
      if (closed) return;
      source = new EventSource(`${url}/events/${activeSessionId}`);
      source.onmessage = (evt) => {
        const payload = JSON.parse(evt.data) as EventPayload;
        if (payload.type === "message_delta") {
          setAwaitingResponse(false);
          setMessages((current) => {
            const index = current.findIndex((message) => message.streamId === payload.streamId);
            if (index < 0) {
              return [...current, { ...payload.message, streamId: payload.streamId }];
            }
            return current.map((message, messageIndex) =>
              messageIndex === index
                ? { ...message, content: message.content + payload.message.content }
                : message
            );
          });
        }
        if (payload.type === "message") {
          if (payload.message.role !== "user") setAwaitingResponse(false);
          setMessages((current) => {
            const index = payload.streamId
              ? current.findIndex((message) => message.streamId === payload.streamId)
              : -1;
            if (index < 0) {
              return [...current, payload.message];
            }
            return current.map((message, messageIndex) =>
              messageIndex === index ? { ...payload.message, streamId: message.streamId } : message
            );
          });
        }
        if (payload.type === "auth_request") {
          setAwaitingResponse(false);
          setAuthRequests((current) => [...current, payload.auth]);
        }
        if (payload.type === "status") {
          setSessions((current) =>
            current.map((session) =>
              session.id === activeSessionId ? { ...session, status: payload.status } : session
            )
          );
        }
      };
      source.onerror = () => source.close();
    });

    return () => {
      closed = true;
      source?.close();
    };
  }, [activeSessionId]);

  async function refresh() {
    try {
      const items = await rpc<Workspace[]>("workspace.list");
      setWorkspaces(items);
      if (!activeWorkspaceId && items.length > 0) setActiveWorkspaceId(items[0].id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function createWorkspace() {
    setError(null);
    let selected: string | null = null;
    if ("__TAURI_INTERNALS__" in window) {
      const picked = await open({ directory: true, multiple: false, title: "Select workspace" });
      if (!picked || Array.isArray(picked)) return;
      selected = picked;
    } else {
      selected = window.prompt("Workspace path");
      if (!selected) return;
    }
    const workspace = await rpc<Workspace>("workspace.create", { path: selected });
    setWorkspaces((current) => [workspace, ...current]);
    setActiveWorkspaceId(workspace.id);
  }

  async function startSession() {
    if (!activeWorkspaceId) {
      setError("Select or create a workspace before starting a session.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const conversation = await rpc<Conversation>("session.start", { workspaceId: activeWorkspaceId });
      setSessions((current) => [conversation.session, ...current]);
      setActiveSessionId(conversation.session.id);
      setMessages(conversation.messages);
      setAuthRequests(conversation.authRequests);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function loadSession(sessionId: number) {
    setError(null);
    try {
      const conversation = await rpc<Conversation>("session.get", { sessionId });
      setActiveSessionId(sessionId);
      setMessages(conversation.messages);
      setAuthRequests(conversation.authRequests);
      setSessions((current) =>
        current.map((session) => (session.id === sessionId ? conversation.session : session))
      );
      if (conversation.resumeError) setError(conversation.resumeError);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  function clearConversation() {
    setActiveSessionId(null);
    setMessages([]);
    setAuthRequests([]);
    setAwaitingResponse(false);
    setPrompt("");
    setImages([]);
  }

  async function confirmDelete() {
    if (!deleteTarget || deleting) return;
    const target = deleteTarget;
    setDeleting(true);
    setError(null);
    try {
      if (target.kind === "workspace") {
        await rpc("workspace.delete", { workspaceId: target.id });
        const remaining = workspaces.filter((workspace) => workspace.id !== target.id);
        setWorkspaces(remaining);
        if (activeWorkspaceId === target.id) {
          const nextWorkspace = remaining[0] ?? null;
          clearConversation();
          setActiveWorkspaceId(nextWorkspace?.id ?? null);
        }
      } else {
        await rpc("session.delete", { sessionId: target.id });
        const remaining = sessions.filter((session) => session.id !== target.id);
        setSessions(remaining);
        if (activeSessionId === target.id) {
          clearConversation();
          const nextSession = remaining[0] ?? null;
          if (nextSession) {
            await loadSession(nextSession.id);
          }
        }
      }
      setDeleteTarget(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(false);
    }
  }

  function attachImages(files: FileList | null) {
    if (!files) return;
    Array.from(files).forEach((file) => {
      const reader = new FileReader();
      reader.onload = () => setImages((current) => [...current, String(reader.result)]);
      reader.readAsDataURL(file);
    });
  }

  async function sendPrompt() {
    if (!activeSessionId || (!prompt.trim() && images.length === 0)) return;
    const text = prompt.trim();
    setPrompt("");
    setImages([]);
    setAwaitingResponse(true);
    try {
      await rpc("session.send", { sessionId: activeSessionId, text, images });
    } catch (err) {
      setAwaitingResponse(false);
      setError(err instanceof Error ? err.message : String(err));
      return;
    }
    if (text) {
      const title = sessionTitle(text);
      setSessions((current) =>
        current.map((session) =>
          session.id === activeSessionId && (session.title === "New session" || session.title === "")
            ? { ...session, title }
            : session
        )
      );
    }
  }

  async function answerAuth(requestId: string, approved: boolean) {
    if (!activeSessionId) return;
    await rpc("session.authorize", { sessionId: activeSessionId, requestId, approved });
    setAuthRequests((current) => current.filter((request) => request.requestId !== requestId));
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <Sparkles size={18} />
          </div>
          <div>
            <strong>NextX</strong>
            <span>Codex Workspace</span>
          </div>
        </div>

        <button className="primary-action" onClick={startSession} disabled={!activeWorkspaceId || busy}>
          <MessageSquarePlus size={18} />
          <span>{busy ? "Starting..." : "New session"}</span>
        </button>

        <section className="panel">
          <div className="panel-heading">
            <span>Workspaces</span>
            <button className="ghost-button" onClick={createWorkspace} title="Create workspace">
              <FolderPlus size={16} />
            </button>
          </div>
          <div className="workspace-list">
            {workspaces.map((workspace) => (
              <div
                key={workspace.id}
                className={`workspace-item ${workspace.id === activeWorkspaceId ? "active" : ""}`}
                onClick={() => setActiveWorkspaceId(workspace.id)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") setActiveWorkspaceId(workspace.id);
                }}
                role="button"
                tabIndex={0}
              >
                <span className="workspace-icon">
                  <Folder size={16} />
                </span>
                <span className="workspace-copy">
                  <strong>{workspace.name}</strong>
                  <small>{workspace.path}</small>
                </span>
                <span className="item-actions">
                  <button
                    className="delete-button"
                    onClick={(event) => {
                      event.stopPropagation();
                      setDeleteTarget({
                        kind: "workspace",
                        id: workspace.id,
                        name: workspace.name
                      });
                    }}
                    title={`Delete workspace ${workspace.name}`}
                    aria-label={`Delete workspace ${workspace.name}`}
                  >
                    <Trash2 size={15} />
                  </button>
                </span>
              </div>
            ))}
            {workspaces.length === 0 && (
              <div className="empty-note">Create a workspace to connect Codex to a local folder.</div>
            )}
          </div>
        </section>

        <section className="panel sessions">
          <div className="panel-heading">
            <span>Sessions</span>
            <small>{sessions.length}</small>
          </div>
          <div className="session-list">
            {sessions.map((session) => (
              <div
                key={session.id}
                className={`session-item ${session.id === activeSessionId ? "active" : ""}`}
                onClick={() => loadSession(session.id)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") void loadSession(session.id);
                }}
                role="button"
                tabIndex={0}
              >
                <span className="session-icon">
                  <MessageSquare size={15} />
                </span>
                <span className="session-copy">
                  <strong>{session.title || "New session"}</strong>
                  <small>
                    {statusLabel(session.status)}
                    {formatTime(session.updatedAt) ? ` · ${formatTime(session.updatedAt)}` : ""}
                  </small>
                </span>
                <span className="item-actions">
                  <ChevronRight className="session-chevron" size={15} />
                  <button
                    className="delete-button"
                    onClick={(event) => {
                      event.stopPropagation();
                      setDeleteTarget({ kind: "session", id: session.id, name: session.title || "New session" });
                    }}
                    title={`Delete session ${session.title || "New session"}`}
                    aria-label={`Delete session ${session.title || "New session"}`}
                  >
                    <Trash2 size={15} />
                  </button>
                </span>
              </div>
            ))}
            {sessions.length === 0 && <div className="empty-note">No sessions in this workspace yet.</div>}
          </div>
        </section>
      </aside>

      <section className="conversation">
        <header className="conversation-head">
          <div className="title-block">
            <span className="eyebrow">
              <Command size={14} />
              {activeSession ? `Session ${activeSession.id}` : "Ready"}
            </span>
            <h1>{activeSession?.title || activeWorkspace?.name || "NextX Agent"}</h1>
            <p>{activeWorkspace?.path ?? "Choose a workspace to start a Codex session."}</p>
          </div>
          <div className="head-meta">
            <div className="metric">
              <Clock3 size={15} />
              <span>{activeSession ? statusLabel(activeSession.status) : "idle"}</span>
            </div>
            <div className="metric">
              <Bot size={15} />
              <span>{assistantMessages} replies</span>
            </div>
          </div>
        </header>

        {error && (
          <div className="error-banner">
            <Circle size={12} />
            <span>{error}</span>
          </div>
        )}

        <div className="feed">
          {messages.length === 0 && authRequests.length === 0 && (
            <section className="welcome">
              <div className="welcome-icon">
                <Sparkles size={22} />
              </div>
              <h2>{activeSessionId ? "Ask Codex to inspect, change, or explain this workspace." : "Start a session"}</h2>
              <p>
                {activeSessionId
                  ? "Messages, command output, and permission requests will appear here."
                  : "Select a workspace, create a session, then send text or images to Codex."}
              </p>
            </section>
          )}

          {messages.map((message, index) => {
            const key = messageKey(message, index);
            const live = isStreamingMessage(message);
            return message.kind === "reasoning" ? (
              <ReasoningMessage key={key} message={message} live={live} />
            ) : message.role === "system" ? (
              <details key={key} className="message system" open>
                <summary>
                  <span className="avatar">{roleIcon(message.role)}</span>
                  <span className="message-meta">System event</span>
                </summary>
                {message.kind === "image" ? (
                  <img src={message.content} alt="" />
                ) : (
                  <StreamingMarkdown content={message.content} streaming={live} />
                )}
              </details>
            ) : (
              <article key={key} className={`message ${message.role}${live ? " streaming" : ""}`}>
                <div className="message-top">
                  <span className="avatar">{roleIcon(message.role)}</span>
                  <span className="message-meta">{message.role}</span>
                </div>
                {message.kind === "image" ? (
                  <img src={message.content} alt="" />
                ) : (
                  <StreamingMarkdown content={message.content} streaming={live} />
                )}
              </article>
            );
          })}

          {awaitingResponse && (
            <article className="message assistant thinking-card">
              <div className="message-top">
                <span className="avatar">
                  <Bot size={16} />
                </span>
                <span className="message-meta">Thinking</span>
              </div>
              <div className="thinking-text">
                <span>Codex is thinking</span>
                <span className="thinking-dots" aria-hidden="true">
                  <i />
                  <i />
                  <i />
                </span>
              </div>
            </article>
          )}

          {authRequests.map((request) => (
            <article className="auth-card" key={request.requestId}>
              <div className="auth-copy">
                <span className="auth-kicker">Authorization required</span>
                <strong>{request.title}</strong>
                <p>{request.body}</p>
                <code>{request.action}</code>
              </div>
              <div className="auth-actions">
                <button onClick={() => answerAuth(request.requestId, false)} title="Deny">
                  <ShieldX size={18} />
                </button>
                <button className="approve" onClick={() => answerAuth(request.requestId, true)} title="Approve">
                  <ShieldCheck size={18} />
                </button>
              </div>
            </article>
          ))}
          <div ref={feedEndRef} className="feed-end" />
        </div>

        <footer className="composer">
          <div className="composer-box">
            {images.length > 0 && (
              <div className="attachments">
                {images.map((image, index) => (
                  <img key={index} src={image} alt="" />
                ))}
              </div>
            )}
            <textarea
              value={prompt}
              rows={2}
              placeholder={activeSessionId ? "Send a message..." : "Start a session first..."}
              onChange={(event) => setPrompt(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  sendPrompt();
                }
              }}
              disabled={!activeSessionId}
            />
            <div className="composer-toolbar">
              <div className="composer-tools">
                <label className="composer-tool" title="Attach images">
                  <Plus size={21} />
                  <input
                    accept="image/*"
                    multiple
                    type="file"
                    disabled={!activeSessionId}
                    onChange={(event) => attachImages(event.target.files)}
                  />
                </label>
                <span className="composer-agent">
                  <Bot size={17} />
                  <span>Codex</span>
                </span>
              </div>
              <button
                className="send-button"
                onClick={sendPrompt}
                disabled={!activeSessionId || (!prompt.trim() && images.length === 0)}
                title="Send"
              >
                <ArrowUp size={20} />
              </button>
            </div>
          </div>
        </footer>
      </section>

      {deleteTarget && (
        <div className="confirm-backdrop" role="presentation" onMouseDown={() => !deleting && setDeleteTarget(null)}>
          <section
            className="confirm-dialog"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="delete-dialog-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="confirm-icon">
              <Trash2 size={20} />
            </div>
            <h2 id="delete-dialog-title">
              Delete {deleteTarget.kind === "workspace" ? "workspace" : "session"}?
            </h2>
            <p>
              {deleteTarget.kind === "workspace"
                ? `"${deleteTarget.name}" and all of its sessions will be permanently deleted.`
                : `"${deleteTarget.name}" and all of its messages will be permanently deleted.`}
            </p>
            <div className="confirm-actions">
              <button className="cancel-button" onClick={() => setDeleteTarget(null)} disabled={deleting}>
                Cancel
              </button>
              <button className="danger-button" onClick={confirmDelete} disabled={deleting}>
                <Trash2 size={15} />
                {deleting ? "Deleting..." : "Delete"}
              </button>
            </div>
          </section>
        </div>
      )}
    </main>
  );
}

createRoot(document.getElementById("root")!).render(<App />);
