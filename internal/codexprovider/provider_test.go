package codexprovider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDisabledProviderHasNoEnvironment(t *testing.T) {
	env, err := (Config{Enabled: false, BaseURL: "not a URL"}).Environment()
	if err != nil {
		t.Fatalf("disabled provider: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("disabled provider returned env: %+v", env)
	}
}

func TestEnvironmentBuildsCodexProvider(t *testing.T) {
	config := Config{
		Enabled:          true,
		Name:             "  DeepSeek  ",
		BaseURL:          "https://api.example.com/v1/",
		APIKey:           "sk-test",
		Models:           []string{" deepseek-chat ", "deepseek-chat", "", "deepseek-reasoner"},
		ReasoningEfforts: []string{"low", " medium ", "low", "high"},
		TitleModel:       " deepseek-chat ",
	}
	env, err := config.Environment()
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env[ModelProviderEnv] != ProviderID {
		t.Errorf("%s = %q, want %q", ModelProviderEnv, env[ModelProviderEnv], ProviderID)
	}
	if env[APIKeyEnv] != "sk-test" {
		t.Errorf("%s = %q", APIKeyEnv, env[APIKeyEnv])
	}
	if env[ModelsEnv] != `["deepseek-chat","deepseek-reasoner"]` {
		t.Errorf("%s = %q", ModelsEnv, env[ModelsEnv])
	}
	if env[ReasoningEffortsEnv] != `["low","medium","high"]` {
		t.Errorf("%s = %q", ReasoningEffortsEnv, env[ReasoningEffortsEnv])
	}
	if env[TitleModelEnv] != "deepseek-chat" {
		t.Errorf("%s = %q", TitleModelEnv, env[TitleModelEnv])
	}

	var codexConfig struct {
		ModelProvider  string `json:"model_provider"`
		Model          string `json:"model"`
		ModelProviders map[string]struct {
			Name           string            `json:"name"`
			BaseURL        string            `json:"base_url"`
			WireAPI        string            `json:"wire_api"`
			EnvKey         string            `json:"env_key"`
			RequiresOpenAI bool              `json:"requires_openai_auth"`
			HTTPHeaders    map[string]string `json:"http_headers"`
		} `json:"model_providers"`
	}
	if err := json.Unmarshal([]byte(env[ConfigEnv]), &codexConfig); err != nil {
		t.Fatalf("CODEX_CONFIG JSON: %v\n%s", err, env[ConfigEnv])
	}
	if codexConfig.ModelProvider != ProviderID || codexConfig.Model != "deepseek-chat" {
		t.Errorf("provider/model = %q/%q", codexConfig.ModelProvider, codexConfig.Model)
	}
	provider := codexConfig.ModelProviders[ProviderID]
	if provider.Name != "DeepSeek" || provider.BaseURL != "https://api.example.com/v1" {
		t.Errorf("provider definition = %+v", provider)
	}
	if provider.WireAPI != "responses" || provider.EnvKey != APIKeyEnv || provider.RequiresOpenAI {
		t.Errorf("provider fields = %+v", provider)
	}
	if len(provider.HTTPHeaders) > 0 {
		t.Errorf("API key should not be embedded in http_headers: %s", env[ConfigEnv])
	}
}

func TestDisabledProviderStillAppliesModelOverrides(t *testing.T) {
	env, err := (Config{
		Enabled:    false,
		Models:     []string{"deepseek-chat"},
		TitleModel: "deepseek-chat",
	}).Environment()
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if _, ok := env[ConfigEnv]; ok {
		t.Error("disabled provider exported CODEX_CONFIG")
	}
	if _, ok := env[ModelProviderEnv]; ok {
		t.Error("disabled provider exported MODEL_PROVIDER")
	}
	if _, ok := env[APIKeyEnv]; ok {
		t.Error("disabled provider exported an API key")
	}
	if env[ModelsEnv] != `["deepseek-chat"]` {
		t.Errorf("%s = %q", ModelsEnv, env[ModelsEnv])
	}
	if env[ReasoningEffortsEnv] != `["medium"]` {
		t.Errorf("%s = %q, want the default level", ReasoningEffortsEnv, env[ReasoningEffortsEnv])
	}
	if env[TitleModelEnv] != "deepseek-chat" {
		t.Errorf("%s = %q", TitleModelEnv, env[TitleModelEnv])
	}
}

func TestEnvironmentWithoutOptionalValues(t *testing.T) {
	env, err := (Config{Enabled: true, BaseURL: "https://api.example.com/v1"}).Environment()
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if _, ok := env[APIKeyEnv]; ok {
		t.Error("empty API key was exported")
	}
	if _, ok := env[ModelsEnv]; ok {
		t.Error("empty model list was exported")
	}
	if _, ok := env[ReasoningEffortsEnv]; ok {
		t.Error("reasoning levels were exported without a custom model")
	}
	if _, ok := env[TitleModelEnv]; ok {
		t.Error("empty title model was exported")
	}
	if strings.Contains(env[ConfigEnv], `"model"`) {
		t.Errorf("CODEX_CONFIG should not set a model without a model list: %s", env[ConfigEnv])
	}
	if strings.Contains(env[ConfigEnv], "env_key") {
		t.Errorf("CODEX_CONFIG should not name an env_key without an API key: %s", env[ConfigEnv])
	}
}

func TestValidateChecksOverridesWhileDisabled(t *testing.T) {
	if err := (Config{Enabled: false, Models: []string{"two words"}}).Validate(); err == nil {
		t.Fatal("disabled provider did not validate its model list")
	}
	if err := (Config{Enabled: false, TitleModel: "two words"}).Validate(); err == nil {
		t.Fatal("disabled provider did not validate its title model")
	}
}

func TestValidateRejectsBadProvider(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{"missing URL", Config{Enabled: true}, "base URL is required"},
		{"relative URL", Config{Enabled: true, BaseURL: "api.example.com"}, "http or https"},
		{"credential URL", Config{Enabled: true, BaseURL: "https://user:key@api.example.com/v1"}, "credentials"},
		{"bad model", Config{Enabled: true, BaseURL: "https://api.example.com/v1", Models: []string{"two words"}}, "whitespace"},
		{"bad reasoning level", Config{Enabled: true, BaseURL: "https://api.example.com/v1", ReasoningEfforts: []string{"two words"}}, "whitespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Normalize().Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDefaultModelIsTheFirstModel(t *testing.T) {
	if got := (Config{Models: []string{"a", "b"}}).DefaultModel(); got != "a" {
		t.Errorf("DefaultModel = %q, want a", got)
	}
	if got := (Config{}).DefaultModel(); got != "" {
		t.Errorf("empty DefaultModel = %q", got)
	}
}

func TestReasoningLevelsDefaultToMedium(t *testing.T) {
	config := Config{}
	if got := config.EffortLevels(); !reflect.DeepEqual(got, []string{"medium"}) {
		t.Errorf("EffortLevels = %v, want [medium]", got)
	}
	if got := config.DefaultReasoningEffort(); got != "medium" {
		t.Errorf("DefaultReasoningEffort = %q, want medium", got)
	}
}

func TestReasoningLevelsPreferMediumThenFirst(t *testing.T) {
	tests := []struct {
		name   string
		levels []string
		want   string
	}{
		{"medium in the middle", []string{"low", "medium", "high"}, "medium"},
		{"medium absent", []string{"high", "xhigh"}, "high"},
		{"none only", []string{"none"}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{ReasoningEfforts: tt.levels}
			if got := config.DefaultReasoningEffort(); got != tt.want {
				t.Errorf("DefaultReasoningEffort = %q, want %q", got, tt.want)
			}
			if got := config.EffortLevels(); !reflect.DeepEqual(got, tt.levels) {
				t.Errorf("EffortLevels = %v, want %v", got, tt.levels)
			}
		})
	}
}
