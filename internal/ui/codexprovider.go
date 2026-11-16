package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/codexprovider"
)

const (
	settingsSectionCodexProvider = "codex-provider"
	codexReasoningConfigID       = "reasoning_effort"
)

// codexProvider returns the saved provider when it is enabled and valid. A
// disabled or broken entry keeps Codex on the user's own configuration.
func (a *App) codexProvider() *codexprovider.Config {
	if !a.CodexProvider.HasOverrides() {
		return nil
	}
	provider := a.CodexProvider.Normalize()
	if err := provider.Validate(); err != nil {
		return nil
	}
	if path := a.codexCatalogPath(); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			provider.CatalogPath = path
		}
	}
	return &provider
}

// codexCatalogPath is the generated catalog location. It is empty when the
// app runs without a data directory (tests, portable runs).
func (a *App) codexCatalogPath() string {
	if strings.TrimSpace(a.DataDir) == "" {
		return ""
	}
	return filepath.Join(a.DataDir, codexprovider.CatalogFileName)
}

// refreshCodexCatalog rewrites the model metadata Codex reads. A disabled
// provider or an empty model list removes the file so stale models never
// survive a settings change.
func (a *App) refreshCodexCatalog() error {
	path := a.codexCatalogPath()
	if path == "" {
		return nil
	}
	return a.CodexProvider.SaveCatalog(path)
}

// loadCodexProviderDraft copies the saved provider into the settings editor.
// Closing and reopening the panel therefore discards unsaved edits.
func (a *App) loadCodexProviderDraft() {
	draft := a.CodexProvider.Normalize()
	a.CodexProviderDraft = draft
	a.CodexProviderModelsText = strings.Join(draft.Models, "\n")
	a.setCodexProviderEfforts(draft.EffortLevels())
	a.CodexProviderNotice = ""
}

// saveCodexProvider validates the editor, persists it and rebuilds the Codex
// launch spec. Only new Codex processes use the change; an already connected
// session keeps the provider it started with.
func (a *App) saveCodexProvider() {
	draft := a.CodexProviderDraft
	draft.Models = splitModelLines(a.CodexProviderModelsText)
	draft.ReasoningEfforts = a.selectedCodexProviderEfforts()
	draft = draft.Normalize()
	if len(draft.ReasoningEfforts) == 0 {
		a.CodexProviderNotice = "⚠ Select at least one reasoning level."
		return
	}
	if err := draft.Validate(); err != nil {
		a.CodexProviderNotice = "⚠ " + err.Error()
		return
	}
	a.CodexProviderDraft = draft
	a.CodexProviderModelsText = strings.Join(draft.Models, "\n")
	a.setCodexProviderEfforts(draft.EffortLevels())
	previous := a.CodexProvider
	a.CodexProvider = draft
	catalogErr := a.refreshCodexCatalog()
	if providerRoutingChanged(previous, draft) {
		a.forgetCodexConfigChoice()
	} else if !slices.Equal(previous.EffortLevels(), draft.EffortLevels()) {
		a.forgetCodexReasoningChoice()
	}
	a.persistSettings()
	if err := a.applyCodexProvider(); err != nil {
		a.CodexProviderNotice = "⚠ Saved, but the Codex launch spec failed: " + err.Error()
		return
	}
	if catalogErr != nil {
		a.CodexProviderNotice = "⚠ Saved, but the Codex model catalog failed: " + catalogErr.Error()
		return
	}
	switch {
	case draft.Enabled:
		a.CodexProviderNotice = "Saved. New Codex sessions use this provider."
	case len(draft.Models) > 0 || draft.TitleModel != "":
		a.CodexProviderNotice = "Saved. New Codex sessions keep your own provider and use these model/title overrides."
	default:
		a.CodexProviderNotice = "Saved. Codex uses its own configuration again."
	}
}

func providerRoutingChanged(previous, next codexprovider.Config) bool {
	return previous.Enabled != next.Enabled ||
		previous.BaseURL != next.BaseURL ||
		!slices.Equal(previous.Models, next.Models)
}

// setCodexProviderEfforts fills the checkbox state of the settings editor.
func (a *App) setCodexProviderEfforts(levels []string) {
	a.CodexProviderEfforts = make(map[string]bool, len(levels))
	for _, level := range levels {
		a.CodexProviderEfforts[level] = true
	}
}

// selectedCodexProviderEfforts reads the checkboxes in their display order.
func (a *App) selectedCodexProviderEfforts() []string {
	levels := make([]string, 0, len(a.CodexProviderEfforts))
	for _, level := range codexprovider.ReasoningLevels {
		if a.CodexProviderEfforts[level] {
			levels = append(levels, level)
		}
	}
	return levels
}

// forgetCodexReasoningChoice drops only the old thinking level. The model list
// did not change, so a draft or the last selection can keep its model.
func (a *App) forgetCodexReasoningChoice() {
	if selection, ok := a.lastSelections[agent.CodexID]; ok {
		delete(selection.ConfigOptions, codexReasoningConfigID)
		if len(selection.ConfigOptions) == 0 {
			selection.ConfigOptions = nil
		}
		if len(selection.ConfigOptions) == 0 && selection.Mode == "" {
			delete(a.lastSelections, agent.CodexID)
		} else {
			a.lastSelections[agent.CodexID] = selection
		}
	}
	for _, chat := range a.Chats {
		if chat.Session == nil && chat.Agent.ID == agent.CodexID {
			delete(chat.PendingConfig, codexReasoningConfigID)
		}
	}
}

// forgetCodexConfigChoice drops model/thinking choices that belonged to the
// previous provider. The session mode is kept; it is not model-dependent.
func (a *App) forgetCodexConfigChoice() {
	if selection, ok := a.lastSelections[agent.CodexID]; ok {
		selection.ConfigOptions = nil
		if selection.Mode == "" {
			delete(a.lastSelections, agent.CodexID)
		} else {
			a.lastSelections[agent.CodexID] = selection
		}
	}
	for _, chat := range a.Chats {
		if chat.Session == nil && chat.Agent.ID == agent.CodexID {
			chat.PendingConfig = nil
			chat.ModelProvider = ""
		}
	}
}

func splitModelLines(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
}

// applyCodexProvider rebuilds the catalog entry for Codex from the pristine
// login environment. Discovery applies the provider when discovery later
// completes; an injected-agent test or a not-yet-resolved environment simply
// keeps the saved settings for the next launch.
func (a *App) applyCodexProvider() error {
	if a.injectedAgents || a.Env.NodePath == "" || a.Env.CodexPath == "" {
		return nil
	}
	spec, err := agent.CodexLaunchSpec(a.Resources, a.Env, a.codexProvider())
	if err != nil {
		return err
	}
	found := false
	for i := range a.Agents {
		if a.Agents[i].ID == agent.CodexID {
			a.Agents[i] = spec
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	for _, chat := range a.Chats {
		if chat.Session == nil && chat.Agent.ID == agent.CodexID {
			chat.Agent = spec
		}
	}
	// The provider can change the model catalog, so cached model options and
	// per-model reasoning levels no longer describe this agent.
	delete(a.AgentConfigs, agent.CodexID)
	delete(a.AgentModes, agent.CodexID)
	if _, ok := a.AgentModelConfigs[agent.CodexID]; ok {
		delete(a.AgentModelConfigs, agent.CodexID)
		a.saveModelConfigs()
	}
	if a.agentConfigAt != nil {
		delete(a.agentConfigAt, agent.CodexID)
	}
	a.probeAgentConfig(spec, true)
	return nil
}

func (a *App) codexProviderSettings(c *mui.Context) {
	if a.CodexProviderEfforts == nil {
		a.setCodexProviderEfforts(a.CodexProviderDraft.EffortLevels())
	}
	t := c.Theme()
	mui.Column(c).Gap(10).Children(func() {
		mui.Text(c, "Codex provider").FontSize(15).Bold()
		mutedText(c, "Use an OpenAI-compatible provider for Codex instead of the provider in ~/.codex/config.toml. The app passes this configuration to its own Codex child processes and never writes your Codex config. Models, Reasoning levels and Title model also apply when the switch is off, so you can extend your existing Codex provider.", 10)

		mui.Row(c).Gap(12).AlignItems(mui.Center).Padding(10, 12).Radius(8).
			Background(t.Surface).Children(func() {
			mui.Column(c).Gap(3).Grow(1).Children(func() {
				mui.Text(c, "Custom provider").FontSize(12)
				mutedText(c, "Off keeps your own Codex provider; Models, Reasoning levels and Title model below still apply.", 10)
			})
			sw := mui.Switch(c, &a.CodexProviderDraft.Enabled).Label("Toggle custom Codex provider")
			if sw.Changed() {
				a.CodexProviderNotice = ""
			}
		})

		a.codexProviderField(c, "Provider name", "Shown by Codex and in the model panel.",
			&a.CodexProviderDraft.Name, "Custom Codex provider")
		a.codexProviderField(c, "Base URL", "OpenAI Responses API root, for example https://api.example.com/v1.",
			&a.CodexProviderDraft.BaseURL, "https://api.example.com/v1")
		mui.TextInput(c, &a.CodexProviderDraft.APIKey).
			Label("API key").
			Placeholder("Optional when the provider needs no key").
			Password()
		mutedText(c, "The key is stored in settings.json (mode 0600) and passed to Codex only through its environment.", 9)

		mui.TextAreaBase(c, &a.CodexProviderModelsText).
			Label("Models").
			Placeholder("One model id per line, for example:\ndeepseek-chat\ndeepseek-reasoner").
			Lines(4, 8).
			Padding(6, 8).
			FontSize(11).
			Radius(t.Radius).
			Border(1, t.Border).
			Background(t.Background)
		mutedText(c, "These ids are added to the Codex model list even while the provider switch is off. For DeepSeek targets, the switch on also writes a Codex model catalog from DeepSeek Flash/Pro metadata (1M context, the checked reasoning levels) so Codex does not fall back to unknown-model metadata. The first id is the default model; leave the list empty to use only Codex's own catalog.", 9)

		mui.Column(c).Gap(2).Children(func() {
			mui.Text(c, "Reasoning levels").FontSize(12)
			mui.Row(c).Gap(14).Wrap().Children(func() {
				for _, level := range codexprovider.ReasoningLevels {
					level := level
					checked := a.CodexProviderEfforts[level]
					if mui.Checkbox(c, &checked, codexReasoningLabel(level)).Changed() {
						a.CodexProviderEfforts[level] = checked
						a.CodexProviderNotice = ""
					}
				}
			})
			mutedText(c, "Shown in Thinking for model ids that Codex's catalog does not know. Medium is the default when it is checked; otherwise the first checked level is the default.", 9)
		})

		a.codexProviderField(c, "Title model", "Optional. Empty follows the conversation model; this replaces the fixed gpt-5.6-luna title model.",
			&a.CodexProviderDraft.TitleModel, "Optional")

		mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
			if primaryButton(c, "Save provider").Clicked() {
				a.saveCodexProvider()
			}
			caption(c, "Saving updates new Codex processes only.")
		})
		if notice := strings.TrimSpace(a.CodexProviderNotice); notice != "" {
			if strings.HasPrefix(notice, "⚠") {
				mui.Text(c, notice).FontSize(10).TextColor(t.Danger).Selectable()
			} else {
				mutedText(c, notice, 10).Selectable()
			}
		}
	})
}

func codexReasoningLabel(level string) string {
	if level == "" {
		return level
	}
	return strings.ToUpper(level[:1]) + level[1:]
}

func (a *App) codexProviderField(c *mui.Context, label, note string, value *string, placeholder string) {
	mui.Column(c).Gap(2).Children(func() {
		mui.TextInput(c, value).Label(label).Placeholder(placeholder)
		mutedText(c, note, 9)
	})
}
