package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/reed/acp-gui/internal/codexprovider"
)

func TestCodexProviderWritesModelCatalog(t *testing.T) {
	dataDir := t.TempDir()
	app, _ := newSettingsApp(t, dataDir, t.TempDir())
	app.CodexProviderDraft = codexprovider.Config{
		Enabled: true,
		Name:    "Deepseek",
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test",
	}
	app.CodexProviderModelsText = "deepseek/deepseek-v4.1-flash"
	app.CodexProviderEfforts = map[string]bool{"low": true, "high": true, "max": true}
	app.saveCodexProvider()

	path := filepath.Join(dataDir, codexprovider.CatalogFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var document struct {
		Models []struct {
			Slug             string `json:"slug"`
			ContextWindow    int    `json:"context_window"`
			BaseInstructions string `json:"base_instructions"`
			ModelMessages    struct {
				InstructionsTemplate string `json:"instructions_template"`
			} `json:"model_messages"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("catalog JSON: %v\n%s", err, data)
	}
	if len(document.Models) != 1 || document.Models[0].Slug != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("catalog models = %+v", document.Models)
	}
	if document.Models[0].ContextWindow != 1048576 {
		t.Errorf("context window = %d", document.Models[0].ContextWindow)
	}
	if document.Models[0].BaseInstructions == "" || document.Models[0].ModelMessages.InstructionsTemplate == "" {
		t.Error("catalog model is missing instructions; Codex will refuse the catalog")
	}
	if provider := app.codexProvider(); provider == nil || provider.CatalogPath != path {
		t.Fatalf("launch provider = %+v, want catalog %s", provider, path)
	}
	file := loadPersistedSettings(t, dataDir)
	if len(file.CodexProvider.Models) != 1 || file.CodexProvider.Models[0] != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("persisted provider = %+v", file.CodexProvider)
	}

	// Startup writes the catalog from settings.json before discovery.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	app.loadSettings(dataDir, false)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("startup catalog: %v", err)
	}

	app.CodexProviderDraft.Enabled = false
	app.saveCodexProvider()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("catalog still exists after disabling the provider: %v", err)
	}
}
