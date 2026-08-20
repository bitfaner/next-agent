# NextX Agent

NextX Agent is a local desktop shell for running Codex CLI sessions inside a selected workspace. It combines a Tauri desktop app, a React UI, and a Go backend with SQLite persistence.

## What it does

- Lets you create and select local workspaces.
- Starts Codex sessions inside the chosen workspace.
- Streams Codex output and authorization requests to the UI over SSE.
- Stores workspaces, sessions, messages, and auth requests in SQLite.
- Supports session reload/resume from persisted history.

## Repository Layout

- `apps/desktop` - Tauri shell and React frontend.
- `backend` - Go JSON-RPC + SSE server and Codex bridge.
- `scripts` - helper scripts for native sidecar builds.

## Architecture

- Frontend talks to the backend through JSON-RPC at `/rpc`.
- Session events stream from `/events/{sessionId}`.
- Backend starts one Codex process per session.
- The selected workspace path is passed to Codex as the process working directory.
- SQLite is used for local persistence only.

## Development

Start the backend:

```sh
cd backend
go run ./cmd/nextx-agent
```

Start the desktop UI:

```sh
cd apps/desktop
npm install
npm run dev
```

By default the UI expects the backend at `http://127.0.0.1:34121`.
When running the frontend outside Tauri, set `VITE_BACKEND_URL` if the backend is elsewhere.

## Codex CLI Configuration

The backend looks for Codex using these environment variables:

- `NEXTX_CODEX_BIN` - Codex executable path, defaults to `codex`
- `NEXTX_CODEX_ARGS` - Codex arguments, defaults to `app-server --stdio`

Examples:

```sh
NEXTX_CODEX_BIN=/path/to/codex
NEXTX_CODEX_ARGS="app-server --stdio"
```

The backend also supports legacy `codex json-rpc` mode if you set:

```sh
NEXTX_CODEX_ARGS="json-rpc"
```

## Data Storage

The backend stores data in SQLite. The default database path is:

- macOS/Linux: `~/.config/nextx-agent/nextx.db`
- fallback: the system temp directory

Stored data includes:

- workspaces
- sessions
- messages
- authorization requests

## Build and Package

Build native sidecars, then package the Tauri app:

```sh
./scripts/build-sidecars.sh
cd apps/desktop
npm run tauri build
```

The bundled targets are `dmg`, `msi`, and `deb`.

## Desktop App Notes

- A workspace must be selected before starting a session.
- Creating a workspace opens a folder picker in the desktop app.
- Sessions can be reopened from the session list.
- Text and images can be sent from the composer.
- Authorization cards returned by the backend are shown inline in the conversation.
