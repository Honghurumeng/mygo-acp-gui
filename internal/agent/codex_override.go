package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// DefaultCodexAPIKeyEnv is the environment variable used when the user
// pastes a literal key but leaves the variable name empty.
const DefaultCodexAPIKeyEnv = "OPENAI_API_KEY"

// CodexOverride is the provider/model layer ACP GUI adds to Codex sessions.
// It is serialized into CODEX_CONFIG and passed by codex-acp to the Codex
// app-server, so the user's ~/.codex/config.toml is never rewritten. Fields
// not set here still come from that file.
type CodexOverride struct {
	ProviderID   string
	ProviderName string
	BaseURL      string
	// APIKeyEnv names the environment variable Codex reads. APIKey is an
	// optional literal value: it is injected into the child environment under
	// APIKeyEnv (or DefaultCodexAPIKeyEnv) and never serialized into
	// CODEX_CONFIG. A literal key wins over an inherited variable.
	APIKeyEnv       string
	APIKey          string
	UseCodexLogin   bool
	ModelCatalogURL string
	Model           string
}

func (o CodexOverride) normalized() CodexOverride {
	o.ProviderID = strings.TrimSpace(o.ProviderID)
	o.ProviderName = strings.TrimSpace(o.ProviderName)
	o.BaseURL = normalizeCodexBaseURL(o.BaseURL)
	o.APIKeyEnv = strings.TrimSpace(o.APIKeyEnv)
	o.APIKey = strings.TrimSpace(o.APIKey)
	o.ModelCatalogURL = strings.TrimSpace(o.ModelCatalogURL)
	o.Model = strings.TrimSpace(o.Model)
	return o
}

func (o CodexOverride) apiKeyEnv() string {
	if o.APIKeyEnv != "" {
		return o.APIKeyEnv
	}
	if o.APIKey != "" {
		return DefaultCodexAPIKeyEnv
	}
	return ""
}

// Validate checks the values that would make Codex fail at startup.
func (o CodexOverride) Validate() error {
	o = o.normalized()
	if o.ProviderID == "" {
		return fmt.Errorf("codex: provider id is empty")
	}
	if !validCodexProviderID(o.ProviderID) {
		return fmt.Errorf("codex: provider id %q may only contain letters, digits, '-' and '_'", o.ProviderID)
	}
	if strings.TrimSpace(o.BaseURL) == "" {
		return fmt.Errorf("codex: base URL is empty")
	}
	if err := validateCodexURL("base URL", o.BaseURL); err != nil {
		return err
	}
	if !strings.HasSuffix(strings.ToLower(o.BaseURL), "/v1") {
		return fmt.Errorf("codex: base URL must end in /v1")
	}
	if strings.TrimSpace(o.Model) == "" {
		return fmt.Errorf("codex: model is empty")
	}
	if o.ModelCatalogURL != "" {
		if err := validateCodexURL("model catalog URL", o.ModelCatalogURL); err != nil {
			return err
		}
	}
	if o.APIKeyEnv != "" && !o.UseCodexLogin && !validEnvName(o.APIKeyEnv) {
		return fmt.Errorf("codex: API key environment variable %q is invalid", o.APIKeyEnv)
	}
	if strings.ContainsAny(o.APIKey, "\x00\r\n") {
		return fmt.Errorf("codex: API key contains a NUL or newline")
	}
	return nil
}

// ConfigJSON builds the CODEX_CONFIG object for this override. It is merged
// with any existing CODEX_CONFIG so unrelated settings survive.
func (o CodexOverride) ConfigJSON(existing string) (string, error) {
	o = o.normalized()
	root := make(map[string]any)
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &root); err != nil {
			return "", fmt.Errorf("codex: parse existing CODEX_CONFIG: %w", err)
		}
	}
	name := strings.TrimSpace(o.ProviderName)
	if name == "" {
		name = o.ProviderID
	}
	provider := map[string]any{
		"name":     name,
		"base_url": strings.TrimSpace(o.BaseURL),
		"wire_api": "responses",
	}
	if o.UseCodexLogin {
		provider["requires_openai_auth"] = true
	} else if envKey := o.apiKeyEnv(); envKey != "" {
		provider["env_key"] = envKey
	}
	if o.ModelCatalogURL != "" {
		provider["model_catalog_url"] = o.ModelCatalogURL
	}
	providers, _ := root["model_providers"].(map[string]any)
	if providers == nil {
		providers = make(map[string]any)
	}
	providers[o.ProviderID] = provider
	root["model_providers"] = providers
	root["model_provider"] = o.ProviderID
	root["model"] = strings.TrimSpace(o.Model)
	data, err := json.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("codex: encode CODEX_CONFIG: %w", err)
	}
	return string(data), nil
}

// WithCodexOverride returns spec with CODEX_CONFIG applied. A nil override
// returns spec unchanged; callers pass the base discovery spec so a previous
// override is not appended twice.
func WithCodexOverride(spec LaunchSpec, override *CodexOverride) (LaunchSpec, error) {
	if override == nil {
		return spec, nil
	}
	o := override.normalized()
	if err := o.Validate(); err != nil {
		return spec, err
	}
	config, err := o.ConfigJSON(envValue(spec.Env, "CODEX_CONFIG"))
	if err != nil {
		return spec, err
	}
	spec.Env = append([]string(nil), spec.Env...)
	spec.Env = setEnv(spec.Env, "CODEX_CONFIG", config)
	// codex-acp uses MODEL_PROVIDER for its own session config and resume
	// path; keep it aligned with the provider it starts app-server with.
	spec.Env = setEnv(spec.Env, "MODEL_PROVIDER", o.ProviderID)
	if !o.UseCodexLogin && o.APIKey != "" {
		spec.Env = setEnv(spec.Env, o.apiKeyEnv(), o.APIKey)
	}
	return spec, nil
}

// EnvValue returns the last value of key in an environment slice. It is
// exported for the settings UI, which checks whether an API-key variable is
// present in the login-shell environment without reading the secret.
func EnvValue(env []string, key string) string {
	return envValue(env, key)
}

func normalizeCodexBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	// A query or fragment makes appending unsafe; leave it for validation to
	// reject instead of producing https://host?x=1/v1.
	if strings.ContainsAny(raw, "?#") {
		return raw
	}
	if strings.HasSuffix(strings.ToLower(raw), "/v1") {
		return raw
	}
	return raw + "/v1"
}

func validCodexProviderID(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return id != ""
}

func validEnvName(name string) bool {
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return name != ""
}

func validateCodexURL(label, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("codex: %s: %w", label, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("codex: %s must use http or https", label)
	}
	if parsed.Host == "" {
		return fmt.Errorf("codex: %s has no host", label)
	}
	return nil
}
