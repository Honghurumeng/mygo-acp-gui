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
			Slug              string   `json:"slug"`
			DisplayName       string   `json:"display_name"`
			ContextWindow     int      `json:"context_window"`
			MaxContextWindow  int      `json:"max_context_window"`
			AutoCompactLimit  int      `json:"auto_compact_token_limit"`
			DefaultLevel      string   `json:"default_reasoning_level"`
			MultiAgentVersion string   `json:"multi_agent_version"`
			InputModalities   []string `json:"input_modalities"`
			BaseInstructions  string   `json:"base_instructions"`
			ModelMessages     struct {
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
