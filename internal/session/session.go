// Package session owns one ACP agent process and one ACP session.
//
// It spawns the agent in its own process group, speaks ACP over its
// stdin/stdout, performs initialize plus session/new or session/load,
// forwards session updates, and kills the process group on Close. All
// callbacks run on background goroutines and must not block; the UI hands
// work to Window.Update.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

const (
	initializeTimeout = 20 * time.Second
	sessionTimeout    = 20 * time.Second
	sessionCloseGrace = time.Second
	closeGrace        = 1500 * time.Millisecond
	killWait          = 2 * time.Second
	stderrDrain       = 500 * time.Millisecond
	readDrain         = 300 * time.Millisecond
	stderrTail        = 64 * 1024
)

// ErrBusy is returned when a prompt is already in flight.
var ErrBusy = errors.New("session: a prompt is already in flight")

// Config describes one agent process and session.
type Config struct {
	Launch     agent.LaunchSpec
	Workspace  string
	ClientInfo acp.Implementation

	// MCPServers is passed to session/new and session/load. Nil becomes [].
	MCPServers []any
	// LoadSessionID attaches an existing session and replays its history.
	// When empty a new session is created.
	LoadSessionID string
	// InitializeOnly stops after initialize, for probes and session lists.
	InitializeOnly bool
	// DeferUpdates buffers session/update events until Activate is called,
	// so the UI can install the Session before receiving history.
	DeferUpdates bool

	// OnUpdate receives parsed session/update notifications.
	OnUpdate func(Event)
	// OnPermission answers session/request_permission. It may block while
	// the user decides; the context is cancelled when the session closes.
	OnPermission func(context.Context, PermissionRequest) (acp.RequestPermissionResult, error)
	// OnStderr receives chunks of agent stderr.
	OnStderr func(string)
	// OnExit receives the process exit, unless startup failed.
	OnExit func(Exit)
	// OnProtocolError receives malformed output without stopping the client.
	OnProtocolError func(error)
}

// Session is a running agent process and ACP session.
type Session struct {
	cfg Config

	cmd    *exec.Cmd
	pid    int
	client *acp.Client
	stdin  *os.File
	stdout *os.File
	stderr *os.File

	stderrBuf  *tailBuffer
	stderrDone chan struct{}

	mcpMu      sync.RWMutex
	mcpServers []any

	infoMu sync.RWMutex
	info   Info

	ready          atomic.Bool
	closeRequested atomic.Bool
	prompting      atomic.Bool

	eventsMu     sync.Mutex
	eventsActive bool
	events       []Event

	closeOnce   sync.Once
	finishOnce  sync.Once
	processDone chan struct{}
	done        chan struct{}

	mu       sync.Mutex
	exitErr  error
	closeErr error
}

// Start spawns the agent and performs initialize plus session/new or
// session/load. With InitializeOnly it stops after initialize. On failure the
// process group is already cleaned up.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	if err := cfg.Launch.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cfg.Workspace) {
		return nil, fmt.Errorf("session: workspace must be absolute: %q", cfg.Workspace)
	}
	info, err := os.Stat(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("session: workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("session: workspace is not a directory: %s", cfg.Workspace)
	}
	if cfg.ClientInfo.Name == "" {
		cfg.ClientInfo = acp.Implementation{Name: "acp-gui", Title: "ACP GUI", Version: "dev"}
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = []any{}
	}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("session: stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return nil, fmt.Errorf("session: stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, fmt.Errorf("session: stderr pipe: %w", err)
	}

	cmd := exec.Command(cfg.Launch.Command, cfg.Launch.Args...)
	cmd.Dir = cfg.Workspace
	cmd.Env = cfg.Launch.Env
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		return nil, fmt.Errorf("session: start %s: %w", cfg.Launch.Command, err)
	}
	// The child holds its own copies of these ends.
	_ = stdinR.Close()
	_ = stdoutW.Close()
	_ = stderrW.Close()

	s := &Session{
		cfg:          cfg,
		cmd:          cmd,
		pid:          cmd.Process.Pid,
		stdin:        stdinW,
		stdout:       stdoutR,
		stderr:       stderrR,
		stderrBuf:    newTailBuffer(stderrTail),
		stderrDone:   make(chan struct{}),
		mcpServers:   append([]any(nil), cfg.MCPServers...),
		processDone:  make(chan struct{}),
		done:         make(chan struct{}),
		eventsActive: !cfg.DeferUpdates,
	}
	s.client = acp.NewClient(stdoutR, stdinW,
		acp.WithNotificationHandler(s.handleNotification),
		acp.WithRequestHandler(s.handleRequest),
		acp.WithProtocolErrorHandler(func(_ []byte, err error) {
			s.protocolError(err)
		}),
	)

	go s.readStderr()
	s.client.Start()
	go s.waitLoop()

	if err := s.handshake(ctx); err != nil {
		s.Close()
		return nil, s.withStderr(err)
	}
	select {
	case <-s.processDone:
		s.Close()
		return nil, s.withStderr(errors.New("agent exited during startup"))
	default:
	}
	s.ready.Store(true)
	return s, nil
}

// Activate releases session/update events buffered by DeferUpdates. Events
// delivered after it keep their order.
func (s *Session) Activate() {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if s.eventsActive {
		return
	}
	s.eventsActive = true
	pending := s.events
	s.events = nil
	for _, ev := range pending {
		if s.cfg.OnUpdate != nil {
			s.cfg.OnUpdate(ev)
		}
	}
}

func (s *Session) handshake(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	raw, err := s.client.Call(initCtx, "initialize", acp.InitializeParams{
		ProtocolVersion:    acp.ProtocolVersion,
		ClientCapabilities: acp.DefaultClientCapabilities(),
		ClientInfo:         &s.cfg.ClientInfo,
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	var init acp.InitializeResult
	if err := json.Unmarshal(raw, &init); err != nil {
		return fmt.Errorf("initialize: decode result: %w", err)
	}
	if init.ProtocolVersion != acp.ProtocolVersion {
		return fmt.Errorf("initialize: agent speaks protocol %d, want %d", init.ProtocolVersion, acp.ProtocolVersion)
	}

	s.infoMu.Lock()
	s.info = Info{
		ProtocolVersion:   init.ProtocolVersion,
		AgentInfo:         init.AgentInfo,
		AuthMethods:       init.AuthMethods,
		AgentCapabilities: init.AgentCapabilities,
	}
	s.infoMu.Unlock()

	if s.cfg.InitializeOnly {
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, sessionTimeout)
	defer cancel()
	var method string
	var params any
	if s.cfg.LoadSessionID != "" {
		method = "session/load"
		params = acp.LoadSessionParams{
			SessionID:  s.cfg.LoadSessionID,
			Cwd:        s.cfg.Workspace,
			MCPServers: s.cfg.MCPServers,
		}
	} else {
		method = "session/new"
		params = acp.NewSessionParams{
			Cwd:        s.cfg.Workspace,
			MCPServers: s.cfg.MCPServers,
		}
	}
	raw, err = s.client.Call(callCtx, method, params)
	if err != nil {
		if len(init.AuthMethods) > 0 && isAuthError(err) {
			return &AuthRequiredError{Methods: init.AuthMethods, Err: fmt.Errorf("%s: %w", method, err)}
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	var state acp.SessionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	sessionID := state.SessionID
	if sessionID == "" {
		sessionID = s.cfg.LoadSessionID
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("%s: empty sessionId", method)
	}

	s.infoMu.Lock()
	s.info.SessionID = sessionID
	applySessionState(&s.info, state)
	s.infoMu.Unlock()
	return nil
}

// Prompt sends a text turn and waits for its result. The session handles
// one prompt at a time; call it from a goroutine.
func (s *Session) Prompt(ctx context.Context, text string) (acp.PromptResult, error) {
	if strings.TrimSpace(text) == "" {
		return acp.PromptResult{}, errors.New("session: empty prompt")
	}
	return s.PromptContent(ctx, []acp.ContentBlock{{Type: "text", Text: text}})
}

// PromptContent sends text and/or image blocks as one turn.
func (s *Session) PromptContent(ctx context.Context, blocks []acp.ContentBlock) (acp.PromptResult, error) {
	if !hasPromptContent(blocks) {
		return acp.PromptResult{}, errors.New("session: empty prompt")
	}
	sessionID := s.currentSessionID()
	if sessionID == "" {
		return acp.PromptResult{}, errors.New("session: no active session")
	}
	if !s.prompting.CompareAndSwap(false, true) {
		return acp.PromptResult{}, ErrBusy
	}
	defer s.prompting.Store(false)

	raw, err := s.client.Call(ctx, "session/prompt", acp.PromptParams{
		SessionID: sessionID,
		Prompt:    blocks,
	})
	if err != nil {
		return acp.PromptResult{}, fmt.Errorf("session/prompt: %w", err)
	}
	var result acp.PromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return acp.PromptResult{}, fmt.Errorf("session/prompt: decode result: %w", err)
	}
	return result, nil
}

func hasPromptContent(blocks []acp.ContentBlock) bool {
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				return true
			}
		case "image":
			if block.Data != "" || block.URI != "" {
				return true
			}
		case "resource_link":
			if block.URI != "" {
				return true
			}
		case "resource":
			if block.Resource != nil && (block.Resource.Text != "" || block.Resource.URI != "") {
				return true
			}
		}
	}
	return false
}

// Cancel asks the agent to stop the current turn. The agent answers the
// prompt with a cancelled stop reason.
func (s *Session) Cancel() error {
	sessionID := s.currentSessionID()
	if sessionID == "" {
		return nil
	}
	if err := s.client.Notify("session/cancel", acp.CancelParams{SessionID: sessionID}); err != nil {
		return fmt.Errorf("session/cancel: %w", err)
	}
	return nil
}

// maxSessionListPages bounds pagination so a broken nextCursor cannot loop
// forever. Agents page by 50, so this still covers a few thousand sessions.
const maxSessionListPages = 40

// ListSessions asks the agent for saved sessions. An empty cwd lists all
// workspaces: it is sent as an explicit null, not omitted. Pages are followed
// until nextCursor is empty.
func (s *Session) ListSessions(ctx context.Context, cwd string) ([]acp.SessionSummary, error) {
	var filter *string
	if cwd != "" {
		filter = &cwd
	}
	var all []acp.SessionSummary
	cursor := ""
	for page := 0; page < maxSessionListPages; page++ {
		params := acp.ListSessionsParams{Cwd: filter}
		if cursor != "" {
			params.Cursor = cursor
		}
		raw, err := s.client.Call(ctx, "session/list", params)
		if err != nil {
			return nil, fmt.Errorf("session/list: %w", err)
		}
		var result acp.ListSessionsResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("session/list: decode result: %w", err)
		}
		all = append(all, result.Sessions...)
		if result.NextCursor == "" || result.NextCursor == cursor {
			return all, nil
		}
		cursor = result.NextCursor
	}
	return all, nil
}

// SetConfigOption changes a model, effort or similar session setting.
func (s *Session) SetConfigOption(ctx context.Context, configID, value string) ([]acp.ConfigOption, error) {
	sessionID := s.currentSessionID()
	if sessionID == "" {
		return nil, errors.New("session: no active session")
	}
	raw, err := s.client.Call(ctx, "session/set_config_option", acp.SetConfigOptionParams{
		SessionID: sessionID,
		ConfigID:  configID,
		Value:     value,
	})
	if err != nil {
		return nil, fmt.Errorf("session/set_config_option: %w", err)
	}
	var result acp.SetConfigOptionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("session/set_config_option: decode result: %w", err)
	}
	if len(result.ConfigOptions) > 0 {
		s.updateConfigOptions(result.ConfigOptions)
	}
	return result.ConfigOptions, nil
}

// SetMode changes the agent's permission/session mode.
func (s *Session) SetMode(ctx context.Context, modeID string) error {
	sessionID := s.currentSessionID()
	if sessionID == "" {
		return errors.New("session: no active session")
	}
	if _, err := s.client.Call(ctx, "session/set_mode", acp.SetModeParams{
		SessionID: sessionID,
		ModeID:    modeID,
	}); err != nil {
		return fmt.Errorf("session/set_mode: %w", err)
	}
	s.infoMu.Lock()
	s.info.CurrentModeID = modeID
	s.infoMu.Unlock()
	return nil
}

// LoadSession attaches a saved session in the current workspace and
// replays its history through OnUpdate. The previous ACP session stays open
// until CloseSessionID is called with its id.
func (s *Session) LoadSession(ctx context.Context, sessionID string) (Info, error) {
	return s.LoadSessionAt(ctx, sessionID, s.cfg.Workspace)
}

// LoadSessionAt is LoadSession for a session saved in another workspace.
func (s *Session) LoadSessionAt(ctx context.Context, sessionID, cwd string) (Info, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Info{}, errors.New("session/load: empty session id")
	}
	if cwd == "" {
		cwd = s.cfg.Workspace
	}
	if !filepath.IsAbs(cwd) {
		return Info{}, fmt.Errorf("session/load: cwd must be absolute: %q", cwd)
	}
	raw, err := s.client.Call(ctx, "session/load", acp.LoadSessionParams{
		SessionID:  sessionID,
		Cwd:        cwd,
		MCPServers: s.currentMCPServers(),
	})
	if err != nil {
		return Info{}, fmt.Errorf("session/load: %w", err)
	}
	var state acp.SessionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return Info{}, fmt.Errorf("session/load: decode result: %w", err)
	}
	s.infoMu.Lock()
	s.info.SessionID = sessionID
	applySessionState(&s.info, state)
	info := cloneInfo(s.info)
	s.infoMu.Unlock()
	return info, nil
}

// SetMCPServers changes the servers passed to the next session/load.
// New sessions take their list from the caller.
func (s *Session) SetMCPServers(servers []any) {
	s.mcpMu.Lock()
	s.mcpServers = append([]any(nil), servers...)
	s.mcpMu.Unlock()
}

func (s *Session) currentMCPServers() []any {
	s.mcpMu.RLock()
	defer s.mcpMu.RUnlock()
	return append([]any(nil), s.mcpServers...)
}

// NewSession creates a new ACP session in the running agent process. The
// old session stays open until closeSessionID is called with its id.
func (s *Session) NewSession(ctx context.Context, cwd string, mcpServers []any) (Info, error) {
	if !filepath.IsAbs(cwd) {
		return Info{}, fmt.Errorf("session/new: cwd must be absolute: %q", cwd)
	}
	if mcpServers == nil {
		mcpServers = s.currentMCPServers()
	}
	raw, err := s.client.Call(ctx, "session/new", acp.NewSessionParams{Cwd: cwd, MCPServers: mcpServers})
	if err != nil {
		return Info{}, fmt.Errorf("session/new: %w", err)
	}
	var state acp.SessionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return Info{}, fmt.Errorf("session/new: decode result: %w", err)
	}
	if strings.TrimSpace(state.SessionID) == "" {
		return Info{}, errors.New("session/new: empty sessionId")
	}
	s.infoMu.Lock()
	s.info.SessionID = state.SessionID
	applySessionState(&s.info, state)
	info := cloneInfo(s.info)
	s.infoMu.Unlock()
	return info, nil
}

// CloseSession closes the active ACP session but keeps the agent process.
func (s *Session) CloseSession(ctx context.Context) error {
	return s.CloseSessionID(ctx, s.currentSessionID())
}

// CloseSessionID closes one ACP session by id.
func (s *Session) CloseSessionID(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if _, err := s.client.Call(ctx, "session/close", acp.CloseSessionParams{SessionID: sessionID}); err != nil {
		return fmt.Errorf("session/close: %w", err)
	}
	s.infoMu.Lock()
	if s.info.SessionID == sessionID {
		s.info.SessionID = ""
		s.info.Title = ""
	}
	s.infoMu.Unlock()
	return nil
}

func applySessionState(info *Info, state acp.SessionState) {
	info.Title = ""
	info.ConfigOptions = cloneConfigOptions(state.ConfigOptions)
	info.Models = nil
	if state.Models != nil {
		models := *state.Models
		models.AvailableModels = append([]acp.Model(nil), state.Models.AvailableModels...)
		info.Models = &models
	}
	info.Modes = nil
	info.CurrentModeID = ""
	if state.Modes != nil {
		modes := *state.Modes
		modes.AvailableModes = append([]acp.Mode(nil), state.Modes.AvailableModes...)
		info.Modes = modes.AvailableModes
		info.CurrentModeID = modes.CurrentModeID
	}
	// Older agents only send models; expose it as a model config option so
	// the UI has one code path.
	if info.CurrentModelOption() == nil && info.Models != nil && info.Models.CurrentModelID != "" {
		options := make([]acp.ConfigOptionValue, 0, len(info.Models.AvailableModels))
		for _, model := range info.Models.AvailableModels {
			options = append(options, acp.ConfigOptionValue{Value: model.ModelID, Name: model.Name, Description: model.Description})
		}
		info.ConfigOptions = append(info.ConfigOptions, acp.ConfigOption{
			ID:           "model",
			Name:         "Model",
			Category:     "model",
			Type:         "select",
			CurrentValue: info.Models.CurrentModelID,
			Options:      options,
		})
	}
}

func (s *Session) updateConfigOptions(options []acp.ConfigOption) {
	s.infoMu.Lock()
	s.info.ConfigOptions = cloneConfigOptions(options)
	s.infoMu.Unlock()
}

func (s *Session) currentSessionID() string {
	s.infoMu.RLock()
	defer s.infoMu.RUnlock()
	return s.info.SessionID
}

// Close closes the ACP session, closes stdin, terminates the process group,
// then waits for it. It is idempotent. A process killed on purpose is not an
// error.
func (s *Session) Close() error {
	s.closeRequested.Store(true)
	s.closeOnce.Do(func() {
		if sessionID := s.currentSessionID(); sessionID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), sessionCloseGrace)
			_ = s.CloseSessionID(ctx, sessionID)
			cancel()
		}
		_ = s.stdin.Close()
		_ = signalGroup(s.pid, false)
		select {
		case <-s.processDone:
		case <-time.After(closeGrace):
			_ = signalGroup(s.pid, true)
			select {
			case <-s.processDone:
			case <-time.After(killWait):
				s.mu.Lock()
				if s.closeErr == nil {
					s.closeErr = fmt.Errorf("session: agent process group %d did not exit", s.pid)
				}
				s.mu.Unlock()
			}
		}
	})
	select {
	case <-s.done:
	case <-time.After(killWait + sessionCloseGrace):
		s.mu.Lock()
		if s.closeErr == nil {
			s.closeErr = fmt.Errorf("session: cleanup of agent process group %d timed out", s.pid)
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

// PID is the agent leader's process id; its process group id is the same.
func (s *Session) PID() int { return s.pid }

// Info returns the initialized session metadata.
func (s *Session) Info() Info {
	s.infoMu.RLock()
	defer s.infoMu.RUnlock()
	return cloneInfo(s.info)
}

// Done is closed once the agent process has exited and its pipes are
// cleaned up.
func (s *Session) Done() <-chan struct{} { return s.done }

// Stderr returns the tail of the agent's stderr.
func (s *Session) Stderr() string { return s.stderrBuf.String() }

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.exitErr = err
	s.mu.Unlock()
	close(s.processDone)

	// If the agent leader died on its own, make sure children it left in
	// the same process group (pi, tool processes) do not stay behind.
	if !s.closeRequested.Load() {
		_ = signalGroup(s.pid, false)
	}

	// Give the read loop a moment to drain the last buffered messages.
	select {
	case <-s.client.Done():
	case <-time.After(readDrain):
	}
	_ = s.client.Close()
	_ = s.stdin.Close()
	_ = s.stdout.Close()

	select {
	case <-s.stderrDone:
	case <-time.After(stderrDrain):
		_ = s.stderr.Close()
	}
	_ = s.stderr.Close()

	s.finishOnce.Do(func() { close(s.done) })

	if s.ready.Load() && s.cfg.OnExit != nil {
		expected := s.closeRequested.Load()
		s.cfg.OnExit(Exit{
			Session:  s,
			Err:      err,
			Stderr:   s.Stderr(),
			Expected: expected,
		})
	}
}

func (s *Session) readStderr() {
	defer close(s.stderrDone)
	defer s.stderr.Close()
	buf := make([]byte, 4096)
	for {
		n, err := s.stderr.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = s.stderrBuf.Write(chunk)
			if s.cfg.OnStderr != nil {
				s.cfg.OnStderr(string(chunk))
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.protocolError(fmt.Errorf("session: read stderr: %w", err))
			}
			return
		}
	}
}

func (s *Session) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	ev := parseEvent(s, cloneRaw(params))
	if ev == nil {
		return
	}
	s.applyInfoEvent(*ev)
	s.eventsMu.Lock()
	if !s.eventsActive {
		s.events = append(s.events, *ev)
		s.eventsMu.Unlock()
		return
	}
	if s.cfg.OnUpdate != nil {
		s.cfg.OnUpdate(*ev)
	}
	s.eventsMu.Unlock()
}

func (s *Session) applyInfoEvent(ev Event) {
	s.infoMu.Lock()
	defer s.infoMu.Unlock()
	switch ev.Kind {
	case UpdateConfigOption:
		if len(ev.ConfigOptions) > 0 {
			s.info.ConfigOptions = cloneConfigOptions(ev.ConfigOptions)
		}
	case UpdateCurrentMode:
		if ev.CurrentModeID != "" {
			s.info.CurrentModeID = ev.CurrentModeID
		}
	case UpdateSessionInfo:
		if ev.Title != "" {
			s.info.Title = ev.Title
		}
	}
}

func (s *Session) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method != "session/request_permission" {
		return nil, &acp.RPCError{Code: acp.CodeMethodNotFound, Message: "method not supported: " + method}
	}
	var request acp.RequestPermissionParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, &acp.RPCError{Code: acp.CodeInvalidParams, Message: "invalid permission params: " + err.Error()}
	}
	if s.cfg.OnPermission == nil {
		return acp.CancelledPermission(), nil
	}
	return s.cfg.OnPermission(ctx, PermissionRequest{
		Session:   s,
		SessionID: request.SessionID,
		ToolCall:  parseToolCall(request.ToolCall),
		Options:   request.Options,
	})
}

func (s *Session) protocolError(err error) {
	if err != nil && s.cfg.OnProtocolError != nil {
		s.cfg.OnProtocolError(err)
	}
}

func (s *Session) withStderr(err error) error {
	tail := strings.TrimSpace(s.Stderr())
	if tail == "" {
		return fmt.Errorf("session: %w", err)
	}
	if len(tail) > 2000 {
		tail = "…" + tail[len(tail)-2000:]
	}
	return fmt.Errorf("session: %w (stderr: %s)", err, tail)
}

func isAuthError(err error) bool {
	var rpcErr *acp.RPCError
	if errors.As(err, &rpcErr) {
		if rpcErr.Code == -32000 {
			return true
		}
		msg := strings.ToLower(rpcErr.Message)
		return strings.Contains(msg, "auth") || strings.Contains(msg, "login")
	}
	return strings.Contains(strings.ToLower(err.Error()), "auth")
}

func cloneInfo(info Info) Info {
	out := info
	out.AuthMethods = append([]acp.AuthMethod(nil), info.AuthMethods...)
	out.ConfigOptions = cloneConfigOptions(info.ConfigOptions)
	out.Modes = append([]acp.Mode(nil), info.Modes...)
	if info.Models != nil {
		models := *info.Models
		models.AvailableModels = append([]acp.Model(nil), info.Models.AvailableModels...)
		out.Models = &models
	}
	return out
}

func cloneConfigOptions(options []acp.ConfigOption) []acp.ConfigOption {
	out := make([]acp.ConfigOption, len(options))
	for i, option := range options {
		out[i] = option
		out[i].Options = append([]acp.ConfigOptionValue(nil), option.Options...)
	}
	return out
}
