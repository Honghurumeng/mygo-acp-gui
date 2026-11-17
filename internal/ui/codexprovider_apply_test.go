package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/codexprovider"
	"github.com/reed/acp-gui/internal/modelconfig"
	"github.com/reed/acp-gui/internal/session"
	"github.com/reed/acp-gui/internal/settings"
)

func envEntry(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}

func TestSaveCodexProviderRebuildsLaunchSpecAndClearsCache(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	resources := t.TempDir()
	bundleDir := filepath.Join(resources, "agents", "codex-acp")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(bundleDir, "codex-acp.bundle.mjs")
	if err := os.WriteFile(bundle, []byte("// test bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	app := New(Options{Version: "test", Workspace: workspace, Resources: resources, DataDir: t.TempDir()})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.Env = agent.Environment{
		Env:          []string{"PATH=" + filepath.Dir(node) + ":/usr/bin:/bin", "HOME=" + t.TempDir()},
		NodePath:     node,
		CodexPath:    node,
		NodeMajor:    22,
		NodeVersion:  "v22.20.0",
		CodexVersion: "0.169.0",
	}
	old := agent.LaunchSpec{
		ID:      agent.CodexID,
		Name:    "Codex",
		Command: node,
		Args:    []string{bundle},
		Env:     []string{"PATH=" + filepath.Dir(node) + ":/usr/bin:/bin"},
	}
	app.Agents = []agent.LaunchSpec{old}
	app.AgentIndex = 0
	chat := app.activeChat()
	chat.Agent = old
	app.AgentConfigs[agent.CodexID] = []acp.ConfigOption{{ID: "model", Type: "select"}}
	app.AgentModes[agent.CodexID] = AgentModeState{CurrentModeID: "agent"}
	app.AgentModelConfigs[agent.CodexID] = map[string]modelconfig.Model{
		"deepseek-chat": {ConfigOptions: []acp.ConfigOption{{ID: "model"}}},
	}

	// Keep the post-save config probe in-process; only the launch spec rebuild
	// is under test here.
	app.probeConfig = func(context.Context, agent.LaunchSpec) (session.Info, error) {
		return session.Info{}, nil
	}
	app.probeModels = nil

	app.CodexProviderDraft = codexprovider.Config{
		Enabled:          true,
		Name:             "DeepSeek",
		BaseURL:          "https://api.example.com/v1",
		APIKey:           "sk-test",
		Models:           []string{"deepseek-chat"},
		ReasoningEfforts: []string{"medium"},
		TitleModel:       "deepseek-chat",
	}
	app.CodexProviderModelsText = "deepseek-chat"
	app.CodexProviderContextWindowText = "262144"
	app.CodexProviderCompactPercentText = "75"
	app.CodexProviderEfforts = map[string]bool{"medium": true}
	app.saveCodexProvider()
	app.Flush()

	if len(app.Agents) != 1 || app.Agents[0].ID != agent.CodexID {
		t.Fatalf("agents after save = %+v", app.Agents)
	}
	spec := app.Agents[0]
	if got := envEntry(spec.Env, codexprovider.ModelProviderEnv); got != codexprovider.ProviderID {
		t.Errorf("%s = %q", codexprovider.ModelProviderEnv, got)
	}
	if got := envEntry(spec.Env, codexprovider.APIKeyEnv); got != "sk-test" {
		t.Errorf("%s = %q", codexprovider.APIKeyEnv, got)
	}
	if got := envEntry(spec.Env, codexprovider.ConfigEnv); !strings.Contains(got, `"base_url":"https://api.example.com/v1"`) {
		t.Errorf("%s = %q", codexprovider.ConfigEnv, got)
	}
	if chat.Agent.Env == nil || envEntry(chat.Agent.Env, codexprovider.APIKeyEnv) != "sk-test" {
		t.Errorf("draft chat kept the old launch spec: %+v", chat.Agent)
	}
	if len(app.AgentConfigs[agent.CodexID]) != 0 || len(app.AgentModes[agent.CodexID].Modes) != 0 {
		t.Errorf("cached config/modes were not cleared: %+v %+v", app.AgentConfigs, app.AgentModes)
	}
	if _, ok := app.AgentModelConfigs[agent.CodexID]; ok {
		t.Errorf("per-model cache survived: %+v", app.AgentModelConfigs[agent.CodexID])
	}
	if app.CodexProviderNotice == "" || strings.HasPrefix(app.CodexProviderNotice, "⚠") {
		t.Errorf("notice = %q", app.CodexProviderNotice)
	}
	if _, err := os.Stat(filepath.Join(app.DataDir, codexprovider.CatalogFileName)); err != nil {
		t.Errorf("catalog was not written: %v", err)
	}

	// Changing only the reasoning levels keeps the saved model and mode but
	// drops the old thinking value, because it belongs to the old level set.
	app.lastSelections[agent.CodexID] = settings.Selection{
		ConfigOptions: map[string]string{"model": "deepseek-chat", codexReasoningConfigID: "medium"},
		Mode:          "agent",
	}
	app.CodexProviderEfforts = map[string]bool{"high": true}
	app.saveCodexProvider()
	selection := app.lastSelections[agent.CodexID]
	if selection.ConfigOptions["model"] != "deepseek-chat" || selection.Mode != "agent" {
		t.Errorf("reasoning-only save lost model/mode: %+v", selection)
	}
	if _, ok := selection.ConfigOptions[codexReasoningConfigID]; ok {
		t.Errorf("reasoning-only save kept the old thinking value: %+v", selection)
	}
}
