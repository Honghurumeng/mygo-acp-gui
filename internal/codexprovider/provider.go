// Package codexprovider describes the optional custom provider the app can
// give to the vendored codex-acp adapter.
//
// The adapter's host settings reach Codex through environment variables:
// CODEX_CONFIG/MODEL_PROVIDER configure the provider, CODEX_ACP_MODELS
// declares model ids that are not in Codex's own catalog,
// CODEX_ACP_REASONING_EFFORTS declares their thinking levels, and
// CODEX_ACP_TITLE_MODEL sets the model used for session titles. The app
// applies these only to Codex processes it starts; the user's
// ~/.codex/config.toml is never read or written.
package codexprovider

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	// ProviderID is the Codex model_providers key created for this app. It is
	// deliberately distinct from the user's own provider ids.
	ProviderID = "acp-gui"

	// APIKeyEnv is the environment variable Codex reads when the provider
	// definition names it as env_key.
	APIKeyEnv = "ACP_GUI_CODEX_PROVIDER_API_KEY"

	// ConfigEnv and ModelProviderEnv are the two variables codex-acp already
	// understands for a custom provider.
	ConfigEnv        = "CODEX_CONFIG"
	ModelProviderEnv = "MODEL_PROVIDER"

	// ModelsEnv declares extra model ids to the vendored codex-acp adapter.
	ModelsEnv = "CODEX_ACP_MODELS"
	// ReasoningEffortsEnv declares the thinking levels of those model ids.
	ReasoningEffortsEnv = "CODEX_ACP_REASONING_EFFORTS"
	// TitleModelEnv overrides the model used for session title generation.
	TitleModelEnv = "CODEX_ACP_TITLE_MODEL"
)

// ReasoningLevels are the thinking levels the settings panel offers for
// custom models, in display order. Codex accepts these strings as reasoning
// efforts; a provider may ignore or reject a level it does not support, so
// the settings default to Medium only.
var ReasoningLevels = [...]string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

// Config is the user's custom Codex provider plus adapter-level model,
// reasoning and title overrides. An empty Config keeps Codex on its own
// configuration; a disabled Config still applies models/reasoningEfforts/
// titleModel without overriding the provider routing. APIKey is stored in
// settings.json (mode 0600) and is passed to the child only through APIKeyEnv;
// it never appears in a command line.
type Config struct {
	Enabled          bool     `json:"enabled,omitempty"`
	Name             string   `json:"name,omitempty"`
	BaseURL          string   `json:"baseURL,omitempty"`
	APIKey           string   `json:"apiKey,omitempty"`
	Models           []string `json:"models,omitempty"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
	TitleModel       string   `json:"titleModel,omitempty"`
	// CatalogPath is the generated Codex model catalog for these model ids.
	// It is runtime-only: settings.json never stores it.
	CatalogPath string `json:"-"`
}

// Normalize trims fields, drops empty/duplicate model ids and reasoning
// levels, and supplies the default provider name. It never disables an
// invalid entry; Validate does that so the settings loader can warn about it.
func (c Config) Normalize() Config {
	c.Name = strings.TrimSpace(c.Name)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.TitleModel = strings.TrimSpace(c.TitleModel)
	if c.Name == "" && c.Enabled {
		c.Name = "Custom Codex provider"
	}
	seen := make(map[string]bool, len(c.Models))
	models := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	c.Models = models
	seen = make(map[string]bool, len(c.ReasoningEfforts))
	efforts := make([]string, 0, len(c.ReasoningEfforts))
	for _, effort := range c.ReasoningEfforts {
		effort = strings.TrimSpace(effort)
		if effort == "" || seen[effort] {
			continue
		}
		seen[effort] = true
		efforts = append(efforts, effort)
	}
	c.ReasoningEfforts = efforts
	return c
}

// HasOverrides reports whether the config changes anything for Codex. Models,
// reasoning levels and the title model work without the custom provider, so a
// user can keep the provider from ~/.codex/config.toml and only extend its
// model list.
func (c Config) HasOverrides() bool {
	return c.Enabled || len(c.Models) > 0 || strings.TrimSpace(c.TitleModel) != ""
}

// Validate checks the adapter-level overrides and, when enabled, the provider
// routing. Models, reasoning levels and the title model are validated even
// while the custom provider is off.
func (c Config) Validate() error {
	for _, model := range c.Models {
		if strings.ContainsAny(model, " \t\r\n") {
			return fmt.Errorf("model id %q contains whitespace", model)
		}
	}
	for _, effort := range c.ReasoningEfforts {
		if strings.ContainsAny(effort, " \t\r\n") {
			return fmt.Errorf("reasoning level %q contains whitespace", effort)
		}
	}
	if strings.ContainsAny(c.TitleModel, " \t\r\n") {
		return fmt.Errorf("title model %q contains whitespace", c.TitleModel)
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("base URL is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base URL must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("base URL has no host")
	}
	if parsed.User != nil {
		return fmt.Errorf("base URL must not contain credentials")
	}
	return nil
}

// DefaultModel is the model a new Codex thread starts with. The first
// configured model wins; an empty list keeps Codex's own default.
func (c Config) DefaultModel() string {
	if len(c.Models) == 0 {
		return ""
	}
	return c.Models[0]
}

// EffortLevels are the thinking levels a custom model advertises. An empty
// setting keeps the adapter's historical single Medium level, so files saved
// before this field existed behave exactly as before.
func (c Config) EffortLevels() []string {
	if len(c.ReasoningEfforts) == 0 {
		return []string{"medium"}
	}
	return append([]string(nil), c.ReasoningEfforts...)
}

// DefaultReasoningEffort is the level a custom model starts with. Medium wins
// when it is offered; otherwise the first configured level is the default.
func (c Config) DefaultReasoningEffort() string {
	levels := c.EffortLevels()
	for _, level := range levels {
		if level == "medium" {
			return level
		}
	}
	return levels[0]
}

// Environment returns the variables that apply this config to a Codex
// process. A disabled provider still exports the model, reasoning and title
// overrides; it omits CODEX_CONFIG/MODEL_PROVIDER/APIKeyEnv so Codex keeps the
// user's own provider.
func (c Config) Environment() (map[string]string, error) {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.HasOverrides() {
		return nil, nil
	}

	env := map[string]string{}
	if c.Enabled {
		definition := map[string]any{
			"name":                 c.Name,
			"base_url":             c.BaseURL,
			"wire_api":             "responses",
			"requires_openai_auth": false,
		}
		if c.APIKey != "" {
			definition["env_key"] = APIKeyEnv
		}
		codexConfig := map[string]any{
			"model_provider": ProviderID,
			"model_providers": map[string]any{
				ProviderID: definition,
			},
		}
		if model := c.DefaultModel(); model != "" {
			codexConfig["model"] = model
		}
		if c.CatalogPath != "" {
			codexConfig["model_catalog_json"] = c.CatalogPath
		}
		data, err := json.Marshal(codexConfig)
		if err != nil {
			return nil, fmt.Errorf("encode CODEX_CONFIG: %w", err)
		}
		env[ConfigEnv] = string(data)
		env[ModelProviderEnv] = ProviderID
		if c.APIKey != "" {
			env[APIKeyEnv] = c.APIKey
		}
	}
	if len(c.Models) > 0 {
		models, err := json.Marshal(c.Models)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", ModelsEnv, err)
		}
		env[ModelsEnv] = string(models)
		efforts, err := json.Marshal(c.EffortLevels())
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", ReasoningEffortsEnv, err)
		}
		env[ReasoningEffortsEnv] = string(efforts)
	}
	if c.TitleModel != "" {
		env[TitleModelEnv] = c.TitleModel
	}
	return env, nil
}
