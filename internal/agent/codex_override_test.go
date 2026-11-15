package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWithCodexOverrideBuildsConfigAndPreservesExisting(t *testing.T) {
	spec := LaunchSpec{
		ID:   CodexID,
		Name: "Codex",
		Env: []string{
			"PATH=/usr/bin",
			`CODEX_CONFIG={"model":"user-model","features":{"hooks":true}}`,
		},
	}
	override := &CodexOverride{
		ProviderID:      "deepseek",
		ProviderName:    "DeepSeek",
		BaseURL:         "https://api.deepseek.com",
		APIKeyEnv:       "DEEPSEEK_API_KEY",
		ModelCatalogURL: "https://api.deepseek.com/models",
		Model:           "deepseek-chat",
	}
	updated, err := WithCodexOverride(spec, override)
	if err != nil {
		t.Fatalf("WithCodexOverride: %v", err)
	}
	if updated.Env[0] != spec.Env[0] {
		t.Fatal("spec environment order changed")
	}
	if got := envValue(updated.Env, "CODEX_CONFIG"); got == "" {
		t.Fatal("CODEX_CONFIG was not set")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(envValue(updated.Env, "CODEX_CONFIG")), &cfg); err != nil {
		t.Fatalf("decode CODEX_CONFIG: %v", err)
	}
	if cfg["model"] != "deepseek-chat" || cfg["model_provider"] != "deepseek" {
		t.Errorf("config = %#v", cfg)
	}
	if got := envValue(updated.Env, "MODEL_PROVIDER"); got != "deepseek" {
		t.Errorf("MODEL_PROVIDER = %q, want deepseek", got)
	}
	if cfg["features"] == nil {
		t.Error("existing CODEX_CONFIG keys were lost")
	}
	providers, _ := cfg["model_providers"].(map[string]any)
	provider, _ := providers["deepseek"].(map[string]any)
	if provider["name"] != "DeepSeek" || provider["base_url"] != "https://api.deepseek.com/v1" ||
		provider["wire_api"] != "responses" || provider["env_key"] != "DEEPSEEK_API_KEY" ||
		provider["model_catalog_url"] != "https://api.deepseek.com/models" {
		t.Errorf("provider = %#v", provider)
	}
	if envValue(spec.Env, "CODEX_CONFIG") == envValue(updated.Env, "CODEX_CONFIG") {
		t.Error("WithCodexOverride mutated the input spec")
	}
}

func TestWithCodexOverrideUsesCodexLogin(t *testing.T) {
	spec := LaunchSpec{ID: CodexID, Env: []string{"PATH=/usr/bin"}}
	updated, err := WithCodexOverride(spec, &CodexOverride{
		ProviderID:    "local",
		BaseURL:       "http://127.0.0.1:8080/v1",
		Model:         "demo",
		UseCodexLogin: true,
		APIKeyEnv:     "IGNORED_KEY",
	})
	if err != nil {
		t.Fatalf("WithCodexOverride: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(envValue(updated.Env, "CODEX_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	providers, _ := cfg["model_providers"].(map[string]any)
	provider, _ := providers["local"].(map[string]any)
	if provider["requires_openai_auth"] != true {
		t.Errorf("requires_openai_auth = %#v", provider["requires_openai_auth"])
	}
	if _, ok := provider["env_key"]; ok {
		t.Errorf("env_key should be omitted with Codex login: %#v", provider)
	}
}

func TestWithCodexOverrideValidation(t *testing.T) {
	base := CodexOverride{
		ProviderID: "gw",
		BaseURL:    "https://example.com/v1",
		Model:      "demo",
	}
	cases := []struct {
		name   string
		mutate func(*CodexOverride)
		want   string
	}{
		{"bad id", func(o *CodexOverride) { o.ProviderID = "a.b" }, "provider id"},
		{"bad url", func(o *CodexOverride) { o.BaseURL = "ftp://example.com" }, "http or https"},
		{"no host", func(o *CodexOverride) { o.BaseURL = "https://" }, "host"},
		{"no model", func(o *CodexOverride) { o.Model = " " }, "model is empty"},
		{"bad env", func(o *CodexOverride) { o.APIKeyEnv = "1BAD" }, "environment variable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := base
			tc.mutate(&o)
			if err := o.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWithCodexOverrideNilLeavesSpec(t *testing.T) {
	spec := LaunchSpec{ID: CodexID, Env: []string{"PATH=/usr/bin"}}
	updated, err := WithCodexOverride(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if envValue(updated.Env, "CODEX_CONFIG") != "" {
		t.Error("nil override added CODEX_CONFIG")
	}
}

func TestWithCodexOverrideLiteralAPIKey(t *testing.T) {
	spec := LaunchSpec{ID: CodexID, Env: []string{"PATH=/usr/bin", "DEEPSEEK_API_KEY=old"}}
	updated, err := WithCodexOverride(spec, &CodexOverride{
		ProviderID: "deepseek",
		BaseURL:    "https://api.deepseek.com",
		APIKeyEnv:  "DEEPSEEK_API_KEY",
		APIKey:     "sk-literal-secret",
		Model:      "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("WithCodexOverride: %v", err)
	}
	if got := envValue(updated.Env, "DEEPSEEK_API_KEY"); got != "sk-literal-secret" {
		t.Errorf("DEEPSEEK_API_KEY = %q, want literal key", got)
	}
	raw := envValue(updated.Env, "CODEX_CONFIG")
	if strings.Contains(raw, "sk-literal-secret") {
		t.Fatalf("literal key leaked into CODEX_CONFIG: %s", raw)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	providers, _ := cfg["model_providers"].(map[string]any)
	provider, _ := providers["deepseek"].(map[string]any)
	if provider["env_key"] != "DEEPSEEK_API_KEY" {
		t.Errorf("env_key = %#v", provider["env_key"])
	}
}

func TestWithCodexOverrideLiteralKeyUsesDefaultEnv(t *testing.T) {
	spec := LaunchSpec{ID: CodexID, Env: []string{"PATH=/usr/bin"}}
	updated, err := WithCodexOverride(spec, &CodexOverride{
		ProviderID: "gw",
		BaseURL:    "https://example.com",
		APIKey:     "sk-literal-secret",
		Model:      "demo",
	})
	if err != nil {
		t.Fatalf("WithCodexOverride: %v", err)
	}
	if got := envValue(updated.Env, DefaultCodexAPIKeyEnv); got != "sk-literal-secret" {
		t.Errorf("%s = %q", DefaultCodexAPIKeyEnv, got)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(envValue(updated.Env, "CODEX_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	providers, _ := cfg["model_providers"].(map[string]any)
	provider, _ := providers["gw"].(map[string]any)
	if provider["env_key"] != DefaultCodexAPIKeyEnv {
		t.Errorf("env_key = %#v", provider["env_key"])
	}
	if provider["base_url"] != "https://example.com/v1" {
		t.Errorf("base_url = %#v", provider["base_url"])
	}
}

func TestWithCodexOverrideBaseURLNormalization(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://api.example.com", "https://api.example.com/v1"},
		{"https://api.example.com/", "https://api.example.com/v1"},
		{"https://api.example.com/v1", "https://api.example.com/v1"},
		{"https://api.example.com/v1/", "https://api.example.com/v1"},
		{"https://api.example.com/api", "https://api.example.com/api/v1"},
	} {
		o := CodexOverride{ProviderID: "gw", BaseURL: tc.in, Model: "demo"}
		if err := o.Validate(); err != nil {
			t.Errorf("Validate(%q): %v", tc.in, err)
			continue
		}
		raw, err := o.ConfigJSON("")
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		providers, _ := cfg["model_providers"].(map[string]any)
		provider, _ := providers["gw"].(map[string]any)
		if provider["base_url"] != tc.want {
			t.Errorf("base_url(%q) = %#v, want %q", tc.in, provider["base_url"], tc.want)
		}
	}
}
