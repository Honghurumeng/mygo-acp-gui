package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/agent"
)

// TestRealCodex is an opt-in smoke test against the installed Codex CLI and
// its existing credentials:
//
//	ACP_REAL_CODEX=1 go test ./internal/session -run TestRealCodex -v -count=1
//
// It calls the real model and may spend credits.
func TestRealCodex(t *testing.T) {
	if os.Getenv("ACP_REAL_CODEX") == "" {
		t.Skip("set ACP_REAL_CODEX=1 to run against the real codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	spec, err := agent.CodexLaunchSpec(filepath.Join(repoRoot(t), "resources"), env)
	if err != nil {
		t.Fatalf("codex launch spec: %v", err)
	}
	t.Logf("resolved %s %s at %s (codex %s)", spec.Name, spec.Version, spec.Command, env.CodexPath)

	updates := make(chan Event, 4096)
	s, err := Start(ctx, Config{
		Launch:    spec,
		Workspace: t.TempDir(),
		OnUpdate: func(ev Event) {
			select {
			case updates <- ev:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("start codex: %v", err)
	}
	defer closeSession(t, s)

	info := s.Info()
	if info.SessionID == "" {
		t.Fatal("empty session id")
	}
	if info.AgentInfo == nil || !strings.Contains(strings.ToLower(info.AgentInfo.Name), "codex") {
		t.Fatalf("agent info = %+v", info.AgentInfo)
	}
	if len(info.AuthMethods) == 0 {
		t.Error("initialize returned no authMethods")
	}
	if info.ConfigOption("model") == nil {
		t.Errorf("configOptions = %+v", info.ConfigOptions)
	}

	promptCtx, promptCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer promptCancel()
	res, err := s.Prompt(promptCtx, "Reply with exactly: ok")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	text := drainEventsText(t, updates)
	t.Logf("stop=%s text=%q", res.StopReason, strings.TrimSpace(text))
	if strings.TrimSpace(text) == "" {
		t.Fatal("codex returned no agent message")
	}
}

// drainEventsText drains message chunks already queued for this turn.
func drainEventsText(t *testing.T, updates <-chan Event) string {
	t.Helper()
	var text strings.Builder
	for {
		select {
		case ev := <-updates:
			if ev.Kind == UpdateMessageChunk {
				text.WriteString(ev.Text)
			}
		default:
			return text.String()
		}
	}
}
