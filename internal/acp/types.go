package acp

import "encoding/json"

// ProtocolVersion is the ACP protocol version this client speaks.
const ProtocolVersion = 1

// Implementation identifies a client or an agent in initialize.
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// FileSystemCapabilities mirrors ACP's fs client capabilities.
type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// ClientCapabilities is sent in initialize. The GUI deliberately advertises
// no filesystem or terminal access: agents work in the workspace themselves
// and the client only renders updates and asks the user about permissions.
type ClientCapabilities struct {
	Fs       FileSystemCapabilities `json:"fs"`
	Terminal bool                   `json:"terminal"`
}

// DefaultClientCapabilities returns the fixed capabilities of this client.
func DefaultClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		Fs: FileSystemCapabilities{
			ReadTextFile:  false,
			WriteTextFile: false,
		},
		Terminal: false,
	}
}

// InitializeParams is the request sent to an agent on connect.
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
}

// AuthMethod is an agent-provided way to authenticate (e.g. pi's terminal
// login). Args are the arguments of the login command.
type AuthMethod struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// InitializeResult is the agent's initialize response. Capabilities are kept
// raw: callers only need a few fields now, and unknown fields must not fail.
type InitializeResult struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	AgentInfo         *Implementation `json:"agentInfo,omitempty"`
	AgentCapabilities json.RawMessage `json:"agentCapabilities,omitempty"`
	AuthMethods       []AuthMethod    `json:"authMethods,omitempty"`
}
