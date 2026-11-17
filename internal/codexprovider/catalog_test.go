package codexprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCatalogRewritesModelNamesAndReasoningLevels(t *testing.T) {
	config := Config{
		Enabled:            true,
		BaseURL:            "https://api.example.com/v1",
		Models:             []string{"deepseek/deepseek-v4.1-flash", "deepseek-v4-pro"},
		ContextWindow:      262_144,
		AutoCompactPercent: 75,
		ReasoningEfforts:   []string{"low", "max"},
	}
	data, err := config.Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	var document struct {
		Models []struct {
			Slug               string   `json:"slug"`
			DisplayName        string   `json:"display_name"`
			ContextWindow      int      `json:"context_window"`
			MaxContextWindow   int      `json:"max_context_window"`
			AutoCompactLimit   int      `json:"auto_compact_token_limit"`
			DefaultLevel       string   `json:"default_reasoning_level"`
			MultiAgentVersion  string   `json:"multi_agent_version"`
			SupportsSearchTool bool     `json:"supports_search_tool"`
			InputModalities    []string `json:"input_modalities"`
			BaseInstructions   string   `json:"base_instructions"`
			ModelMessages      struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
			SupportedLevels []struct {
				Effort      string `json:"effort"`
				Description string `json:"description"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("catalog JSON: %v\n%s", err, data)
	}
	if len(document.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(document.Models))
	}
	flash, pro := document.Models[0], document.Models[1]
	if flash.Slug != "deepseek/deepseek-v4.1-flash" || flash.DisplayName != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("flash slug/display = %q/%q", flash.Slug, flash.DisplayName)
	}
	if pro.Slug != "deepseek-v4-pro" {
		t.Errorf("pro slug = %q", pro.Slug)
	}
	if flash.ContextWindow != 262144 || pro.ContextWindow != 262144 ||
		flash.MaxContextWindow != 262144 || pro.MaxContextWindow != 262144 {
		t.Errorf("context windows = %d/%d max %d/%d", flash.ContextWindow, pro.ContextWindow, flash.MaxContextWindow, pro.MaxContextWindow)
	}
	if flash.AutoCompactLimit != 196608 || pro.AutoCompactLimit != 196608 {
		t.Errorf("auto compact limits = %d/%d, want 196608", flash.AutoCompactLimit, pro.AutoCompactLimit)
	}
	if flash.DefaultLevel != "low" {
		t.Errorf("default level = %q, want low", flash.DefaultLevel)
	}
	for _, model := range []struct {
		name    string
		version string
	}{{"flash", flash.MultiAgentVersion}, {"pro", pro.MultiAgentVersion}} {
		if model.version != "v1" {
			t.Errorf("%s multi_agent_version = %q, want v1", model.name, model.version)
		}
	}
	if flash.SupportsSearchTool || pro.SupportsSearchTool {
		t.Errorf("supports_search_tool = %t/%t, want false so multi_agent_v1 is direct", flash.SupportsSearchTool, pro.SupportsSearchTool)
	}
	gotEfforts := make([]string, 0, len(flash.SupportedLevels))
	for _, level := range flash.SupportedLevels {
		gotEfforts = append(gotEfforts, level.Effort)
	}
	if !reflect.DeepEqual(gotEfforts, []string{"low", "max"}) {
		t.Errorf("efforts = %v", gotEfforts)
	}
	if !reflect.DeepEqual(flash.InputModalities, []string{"text", "image"}) {
		t.Errorf("flash modalities = %v", flash.InputModalities)
	}
	if !reflect.DeepEqual(pro.InputModalities, []string{"text"}) {
		t.Errorf("pro modalities = %v", pro.InputModalities)
	}
	if flash.BaseInstructions == "" || flash.ModelMessages.InstructionsTemplate == "" {
		t.Error("catalog model is missing instructions; Codex will refuse the catalog")
	}
	if flash.BaseInstructions != flash.ModelMessages.InstructionsTemplate {
		t.Error("legacy base_instructions differs from model_messages.instructions_template")
	}
	if !strings.Contains(flash.ModelMessages.InstructionsTemplate, "You are Codex") {
		t.Error("catalog instructions do not look like the Codex prompt")
	}
	for _, want := range []string{
		"Codex Multi-Agent V1",
		"is already available",
		"Call `multi_agent_v1.<tool>` directly",
		"do not run a shell command",
		"`multi_agent_v1.spawn_agent`",
		"`multi_agent_v1.wait_agent`",
		"never invoke its tools through",
		"Do not run `codex exec`",
	} {
		if !strings.Contains(flash.ModelMessages.InstructionsTemplate, want) {
			t.Errorf("catalog instructions missing %q", want)
		}
		if !strings.Contains(pro.ModelMessages.InstructionsTemplate, want) {
			t.Errorf("pro catalog instructions missing %q", want)
		}
	}
}

func TestCatalogIsEmptyWithoutAnEnabledProvider(t *testing.T) {
	for _, config := range []Config{
		{Models: []string{"deepseek-chat"}},
		{Enabled: true, BaseURL: "https://api.example.com/v1"},
		{Enabled: true, Name: "Other", BaseURL: "https://api.example.com/v1", Models: []string{"qwen-coder"}},
	} {
		data, err := config.Catalog()
		if err != nil {
			t.Fatalf("Catalog: %v", err)
		}
		if len(data) != 0 {
			t.Errorf("Catalog = %s, want empty", data)
		}
	}
}

func TestSaveCatalogWritesAndRemovesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), CatalogFileName)
	config := Config{
		Enabled: true,
		BaseURL: "https://api.example.com/v1",
		Models:  []string{"deepseek-chat"},
	}
	if err := config.SaveCatalog(path); err != nil {
		t.Fatalf("SaveCatalog: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("catalog file: %v", err)
	}
	if err := (Config{}).SaveCatalog(path); err != nil {
		t.Fatalf("SaveCatalog disabled: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled catalog still exists: %v", err)
	}
}

func TestEnvironmentAddsTheCatalogPath(t *testing.T) {
	env, err := (Config{
		Enabled:     true,
		BaseURL:     "https://api.example.com/v1",
		Models:      []string{"deepseek-chat"},
		CatalogPath: "/tmp/codex-models.json",
	}).Environment()
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	var codexConfig struct {
		Catalog string `json:"model_catalog_json"`
	}
	if err := json.Unmarshal([]byte(env[ConfigEnv]), &codexConfig); err != nil {
		t.Fatal(err)
	}
	if codexConfig.Catalog != "/tmp/codex-models.json" {
		t.Errorf("model_catalog_json = %q", codexConfig.Catalog)
	}
}

func TestSaveCatalogIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, CatalogFileName)
	config := Config{
		Enabled: true,
		BaseURL: "https://api.example.com/v1",
		Models:  []string{"deepseek-chat"},
	}
	if err := config.SaveCatalog(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("catalog mode = %o, want 600", perm)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".codex-models-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Errorf("temporary catalog files left behind: %v", leftovers)
	}
}
