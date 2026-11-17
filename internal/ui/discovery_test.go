package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
)

// fakeDiscoveryEnvironment writes version-only tools and vendored bundle
// placeholders. Discovery tests replace the probe, so the programs never run;
// they only have to exist and answer --version for environment resolution.
func fakeDiscoveryEnvironment(t *testing.T, missing ...string) (string, agent.ResolveOptions, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executables use a shebang")
	}
	bin := t.TempDir()
	versions := map[string]string{
		"node":         "v22.20.0",
		"pi":           "1.5.2",
		"command-code": "1.88.5",
		"codex":        "codex-cli 0.169.0",
	}
	for name, version := range versions {
		path := filepath.Join(bin, name)
		script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + version + "'; fi\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range missing {
		if err := os.Remove(filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}

	shell := filepath.Join(t.TempDir(), "fake-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexec /bin/sh -c \"$2\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resources := t.TempDir()
	for _, name := range []string{"pi-acp", "codex-acp"} {
		dir := filepath.Join(resources, "agents", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".bundle.mjs"), []byte("// test bundle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	resolve := agent.ResolveOptions{
		Shell:   shell,
		Env:     []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()},
		Timeout: 2 * time.Second,
	}
	return resources, resolve, bin
}

func newDiscoveryApp(t *testing.T, resources string, resolve agent.ResolveOptions, probe func(context.Context, agent.LaunchSpec) error) *App {
	t.Helper()
	app := New(Options{
		Version:    "test",
		Workspace:  t.TempDir(),
		Resources:  resources,
		Resolve:    resolve,
		ProbeAgent: probe,
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	// The config probe and saved-session import are covered elsewhere; keep
	// these tests focused on the discovery status machine.
	app.probeConfig = nil
	app.probeModels = nil
	app.savedSynced = map[string]bool{"pi": true, "command-code": true, "codex": true}
	return app
}

func TestDiscoveryPublishesStatusesAndKeepsAvailableAgents(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t, "pi")
	var (
		mu     sync.Mutex
		probed []string
	)
	app := newDiscoveryApp(t, resources, resolve, func(ctx context.Context, spec agent.LaunchSpec) error {
		started := time.Now()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Errorf("%s probe has no deadline", spec.ID)
		} else if remaining := deadline.Sub(started); remaining < 4*time.Second || remaining > 5100*time.Millisecond {
			t.Errorf("%s probe deadline = %s, want about 5s", spec.ID, remaining)
		}
		mu.Lock()
		probed = append(probed, spec.ID)
		mu.Unlock()
		return nil
	})

	app.discover(context.Background())
	app.Flush()

	if app.Resolving {
		t.Error("discovery left the app resolving")
	}
	if app.ResolveError != "" {
		t.Errorf("resolve error = %q", app.ResolveError)
	}
	if len(app.AgentStatuses) != 3 {
		t.Fatalf("statuses = %+v", app.AgentStatuses)
	}
	want := map[string]string{
		agent.PiID:          AgentMissing,
		agent.CommandCodeID: AgentReady,
		agent.CodexID:       AgentReady,
	}
	for _, status := range app.AgentStatuses {
		if got := want[status.ID]; status.Status != got {
			t.Errorf("%s status = %q, want %q (%s)", status.ID, status.Status, got, status.Detail)
		}
	}
	if len(app.Agents) != 2 || app.Agents[0].ID != agent.CommandCodeID || app.Agents[1].ID != agent.CodexID {
		t.Fatalf("available agents = %+v", app.Agents)
	}
	seen := map[string]bool{}
	for _, id := range probed {
		seen[id] = true
	}
	if len(probed) != 2 || !seen[agent.CommandCodeID] || !seen[agent.CodexID] {
		t.Errorf("probed = %v", probed)
	}
	// A draft created before discovery finished must pick up a ready agent.
	if chat := app.activeChat(); chat == nil || chat.Agent.ID != agent.CommandCodeID {
		t.Fatalf("draft after discovery = %+v", chat)
	}

	tt := mui.NewTester(app.View, 1200, 760)
	tt.Frame()
	for _, text := range []string{"not-installed", "ready"} {
		if !tt.HasText(text) {
			t.Errorf("sidebar status %q missing; texts: %q", text, tt.Texts())
		}
	}
}

func TestDiscoveryClassifiesNonACPAgentAndProbeTimeout(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t)
	var calls atomic.Int32
	app := newDiscoveryApp(t, resources, resolve, func(_ context.Context, spec agent.LaunchSpec) error {
		calls.Add(1)
		switch spec.ID {
		case agent.PiID:
			return nil
		case agent.CommandCodeID:
			return errors.New("stdout is not clean ACP JSON")
		case agent.CodexID:
			return context.DeadlineExceeded
		default:
			return nil
		}
	})

	app.discover(context.Background())
	app.Flush()

	if got := calls.Load(); got != 3 {
		t.Fatalf("probe calls = %d, want one per builtin; statuses = %+v", got, app.AgentStatuses)
	}
	if len(app.AgentStatuses) != 3 {
		t.Fatalf("statuses = %+v", app.AgentStatuses)
	}
	byID := map[string]AgentStatus{}
	for _, status := range app.AgentStatuses {
		byID[status.ID] = status
	}
	if got := byID[agent.PiID].Status; got != AgentReady {
		t.Errorf("pi status = %q", got)
	}
	cc := byID[agent.CommandCodeID]
	if cc.Status != AgentNotACP || !strings.Contains(cc.Detail, "not clean ACP") {
		t.Errorf("command-code = %+v", cc)
	}
	codex := byID[agent.CodexID]
	if codex.Status != AgentError || codex.Detail != "initialize probe timed out after 5s" {
		t.Errorf("codex = %+v", codex)
	}
	if len(app.Agents) != 1 || app.Agents[0].ID != agent.PiID {
		t.Fatalf("available agents = %+v", app.Agents)
	}
	if app.ResolveError != "" {
		t.Errorf("resolve error = %q, want empty while pi is ready", app.ResolveError)
	}
}

func TestDiscoveryReportsWhenNoAgentIsAvailable(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t, "node", "pi", "command-code", "codex")
	app := newDiscoveryApp(t, resources, resolve, func(context.Context, agent.LaunchSpec) error {
		t.Fatal("probe ran without a resolved launch spec")
		return nil
	})

	app.discover(context.Background())
	app.Flush()

	if app.Resolving {
		t.Error("discovery left the app resolving")
	}
	if len(app.Agents) != 0 {
		t.Fatalf("available agents = %+v", app.Agents)
	}
	for _, name := range []string{"pi", "Command Code", "Codex"} {
		if !strings.Contains(app.ResolveError, name+": "+AgentMissing) {
			t.Errorf("resolve error missing %s: %q", name, app.ResolveError)
		}
	}

	tt := mui.NewTester(app.View, 1200, 760)
	tt.Frame()
	if !tt.HasText("No ACP agent available") {
		t.Errorf("empty state missing; texts: %q", tt.Texts())
	}
}

func TestDiscoveryRestoresPreferredAgentAndRefreshes(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t)
	var probes atomic.Int32
	app := newDiscoveryApp(t, resources, resolve, func(context.Context, agent.LaunchSpec) error {
		probes.Add(1)
		return nil
	})
	app.preferredAgentID = agent.CodexID

	app.discover(context.Background())
	app.Flush()
	if app.AgentIndex != 2 || app.Agents[app.AgentIndex].ID != agent.CodexID {
		t.Fatalf("selection = %d %+v", app.AgentIndex, app.Agents)
	}
	if chat := app.activeChat(); chat == nil || chat.Agent.ID != agent.CodexID {
		t.Fatalf("draft agent = %+v", chat)
	}

	tt := mui.NewTester(app.View, 1200, 760)
	app.retryDiscovery()
	waitFor(t, app, tt, func() bool {
		return !app.Resolving && len(app.AgentStatuses) == 3
	})
	if got := probes.Load(); got < 6 {
		t.Errorf("retry probed %d agents, want a second full sweep", got)
	}
	if app.AgentIndex != 2 || app.Agents[app.AgentIndex].ID != agent.CodexID {
		t.Errorf("selection lost after refresh: %d %+v", app.AgentIndex, app.Agents)
	}
}
