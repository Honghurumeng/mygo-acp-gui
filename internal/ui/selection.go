package ui

import (
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/settings"
)

// restoreSelection fills a draft with the config values and mode last used
// with its agent. Connect validates them against session/new's config options
// and modes, so a model, thinking level or mode the agent dropped is skipped
// instead of being applied blind.
func (a *App) restoreSelection(chat *Chat) {
	if chat == nil || chat.Session != nil || chat.Agent.ID == "" {
		return
	}
	selection, ok := a.lastSelections[chat.Agent.ID]
	if !ok {
		return
	}
	if len(selection.ConfigOptions) > 0 {
		chat.PendingConfig = make(map[string]string, len(selection.ConfigOptions))
		for id, value := range selection.ConfigOptions {
			chat.PendingConfig[id] = value
		}
	}
	if selection.Mode != "" {
		chat.PendingMode = selection.Mode
	}
}

// rememberSelection records what a chat currently uses: the live session's
// config values and mode, or a draft's pending choices. The settings file is
// rewritten only when the values changed, so agent config_option_update
// traffic does not hit the disk every time.
func (a *App) rememberSelection(chat *Chat) {
	if chat == nil || chat.Agent.ID == "" {
		return
	}
	selection := settings.Selection{}
	if chat.Session != nil {
		selection.ConfigOptions = selectionFromOptions(chat.Info.ConfigOptions)
		selection.Mode = chat.Info.CurrentModeID
	} else {
		if len(chat.PendingConfig) > 0 {
			selection.ConfigOptions = make(map[string]string, len(chat.PendingConfig))
			for id, value := range chat.PendingConfig {
				selection.ConfigOptions[id] = value
			}
		}
		selection.Mode = chat.PendingMode
	}
	if len(selection.ConfigOptions) == 0 && selection.Mode == "" {
		return
	}
	if a.lastSelections == nil {
		a.lastSelections = make(map[string]settings.Selection)
	}
	if existing, ok := a.lastSelections[chat.Agent.ID]; ok && sameSelection(existing, selection) {
		return
	}
	a.lastSelections[chat.Agent.ID] = selection
	a.persistSettings()
}

// selectionFromOptions keeps the current value of every select option, which
// is exactly what the model panel shows: model, thinking and extra selects.
func selectionFromOptions(options []acp.ConfigOption) map[string]string {
	var out map[string]string
	for _, option := range options {
		if len(option.Options) == 0 || option.CurrentValue == "" {
			continue
		}
		if option.Type != "" && option.Type != "select" {
			continue
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[option.ID] = option.CurrentValue
	}
	return out
}

func sameSelection(a, b settings.Selection) bool {
	if a.Mode != b.Mode || len(a.ConfigOptions) != len(b.ConfigOptions) {
		return false
	}
	for id, value := range a.ConfigOptions {
		if b.ConfigOptions[id] != value {
			return false
		}
	}
	return true
}
