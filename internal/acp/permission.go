package acp

import "encoding/json"

// PermissionOption is one choice the agent offers for a tool call.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// RequestPermissionParams is the payload of session/request_permission.
type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  json.RawMessage    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionOutcome is the selection returned to the agent.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// RequestPermissionResult is the result of session/request_permission.
type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// SelectedPermission selects one of the agent's options.
func SelectedPermission(optionID string) RequestPermissionResult {
	return RequestPermissionResult{
		Outcome: PermissionOutcome{Outcome: "selected", OptionID: optionID},
	}
}

// CancelledPermission tells the agent the user declined to decide.
func CancelledPermission() RequestPermissionResult {
	return RequestPermissionResult{
		Outcome: PermissionOutcome{Outcome: "cancelled"},
	}
}
