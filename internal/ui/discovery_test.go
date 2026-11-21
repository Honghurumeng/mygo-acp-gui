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
	"github.com/reed/acp-gui/internal/agentupdate"
	"github.com/reed/acp-gui/internal/updatecheck"
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
		// Discovery tests use fake paths and fake versions; never let the
		// background update check reach the real npm registry.
		CheckUpdates: func(context.Context, []updatecheck.Target) map[string]updatecheck.Result {
			return nil
		},
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

func TestDiscoveryReportsNewerAgentVersions(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t)
	app := newDiscoveryApp(t, resources, resolve, func(context.Context, agent.LaunchSpec) error {
		return nil
	})
	var targets []updatecheck.Target
	app.checkUpdates = func(_ context.Context, got []updatecheck.Target) map[string]updatecheck.Result {
		targets = append([]updatecheck.Target(nil), got...)
		return map[string]updatecheck.Result{
			agent.PiID:          {Current: "1.5.2", Latest: "1.6.0"},
			agent.CommandCodeID: {Current: "1.88.5", Latest: "1.88.5"},
			agent.CodexID:       {Current: "0.169.0", Latest: "0.170.0"},
		}
	}

	app.discover(context.Background())
	app.Flush()

	wantPackages := map[string]string{
		agent.PiID:          "@earendil-works/pi-coding-agent",
		agent.CommandCodeID: "command-code",
		agent.CodexID:       "@openai/codex",
	}
	if len(targets) != len(wantPackages) {
		t.Fatalf("update targets = %+v", targets)
	}
	for _, target := range targets {
		if got := wantPackages[target.ID]; got == "" || target.Package != got {
			t.Errorf("target %s package = %q, want %q", target.ID, target.Package, got)
		}
	}

	byID := map[string]AgentStatus{}
	for _, status := range app.AgentStatuses {
		byID[status.ID] = status
	}
	if got := byID[agent.PiID].LatestVersion; got != "1.6.0" {
		t.Errorf("pi latest = %q, want 1.6.0", got)
	}
	if got := byID[agent.CommandCodeID].LatestVersion; got != "" {
		t.Errorf("command-code latest = %q, want no notice", got)
	}
	if got := byID[agent.CodexID].LatestVersion; got != "0.170.0" {
		t.Errorf("codex latest = %q, want 0.170.0", got)
	}

	tt := mui.NewTester(app.View, 1200, 760)
	tt.Frame()
	if !tt.HasText("Update available: v1.6.0") {
		t.Errorf("update notice missing; texts: %q", tt.Texts())
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

func TestDiscoveryUpdateButtonUpdatesThenRefreshesOneAgent(t *testing.T) {
	resources, resolve, bin := fakeDiscoveryEnvironment(t)
	app := newDiscoveryApp(t, resources, resolve, func(context.Context, agent.LaunchSpec) error {
		return nil
	})
	var checks atomic.Int32
	app.checkUpdates = func(_ context.Context, targets []updatecheck.Target) map[string]updatecheck.Result {
		if checks.Add(1) > 1 {
			// The fake CLI keeps reporting 1.5.2 after the fake update, so
			// stop answering with a newer version to keep the notice gone.
			return nil
		}
		found := map[string]updatecheck.Result{}
		for _, target := range targets {
			if target.ID == agent.PiID {
				found[target.ID] = updatecheck.Result{Current: target.Current, Latest: "1.6.0"}
			}
		}
		return found
	}
	var (
		mu  sync.Mutex
		got agentupdate.Target
	)
	app.runAgentUpdate = func(_ context.Context, _ []string, target agentupdate.Target) (agentupdate.Result, error) {
		mu.Lock()
		got = target
		mu.Unlock()
		return agentupdate.Result{Plan: agentupdate.Plan{Executable: bin + "/pi", Args: []string{"update"}}}, nil
	}

	app.discover(context.Background())
	app.Flush()

	tt := mui.NewTester(app.View, 1200, 760)
	tt.Frame()
	if !tt.HasText("Update available: v1.6.0") {
		t.Fatalf("update notice missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Update"); err != nil {
		t.Fatalf("click Update: %v", err)
	}
	waitFor(t, app, tt, func() bool { return !app.anyAgentUpdating() && !app.Resolving })

	mu.Lock()
	target := got
	mu.Unlock()
	if target.ID != agent.PiID || target.Name != "pi" {
		t.Fatalf("update target = %+v", target)
	}
	if want := filepath.Join(bin, "pi"); target.Command != want {
		t.Errorf("update command = %q, want %q", target.Command, want)
	}
	idx := app.statusIndex(agent.PiID)
	if idx < 0 {
		t.Fatal("pi status disappeared")
	}
	status := app.AgentStatuses[idx]
	if status.Updating {
		t.Error("status still updating")
	}
	if status.UpdateDone != "1.5.2" || status.LatestVersion != "" {
		t.Errorf("status after update = %+v, want done 1.5.2 and no notice", status)
	}
	tt.Frame()
	if !tt.HasText("Updated to v1.5.2") {
		t.Errorf("updated line missing; texts: %q", tt.Texts())
	}
}

func TestDiscoveryUpdateFailureKeepsTheNoticeForRetry(t *testing.T) {
	resources, resolve, _ := fakeDiscoveryEnvironment(t)
	app := newDiscoveryApp(t, resources, resolve, func(context.Context, agent.LaunchSpec) error {
		return nil
	})
	app.checkUpdates = func(_ context.Context, targets []updatecheck.Target) map[string]updatecheck.Result {
		for _, target := range targets {
			if target.ID == agent.PiID {
				return map[string]updatecheck.Result{
					target.ID: {Current: target.Current, Latest: "1.6.0"},
				}
			}
		}
		return nil
	}
	app.runAgentUpdate = func(context.Context, []string, agentupdate.Target) (agentupdate.Result, error) {
		return agentupdate.Result{Plan: agentupdate.Plan{
			Executable: "/usr/local/bin/pi",
			Args:       []string{"update"},
		}}, errors.New("exit status 243")
	}

	app.discover(context.Background())
	app.Flush()
	tt := mui.NewTester(app.View, 1200, 760)
	tt.Frame()
	if err := tt.Click("Update"); err != nil {
		t.Fatalf("click Update: %v", err)
	}
	waitFor(t, app, tt, func() bool { return !app.anyAgentUpdating() })

	idx := app.statusIndex(agent.PiID)
	if idx < 0 {
		t.Fatal("pi status disappeared")
	}
	status := app.AgentStatuses[idx]
	if !strings.Contains(status.UpdateError, "exit status 243") {
		t.Errorf("update error = %q", status.UpdateError)
	}
	if status.LatestVersion != "1.6.0" {
		t.Errorf("latest = %q, want the notice kept for a retry", status.LatestVersion)
	}
	tt.Frame()
	if !tt.HasText("Update failed:") || !tt.HasText("Update available: v1.6.0") {
		t.Errorf("failure text missing; texts: %q", tt.Texts())
	}
}
