package acp

import "encoding/json"

// ContentBlock is one piece of a prompt or a session update. Only text is
// used by the app today; the other fields keep the protocol types honest.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
}

// NewSessionParams creates a session. Cwd must be an absolute path.
type NewSessionParams struct {
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

// PromptParams sends one user turn.
type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResult is the end of a turn. StopReason is "end_turn",
// "max_tokens", "refusal", "cancelled" and similar.
type PromptResult struct {
	StopReason string          `json:"stopReason"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// CancelParams is the session/cancel notification.
type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// SessionUpdateParams is the payload of session/update. Update is kept raw:
// ACP has several update kinds and adds more over time.
type SessionUpdateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}
