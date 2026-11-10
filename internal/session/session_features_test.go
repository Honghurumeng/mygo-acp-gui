package session

import (
	"context"
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

func TestFakeAgentListLoadResumeClose(t *testing.T) {
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

	info, err = s.ResumeSession(ctx, "fake-history")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if info.SessionID != "fake-history" {
		t.Errorf("resumed id = %q", info.SessionID)
	}
	if text, _ := drainEvents(t, updates); text != "" {
		t.Errorf("resume replayed history: %q", text)
	}

	info, err = s.NewSession(ctx, s.cfg.Workspace, nil)
	if err != nil {
		t.Fatalf("new after resume: %v", err)
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

	sessions, err := ListSessions(ctx, launch, workspace)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "fake-history" {
		t.Errorf("sessions = %+v", sessions)
	}
}
