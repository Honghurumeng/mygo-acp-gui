package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

// TestRealCommandCode exercises the complete ACP flow of the installed
// Command Code CLI: discovery, initialize, session/new, streaming,
// permission approval, cancel and another prompt on the same session.
//
//	ACP_REAL_COMMAND_CODE=1 go test ./internal/session -run TestRealCommandCode -v
//
// It calls the real model and may spend credits.
func TestRealCommandCode(t *testing.T) {
	if os.Getenv("ACP_REAL_COMMAND_CODE") == "" {
		t.Skip("set ACP_REAL_COMMAND_CODE=1 to run against the real command-code")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	spec, err := agent.CommandCodeLaunchSpec(env)
	if err != nil {
		t.Fatalf("discover command-code: %v", err)
	}
	t.Logf("resolved %s %s at %s", spec.Name, spec.Version, spec.Command)

	updates := make(chan Event, 4096)
	permissions := make(chan PermissionRequest, 16)
	cfg := Config{
		Launch:    spec,
		Workspace: t.TempDir(),
		OnUpdate: func(ev Event) {
			select {
			case updates <- ev:
			default:
			}
		},
		OnPermission: func(ctx context.Context, req PermissionRequest) (acp.RequestPermissionResult, error) {
			select {
			case permissions <- req:
			default:
			}
			for _, option := range req.Options {
				if option.Kind == "allow_once" {
					return acp.SelectedPermission(option.OptionID), nil
				}
			}
			for _, option := range req.Options {
				if strings.HasPrefix(option.Kind, "allow") {
					return acp.SelectedPermission(option.OptionID), nil
				}
			}
			return acp.CancelledPermission(), nil
		},
	}
	s, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start command-code: %v", err)
	}
	defer closeSession(t, s)

	info := s.Info()
	if info.SessionID == "" {
		t.Fatal("empty session id")
	}
	if info.AgentInfo == nil || !strings.Contains(strings.ToLower(info.AgentInfo.Name), "command code") {
		t.Fatalf("agent info = %+v", info.AgentInfo)
	}
	if len(info.AuthMethods) == 0 {
		t.Error("initialize returned no authMethods")
	}
	if info.ConfigOption("model") == nil {
		t.Errorf("configOptions = %+v", info.ConfigOptions)
	}
	if len(info.Modes) == 0 || info.CurrentModeID == "" {
		t.Errorf("modes = %+v, current = %q", info.Modes, info.CurrentModeID)
	}

	t.Run("prompt streams a reply", func(t *testing.T) {
		drainEvents(t, updates)
		out := runPrompt(t, ctx, s, updates, "Reply with exactly: ok", false)
		if out.err != nil {
			t.Fatalf("prompt: %v", out.err)
		}
		if out.res.StopReason != "end_turn" {
			t.Errorf("stop reason = %q", out.res.StopReason)
		}
		if text := eventText(out.events, UpdateMessageChunk); !strings.Contains(strings.ToLower(text), "ok") {
			t.Errorf("streamed text = %q", text)
		}
		if !hasUsage(out.events) {
			t.Error("no usage_update was streamed")
		}
	})

	t.Run("permission approves a write", func(t *testing.T) {
		drainEvents(t, updates)
		drainPermissions(t, permissions)
		out := runPrompt(t, ctx, s, updates, "Create a file named acp-gui-permission.txt in the workspace containing exactly: hello. Use a tool, then reply done.", false)
		if out.err != nil {
			t.Fatalf("prompt: %v", out.err)
		}
		if out.res.StopReason != "end_turn" {
			t.Errorf("stop reason = %q", out.res.StopReason)
		}
		select {
		case req := <-permissions:
			t.Logf("permission: %s (%d options)", req.ToolCall.Title, len(req.Options))
		default:
			t.Error("no session/request_permission was sent")
		}
		if !hasToolUpdate(out.events) {
			t.Error("no tool_call/tool_call_update was streamed")
		}
		data, err := os.ReadFile(filepath.Join(cfg.Workspace, "acp-gui-permission.txt"))
		if err != nil {
			t.Errorf("created file: %v", err)
		} else if strings.TrimSpace(string(data)) != "hello" {
			t.Errorf("file contents = %q", data)
		}
	})

	t.Run("config and mode", func(t *testing.T) {
		drainEvents(t, updates)
		if effort := s.Info().ConfigOption("effort"); effort != nil {
			options, err := s.SetConfigOption(ctx, "effort", "low")
			if err != nil {
				t.Fatalf("set effort: %v", err)
			}
			if current := s.Info().ConfigOption("effort"); current == nil || current.CurrentValue != "low" {
				t.Errorf("effort options = %+v", options)
			}
		}
		if err := s.SetMode(ctx, "plan"); err != nil {
			t.Fatalf("set mode plan: %v", err)
		}
		if got := s.Info().CurrentModeID; got != "plan" {
			t.Errorf("mode = %q", got)
		}
		if err := s.SetMode(ctx, "default"); err != nil {
			t.Fatalf("set mode default: %v", err)
		}
	})

	t.Run("image prompt", func(t *testing.T) {
		drainEvents(t, updates)
		res, err := s.PromptContent(ctx, []acp.ContentBlock{
			{Type: "text", Text: "Reply with exactly: image-ok"},
			{Type: "image", MimeType: "image/png", Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="},
		})
		if err != nil {
			t.Fatalf("image prompt: %v", err)
		}
		if res.StopReason != "end_turn" {
			t.Errorf("stop = %q", res.StopReason)
		}
		text, _ := drainEvents(t, updates)
		if strings.TrimSpace(text) == "" {
			t.Error("no text streamed for the image prompt")
		}
	})

	t.Run("cancel stops the turn and keeps the session usable", func(t *testing.T) {
		drainEvents(t, updates)
		out := runPrompt(t, ctx, s, updates, "Write a detailed 3000-word essay about the history of computing. Do not use tools or ask questions; start writing immediately.", true)
		if out.err != nil {
			t.Fatalf("prompt: %v", out.err)
		}
		if out.res.StopReason != "cancelled" {
			t.Errorf("stop reason = %q, want cancelled", out.res.StopReason)
		}

		drainEvents(t, updates)
		again := runPrompt(t, ctx, s, updates, "Reply with exactly: after-cancel", false)
		if again.err != nil {
			t.Fatalf("prompt after cancel: %v", again.err)
		}
		if again.res.StopReason != "end_turn" {
			t.Errorf("stop reason after cancel = %q", again.res.StopReason)
		}
		if text := eventText(again.events, UpdateMessageChunk); !strings.Contains(text, "after-cancel") {
			t.Errorf("streamed text after cancel = %q", text)
		}
	})
	t.Run("history list load close", func(t *testing.T) {
		drainEvents(t, updates)
		id := s.Info().SessionID
		sessions, err := s.ListSessions(ctx, "")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, item := range sessions {
			if item.SessionID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("current session %s not in session/list (%d rows)", id, len(sessions))
		}

		if err := s.CloseSession(ctx); err != nil {
			t.Fatalf("close: %v", err)
		}
		if got := s.Info().SessionID; got != "" {
			t.Errorf("id after close = %q", got)
		}
		if _, err := s.LoadSession(ctx, id); err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := s.Info().SessionID; got != id {
			t.Errorf("loaded id = %q", got)
		}
		if text, _ := drainEvents(t, updates); strings.TrimSpace(text) == "" {
			t.Error("session/load replayed no history")
		}

		if err := s.CloseSession(ctx); err != nil {
			t.Fatalf("close after load: %v", err)
		}
		fresh, err := s.NewSession(ctx, cfg.Workspace, nil)
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if fresh.SessionID == "" || fresh.SessionID == id {
			t.Errorf("new session = %q", fresh.SessionID)
		}
		if err := s.CloseSession(ctx); err != nil {
			t.Fatalf("close fresh: %v", err)
		}
	})
}

type promptOutcome struct {
	res    acp.PromptResult
	err    error
	events []Event
}

// runPrompt drives one turn. When cancelAfterActivity is set it sends
// session/cancel as soon as the agent shows it started working.
func runPrompt(t *testing.T, ctx context.Context, s *Session, updates <-chan Event, text string, cancelAfterActivity bool) promptOutcome {
	t.Helper()
	promptCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	done := make(chan struct {
		res acp.PromptResult
		err error
	}, 1)
	go func() {
		res, err := s.Prompt(promptCtx, text)
		done <- struct {
			res acp.PromptResult
			err error
		}{res: res, err: err}
	}()

	var cancelOnce sync.Once
	var sendErr error
	var events []Event
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case ev := <-updates:
			events = append(events, ev)
			if cancelAfterActivity && isActivity(ev) {
				cancelOnce.Do(func() {
					if err := s.Cancel(); err != nil {
						sendErr = err
					}
				})
			}
		case out := <-done:
			if sendErr != nil {
				t.Fatalf("session/cancel: %v", sendErr)
			}
			return promptOutcome{res: out.res, err: out.err, events: events}
		case <-timer.C:
			_ = s.Cancel()
			t.Fatal("prompt timed out")
		}
	}
}

func isActivity(ev Event) bool {
	switch ev.Kind {
	case UpdateMessageChunk, UpdateThoughtChunk, UpdateToolCall, UpdateToolCallDone:
		return true
	default:
		return false
	}
}

func eventText(events []Event, kind string) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Kind == kind {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

func hasUsage(events []Event) bool {
	for _, ev := range events {
		if ev.Kind == UpdateUsage && ev.Usage != nil {
			return true
		}
	}
	return false
}

func hasToolUpdate(events []Event) bool {
	for _, ev := range events {
		if ev.Kind == UpdateToolCall || ev.Kind == UpdateToolCallDone {
			return true
		}
	}
	return false
}

func drainPermissions(t *testing.T, ch <-chan PermissionRequest) {
	t.Helper()
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// TestRealCommandCodeAuthRequired checks the unauthenticated path: initialize
// must still succeed, session/new must report -32000, and the app must expose
// the agent's authMethods plus a login hint. It uses a fresh HOME so it never
// touches the user's real credentials.
func TestRealCommandCodeAuthRequired(t *testing.T) {
	if os.Getenv("ACP_REAL_COMMAND_CODE") == "" {
		t.Skip("set ACP_REAL_COMMAND_CODE=1 to run against the real command-code")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	spec, err := agent.CommandCodeLaunchSpec(env)
	if err != nil {
		t.Fatalf("discover command-code: %v", err)
	}
	home := t.TempDir()
	spec.Env = replaceEnv(spec.Env, "HOME", home)
	spec.Env = replaceEnv(spec.Env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	_, err = Start(ctx, Config{Launch: spec, Workspace: t.TempDir()})
	if err == nil {
		t.Fatal("session/new succeeded with a fresh HOME; cannot verify the auth path")
	}
	if !IsAuthRequired(err) {
		t.Fatalf("error is not auth-required: %v", err)
	}
	var auth *AuthRequiredError
	if !errors.As(err, &auth) {
		t.Fatalf("error does not wrap AuthRequiredError: %v", err)
	}
	if len(auth.Methods) == 0 {
		t.Fatal("auth error has no methods")
	}
	if spec.LoginHint == "" {
		t.Fatal("launch spec has no login hint")
	}
	t.Logf("auth required: %v; hint: %s", err, spec.LoginHint)
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
