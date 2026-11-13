package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

const testTimeout = 15 * time.Second

func TestFakeAgentPromptAndUpdates(t *testing.T) {
	updates := make(chan Event, 256)
	s := startFake(t, nil, Config{OnUpdate: func(ev Event) { updates <- ev }})
	defer closeSession(t, s)

	res, err := s.Prompt(context.Background(), "/thought hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Errorf("stop reason = %q", res.StopReason)
	}
	text, thought := drainEvents(t, updates)
	if !strings.Contains(text, "收到：/thought hello") {
		t.Errorf("streamed text = %q", text)
	}
	if !strings.Contains(text, "demo/small") {
		t.Errorf("streamed text misses model: %q", text)
	}
	if !strings.Contains(thought, "think") {
		t.Errorf("thought = %q", thought)
	}

	info := s.Info()
	if info.SessionID != "fake-session" {
		t.Errorf("session id = %q", info.SessionID)
	}
	if info.AgentInfo == nil || info.AgentInfo.Name != "fake-agent" {
		t.Errorf("agent info = %+v", info.AgentInfo)
	}
	if !strings.Contains(s.Stderr(), "fake-agent ready") {
		t.Errorf("stderr = %q", s.Stderr())
	}
}

func TestFakeAgentCancel(t *testing.T) {
	updates := make(chan Event, 256)
	s := startFake(t, nil, Config{OnUpdate: func(ev Event) { updates <- ev }})
	defer closeSession(t, s)

	type result struct {
		res acp.PromptResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := s.Prompt(context.Background(), "/slow please stop")
		done <- result{res: res, err: err}
	}()

	// Wait for a chunk so the agent is definitely mid-turn.
	deadline := time.After(testTimeout)
	for {
		select {
		case ev := <-updates:
			if ev.Kind == UpdateMessageChunk && ev.Text != "" {
				if err := s.Cancel(); err != nil {
					t.Fatalf("cancel: %v", err)
				}
				goto cancelled
			}
		case <-deadline:
			t.Fatal("no streaming update before cancel")
		}
	}
cancelled:
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("prompt: %v", got.err)
		}
		if got.res.StopReason != "cancelled" {
			t.Errorf("stop reason = %q, want cancelled", got.res.StopReason)
		}
	case <-time.After(testTimeout):
		t.Fatal("prompt did not finish after session/cancel")
	}
}

func TestFakeAgentPermission(t *testing.T) {
	requests := make(chan PermissionRequest, 1)
	choices := make(chan acp.RequestPermissionResult, 1)
	updates := make(chan Event, 256)
	cfg := Config{
		OnUpdate: func(ev Event) { updates <- ev },
		OnPermission: func(ctx context.Context, req PermissionRequest) (acp.RequestPermissionResult, error) {
			requests <- req
			select {
			case result := <-choices:
				return result, nil
			case <-ctx.Done():
				return acp.CancelledPermission(), nil
			}
		},
	}
	s := startFake(t, nil, cfg)
	defer closeSession(t, s)

	type result struct {
		res acp.PromptResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := s.Prompt(context.Background(), "/permission")
		done <- result{res: res, err: err}
	}()

	var req PermissionRequest
	select {
	case req = <-requests:
	case <-time.After(testTimeout):
		t.Fatal("no permission request")
	}
	if len(req.Options) < 2 {
		t.Fatalf("permission options = %+v", req.Options)
	}
	if req.ToolCall.Title != "Fake tool needs approval" {
		t.Errorf("tool title = %q", req.ToolCall.Title)
	}
	choices <- acp.SelectedPermission("allow-once")

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("prompt: %v", got.err)
		}
		if got.res.StopReason != "end_turn" {
			t.Errorf("stop reason = %q", got.res.StopReason)
		}
	case <-time.After(testTimeout):
		t.Fatal("prompt did not finish after permission")
	}

	text, _ := drainEvents(t, updates)
	if !strings.Contains(text, "permission granted") {
		t.Errorf("streamed text = %q", text)
	}
}

func fakeLaunch(t *testing.T) (agent.LaunchSpec, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-agent integration test is Unix-only for now")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	node, err = filepath.Abs(node)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot(t), "scripts", "fake-agent.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("fake agent: %v", err)
	}
	return agent.LaunchSpec{
		ID:      "fake",
		Name:    "fake-agent",
		Command: node,
		Args:    []string{script},
		Env:     os.Environ(),
		Source:  agent.SourceBuiltin,
	}, t.TempDir()
}

func startFake(t *testing.T, extraEnv []string, cfg Config) *Session {
	t.Helper()
	cfg.Launch, cfg.Workspace = fakeLaunch(t)
	if len(extraEnv) > 0 {
		cfg.Launch.Env = append(append([]string(nil), cfg.Launch.Env...), extraEnv...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	s, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start fake agent: %v", err)
	}
	if s.PID() <= 0 {
		t.Fatal("session has no pid")
	}
	return s
}

func closeSession(t *testing.T, s *Session) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("close timed out")
	}
}

func drainEvents(t *testing.T, updates <-chan Event) (text, thought string) {
	t.Helper()
	for {
		select {
		case ev := <-updates:
			switch ev.Kind {
			case UpdateMessageChunk, UpdateUserChunk:
				text += ev.Text
			case UpdateThoughtChunk:
				thought += ev.Text
			}
		default:
			return text, thought
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestLegacyThinkingModesAreDropped(t *testing.T) {
	// pi-acp v1 still fills the legacy `modes` block with thinking levels.
	// The thought_level config option is authoritative; showing the same
	// values twice leaves a stale mode menu when the model changes.
	pi := acp.SessionState{
		ConfigOptions: []acp.ConfigOption{{
			ID: "thought_level", Category: "thought_level", Type: "select", CurrentValue: "max",
			Options: []acp.ConfigOptionValue{
				{Value: "low"}, {Value: "high"}, {Value: "max"},
			},
		}},
		Modes: &acp.ModeState{
			CurrentModeID: "max",
			AvailableModes: []acp.Mode{
				{ID: "low", Name: "Thinking: low"},
				{ID: "high", Name: "Thinking: high"},
				{ID: "max", Name: "Thinking: max"},
			},
		},
	}
	var info Info
	applySessionState(&info, pi)
	if len(info.Modes) != 0 || info.CurrentModeID != "" {
		t.Errorf("legacy thinking modes survived: modes=%+v current=%q", info.Modes, info.CurrentModeID)
	}
	if option := acp.ThinkingOption(info.ConfigOptions); option == nil || option.CurrentValue != "max" {
		t.Errorf("thinking option lost: %+v", info.ConfigOptions)
	}

	// Real session modes (Command Code) are not thinking values and stay.
	cc := acp.SessionState{
		ConfigOptions: []acp.ConfigOption{{
			ID: "effort", Category: "thought_level", Type: "select", CurrentValue: "max",
			Options: []acp.ConfigOptionValue{{Value: "low"}, {Value: "high"}, {Value: "max"}},
		}},
		Modes: &acp.ModeState{
			CurrentModeID: "plan",
			AvailableModes: []acp.Mode{
				{ID: "default", Name: "Default"},
				{ID: "plan", Name: "Plan"},
			},
		},
	}
	info = Info{}
	applySessionState(&info, cc)
	if len(info.Modes) != 2 || info.CurrentModeID != "plan" {
		t.Errorf("real modes were dropped: modes=%+v current=%q", info.Modes, info.CurrentModeID)
	}
}
