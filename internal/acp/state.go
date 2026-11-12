package acp

import "encoding/json"

// ConfigOptionValue is one choice of a select config option.
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is an agent-provided session setting such as model or
// reasoning effort.
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type,omitempty"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

// Mode is one permission/session mode an agent offers.
type Mode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModeState is the current mode plus the available ones.
type ModeState struct {
	CurrentModeID  string `json:"currentModeId"`
	AvailableModes []Mode `json:"availableModes"`
}

// Model is one entry of the legacy models block some agents still send.
type Model struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// ModelState is the current model plus the available ones.
type ModelState struct {
	CurrentModelID  string  `json:"currentModelId"`
	AvailableModels []Model `json:"availableModels"`
}

// CommandInput describes whether a slash command takes arguments.
type CommandInput struct {
	Hint string `json:"hint,omitempty"`
}

// AvailableCommand is a slash command advertised by the agent.
type AvailableCommand struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Input       *CommandInput `json:"input,omitempty"`
}

// Cost is the monetary cost of a turn or session.
type Cost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// Usage is the payload of the usage_update notification.
type Usage struct {
	Used int   `json:"used"`
	Size int   `json:"size"`
	Cost *Cost `json:"cost,omitempty"`
}

// SessionState is the state returned by session/new and session/load.
// session/new also carries sessionId; session/load is addressed by the id
// the client already knows.
type SessionState struct {
	SessionID     string         `json:"sessionId,omitempty"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
	Models        *ModelState    `json:"models,omitempty"`
	Modes         *ModeState     `json:"modes,omitempty"`
}

// UnmarshalJSON tolerates agents that send an empty modes array instead of
// the object form used by current ACP versions.
func (s *SessionState) UnmarshalJSON(data []byte) error {
	var raw struct {
		SessionID     string          `json:"sessionId"`
		ConfigOptions []ConfigOption  `json:"configOptions"`
		Models        json.RawMessage `json:"models"`
		Modes         json.RawMessage `json:"modes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.SessionID = raw.SessionID
	s.ConfigOptions = raw.ConfigOptions
	if len(raw.Models) > 0 && string(raw.Models) != "null" {
		var models ModelState
		if err := json.Unmarshal(raw.Models, &models); err == nil {
			s.Models = &models
		}
	}
	if len(raw.Modes) > 0 && string(raw.Modes) != "null" {
		var modes ModeState
		if err := json.Unmarshal(raw.Modes, &modes); err == nil {
			s.Modes = &modes
		}
	}
	return nil
}

// NewSessionResult is the session/new response.
type NewSessionResult = SessionState

// LoadSessionParams is the payload of session/load.
type LoadSessionParams struct {
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

// CloseSessionParams is the payload of session/close.
type CloseSessionParams struct {
	SessionID string `json:"sessionId"`
}

// SessionSummary is one row of session/list.
type SessionSummary struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ListSessionsParams is the payload of session/list. Cwd filters to one
// workspace when set. Nil is marshalled as an explicit "cwd": null so agents
// do not fall back to the process workspace: pi-acp uses
// `params.cwd ?? lastSessionCwd` and would hide sessions from other
// workspaces when the field is omitted entirely.
type ListSessionsParams struct {
	Cwd    *string `json:"cwd"`
	Cursor string  `json:"cursor,omitempty"`
}

// ListSessionsResult is the session/list response. NextCursor is empty on the
// last page.
type ListSessionsResult struct {
	Sessions   []SessionSummary `json:"sessions"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// SetConfigOptionParams is the payload of session/set_config_option.
type SetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetConfigOptionResult is the session/set_config_option response.
type SetConfigOptionResult struct {
	ConfigOptions []ConfigOption `json:"configOptions"`
}

// SetModeParams is the payload of session/set_mode.
type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}
