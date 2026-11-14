package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	// StartupInfo marks the synthetic pi-acp startup prelude. It is not part
	// of the model's answer and should be shown separately or dropped.
	StartupInfo bool
	// Notice marks adapter status text (retry, compaction, queue, extension
	// notification) that must not be appended to the assistant message.
	Notice *Notice
	// Replay identifies one persisted message during session/load. Chunks of
	// the same message share Key; a different Key starts a new row even when
	// the previous persisted message produced no visible chunk (for example a
	// failed turn with empty content). Timestamp is the agent's send time.
	Replay *Replay
	// Content holds the message chunk's blocks. Text is their concatenated
	// text; images and resource blocks are kept so replay can render them
	// instead of dropping everything but text.
	Content []acp.ContentBlock
	Update  json.RawMessage
}

// Notice is adapter-generated status text carried on an agent message chunk.
type Notice struct {
	Kind  string
	Level string
}

// Replay is the per-message metadata pi-acp attaches to session/load chunks.
type Replay struct {
	Key       string
	Timestamp time.Time
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

// Info is the result of initialize plus new/load.
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
	var meta struct {
		Meta *struct {
			PiAcp *struct {
				StartupInfo bool `json:"startupInfo"`
				Notice      *struct {
					Kind  string `json:"kind"`
					Level string `json:"level"`
				} `json:"notice"`
				Notify *struct {
					Level string `json:"level"`
				} `json:"notify"`
				Replay *struct {
					Key       string          `json:"key"`
					Timestamp json.RawMessage `json:"timestamp"`
				} `json:"replay"`
			} `json:"piAcp"`
		} `json:"_meta"`
	}
	if json.Unmarshal(update, &meta) == nil && meta.Meta != nil && meta.Meta.PiAcp != nil {
		pi := meta.Meta.PiAcp
		ev.StartupInfo = pi.StartupInfo
		if pi.Notice != nil {
			ev.Notice = &Notice{Kind: pi.Notice.Kind, Level: pi.Notice.Level}
		} else if pi.Notify != nil {
			ev.Notice = &Notice{Kind: "extension", Level: pi.Notify.Level}
		}
		if pi.Replay != nil {
			ev.Replay = &Replay{Key: strings.TrimSpace(pi.Replay.Key)}
			if at, ok := replayTimestamp(pi.Replay.Timestamp); ok {
				ev.Replay.Timestamp = at
			}
		}
	}
	switch head.Kind {
	case UpdateMessageChunk, UpdateUserChunk, UpdateThoughtChunk:
		ev.Content = contentBlocks(update)
		ev.Text = blocksText(ev.Content)
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

func contentBlocks(raw json.RawMessage) []acp.ContentBlock {
	var update struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &update); err != nil || len(update.Content) == 0 {
		return nil
	}
	// A string is non-standard but harmless to accept.
	var text string
	if json.Unmarshal(update.Content, &text) == nil {
		return []acp.ContentBlock{{Type: "text", Text: text}}
	}
	var blocks []acp.ContentBlock
	if json.Unmarshal(update.Content, &blocks) == nil {
		for i := range blocks {
			if blocks[i].Type == "" {
				blocks[i].Type = "text"
			}
		}
		return blocks
	}
	var block acp.ContentBlock
	if json.Unmarshal(update.Content, &block) == nil {
		if block.Type == "" {
			block.Type = "text"
		}
		return []acp.ContentBlock{block}
	}
	return nil
}

func blocksText(blocks []acp.ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "" || block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

// replayTimestamp accepts the millisecond number pi-acp sends for a persisted
// message. An RFC3339 string is tolerated for forward compatibility.
func replayTimestamp(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	var ms int64
	if json.Unmarshal(raw, &ms) == nil && ms > 0 {
		return time.UnixMilli(ms), true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text)); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
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
