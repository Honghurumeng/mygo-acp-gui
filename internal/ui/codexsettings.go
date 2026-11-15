package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/settings"
)

// agentDefaultAPIKeyEnv mirrors agent.DefaultCodexAPIKeyEnv for the settings
// hint; keeping the name in one place avoids a stale label.
const agentDefaultAPIKeyEnv = agent.DefaultCodexAPIKeyEnv

// codexOverride converts the saved Codex settings into the launch override.
// A disabled override returns nil, which leaves Codex's own configuration in
// charge. Validation happens here so the settings panel can show the exact
// reason before a Codex process is spawned.
func (a *App) codexOverride() (*agent.CodexOverride, error) {
	codex := settings.NormalizeCodex(a.CodexSettings)
	if !codex.Override {
		return nil, nil
	}
	provider := codex.Provider
	override := &agent.CodexOverride{
		ProviderID:      provider.ID,
		ProviderName:    provider.Name,
		BaseURL:         provider.BaseURL,
		APIKeyEnv:       provider.APIKeyEnv,
		APIKey:          provider.APIKey,
		UseCodexLogin:   provider.UseCodexLogin,
		ModelCatalogURL: provider.ModelCatalogURL,
		Model:           provider.Model,
	}
	if err := override.Validate(); err != nil {
		return nil, err
	}
	// Without an explicit catalog URL Codex has no metadata for a custom
	// model: it logs "Model metadata ... not found", uses fallback limits and
	// offers no reasoning effort. Serve a generated catalog instead.
	if override.ModelCatalogURL == "" {
		catalogURL, err := a.serveCodexCatalog(provider)
		if err != nil {
			return nil, err
		}
		override.ModelCatalogURL = catalogURL
	}
	return override, nil
}

// serveCodexCatalog starts the loopback catalog server on first use and
// replaces its document with the current provider/model metadata. Codex
// refetches the document for every app-server, so Apply affects new sessions
// without restarting the GUI.
func (a *App) serveCodexCatalog(provider settings.CodexProvider) (string, error) {
	server, err := a.ensureCodexCatalog()
	if err != nil {
		return "", err
	}
	display := strings.TrimSpace(provider.Name)
	description := "Model served through a custom provider."
	if display != "" {
		description = fmt.Sprintf("Model served by %s through a custom provider.", display)
	}
	model := agent.CodexCatalogModel{
		Slug:                  provider.Model,
		DisplayName:           provider.Model,
		Description:           description,
		ContextWindow:         provider.ContextWindow,
		MaxContextWindow:      provider.ContextWindow,
		ReasoningLevels:       provider.ReasoningLevels,
		DefaultReasoningLevel: "medium",
	}
	if err := server.SetModels([]agent.CodexCatalogModel{model}); err != nil {
		return "", err
	}
	return server.URL(), nil
}

func (a *App) ensureCodexCatalog() (*agent.CodexCatalogServer, error) {
	a.codexMu.Lock()
	defer a.codexMu.Unlock()
	if a.codexCatalog != nil {
		return a.codexCatalog, nil
	}
	server, err := agent.NewCodexCatalogServer()
	if err != nil {
		return nil, err
	}
	a.codexCatalog = server
	return server, nil
}

// syncCodexSettingsText mirrors the normalized provider values into the
// settings text fields. It runs after load and after a successful Apply so
// the generated defaults are visible before the user edits them.
func (a *App) syncCodexSettingsText() {
	a.CodexReasoningText = strings.Join(a.CodexSettings.Provider.ReasoningLevels, ", ")
	window := a.CodexSettings.Provider.ContextWindow
	if window <= 0 {
		window = settings.DefaultCodexContextWindow
	}
	a.CodexContextText = strconv.Itoa(window)
}

// applyCodexTextSettings parses the two catalog text fields into
// CodexSettings. It leaves the settings untouched on an invalid number so a
// typo does not silently replace the previous value.
func (a *App) applyCodexTextSettings() bool {
	window, err := settings.ParseCodexContextWindow(a.CodexContextText)
	if err != nil {
		a.CodexError = err.Error()
		a.CodexNotice = ""
		return false
	}
	a.CodexSettings.Provider.ReasoningLevels = settings.ParseCodexReasoningLevels(a.CodexReasoningText)
	a.CodexSettings.Provider.ContextWindow = window
	return true
}

// applyCodexSettings rebuilds the Codex launch spec from the base discovery
// spec and the current settings. Other agents are untouched. It does not
// spawn a process or persist: Connect and probe pick up the new spec, while a
// settings Apply calls persist and refresh explicitly.
func (a *App) applyCodexSettings() {
	override, err := a.codexOverride()
	if err != nil {
		a.CodexError = err.Error()
		a.CodexNotice = ""
		return
	}
	base := a.baseAgents
	if len(base) == 0 {
		base = a.Agents
	}
	if len(base) == 0 {
		a.CodexSettings = settings.NormalizeCodex(a.CodexSettings)
		a.syncCodexSettingsText()
		a.CodexError = ""
		return
	}
	effective := make([]agent.LaunchSpec, 0, len(base))
	for _, spec := range base {
		if spec.ID == agent.CodexID {
			updated, replaceErr := agent.WithCodexOverride(spec, override)
			if replaceErr != nil {
				a.CodexError = replaceErr.Error()
				a.CodexNotice = ""
				return
			}
			spec = updated
		}
		effective = append(effective, spec)
	}
	a.Agents = effective
	a.CodexSettings = settings.NormalizeCodex(a.CodexSettings)
	a.syncCodexSettingsText()
	a.CodexError = ""
	if override == nil {
		a.CodexNotice = "Codex uses its own configuration."
		return
	}
	name := strings.TrimSpace(override.ProviderName)
	if name == "" {
		name = override.ProviderID
	}
	metadata := "generated model catalog"
	if override.ModelCatalogURL == "" {
		metadata = "no model catalog"
	} else if catalogURL := a.codexCatalogURL(); catalogURL != "" && catalogURL == override.ModelCatalogURL {
		metadata = "local model catalog"
	} else {
		metadata = "custom model catalog"
	}
	a.CodexNotice = fmt.Sprintf("New Codex sessions use %s / %s with a %s.", name, override.Model, metadata)
}

// codexCatalogURL reports the generated catalog URL, if the server is up.
func (a *App) codexCatalogURL() string {
	a.codexMu.Lock()
	defer a.codexMu.Unlock()
	if a.codexCatalog == nil {
		return ""
	}
	return a.codexCatalog.URL()
}

// currentCodexSpec returns the effective Codex launch spec, if Codex is
// available. The settings panel uses it for status and probes.
func (a *App) currentCodexSpec() (agent.LaunchSpec, bool) {
	for _, spec := range a.Agents {
		if spec.ID == agent.CodexID {
			return spec, true
		}
	}
	return agent.LaunchSpec{}, false
}

// codexEnvKey returns the value of key in the resolved login environment.
// The settings panel only uses it to tell the user whether the variable is
// present; the secret is never displayed or stored.
func (a *App) codexEnvKey(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return agent.EnvValue(a.Env.Env, key)
}
