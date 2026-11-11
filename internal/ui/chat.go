package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

// Chat is one independent agent process and ACP session, together with the
// conversation it shows. Chats run in parallel: each owns its process,
// messages, composer and permission requests. Switching chats only changes
// which one is drawn.
//
// All fields are read and written on the UI thread; the only exceptions are
// the connect fields, which Shutdown may touch from another goroutine.
type Chat struct {
	ID    int
	Agent agent.LaunchSpec

	Workspace  string
	MCPServers []any

	Session        *session.Session
	Info           session.Info
	Connecting     bool
	Connected      bool
	Busy           string
	ConnectError   string
	WorkspaceError string

	// Stderr is the tail of this chat's agent stderr, shown in Diagnostics.
	// StderrBytes counts everything received, including the trimmed prefix.
	Stderr      string
	StderrBytes int64

	Messages   []*Message
	Input      string
	Streaming  bool
	Cancelling bool
	LastStop   string

	Commands    []acp.AvailableCommand
	Attachments []Attachment
	Plan        []session.PlanEntry
	PlanOpen    bool
	Usage       *acp.Usage
	TurnUsage   *acp.PromptUsage
	Title       string
	ConfigBusy  string

	// ComposerHeight is the draggable height of the composer card.
	ComposerHeight float32
	// ModelOpen, ModeOpen and MenuOpen are the popovers of the composer and
	// the sidebar; the view closes them when the chat is left.
	ModelOpen     bool
	ModelProvider string
	ModeOpen      bool
	MenuOpen      bool

	// pendingSend remembers a submit made while the chat was still a draft:
	// Connect runs first, then the prompt is sent.
	pendingSend bool

	gitWorkspace  string
	gitBranchName string

	Unread    bool
	Attention bool

	pendingSessionID     string
	pendingSessionAction string
	pendingSessionTitle  string
	replaying            bool

	Closed bool

	turn *turnState
	list mui.ListState

	connectMu     sync.Mutex
	connectGen    int
	connectCancel context.CancelFunc
}

// name is the label shown in the chat tabs.
func (c *Chat) name() string {
	if c == nil {
		return ""
	}
	if title := strings.TrimSpace(c.Title); title != "" {
		return truncate(singleLine(title), 48)
	}
	return fmt.Sprintf("Chat %d", c.ID)
}

func (c *Chat) agentName() string {
	if c == nil {
		return ""
	}
	if c.Agent.Name != "" {
		return c.Agent.Name
	}
	return c.Agent.ID
}

func (c *Chat) live() bool {
	return c != nil && (c.Session != nil || c.Connecting)
}

func (c *Chat) clearPendingSession() {
	if c == nil {
		return
	}
	c.pendingSessionID = ""
	c.pendingSessionAction = ""
	c.pendingSessionTitle = ""
}

// beginConnect cancels any older connect and returns a context whose
// generation identifies its callbacks.
func (c *Chat) beginConnect() (context.Context, int, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	c.connectMu.Lock()
	if c.connectCancel != nil {
		c.connectCancel()
	}
	c.connectGen++
	c.connectCancel = cancel
	gen := c.connectGen
	c.connectMu.Unlock()
	return ctx, gen, cancel
}

// cancelConnect invalidates callbacks of the current connect and cancels it.
func (c *Chat) cancelConnect() {
	c.connectMu.Lock()
	c.connectGen++
	cancel := c.connectCancel
	c.connectCancel = nil
	c.connectMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Chat) clearConnect(gen int) {
	c.connectMu.Lock()
	if c.connectGen == gen {
		c.connectCancel = nil
	}
	c.connectMu.Unlock()
}

func (c *Chat) connectCurrent(gen int) bool {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()
	return c.connectGen == gen
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// conversationState restores a chat when opening a saved session fails.
type conversationState struct {
	messages    []*Message
	turn        *turnState
	info        session.Info
	title       string
	commands    []acp.AvailableCommand
	plan        []session.PlanEntry
	planOpen    bool
	usage       *acp.Usage
	turnUsage   *acp.PromptUsage
	attachments []Attachment
	lastStop    string
}

func (c *Chat) snapshot() conversationState {
	return conversationState{
		messages:    c.Messages,
		turn:        c.turn,
		info:        c.Info,
		title:       c.Title,
		commands:    c.Commands,
		plan:        c.Plan,
		planOpen:    c.PlanOpen,
		usage:       c.Usage,
		turnUsage:   c.TurnUsage,
		attachments: c.Attachments,
		lastStop:    c.LastStop,
	}
}

func (c *Chat) restore(state conversationState) {
	c.Messages = state.messages
	c.turn = state.turn
	c.Info = state.info
	c.Title = state.title
	c.Commands = state.commands
	c.Plan = state.plan
	c.PlanOpen = state.planOpen
	c.Usage = state.usage
	c.TurnUsage = state.turnUsage
	c.Attachments = state.attachments
	c.LastStop = state.lastStop
	c.replaying = false
	c.Streaming = false
	c.Cancelling = false
}

// chatIndex returns the position of chat in the tabs.
func (a *App) chatIndex(chat *Chat) int {
	if chat == nil {
		return -1
	}
	for i, candidate := range a.Chats {
		if candidate == chat {
			return i
		}
	}
	return -1
}

// activeChat is the chat shown in the main area, or nil when all are closed.
func (a *App) activeChat() *Chat {
	if a.Active < 0 || a.Active >= len(a.Chats) {
		return nil
	}
	return a.Chats[a.Active]
}

// preferredAgent is the launch spec used for a new chat when the active chat
// has none.
func (a *App) preferredAgent() (agent.LaunchSpec, bool) {
	if chat := a.activeChat(); chat != nil && chat.Agent.ID != "" {
		return chat.Agent, true
	}
	if a.AgentIndex >= 0 && a.AgentIndex < len(a.Agents) {
		return a.Agents[a.AgentIndex], true
	}
	if len(a.Agents) > 0 {
		return a.Agents[0], true
	}
	return agent.LaunchSpec{}, false
}

func (a *App) defaultWorkspace() string {
	if chat := a.activeChat(); chat != nil && strings.TrimSpace(chat.Workspace) != "" {
		return chat.Workspace
	}
	if strings.TrimSpace(a.Workspace) != "" {
		return a.Workspace
	}
	return "."
}

func (a *App) newChatTabIn(workspace string) *Chat {
	chat := &Chat{
		ID:             a.nextChatID(),
		MCPServers:     append([]any(nil), a.MCPServers...),
		Workspace:      workspace,
		ComposerHeight: defaultComposerHeight,
	}
	if spec, ok := a.preferredAgent(); ok {
		chat.Agent = spec
	}
	if current := a.activeChat(); current != nil {
		chat.MCPServers = append([]any(nil), current.MCPServers...)
	}
	a.chatsMu.Lock()
	a.Chats = append(a.Chats, chat)
	a.Active = len(a.Chats) - 1
	a.chatsMu.Unlock()
	a.AgentIndex = a.agentIndex(chat.Agent.ID)
	a.Workspace = chat.Workspace
	a.ensureWorkspace(chat.Workspace)
	a.WorkspaceCollapsed[chat.Workspace] = false
	return chat
}

// newChatTab opens a draft chat in the current workspace.
func (a *App) newChatTab() *Chat {
	return a.newChatTabIn(a.defaultWorkspace())
}

// ensureChat returns the active chat, opening a draft tab when none exists.
func (a *App) ensureChat() *Chat {
	if chat := a.activeChat(); chat != nil {
		return chat
	}
	return a.newChatTab()
}

// selectChat makes chat active and clears its unread marker.
func (a *App) selectChat(chat *Chat) {
	index := a.chatIndex(chat)
	if index < 0 {
		return
	}
	if index == a.Active {
		chat.Unread = false
		chat.gitWorkspace = ""
		return
	}
	previous := a.activeChat()
	if previous != nil {
		previous.ModelOpen = false
		previous.ModeOpen = false
		previous.MenuOpen = false
	}
	a.Active = index
	chat.Unread = false
	chat.gitWorkspace = ""
	chat.ModelOpen = false
	chat.ModeOpen = false
	chat.MenuOpen = false
	a.AgentIndex = a.agentIndex(chat.Agent.ID)
	a.Workspace = chat.Workspace
	a.ensureWorkspace(chat.Workspace)
	a.WorkspaceCollapsed[chat.Workspace] = false
	a.persistSettings()
}

func (a *App) agentIndex(id string) int {
	for i, spec := range a.Agents {
		if spec.ID == id {
			return i
		}
	}
	return 0
}

// removeChat closes the chat's process and drops its tab.
func (a *App) removeChat(chat *Chat) {
	index := a.chatIndex(chat)
	if index < 0 || chat.Closed {
		return
	}
	chat.Closed = true
	chat.cancelConnect()
	chat.pendingSend = false
	a.cancelPermissionsFor(chat)
	if a.RenameChat == chat {
		a.RenameOpen = false
		a.RenameChat = nil
	}

	a.chatsMu.Lock()
	a.Chats = append(a.Chats[:index], a.Chats[index+1:]...)
	if a.Active >= len(a.Chats) {
		a.Active = len(a.Chats) - 1
	} else if a.Active > index {
		a.Active--
	}
	a.chatsMu.Unlock()
	if current := a.activeChat(); current != nil {
		current.Unread = false
		a.AgentIndex = a.agentIndex(current.Agent.ID)
		a.Workspace = current.Workspace
	}
	a.persistSettings()

	if s := chat.Session; s != nil {
		chat.Session = nil
		a.closeInBackground(chat, s)
	}
	a.resetConversation(chat)
}

func (a *App) chatsSnapshot() []*Chat {
	a.chatsMu.Lock()
	defer a.chatsMu.Unlock()
	return append([]*Chat(nil), a.Chats...)
}

func (a *App) activeBusy() bool {
	chat := a.activeChat()
	return chat != nil && (chat.Busy != "" || chat.Streaming)
}

// anyLiveChat reports whether any tab is connected or connecting.
func (a *App) anyLiveChat() bool {
	for _, chat := range a.Chats {
		if chat.live() {
			return true
		}
	}
	return false
}
