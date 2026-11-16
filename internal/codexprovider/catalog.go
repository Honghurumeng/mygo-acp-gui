package codexprovider

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CatalogFileName is the generated Codex model catalog inside the app data
// directory. CODEX_CONFIG points Codex at its absolute path.
const CatalogFileName = "codex-models.json"

// catalogTemplate is the model metadata published by DeepSeek's setup script
// (codex-deepseek-setup.sh). It contains Flash and Pro templates. Slugs,
// reasoning levels and instructions are replaced per configured model when the
// catalog is generated.
//
//go:embed catalog_deepseek.json
var catalogTemplate []byte

// catalogInstructions is the literal Codex system prompt from the same setup
// script. Codex rejects catalog models that omit both base_instructions and
// model_messages.instructions_template, so every generated entry carries it.
// base_instructions is the legacy field for older Codex builds; current builds
// read model_messages.instructions_template.
//
//go:embed catalog_instructions.txt
var catalogInstructions string

// Catalog returns the Codex model catalog for the custom models, or nil when
// the provider is off, has no models, or does not look like DeepSeek. Each
// configured id becomes one entry: ids containing "pro" use the Pro metadata,
// everything else uses Flash.
//
// Every entry forces the V1 multi-agent backend. Codex Multi-Agent V2 delivers
// spawned tasks as an OpenAI-only agent_message input item; third-party
// Responses gateways reject that item with unsupported_input_item, so a V2
// child fails before its first model call. V1 sends the same task as an
// ordinary user message and works through those gateways. See
// https://github.com/openai/codex/issues/33551.
func (c Config) Catalog() ([]byte, error) {
	c = c.Normalize()
	if !c.Enabled || len(c.Models) == 0 || !c.deepSeekCatalog() {
		return nil, nil
	}
	var template struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(catalogTemplate, &template); err != nil {
		return nil, fmt.Errorf("codexprovider: catalog template: %w", err)
	}
	if len(template.Models) < 2 {
		return nil, fmt.Errorf("codexprovider: catalog template has %d models, want 2", len(template.Models))
	}
	flash, pro := template.Models[0], template.Models[1]
	levels := catalogReasoningLevels(c.EffortLevels())
	defaultLevel := c.DefaultReasoningEffort()
	contextWindow := c.EffectiveContextWindow()
	autoCompactLimit := c.AutoCompactTokenLimit()
	models := make([]map[string]any, 0, len(c.Models))
	for _, id := range c.Models {
		source := flash
		if strings.Contains(strings.ToLower(id), "pro") {
			source = pro
		}
		entry := make(map[string]any, len(source))
		for key, value := range source {
			entry[key] = value
		}
		entry["slug"] = id
		entry["display_name"] = id
		entry["context_window"] = contextWindow
		entry["max_context_window"] = contextWindow
		entry["auto_compact_token_limit"] = autoCompactLimit
		entry["default_reasoning_level"] = defaultLevel
		entry["supported_reasoning_levels"] = levels
		// The setup-script template says v2, which breaks subagents on
		// non-OpenAI Responses providers. The catalog is only generated for
		// custom providers, so V1 is the portable choice.
		entry["multi_agent_version"] = "v1"
		entry["base_instructions"] = catalogInstructions
		entry["model_messages"] = map[string]any{
			"instructions_template": catalogInstructions,
		}
		models = append(models, entry)
	}
	document := struct {
		Models []map[string]any `json:"models"`
	}{Models: models}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codexprovider: encode catalog: %w", err)
	}
	return append(data, '\n'), nil
}

// SaveCatalog writes Catalog to path atomically. A disabled provider or an
// empty model list removes the catalog so Codex never reads stale metadata.
func (c Config) SaveCatalog(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := c.Catalog()
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("codexprovider: remove catalog: %w", err)
		}
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("codexprovider: create catalog directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".codex-models-*.json")
	if err != nil {
		return fmt.Errorf("codexprovider: create catalog: %w", err)
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("codexprovider: write catalog: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		remove()
		return fmt.Errorf("codexprovider: chmod catalog: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("codexprovider: sync catalog: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("codexprovider: close catalog: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("codexprovider: replace catalog: %w", err)
	}
	return nil
}

func (c Config) deepSeekCatalog() bool {
	if strings.Contains(strings.ToLower(c.Name), "deepseek") || strings.Contains(strings.ToLower(c.BaseURL), "deepseek") {
		return true
	}
	for _, id := range c.Models {
		if strings.Contains(strings.ToLower(id), "deepseek") {
			return true
		}
	}
	return false
}

func catalogReasoningLevels(levels []string) []map[string]string {
	out := make([]map[string]string, 0, len(levels))
	for _, level := range levels {
		out = append(out, map[string]string{
			"effort":      level,
			"description": catalogReasoningDescription(level),
		})
	}
	return out
}

func catalogReasoningDescription(level string) string {
	switch level {
	case "none":
		return "No reasoning"
	case "minimal":
		return "Minimal reasoning"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balances speed and reasoning depth for everyday tasks"
	case "high", "xhigh":
		return "Extra high reasoning depth for complex problems"
	case "max":
		return "Maximum reasoning depth for the hardest problems"
	case "ultra":
		return "Maximum reasoning with automatic task delegation"
	default:
		return level
	}
}
