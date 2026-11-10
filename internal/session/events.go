package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/reed/acp-gui/internal/acp"
)

// Well-known session/update kinds. Unknown kinds are delivered with their
// raw payload instead of being dropped.
const (
	UpdateMessageChunk = "agent_message_chunk"
	UpdateUserChunk    = "user_message_chunk"
	UpdateThoughtChunk = "agent_thought_chunk"
	UpdateToolCall     = "tool_call"
	UpdateToolCallDone = "tool_call_update"
	UpdatePlan         = "plan"
	UpdateConfigOption = "config_option_update"
	UpdateCurrentMode  = "current_mode_update"
	UpdateCommands     = "available_commands_update"
	UpdateSessionInfo  = "session_info_update"
	UpdateUsage        = "usage_update"
)

// Event is one parsed agent -> client update. Unknown kinds remain raw.
type Event struct {
	Session       *Session
	SessionID     string
	Kind          string
	Text          string
	ToolCall      *ToolCall
	Plan          []PlanEntry
	ConfigOptions []acp.ConfigOption
	CurrentModeID string
	Commands      []acp.AvailableCommand
	Title         string
	Usage         *acp.Usage
	Update        json.RawMessage
}

// ToolCall is the common shape of tool_call and tool_call_update.
type ToolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Content    json.RawMessage `json:"content,omitempty"`
	Locations  json.RawMessage `json:"locations,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`
}

// PlanEntry is one item of a plan update.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// Info is the result of initialize plus new/load/resume.
type Info struct {
	SessionID         string
	ProtocolVersion   int
	AgentInfo         *acp.Implementation
	AuthMethods       []acp.AuthMethod
	ConfigOptions     []acp.ConfigOption
	Models            *acp.ModelState
	Modes             []acp.Mode
	CurrentModeID     string
	AgentCapabilities acp.AgentCapabilities
	Title             string
}

// ConfigOption returns the named option, or nil.
func (i Info) ConfigOption(id string) *acp.ConfigOption {
	for idx := range i.ConfigOptions {
		if i.ConfigOptions[idx].ID == id {
			return &i.ConfigOptions[idx]
		}
	}
	return nil
}

// CurrentModelOption is the model config option, if the agent exposes one.
func (i Info) CurrentModelOption() *acp.ConfigOption {
	return i.ConfigOption("model")
}

// PermissionRequest is an agent request for the user to approve a tool.
type PermissionRequest struct {
	Session   *Session
	SessionID string
	ToolCall  ToolCall
	Options   []acp.PermissionOption
}

// Exit reports that the agent process ended.
type Exit struct {
	Session  *Session
	Err      error
	Stderr   string
	Expected bool
}

// AuthRequiredError means initialize succeeded but session/new needs a
// login. AuthMethods are the agent's instructions.
type AuthRequiredError struct {
	Methods []acp.AuthMethod
	Err     error
}

func (e *AuthRequiredError) Error() string {
	if e == nil {
		return ""
	}
	names := make([]string, 0, len(e.Methods))
	for _, m := range e.Methods {
		if m.Name != "" {
			names = append(names, m.Name)
		} else if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	if len(names) > 0 {
		return fmt.Sprintf("authentication required (%s): %v", strings.Join(names, ", "), e.Err)
	}
	return "authentication required: " + e.Err.Error()
}

func (e *AuthRequiredError) Unwrap() error { return e.Err }

// IsAuthRequired reports whether err is an authentication handshake error.
func IsAuthRequired(err error) bool {
	var target *AuthRequiredError
	return errors.As(err, &target)
}

func parseEvent(s *Session, params json.RawMessage) *Event {
	var envelope acp.SessionUpdateParams
	if err := json.Unmarshal(params, &envelope); err != nil {
		return nil
	}
	update := envelope.Update
	if len(update) == 0 {
		// Some older adapters put the update at the top level.
		update = params
	}
	var head struct {
		Kind string `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(update, &head); err != nil || head.Kind == "" {
		return nil
	}
	ev := &Event{
		Session:   s,
		SessionID: envelope.SessionID,
		Kind:      head.Kind,
		Update:    cloneRaw(update),
	}
	switch head.Kind {
	case UpdateMessageChunk, UpdateUserChunk, UpdateThoughtChunk:
		ev.Text = contentText(update)
	case UpdateToolCall, UpdateToolCallDone:
		var call ToolCall
		if err := json.Unmarshal(update, &call); err == nil {
			ev.ToolCall = &call
		}
	case UpdatePlan:
		var plan struct {
			Entries []PlanEntry `json:"entries"`
		}
		if err := json.Unmarshal(update, &plan); err == nil {
			ev.Plan = plan.Entries
		}
	case UpdateConfigOption:
		var payload struct {
			ConfigOptions []acp.ConfigOption `json:"configOptions"`
		}
		if err := json.Unmarshal(update, &payload); err == nil {
			ev.ConfigOptions = payload.ConfigOptions
		}
	case UpdateCurrentMode:
		var payload struct {
			CurrentModeID string `json:"currentModeId"`
		}
		if err := json.Unmarshal(update, &payload); err == nil {
			ev.CurrentModeID = payload.CurrentModeID
		}
	case UpdateCommands:
		var payload struct {
			AvailableCommands []acp.AvailableCommand `json:"availableCommands"`
		}
		if err := json.Unmarshal(update, &payload); err == nil {
			ev.Commands = payload.AvailableCommands
		}
	case UpdateSessionInfo:
		var payload struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(update, &payload); err == nil {
			ev.Title = payload.Title
		}
	case UpdateUsage:
		var payload acp.Usage
		if err := json.Unmarshal(update, &payload); err == nil {
			ev.Usage = &payload
		}
	}
	return ev
}

func contentText(raw json.RawMessage) string {
	var update struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil || len(update.Content) == 0 {
		return ""
	}
	// A string is non-standard but harmless to accept.
	var text string
	if json.Unmarshal(update.Content, &text) == nil {
		return text
	}
	var block acp.ContentBlock
	if json.Unmarshal(update.Content, &block) == nil {
		return block.Text
	}
	var blocks []acp.ContentBlock
	if json.Unmarshal(update.Content, &blocks) == nil {
		for _, block := range blocks {
			if block.Type == "" || block.Type == "text" {
				text += block.Text
			}
		}
	}
	return text
}

func parseToolCall(raw json.RawMessage) ToolCall {
	var call ToolCall
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &call)
	}
	return call
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
