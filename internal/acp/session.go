package acp

import "encoding/json"

// ContentBlock is one piece of a prompt or a session update. ACP accepts
// text, image, audio, resource_link and embedded resource blocks here; the
// fields not used by a kind stay empty. Images carry base64 data, resource
// links point at a file, and embedded resources carry its text inline.
type ContentBlock struct {
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	MimeType    string            `json:"mimeType,omitempty"`
	Data        string            `json:"data,omitempty"`
	URI         string            `json:"uri,omitempty"`
	Name        string            `json:"name,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Size        int64             `json:"size,omitempty"`
	Resource    *ResourceContents `json:"resource,omitempty"`
}

// ResourceContents is the contents of an embedded `resource` block. ACP
// calls text contents text and binary contents blob; the GUI only embeds
// UTF-8 text and uses resource links for everything else.
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text"`
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
// "max_tokens", "refusal", "cancelled" and similar. Usage is the optional
// per-turn token report; it is not the context window (usage_update carries
// used/size).
type PromptResult struct {
	StopReason string          `json:"stopReason"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// PromptUsage is the per-turn token report some agents return with
// session/prompt. Fields are zero when the agent does not report them.
type PromptUsage struct {
	TotalTokens       int `json:"totalTokens"`
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	ThoughtTokens     int `json:"thoughtTokens,omitempty"`
	CachedReadTokens  int `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens int `json:"cachedWriteTokens,omitempty"`
}

// Tokens decodes the optional usage block. ok is false when it is absent,
// malformed, or reports no tokens at all.
func (r PromptResult) Tokens() (PromptUsage, bool) {
	if len(r.Usage) == 0 || string(r.Usage) == "null" {
		return PromptUsage{}, false
	}
	var usage PromptUsage
	if err := json.Unmarshal(r.Usage, &usage); err != nil {
		return PromptUsage{}, false
	}
	if usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.ThoughtTokens == 0 {
		return PromptUsage{}, false
	}
	return usage, true
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
