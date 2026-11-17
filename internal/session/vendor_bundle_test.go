package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reed/acp-gui/internal/agent"
)

// TestVendoredPiBundleInitializes runs the checked-in pi-acp bundle through
// initialize only. It does not spawn pi or touch credentials, but catches a
// missing/corrupt bundle and a broken launch spec in the normal test suite.
func TestVendoredPiBundleInitializes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("vendored bundle test uses the Unix node path")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	if env.NodePath == "" || env.PiPath == "" {
		t.Skip("node or pi is not installed")
	}
	resources := filepath.Join(repoRoot(t), "resources")
	spec, err := agent.PiLaunchSpec(resources, env)
	if err != nil {
		t.Fatalf("pi launch spec: %v", err)
	}
	s, err := Start(ctx, Config{Launch: spec, Workspace: t.TempDir(), InitializeOnly: true})
	if err != nil {
		t.Fatalf("start vendored pi-acp: %v", err)
	}
	defer closeSession(t, s)
	info := s.Info()
	if info.AgentInfo == nil || !strings.Contains(strings.ToLower(info.AgentInfo.Name), "pi") {
		t.Fatalf("pi-acp initialize info = %+v", info.AgentInfo)
	}
	if info.SessionID != "" {
		t.Errorf("initialize-only bundle opened a session: %q", info.SessionID)
	}
}

// TestVendoredCodexBundleInitializes does the same for codex-acp. It only
// needs the Codex CLI to be installed; no login or model call is involved.
func TestVendoredCodexBundleInitializes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("vendored bundle test uses the Unix node path")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	env := agent.ResolveEnvironment(ctx, agent.ResolveOptions{})
	if env.NodePath == "" || env.CodexPath == "" {
		t.Skip("node or codex is not installed")
	}
	resources := filepath.Join(repoRoot(t), "resources")
	spec, err := agent.CodexLaunchSpec(resources, env, nil)
	if err != nil {
		t.Fatalf("codex launch spec: %v", err)
	}
	s, err := Start(ctx, Config{Launch: spec, Workspace: t.TempDir(), InitializeOnly: true})
	if err != nil {
		t.Fatalf("start vendored codex-acp: %v", err)
	}
	defer closeSession(t, s)
	info := s.Info()
	if info.AgentInfo == nil || !strings.Contains(strings.ToLower(info.AgentInfo.Name), "codex") {
		t.Fatalf("codex-acp initialize info = %+v", info.AgentInfo)
	}
}

func TestVendoredBundlesAreCommitted(t *testing.T) {
	for _, name := range []string{"pi-acp", "codex-acp"} {
		path := filepath.Join(repoRoot(t), "resources", "agents", name, name+".bundle.mjs")
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			t.Errorf("vendored bundle %s is missing or empty: %v", path, err)
		}
	}
}
