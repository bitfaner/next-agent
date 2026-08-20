package codex

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"nextx/backend/internal/model"
)

func TestConfigCommandFindsCodexInNVMDir(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".nvm", "versions", "node", "v24.18.0", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("PATH", "")

	bin, env, err := Config{Bin: "codex"}.command()
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if bin != codexPath {
		t.Fatalf("bin = %q, want %q", bin, codexPath)
	}
	if !envHasPathDir(env, binDir) {
		t.Fatalf("PATH does not include %q: %#v", binDir, env)
	}
}

func TestConfigCommandPrefersNewestNVMNodeVersion(t *testing.T) {
	home := t.TempDir()
	for _, version := range []string{"v12.22.12", "v20.20.2", "v24.18.0"} {
		binDir := filepath.Join(home, ".nvm", "versions", "node", version, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("PATH", "")

	want := filepath.Join(home, ".nvm", "versions", "node", "v24.18.0", "bin", "codex")
	bin, env, err := Config{Bin: "codex"}.command()
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if bin != want {
		t.Fatalf("bin = %q, want %q", bin, want)
	}
	if !envHasPathDir(env, filepath.Dir(want)) {
		t.Fatalf("PATH does not include newest NVM bin dir %q: %#v", filepath.Dir(want), env)
	}
}

func TestConfigCommandAddsAbsoluteBinDirToPath(t *testing.T) {
	dir := t.TempDir()
	codexPath := filepath.Join(dir, "codex")
	t.Setenv("PATH", "")

	bin, env, err := Config{Bin: codexPath}.command()
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if bin != codexPath {
		t.Fatalf("bin = %q, want %q", bin, codexPath)
	}
	if !envHasPathDir(env, dir) {
		t.Fatalf("PATH does not include %q: %#v", dir, env)
	}
}

func TestConfigCommandFindsWindowsCmdShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATH extension lookup")
	}
	dir := t.TempDir()
	codexPath := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(codexPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	bin, _, err := Config{Bin: "codex"}.command()
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if !strings.EqualFold(bin, codexPath) {
		t.Fatalf("bin = %q, want %q", bin, codexPath)
	}
}

func TestEnvWithPathReplacesPathCaseInsensitively(t *testing.T) {
	t.Setenv("Path", "old")
	t.Setenv("PATH", "")

	env := envWithPath("new")
	if !envHasPathValue(env, "new") {
		t.Fatalf("PATH was not replaced case-insensitively: %#v", env)
	}
}

func envHasPathDir(env []string, dir string) bool {
	for _, entry := range env {
		if len(entry) < len("PATH=") || entry[:len("PATH=")] != "PATH=" {
			continue
		}
		for _, pathDir := range filepath.SplitList(entry[len("PATH="):]) {
			if pathDir == dir {
				return true
			}
		}
	}
	return false
}

func envHasPathValue(env []string, value string) bool {
	for _, entry := range env {
		key, got, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") && got == value {
			return true
		}
	}
	return false
}

func TestAppServerUserInput(t *testing.T) {
	input, err := appServerUserInput(map[string]any{
		"text":   "hello",
		"images": []string{"data:image/png;base64,abc"},
	})
	if err != nil {
		t.Fatalf("appServerUserInput returned error: %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(input))
	}
	text := input[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "hello" {
		t.Fatalf("unexpected text input: %#v", text)
	}
	image := input[1].(map[string]any)
	if image["type"] != "image" || image["url"] != "data:image/png;base64,abc" {
		t.Fatalf("unexpected image input: %#v", image)
	}
}

func TestAppServerThreadRequestStartsNewThread(t *testing.T) {
	method, params := appServerThreadRequest("/tmp/workspace", "")
	if method != "thread/start" {
		t.Fatalf("method = %q, want thread/start", method)
	}
	if params["cwd"] != "/tmp/workspace" {
		t.Fatalf("cwd = %#v", params["cwd"])
	}
	if _, ok := params["threadId"]; ok {
		t.Fatalf("new thread params include threadId: %#v", params)
	}
}

func TestAppServerThreadRequestResumesStoredThread(t *testing.T) {
	method, params := appServerThreadRequest("/tmp/workspace", "thr_123")
	if method != "thread/resume" {
		t.Fatalf("method = %q, want thread/resume", method)
	}
	if params["threadId"] != "thr_123" {
		t.Fatalf("threadId = %#v", params["threadId"])
	}
	if params["cwd"] != "/tmp/workspace" {
		t.Fatalf("cwd = %#v", params["cwd"])
	}
}

func TestRequestIDString(t *testing.T) {
	if got, ok := requestIDString(json.RawMessage(`0`)); !ok || got != "0" {
		t.Fatalf("numeric request id = %q, %v", got, ok)
	}
	if got, ok := requestIDString(json.RawMessage(`"req_1"`)); !ok || got != "req_1" {
		t.Fatalf("string request id = %q, %v", got, ok)
	}
}

func TestHandleServerRequestPublishesApproval(t *testing.T) {
	var published []Event
	agent := NewAgent(Config{}, 1, "/tmp/workspace", func(_ int64, event Event) {
		published = append(published, event)
	})

	agent.handleServerRequest(
		json.RawMessage(`7`),
		"item/commandExecution/requestApproval",
		json.RawMessage(`{"reason":"need write access","command":"touch /etc/test","cwd":"/tmp/workspace"}`),
	)

	if len(published) != 1 || published[0].Type != "auth_request" {
		t.Fatalf("unexpected published events: %#v", published)
	}
	auth := published[0].Auth
	if auth.RequestID != "7" || auth.Body != "need write access" || auth.Action == "" {
		t.Fatalf("unexpected auth request: %#v", auth)
	}
}

func TestAssistantMessageStreaming(t *testing.T) {
	var published []Event
	agent := NewAgent(Config{}, 1, "/tmp/workspace", func(_ int64, event Event) {
		published = append(published, event)
	})

	agent.handleNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"itemId":"msg_1","delta":"Hel"}`),
	)
	agent.handleNotification(
		"item/completed",
		json.RawMessage(`{"item":{"id":"msg_1","type":"agentMessage","text":"Hello"}}`),
	)

	if len(published) != 2 {
		t.Fatalf("expected 2 stream events, got %d", len(published))
	}
	if published[0].Type != "message_delta" || published[0].StreamID != "msg_1" || published[0].Message.Content != "Hel" {
		t.Fatalf("unexpected delta event: %#v", published[0])
	}
	if published[1].Type != "message" || published[1].StreamID != "msg_1" || published[1].Message.Content != "Hello" {
		t.Fatalf("unexpected final event: %#v", published[1])
	}
}

func TestReasoningStreaming(t *testing.T) {
	var published []Event
	agent := NewAgent(Config{}, 1, "/tmp/workspace", func(_ int64, event Event) {
		published = append(published, event)
	})

	agent.handleNotification(
		"item/reasoning/summaryTextDelta",
		json.RawMessage(`{"itemId":"reason_1","summaryIndex":0,"delta":"Inspecting files"}`),
	)
	agent.handleNotification(
		"item/completed",
		json.RawMessage(`{"item":{"id":"reason_1","type":"reasoning","summary":["Inspecting files"]}}`),
	)

	if len(published) != 2 {
		t.Fatalf("expected 2 events, got %#v", published)
	}
	if published[0].Type != "message_delta" || published[0].StreamID != "reason_1:reasoning" {
		t.Fatalf("unexpected reasoning delta: %#v", published[0])
	}
	if published[0].Message == nil || published[0].Message.Kind != "reasoning" {
		t.Fatalf("unexpected reasoning message: %#v", published[0].Message)
	}
	if published[1].Type != "message" || published[1].Message == nil || published[1].Message.Content != "Inspecting files" {
		t.Fatalf("unexpected completed reasoning: %#v", published[1])
	}
}

func TestManagerLogsCodexMessagesAndAuthorizationRequests(t *testing.T) {
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })

	manager := NewManager(Config{})
	manager.Publish(42, Event{
		Type: "message",
		Message: &model.Message{
			SessionID: 42,
			Role:      "assistant",
			Kind:      "text",
			Content:   "final response",
		},
	})
	manager.Publish(42, Event{
		Type: "message",
		Message: &model.Message{
			SessionID: 42,
			Role:      "user",
			Kind:      "text",
			Content:   "must not be logged as a codex response",
		},
	})
	manager.Publish(42, Event{
		Type: "auth_request",
		Auth: &model.AuthRequest{
			SessionID: 42,
			RequestID: "req_7",
			Title:     "Authorization required",
			Body:      "write file",
			Action:    "apply patch",
		},
	})

	logs := output.String()
	if !strings.Contains(logs, `"msg":"codex response message"`) || !strings.Contains(logs, `"message":"final response"`) {
		t.Fatalf("missing codex response log: %s", logs)
	}
	if !strings.Contains(logs, `"msg":"codex authorization requested"`) || !strings.Contains(logs, `"request_id":"req_7"`) {
		t.Fatalf("missing authorization request log: %s", logs)
	}
	if strings.Contains(logs, "must not be logged") {
		t.Fatalf("user message was logged as a codex response: %s", logs)
	}
}
