package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

// TestRealPi is an opt-in smoke test against the machine's real pi and
// credentials:
//
//	ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v
func TestRealPi(t *testing.T) {
	if os.Getenv("ACP_REAL_PI") == "" {
		t.Skip("set ACP_REAL_PI=1 to run against the real pi")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	resources := filepath.Join(repoRoot(t), "resources")
	spec, err := agent.PiLaunchSpec(resources, env)
	if err != nil {
		t.Fatalf("pi launch spec: %v", err)
	}

	updates := make(chan Event, 512)
	s, err := Start(ctx, Config{
		Launch:    spec,
		Workspace: t.TempDir(),
		OnUpdate:  func(ev Event) { updates <- ev },
	})
	if err != nil {
		t.Fatalf("start pi: %v", err)
	}
	defer closeSession(t, s)

	promptCtx, promptCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer promptCancel()
	res, err := s.Prompt(promptCtx, "Reply with exactly: ok")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	text, usage := drainEventsWithUsage(t, updates)
	t.Logf("stop=%s text=%q", res.StopReason, strings.TrimSpace(text))
	if strings.TrimSpace(text) == "" {
		t.Fatal("pi returned no agent message")
	}
	if usage == nil || usage.Used <= 0 || usage.Size <= 0 {
		t.Fatalf("pi did not emit a usable usage_update: %+v", usage)
	}
	if turnUsage, ok := res.Tokens(); !ok || turnUsage.TotalTokens <= 0 {
		t.Fatalf("pi did not return per-turn usage: %+v (ok=%v)", turnUsage, ok)
	} else {
		t.Logf("context=%d/%d turn=%+v", usage.Used, usage.Size, turnUsage)
	}
}

// drainEventsWithUsage is drainEvents plus the latest usage_update payload.
func drainEventsWithUsage(t *testing.T, updates <-chan Event) (text string, usage *acp.Usage) {
	t.Helper()
	for {
		select {
		case ev := <-updates:
			switch ev.Kind {
			case UpdateMessageChunk:
				// The pi-acp startup prelude (version/extensions/skills) is
				// synthetic adapter text, not the model's answer.
				if !ev.StartupInfo {
					text += ev.Text
				}
			case UpdateUserChunk:
				text += ev.Text
			case UpdateUsage:
				usage = ev.Usage
			}
		default:
			return text, usage
		}
	}
}
