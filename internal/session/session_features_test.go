package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
)

func TestFakeAgentConfigAndMode(t *testing.T) {
	s := startFake(t, nil, Config{})
	defer closeSession(t, s)

	info := s.Info()
	model := info.ConfigOption("model")
	if model == nil || model.CurrentValue != "demo/small" {
		t.Fatalf("model option = %+v", model)
	}
	if info.CurrentModeID != "default" {
		t.Fatalf("mode = %q", info.CurrentModeID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	options, err := s.SetConfigOption(ctx, "model", "demo/large")
	if err != nil {
		t.Fatalf("set model: %v", err)
	}
	if len(options) == 0 || s.Info().ConfigOption("model").CurrentValue != "demo/large" {
		t.Errorf("model did not change: %+v", options)
	}
	if err := s.SetMode(ctx, "plan"); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if got := s.Info().CurrentModeID; got != "plan" {
		t.Errorf("mode = %q", got)
	}

	res, err := s.Prompt(ctx, "hello")
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("prompt: %+v %v", res, err)
	}
}

func TestFakeAgentListLoadClose(t *testing.T) {
	updates := make(chan Event, 256)
	s := startFake(t, nil, Config{OnUpdate: func(ev Event) { updates <- ev }})
	defer closeSession(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sessions, err := s.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "fake-history" {
		t.Fatalf("sessions = %+v", sessions)
	}

	drainEvents(t, updates)
	info, err := s.LoadSession(ctx, "fake-history")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if info.SessionID != "fake-history" {
		t.Errorf("session id = %q", info.SessionID)
	}
	text, _ := drainEvents(t, updates)
	if !strings.Contains(text, "Answer from history") {
		t.Errorf("history text = %q", text)
	}
	// A user chunk from history is delivered too.
	if !strings.Contains(text, "Question from history") {
		t.Errorf("history misses user message: %q", text)
	}

	if err := s.CloseSession(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := s.Info().SessionID; got != "" {
		t.Errorf("session id after close = %q", got)
	}

	info, err = s.NewSession(ctx, s.cfg.Workspace, nil)
	if err != nil {
		t.Fatalf("new after load: %v", err)
	}
	if info.SessionID != "fake-session-2" {
		t.Errorf("new session id = %q", info.SessionID)
	}
}

func TestFakeAgentPromptImagesAndMCP(t *testing.T) {
	s := startFake(t, nil, Config{MCPServers: []any{map[string]any{"name": "demo", "command": "true"}}})
	defer closeSession(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	res, err := s.PromptContent(ctx, []acp.ContentBlock{
		{Type: "text", Text: "look"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
	})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Errorf("stop = %q", res.StopReason)
	}
}

func TestFakeAgentDeferredHistory(t *testing.T) {
	updates := make(chan Event, 256)
	cfg := Config{
		LoadSessionID: "fake-history",
		DeferUpdates:  true,
		OnUpdate:      func(ev Event) { updates <- ev },
	}
	s := startFake(t, nil, cfg)
	defer closeSession(t, s)

	if s.Info().SessionID != "fake-history" {
		t.Fatalf("session id = %q", s.Info().SessionID)
	}
	select {
	case ev := <-updates:
		t.Fatalf("event delivered before Activate: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
	s.Activate()
	text, _ := drainEvents(t, updates)
	if !strings.Contains(text, "Answer from history") || !strings.Contains(text, "Question from history") {
		t.Errorf("history text = %q", text)
	}
}

func TestStandaloneListUsesProcessWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the fake agent")
	}
	launch, workspace := fakeLaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	sessions, err := ListSessionsIn(ctx, launch, workspace, "")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %+v", sessions)
	}
	got, err := filepath.EvalSymlinks(sessions[0].Cwd)
	if err != nil {
		t.Fatalf("session cwd %q: %v", sessions[0].Cwd, err)
	}
	want, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("workspace %q: %v", workspace, err)
	}
	if got != want {
		t.Errorf("session cwd = %q, want process workspace %q", got, want)
	}
}

func TestProbeAndStandaloneList(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the fake agent")
	}
	launch, workspace := fakeLaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	info, err := Probe(ctx, launch)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.AgentInfo == nil || info.AgentInfo.Name != "fake-agent" {
		t.Errorf("probe info = %+v", info)
	}
	if info.SessionID != "" {
		t.Errorf("initialize-only probe opened a session: %q", info.SessionID)
	}

	sessions, err := ListSessions(ctx, launch, workspace)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "fake-history" {
		t.Errorf("sessions = %+v", sessions)
	}
}

func TestProbeConfigReadsConfigOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the fake agent")
	}
	launch, _ := fakeLaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	info, err := ProbeConfig(ctx, launch)
	if err != nil {
		t.Fatalf("probe config: %v", err)
	}
	if info.SessionID == "" {
		t.Fatal("probe config returned no session")
	}
	model := info.ConfigOption("model")
	if model == nil || len(model.Options) == 0 {
		t.Fatalf("config options = %+v", info.ConfigOptions)
	}
	if info.ConfigOption("thought_level") == nil {
		t.Errorf("thinking option missing: %+v", info.ConfigOptions)
	}
	if len(info.Modes) == 0 || info.CurrentModeID == "" {
		t.Errorf("modes missing: %+v current=%q", info.Modes, info.CurrentModeID)
	}

	// The options probe must survive a session/new failure: an agent that
	// needs a login still has to probe cleanly, just without options.
	broken := launch
	broken.Env = append(append([]string(nil), launch.Env...), "FAKE_AGENT_NO_SESSION=1")
	info, err = ProbeConfig(ctx, broken)
	if err != nil {
		t.Fatalf("probe config without session/new: %v", err)
	}
	if info.AgentInfo == nil || len(info.ConfigOptions) != 0 {
		t.Errorf("unexpected fallback info = %+v", info)
	}
}
