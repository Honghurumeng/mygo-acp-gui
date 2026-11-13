package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/modelconfig"
)

const modelConfigFileName = "model-config.json"

// loadModelConfigs restores the per-model config options. The cache is only a
// convenience for drafts: a corrupt or missing file leaves it empty, and the
// next startup sweep refills it.
func (a *App) loadModelConfigs(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir != "" {
		a.modelConfig = modelconfig.Open(filepath.Join(dataDir, modelConfigFileName))
	}
	if a.modelConfig == nil {
		return
	}
	file, err := a.modelConfig.Load()
	if err != nil {
		a.SavedError = err.Error()
	}
	for agentID, models := range file.Agents {
		for model, entry := range models {
			if len(entry.ConfigOptions) == 0 {
				continue
			}
			a.setModelConfig(agentID, model, entry)
		}
	}
}

// setModelConfig installs one agent/model entry in memory without touching
// the file. Loading uses it, and rememberModelConfig saves afterwards.
func (a *App) setModelConfig(agentID, model string, entry modelconfig.Model) {
	if agentID == "" || model == "" || len(entry.ConfigOptions) == 0 {
		return
	}
	if a.AgentModelConfigs == nil {
		a.AgentModelConfigs = make(map[string]map[string]modelconfig.Model)
	}
	models := a.AgentModelConfigs[agentID]
	if models == nil {
		models = make(map[string]modelconfig.Model)
		a.AgentModelConfigs[agentID] = models
	}
	models[model] = modelconfig.Model{
		ConfigOptions: copyConfigOptions(entry.ConfigOptions),
		ProbedAt:      entry.ProbedAt,
	}
}

// modelConfigOptions returns the config options cached for one model, or nil
// when that model has never been swept.
func (a *App) modelConfigOptions(agentID, model string) []acp.ConfigOption {
	if agentID == "" || model == "" {
		return nil
	}
	models := a.AgentModelConfigs[agentID]
	entry, ok := models[model]
	if !ok || len(entry.ConfigOptions) == 0 {
		return nil
	}
	return entry.ConfigOptions
}

// rememberModelConfig stores the config options an agent returned for one
// model. model may be empty, in which case it comes from the options' model
// select. The file is rewritten only when the entry changed, so the frequent
// config_option_update traffic does not hit the disk every time.
func (a *App) rememberModelConfig(agentID, model string, options []acp.ConfigOption) {
	if agentID == "" || len(options) == 0 {
		return
	}
	if model == "" {
		if option := acp.ModelOption(options); option != nil {
			model = option.CurrentValue
		}
	}
	if model == "" {
		return
	}
	if existing := a.modelConfigOptions(agentID, model); sameConfigOptions(existing, options) {
		return
	}
	a.setModelConfig(agentID, model, modelconfig.Model{
		ConfigOptions: options,
		ProbedAt:      time.Now().UTC().Format(time.RFC3339),
	})
	a.saveModelConfigs()
}

// saveModelConfigs writes the whole cache atomically. A missing data
// directory keeps the cache in memory only.
func (a *App) saveModelConfigs() {
	if a.modelConfig == nil {
		return
	}
	file := modelconfig.File{
		Version: modelconfig.CurrentVersion,
		Agents:  make(map[string]map[string]modelconfig.Model, len(a.AgentModelConfigs)),
	}
	for agentID, models := range a.AgentModelConfigs {
		if len(models) == 0 {
			continue
		}
		bucket := make(map[string]modelconfig.Model, len(models))
		for model, entry := range models {
			bucket[model] = entry
		}
		file.Agents[agentID] = bucket
	}
	if err := a.modelConfig.Save(file); err != nil {
		a.SavedError = err.Error()
		return
	}
	if strings.HasPrefix(a.SavedError, "model config:") {
		a.SavedError = ""
	}
}

// knownModels lists the models already cached for one agent, so a config
// probe only switches through the ones that are new.
func (a *App) knownModels(agentID string) map[string]bool {
	models := a.AgentModelConfigs[agentID]
	if len(models) == 0 {
		return nil
	}
	known := make(map[string]bool, len(models))
	for model := range models {
		known[model] = true
	}
	return known
}

// configOptionsForModel renders the fresh option list with the model-scoped
// options that the agent returned for modelValue. The model select itself
// keeps fresh's choices: models can appear or disappear after the cached
// snapshot was taken, and the fresh list is the availability signal.
func configOptionsForModel(snapshot, fresh []acp.ConfigOption, modelValue string) []acp.ConfigOption {
	out := copyConfigOptions(snapshot)
	model := acp.ModelOption(fresh)
	if model == nil {
		return out
	}
	if target := acp.ModelOption(out); target != nil {
		*target = *model
		target.Options = append([]acp.ConfigOptionValue(nil), model.Options...)
		target.CurrentValue = modelValue
		return out
	}
	out = append([]acp.ConfigOption{*model}, out...)
	out[0].Options = append([]acp.ConfigOptionValue(nil), model.Options...)
	out[0].CurrentValue = modelValue
	return out
}

func sameConfigOptions(a, b []acp.ConfigOption) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.Name != y.Name || x.Description != y.Description ||
			x.Category != y.Category || x.Type != y.Type || x.CurrentValue != y.CurrentValue ||
			len(x.Options) != len(y.Options) {
			return false
		}
		for j := range x.Options {
			if x.Options[j] != y.Options[j] {
				return false
			}
		}
	}
	return true
}
