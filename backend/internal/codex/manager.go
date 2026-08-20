package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nextx/backend/internal/model"
)

type Config struct {
	Bin  string
	Args []string
}

func ConfigFromEnv() Config {
	bin := os.Getenv("NEXTX_CODEX_BIN")
	if bin == "" {
		bin = "codex"
	}
	argsText := strings.TrimSpace(os.Getenv("NEXTX_CODEX_ARGS"))
	if argsText == "" {
		argsText = "app-server --stdio"
	}
	return Config{Bin: bin, Args: strings.Fields(argsText)}
}

func (c Config) command() (string, []string, error) {
	pathValue := augmentedPath(c.Bin)
	env := envWithPath(pathValue)
	if hasPathSeparator(c.Bin) {
		return c.Bin, env, nil
	}
	bin, err := lookPath(c.Bin, pathValue)
	if err != nil {
		return "", nil, fmt.Errorf("%q executable file not found in PATH; install Codex CLI or set NEXTX_CODEX_BIN to its absolute path", c.Bin)
	}
	return bin, env, nil
}

func augmentedPath(bin string) string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	if dir := filepath.Dir(bin); hasPathSeparator(bin) && dir != "." {
		dirs = append(dirs, dir)
	}
	dirs = append(dirs, commonUserBinDirs()...)
	return strings.Join(uniqueExistingDirs(dirs), string(os.PathListSeparator))
}

func commonUserBinDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, ".npm-global", "bin"),
		"/usr/local/bin",
		"/opt/homebrew/bin",
	}
	nvmDirs, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
	sort.SliceStable(nvmDirs, func(i, j int) bool {
		return compareNodeVersions(nvmVersion(nvmDirs[i]), nvmVersion(nvmDirs[j])) > 0
	})
	dirs = append(dirs, nvmDirs...)
	if runtime.GOOS == "windows" {
		dirs = append(dirs, commonWindowsBinDirs(home)...)
	}
	return dirs
}

func nvmVersion(binDir string) string {
	return filepath.Base(filepath.Dir(binDir))
}

func compareNodeVersions(left, right string) int {
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	partCount := len(leftParts)
	if len(rightParts) > partCount {
		partCount = len(rightParts)
	}
	for i := 0; i < partCount; i++ {
		leftPart, rightPart := 0, 0
		if i < len(leftParts) {
			leftPart, _ = strconv.Atoi(leftParts[i])
		}
		if i < len(rightParts) {
			rightPart, _ = strconv.Atoi(rightParts[i])
		}
		if leftPart > rightPart {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
	}
	return 0
}

func commonWindowsBinDirs(home string) []string {
	dirs := []string{
		filepath.Join(home, "AppData", "Roaming", "npm"),
		filepath.Join(home, "AppData", "Local", "Microsoft", "WindowsApps"),
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		dirs = append(dirs, filepath.Join(appData, "npm"))
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		dirs = append(dirs, filepath.Join(localAppData, "Microsoft", "WindowsApps"))
		openAIBins, _ := filepath.Glob(filepath.Join(localAppData, "OpenAI", "Codex", "bin", "*"))
		dirs = append(dirs, openAIBins...)
	}
	for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := os.Getenv(key); root != "" {
			resources, _ := filepath.Glob(filepath.Join(root, "WindowsApps", "OpenAI.Codex_*", "app", "resources"))
			dirs = append(dirs, resources...)
		}
	}
	return dirs
}

func uniqueExistingDirs(dirs []string) []string {
	seen := make(map[string]struct{}, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		info, err := os.Stat(clean)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func lookPath(file, pathValue string) (string, error) {
	for _, dir := range filepath.SplitList(pathValue) {
		for _, candidate := range executableCandidates(dir, file) {
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
				return candidate, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func executableCandidates(dir, file string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(file) != "" {
		return []string{filepath.Join(dir, file)}
	}
	exts := []string{".exe", ".cmd", ".bat", ".com"}
	if pathext := os.Getenv("PATHEXT"); pathext != "" {
		for _, ext := range strings.Split(pathext, ";") {
			ext = strings.TrimSpace(strings.ToLower(ext))
			if ext == "" || ext == ".ps1" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			if !containsStringFold(exts, ext) {
				exts = append(exts, ext)
			}
		}
	}
	candidates := make([]string, 0, len(exts)+1)
	for _, ext := range exts {
		candidates = append(candidates, filepath.Join(dir, file+ext))
	}
	candidates = append(candidates, filepath.Join(dir, file))
	return candidates
}

func containsStringFold(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func envWithPath(pathValue string) []string {
	env := os.Environ()
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			env[i] = key + "=" + pathValue
			return env
		}
	}
	return append(env, "PATH="+pathValue)
}

func hasPathSeparator(path string) bool {
	return strings.ContainsRune(path, os.PathSeparator)
}

type Event struct {
	Type     string             `json:"type"`
	StreamID string             `json:"streamId,omitempty"`
	Message  *model.Message     `json:"message,omitempty"`
	Auth     *model.AuthRequest `json:"auth,omitempty"`
	Status   string             `json:"status,omitempty"`
}

type Manager struct {
	cfg     Config
	mu      sync.Mutex
	agents  map[int64]*Agent
	streams map[int64]map[chan Event]struct{}
	sink    func(int64, Event) Event
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:     cfg,
		agents:  make(map[int64]*Agent),
		streams: make(map[int64]map[chan Event]struct{}),
	}
}

func (m *Manager) Start(ctx context.Context, sessionID int64, workspacePath, threadID string) (string, error) {
	m.mu.Lock()
	if agent, ok := m.agents[sessionID]; ok {
		if agent.Running() {
			m.mu.Unlock()
			return agent.ThreadID(), nil
		}
		delete(m.agents, sessionID)
	}
	agent := NewAgentWithThread(m.cfg, sessionID, workspacePath, threadID, m.Publish)
	m.agents[sessionID] = agent
	m.mu.Unlock()
	if threadID != "" {
		slog.Info("resuming codex session",
			"session_id", sessionID,
			"codex_thread_id", threadID,
			"workspace_path", workspacePath,
		)
	}
	if err := agent.Start(ctx); err != nil {
		m.mu.Lock()
		if m.agents[sessionID] == agent {
			delete(m.agents, sessionID)
		}
		m.mu.Unlock()
		if threadID != "" {
			slog.Error("codex session resume failed",
				"session_id", sessionID,
				"codex_thread_id", threadID,
				"error", err,
			)
		}
		return "", err
	}
	activeThreadID := agent.ThreadID()
	if threadID != "" {
		slog.Info("codex session resumed",
			"session_id", sessionID,
			"codex_thread_id", activeThreadID,
		)
	}
	return activeThreadID, nil
}

func (m *Manager) Send(ctx context.Context, sessionID int64, text string, images []string) error {
	agent, err := m.agent(sessionID)
	if err != nil {
		return err
	}
	return agent.Call(ctx, "user_message", map[string]any{"text": text, "images": images})
}

func (m *Manager) Authorize(ctx context.Context, sessionID int64, requestID string, approved bool) error {
	agent, err := m.agent(sessionID)
	if err != nil {
		return err
	}
	return agent.Authorize(ctx, requestID, approved)
}

func (m *Manager) Stop(sessionID int64) {
	m.mu.Lock()
	agent := m.agents[sessionID]
	delete(m.agents, sessionID)
	m.mu.Unlock()
	if agent != nil {
		agent.stop()
	}
}

func (m *Manager) Subscribe(sessionID int64) (<-chan Event, func()) {
	ch := make(chan Event, 256)
	m.mu.Lock()
	if m.streams[sessionID] == nil {
		m.streams[sessionID] = make(map[chan Event]struct{})
	}
	m.streams[sessionID][ch] = struct{}{}
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		delete(m.streams[sessionID], ch)
		if len(m.streams[sessionID]) == 0 {
			delete(m.streams, sessionID)
		}
		m.mu.Unlock()
		close(ch)
	}
}

func (m *Manager) Publish(sessionID int64, event Event) {
	m.mu.Lock()
	sink := m.sink
	m.mu.Unlock()
	if sink != nil {
		event = sink(sessionID, event)
	}
	m.logCodexEvent(sessionID, event)

	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.streams[sessionID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) logCodexEvent(sessionID int64, event Event) {
	threadID := m.threadID(sessionID)
	if event.Type == "message" && event.Message != nil && event.Message.Role != "user" {
		slog.Info("codex response message",
			"session_id", sessionID,
			"codex_thread_id", threadID,
			"role", event.Message.Role,
			"kind", event.Message.Kind,
			"message", event.Message.Content,
		)
	}
	if event.Type == "auth_request" && event.Auth != nil {
		slog.Info("codex authorization requested",
			"session_id", sessionID,
			"codex_thread_id", threadID,
			"request_id", event.Auth.RequestID,
			"title", event.Auth.Title,
			"body", event.Auth.Body,
			"action", event.Auth.Action,
		)
	}
}

func (m *Manager) threadID(sessionID int64) string {
	m.mu.Lock()
	agent := m.agents[sessionID]
	m.mu.Unlock()
	if agent == nil {
		return ""
	}
	return agent.ThreadID()
}

func (m *Manager) SetSink(sink func(int64, Event) Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sink = sink
}

func (m *Manager) agent(sessionID int64) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[sessionID]
	if !ok {
		return nil, errors.New("codex process is not running for this session")
	}
	return agent, nil
}

type Agent struct {
	cfg            Config
	sessionID      int64
	workspacePath  string
	resumeThreadID string
	publish        func(int64, Event)
	nextID         atomic.Int64
	mu             sync.Mutex
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	threadID       string
	pendingMu      sync.Mutex
	pending        map[int64]chan rpcResult
	streamMu       sync.Mutex
	streams        map[string]string
}

func NewAgent(cfg Config, sessionID int64, workspacePath string, publish func(int64, Event)) *Agent {
	return NewAgentWithThread(cfg, sessionID, workspacePath, "", publish)
}

func NewAgentWithThread(cfg Config, sessionID int64, workspacePath, threadID string, publish func(int64, Event)) *Agent {
	return &Agent{
		cfg:            cfg,
		sessionID:      sessionID,
		workspacePath:  workspacePath,
		resumeThreadID: threadID,
		publish:        publish,
		pending:        make(map[int64]chan rpcResult),
		streams:        make(map[string]string),
	}
}

func (a *Agent) Start(_ context.Context) error {
	// The codex subprocess must outlive the session.start HTTP request. Start it
	// with the backend's lifetime instead of the request context, which is
	// cancelled as soon as that RPC returns.
	bin, env, err := a.cfg.command()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), bin, a.cfg.Args...)
	cmd.Dir = a.workspacePath
	cmd.Env = env
	configureChildProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	a.mu.Lock()
	a.cmd = cmd
	a.stdin = stdin
	a.mu.Unlock()

	go a.readStdout(stdout)
	go a.readStderr(stderr)
	go a.wait(cmd)

	if !a.usesAppServer() {
		a.publish(a.sessionID, Event{Type: "status", Status: "running"})
		return nil
	}

	var initResult json.RawMessage
	if err := a.call("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "nextx-agent",
			"version": "0.1.0",
		},
	}, &initResult, 15*time.Second); err != nil {
		a.stop()
		return fmt.Errorf("initialize codex: %w", err)
	}

	var threadResult struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := a.write(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		a.stop()
		return fmt.Errorf("acknowledge codex initialization: %w", err)
	}
	threadMethod, threadParams := appServerThreadRequest(a.workspacePath, a.resumeThreadID)
	if err := a.call(threadMethod, threadParams, &threadResult, 15*time.Second); err != nil {
		a.stop()
		return fmt.Errorf("%s codex thread: %w", threadMethod, err)
	}
	if threadResult.Thread.ID == "" {
		a.stop()
		return fmt.Errorf("%s did not return a thread id", threadMethod)
	}

	a.mu.Lock()
	a.threadID = threadResult.Thread.ID
	a.mu.Unlock()
	a.publish(a.sessionID, Event{Type: "status", Status: "running"})
	return nil
}

func appServerThreadRequest(workspacePath, resumeThreadID string) (string, map[string]any) {
	method := "thread/start"
	params := map[string]any{
		"cwd":            workspacePath,
		"sandbox":        "workspace-write",
		"approvalPolicy": "on-request",
	}
	if resumeThreadID != "" {
		method = "thread/resume"
		params["threadId"] = resumeThreadID
	}
	return method, params
}

func (a *Agent) ThreadID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.threadID
}

func (a *Agent) Running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stdin != nil
}

func (a *Agent) Call(_ context.Context, method string, params any) error {
	if method == "user_message" && a.usesAppServer() {
		input, err := appServerUserInput(params)
		if err != nil {
			return err
		}
		a.mu.Lock()
		threadID := a.threadID
		a.mu.Unlock()
		if threadID == "" {
			return errors.New("codex thread is not running for this session")
		}
		return a.call("turn/start", map[string]any{
			"threadId": threadID,
			"input":    input,
			"summary":  "detailed",
		}, nil, 30*time.Second)
	}
	id := a.nextID.Add(1)
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	return a.write(payload)
}

func (a *Agent) Authorize(_ context.Context, requestID string, approved bool) error {
	if !a.usesAppServer() {
		return a.Call(context.Background(), "authorization_response", map[string]any{
			"request_id": requestID,
			"approved":   approved,
		})
	}
	decision := "cancel"
	if approved {
		decision = "accept"
	}
	var id any = requestID
	if numeric, err := strconv.ParseInt(requestID, 10, 64); err == nil {
		id = numeric
	}
	return a.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"decision": decision},
	})
}

func (a *Agent) usesAppServer() bool {
	return len(a.cfg.Args) == 0 || a.cfg.Args[0] == "app-server"
}

func appServerUserInput(params any) ([]any, error) {
	values, ok := params.(map[string]any)
	if !ok {
		return nil, errors.New("invalid user_message params")
	}
	text, _ := values["text"].(string)
	images, _ := values["images"].([]string)
	input := make([]any, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		input = append(input, map[string]any{"type": "text", "text": text})
	}
	for _, image := range images {
		input = append(input, map[string]any{"type": "image", "url": image})
	}
	return input, nil
}

func (a *Agent) write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stdin == nil {
		return errors.New("codex stdin is not available")
	}
	_, err = a.stdin.Write(data)
	return err
}

func (a *Agent) call(method string, params any, result any, timeout time.Duration) error {
	id := a.nextID.Add(1)
	ch := make(chan rpcResult, 1)
	a.pendingMu.Lock()
	if a.pending == nil {
		a.pending = make(map[int64]chan rpcResult)
	}
	a.pending[id] = ch
	a.pendingMu.Unlock()
	defer func() {
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
	}()

	if err := a.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if result != nil && len(res.result) > 0 {
			return json.Unmarshal(res.result, result)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("codex %s timed out", method)
	}
}

func (a *Agent) stop() {
	a.mu.Lock()
	cmd := a.cmd
	stdin := a.stdin
	a.stdin = nil
	a.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (a *Agent) wait(cmd *exec.Cmd) {
	_ = cmd.Wait()
	a.mu.Lock()
	a.stdin = nil
	a.mu.Unlock()
	a.pendingMu.Lock()
	for id, ch := range a.pending {
		delete(a.pending, id)
		select {
		case ch <- rpcResult{err: errors.New("codex process stopped")}:
		default:
		}
	}
	a.pendingMu.Unlock()
	a.publish(a.sessionID, Event{Type: "status", Status: "stopped"})
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

func (a *Agent) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	buffer := make([]byte, 0, 1024*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	for scanner.Scan() {
		a.handleLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		a.publish(a.sessionID, Event{
			Type: "message",
			Message: &model.Message{
				SessionID: a.sessionID,
				Role:      "system",
				Kind:      "text",
				Content:   fmt.Sprintf("Codex stdout error: %v", err),
			},
		})
	}
}

func (a *Agent) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		a.publish(a.sessionID, Event{
			Type: "message",
			Message: &model.Message{
				SessionID: a.sessionID,
				Role:      "system",
				Kind:      "text",
				Content:   scanner.Text(),
			},
		})
	}
}

func (a *Agent) handleLine(line string) {
	var envelope struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		a.publish(a.sessionID, Event{
			Type: "message",
			Message: &model.Message{
				SessionID: a.sessionID,
				Role:      "assistant",
				Kind:      "text",
				Content:   line,
			},
		})
		return
	}

	if len(envelope.ID) > 0 && envelope.Method != "" {
		a.handleServerRequest(envelope.ID, envelope.Method, envelope.Params)
		return
	}
	if len(envelope.ID) > 0 {
		a.handleResponse(envelope.ID, envelope.Result, envelope.Error)
		return
	}
	if envelope.Method != "" {
		a.handleNotification(envelope.Method, envelope.Params)
		return
	}

	a.handleLegacyEvent(envelope.Params, envelope.Result)
}

func (a *Agent) handleResponse(idRaw json.RawMessage, result, errorRaw json.RawMessage) {
	id, ok := numericID(idRaw)
	if !ok {
		return
	}
	a.pendingMu.Lock()
	ch := a.pending[id]
	delete(a.pending, id)
	a.pendingMu.Unlock()
	if ch == nil {
		if len(errorRaw) == 0 {
			a.handleLegacyEvent(nil, result)
		}
		return
	}
	if len(errorRaw) > 0 {
		var rpcError struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errorRaw, &rpcError)
		ch <- rpcResult{err: fmt.Errorf("codex: %s", first(rpcError.Message, string(errorRaw)))}
		return
	}
	ch <- rpcResult{result: result}
}

func (a *Agent) handleServerRequest(idRaw json.RawMessage, method string, params json.RawMessage) {
	requestID, ok := requestIDString(idRaw)
	if !ok {
		return
	}
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"applyPatchApproval",
		"execCommandApproval":
		var approval struct {
			Reason  string `json:"reason"`
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
		}
		_ = json.Unmarshal(params, &approval)
		title := "Authorization required"
		body := first(approval.Reason, approval.Command)
		if body == "" {
			body = "Codex is requesting approval."
		}
		action := approval.Command
		if action == "" {
			action = method
		}
		if approval.Cwd != "" {
			action += "\n" + approval.Cwd
		}
		a.publish(a.sessionID, Event{
			Type: "auth_request",
			Auth: &model.AuthRequest{
				SessionID: a.sessionID,
				RequestID: requestID,
				Title:     title,
				Body:      body,
				Action:    action,
			},
		})
	}
}

func (a *Agent) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "item/agentMessage/delta":
		var payload struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &payload) != nil {
			return
		}
		a.streamAssistantDelta(payload.ItemID, payload.Delta)
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		var payload struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &payload) != nil {
			return
		}
		a.streamReasoningDelta(payload.ItemID, payload.Delta)
	case "item/completed":
		var payload struct {
			Item struct {
				ID               string   `json:"id"`
				Type             string   `json:"type"`
				Text             string   `json:"text"`
				Command          string   `json:"command"`
				AggregatedOutput string   `json:"aggregatedOutput"`
				ExitCode         *int     `json:"exitCode"`
				Summary          []string `json:"summary"`
				Content          []string `json:"content"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &payload) != nil {
			return
		}
		switch payload.Item.Type {
		case "agentMessage":
			if payload.Item.Text != "" {
				a.finishAssistantMessage(payload.Item.ID, payload.Item.Text)
			}
		case "reasoning":
			content := strings.Join(payload.Item.Summary, "\n\n")
			if content == "" {
				content = strings.Join(payload.Item.Content, "\n\n")
			}
			a.finishReasoningMessage(payload.Item.ID, content)
		case "commandExecution":
			if payload.Item.ExitCode != nil && payload.Item.AggregatedOutput != "" {
				content := payload.Item.AggregatedOutput
				if payload.Item.Command != "" {
					content = "$ " + payload.Item.Command + "\n\n" + content
				}
				a.publishSystemMessage(content)
			}
		}
	case "turn/completed":
		a.publish(a.sessionID, Event{Type: "status", Status: "idle"})
	case "thread/status/changed":
		var payload struct {
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if json.Unmarshal(params, &payload) != nil {
			return
		}
		switch payload.Status.Type {
		case "active":
			a.publish(a.sessionID, Event{Type: "status", Status: "running"})
		case "idle":
			a.publish(a.sessionID, Event{Type: "status", Status: "idle"})
		}
	default:
		a.handleLegacyEvent(params, nil)
	}
}

func (a *Agent) streamAssistantDelta(itemID, delta string) {
	if itemID == "" || delta == "" {
		return
	}
	a.streamMu.Lock()
	a.streams[itemID] += delta
	a.streamMu.Unlock()
	a.publish(a.sessionID, Event{
		Type:     "message_delta",
		StreamID: itemID,
		Message: &model.Message{
			SessionID: a.sessionID,
			Role:      "assistant",
			Kind:      "text",
			Content:   delta,
		},
	})
}

func (a *Agent) streamReasoningDelta(itemID, delta string) {
	if itemID == "" || delta == "" {
		return
	}
	streamID := itemID + ":reasoning"
	a.streamMu.Lock()
	a.streams[streamID] += delta
	a.streamMu.Unlock()
	a.publish(a.sessionID, Event{
		Type:     "message_delta",
		StreamID: streamID,
		Message: &model.Message{
			SessionID: a.sessionID,
			Role:      "assistant",
			Kind:      "reasoning",
			Content:   delta,
		},
	})
}

func (a *Agent) finishAssistantMessage(itemID, content string) {
	if itemID != "" {
		a.streamMu.Lock()
		delete(a.streams, itemID)
		a.streamMu.Unlock()
	}
	if content == "" {
		return
	}
	a.publish(a.sessionID, Event{
		Type:     "message",
		StreamID: itemID,
		Message: &model.Message{
			SessionID: a.sessionID,
			Role:      "assistant",
			Kind:      "text",
			Content:   content,
		},
	})
}

func (a *Agent) finishReasoningMessage(itemID, content string) {
	streamID := itemID + ":reasoning"
	if itemID != "" {
		a.streamMu.Lock()
		if content == "" {
			content = a.streams[streamID]
		}
		delete(a.streams, streamID)
		a.streamMu.Unlock()
	}
	if content == "" {
		return
	}
	a.publish(a.sessionID, Event{
		Type:     "message",
		StreamID: streamID,
		Message: &model.Message{
			SessionID: a.sessionID,
			Role:      "assistant",
			Kind:      "reasoning",
			Content:   content,
		},
	})
}

func (a *Agent) handleLegacyEvent(params, result json.RawMessage) {
	payload := params
	if len(payload) == 0 {
		payload = result
	}
	var event struct {
		Type      string `json:"type"`
		Role      string `json:"role"`
		Kind      string `json:"kind"`
		Content   string `json:"content"`
		Text      string `json:"text"`
		RequestID string `json:"request_id"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Action    string `json:"action"`
		Status    string `json:"status"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &event) != nil {
		return
	}

	switch event.Type {
	case "auth_request", "authorization_request", "approval_request":
		a.publish(a.sessionID, Event{
			Type: "auth_request",
			Auth: &model.AuthRequest{
				SessionID: a.sessionID,
				RequestID: first(event.RequestID, strconv.FormatInt(a.nextID.Add(1), 10)),
				Title:     first(event.Title, "Authorization required"),
				Body:      first(event.Body, event.Text),
				Action:    event.Action,
			},
		})
	case "status":
		a.publish(a.sessionID, Event{Type: "status", Status: first(event.Status, "running")})
	default:
		content := first(event.Content, event.Text)
		if content == "" {
			return
		}
		a.publish(a.sessionID, Event{
			Type: "message",
			Message: &model.Message{
				SessionID: a.sessionID,
				Role:      first(event.Role, "assistant"),
				Kind:      first(event.Kind, "text"),
				Content:   content,
			},
		})
	}
}

func (a *Agent) publishSystemMessage(content string) {
	a.publish(a.sessionID, Event{
		Type: "message",
		Message: &model.Message{
			SessionID: a.sessionID,
			Role:      "system",
			Kind:      "text",
			Content:   content,
		},
	})
}

func numericID(raw json.RawMessage) (int64, bool) {
	var id int64
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, true
	}
	return 0, false
}

func requestIDString(raw json.RawMessage) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return text, true
	}
	if id, ok := numericID(raw); ok {
		return strconv.FormatInt(id, 10), true
	}
	return "", false
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
