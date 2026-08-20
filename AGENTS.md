# AGENTS.md

This is a local desktop agent project. When making modifications, prioritize maintaining the stability of the existing architecture and interfaces; do not transform it into a new product form.

## Project Goals

- The Tauri shell is responsible for launching the desktop application and loading the React frontend.
- The React frontend manages the workspace, sessions, message streams, image inputs, and authorization card display.
- The Go backend handles JSON-RPC, SSE, Codex CLI orchestration, and SQLite persistence.
- SQLite only stores local data and introduces no remote dependencies.

## Current Behavior

- A workspace must be selected or created before a session can be started.
- Creating a workspace requires selecting a local directory via the file manager and saving the path string.
- The workspace path must be passed to the subprocess when launching Codex.
- Session messages, SSE deltas, authorization requests, and session states are all saved to local storage.
- Clicking the session list restores the session content and attempts to resume the Codex thread.

## Code Boundaries

- `apps/desktop` handles only UI and Tauri-related capabilities.
- `backend` handles only local services, Codex bridging, event forwarding, and persistence.
- `scripts/build-sidecars.sh` is responsible for building the backend binaries required for desktop packaging.

## Modification Principles

- Prioritize reusing existing RPC methods, data structures, and directory naming conventions.
- Do not rename JSON fields, RPC methods, or SQLite columns without cause.
- Make only changes directly related to the current task; avoid opportunistic refactoring.
- If uncommitted changes by the user are detected, do not roll them back.
- Use `apply_patch` to perform file modifications.

## Backend Conventions

- The JSON-RPC entry point is `/rpc`, and the SSE entry point is `/events/{sessionId}`.
- Starting sessions, restoring sessions, sending messages, and authorization decisions must all go through the backend.
- Codex processes are managed per session; stopping a workspace or session must synchronously terminate the corresponding process.
- Session titles are currently auto-generated from the first user text message; preserve this behavior.

## Frontend Conventions

- The left side displays the workspace and session list, while the right side displays the chat area.
- The message stream must display text, images, streaming deltas, and authorization cards.
- A workspace must exist before starting a new session.
- After a session is restored, historical messages and pending authorization requests must be reloaded into the interface.

## Verification Suggestions

- After touching `backend`, run the relevant Go tests first.
- After touching `apps/desktop`, run the frontend build first or at least confirm that TypeScript type-checking passes.
- If changes affect the packaging workflow, check that `scripts/build-sidecars.sh` aligns with the Tauri configuration.
