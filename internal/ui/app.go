// Package ui is the native MyGo interface. It owns the selected agent and
// workspace, the message list, and the permission dialog. Session callbacks
// never touch this state directly: they post through Window.Update (or a
// queue in tests), and the view only reads on the UI thread.
package ui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

// Options configures the app. A non-nil Agents slice skips discovery, which
// lets tests and future catalogs inject launch specs.
type Options struct {
	Version   string
	Resources string
	Agents    []agent.LaunchSpec
	Workspace string
	Resolve   agent.ResolveOptions
}

// MessageRole is how a chat row is drawn.
type MessageRole int

const (
	RoleUser MessageRole = iota
	RoleAssistant
	RoleTool
	RoleSystem
	RoleError
)

// Message is one row in the chat list.
type Message struct {
	ID          int
	Role        MessageRole
	Text        string
	Thought     string
	Done        bool
	ShowThought bool
	Title       string
	Status      string
	ToolCallID  string
	Detail      string
	Attachments []Attachment
}

// Attachment is an image attached to a prompt.
type Attachment struct {
	Name     string
	MimeType string
	Data     string
	Bitmap   *mui.Bitmap
}

// AgentStatus is one row of discovery/probe state.
type AgentStatus struct {
	ID          string
	Name        string
	Status      string
	Detail      string
	InstallHint string
}

// Agent status values.
const (
	AgentProbing   = "probing"
	AgentReady     = "ready"
	AgentNeedsAuth = "needs-auth"
	AgentNotACP    = "installed-not-acp"
	AgentMissing   = "not-installed"
	AgentError     = "error"
)

type turnState struct {
	assistant *Message
	tools     map[string]*Message
}

// App is the root UI state.
type App struct {
	Version       string
	Resources     string
	Agents        []agent.LaunchSpec
	AgentIndex    int
	AgentStatuses []AgentStatus
	Env           agent.Environment
	Resolving     bool
	ResolveError  string

	Workspace      string
	WorkspaceError string

	Session      *session.Session
	Info         session.Info
	Connecting   bool
	Connected    bool
	ConnectError string
	SessionBusy  string

	Messages   []*Message
	Input      string
	Streaming  bool
	Cancelling bool
	LastStop   string

	Commands     []acp.AvailableCommand
	CommandsOpen bool
	Attachments  []Attachment
	Plan         []session.PlanEntry
	PlanOpen     bool
	Usage        *acp.Usage
	Title        string

	ConfigBusy   string
	MCPText      string
	MCPServers   []any
	MCPError     string
	SettingsOpen bool

	HistoryOpen     bool
	HistoryLoading  bool
	HistoryError    string
	HistoryAll      bool
	HistorySessions []acp.SessionSummary
	HistorySelected int
	HistoryList     mui.ListState

	pendingSessionID     string
	pendingSessionAction string
	pendingSessionTitle  string
	replaying            bool

	Permission     *permissionPrompt
	PermissionOpen bool

	MD   markdown.Options
	list mui.ListState

	win          *mygo.Window
	postMu       sync.Mutex
	pending      []func()
	closed       atomic.Bool
	shutdownOnce sync.Once
	cleanup      sync.WaitGroup

	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	sessionsMu sync.Mutex
	sessions   map[*session.Session]struct{}

	connectMu     sync.Mutex
	connectCancel context.CancelFunc
	connectGen    int

	discoverMu     sync.Mutex
	discoverCancel context.CancelFunc

	injectedAgents bool
	resolveOpts    agent.ResolveOptions

	turn *turnState
	next int
}

// New creates the root state. Discovery starts when Attach is called, or
// immediately for injected agents.
func New(opts Options) *App {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		if home, err := os.UserHomeDir(); err == nil {
			workspace = home
		}
	}
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	a := &App{
		Version:        version,
		lifeCtx:        lifeCtx,
		lifeCancel:     lifeCancel,
		Resources:      opts.Resources,
		Workspace:      workspace,
		MD:             markdown.Options{},
		sessions:       make(map[*session.Session]struct{}),
		resolveOpts:    opts.Resolve,
		injectedAgents: opts.Agents != nil,
	}
	if opts.Agents != nil {
		a.Agents = append([]agent.LaunchSpec(nil), opts.Agents...)
		a.Resolving = false
		for _, spec := range a.Agents {
			a.AgentStatuses = append(a.AgentStatuses, AgentStatus{ID: spec.ID, Name: spec.Name, Status: AgentReady})
		}
	} else {
		a.Resolving = true
	}
	a.MCPServers = []any{}
	a.MCPText = "[]"
	if a.resolveOpts.Shell == "" {
		a.resolveOpts.Shell = os.Getenv("ACP_GUI_SHELL")
	}
	if a.resolveOpts.NodePath == "" {
		a.resolveOpts.NodePath = os.Getenv("ACP_GUI_NODE")
	}
	if a.resolveOpts.PiPath == "" {
		a.resolveOpts.PiPath = os.Getenv("ACP_GUI_PI")
	}
	if a.resolveOpts.CommandCodePath == "" {
		a.resolveOpts.CommandCodePath = os.Getenv("ACP_GUI_COMMAND_CODE")
	}
	return a
}

// Attach gives the app its window and starts discovery once. Call it on the
// main thread after the window exists.
func (a *App) Attach(win *mygo.Window) {
	a.postMu.Lock()
	a.win = win
	a.postMu.Unlock()
	if !a.injectedAgents {
		a.startDiscovery()
	}
}

func (a *App) window() *mygo.Window {
	a.postMu.Lock()
	defer a.postMu.Unlock()
	return a.win
}

// Post runs fn on the UI thread and redraws. With no window (tests), fn is
// queued for Flush.
func (a *App) Post(fn func()) {
	if fn == nil || a.closed.Load() {
		return
	}
	a.postMu.Lock()
	win := a.win
	if win == nil {
		a.pending = append(a.pending, fn)
		a.postMu.Unlock()
		return
	}
	a.postMu.Unlock()
	win.Update(fn)
}

// Flush applies queued updates in tests that have no window.
func (a *App) Flush() {
	a.postMu.Lock()
	pending := a.pending
	a.pending = nil
	a.postMu.Unlock()
	for _, fn := range pending {
		fn()
	}
}

// Shutdown stops a pending connect and closes every agent process group.
// It is safe to call from OnClose and OnWillQuit.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		// Set closed and take the session snapshot under the same lock
		// trackSession uses, so a session cannot slip in after the
		// snapshot and escape cleanup.
		a.sessionsMu.Lock()
		a.closed.Store(true)
		sessions := make([]*session.Session, 0, len(a.sessions))
		for s := range a.sessions {
			sessions = append(sessions, s)
		}
		a.sessionsMu.Unlock()

		a.connectMu.Lock()
		a.connectGen++
		cancel := a.connectCancel
		a.connectCancel = nil
		a.connectMu.Unlock()
		if cancel != nil {
			cancel()
		}
		a.lifeCancel()
		a.cancelDiscovery()
		for _, s := range sessions {
			_ = s.Close()
		}
		a.cleanup.Wait()
	})
}

// trackSession registers a session before it is posted to the UI so that a
// concurrent shutdown can still clean it up.
func (a *App) trackSession(s *session.Session) bool {
	a.sessionsMu.Lock()
	if a.closed.Load() {
		a.sessionsMu.Unlock()
		// Shutdown may already be waiting for connect goroutines. Close
		// synchronously so the process group is gone when it returns.
		_ = s.Close()
		return false
	}
	a.sessions[s] = struct{}{}
	a.sessionsMu.Unlock()
	return true
}

func (a *App) untrackSession(s *session.Session) {
	a.sessionsMu.Lock()
	delete(a.sessions, s)
	a.sessionsMu.Unlock()
}

func (a *App) currentAgent() (agent.LaunchSpec, bool) {
	if a.AgentIndex < 0 || a.AgentIndex >= len(a.Agents) {
		return agent.LaunchSpec{}, false
	}
	return a.Agents[a.AgentIndex], true
}

func (a *App) agentNames() []string {
	names := make([]string, 0, len(a.Agents))
	for _, spec := range a.Agents {
		names = append(names, spec.Name)
	}
	return names
}

func (a *App) nextID() int {
	a.next++
	return a.next
}

// View builds the window.
func (a *App) View(c *mui.Context) {
	a.list.Key = func(i int) any { return a.Messages[i].ID }
	a.list.FollowEnd = true
	a.list.FollowAnimation = &mui.Smooth

	mui.Column(c).Fill().Children(func() {
		a.toolbar(c)
		mui.List(c, &a.list, len(a.Messages), func(i int) {
			a.message(c, a.Messages[i])
		}).Grow(1).Padding(16, 20).Gap(16).Justify(mui.End).Children(func() {
			if len(a.Messages) == 0 {
				a.emptyState(c)
			}
		})
		a.composer(c)
	})

	a.permissionDialog(c)
	a.historyDialog(c)
	a.settingsDialog(c)
}

func (a *App) toolbar(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Row(c).Gap(10).AlignItems(mui.Center).Padding(10, 20).Children(func() {
			mui.Text(c, "ACP GUI").FontSize(21).Bold()
			mui.Text(c, "v"+a.Version).FontSize(11).TextColor(t.TextMuted)
			mui.Spacer(c)
			mui.Text(c, a.statusLine()).FontSize(11).TextColor(t.TextMuted)
		})
		mui.Row(c).Gap(8).AlignItems(mui.Center).Padding(0, 20, 10, 20).Children(func() {
			mui.Text(c, "Agent").TextColor(t.TextMuted)
			names := a.agentNames()
			if len(names) == 0 {
				mui.Text(c, "none available").TextColor(t.Danger)
			} else {
				if a.AgentIndex < 0 || a.AgentIndex >= len(a.Agents) {
					a.AgentIndex = 0
				}
				choice := a.Agents[a.AgentIndex].Name
				if mui.Select(c, &choice, names).
					Label("Agent").
					Disabled(a.Connected || a.Connecting).
					Changed() {
					for i, name := range names {
						if name == choice {
							a.AgentIndex = i
							break
						}
					}
				}
			}

			mui.Text(c, "Workspace").TextColor(t.TextMuted)
			ws := mui.TextInput(c, &a.Workspace).
				Placeholder("Absolute workspace path").
				Label("Workspace").
				Disabled(a.Connected || a.Connecting).
				Grow(1)
			if ws.Changed() {
				a.WorkspaceError = ""
			}
			if mui.Button(c, "Choose…").
				Disabled(a.Connected || a.Connecting).
				Clicked() {
				a.chooseWorkspace()
			}

			switch {
			case a.Connected:
				if mui.Button(c, "Disconnect").Clicked() {
					a.Disconnect()
				}
			case a.Connecting:
				mui.PrimaryButton(c, "Connecting…").Disabled(true)
				if mui.Button(c, "Cancel").Clicked() {
					a.Disconnect()
				}
			default:
				if mui.Button(c, "History").
					Disabled(a.Resolving || len(a.Agents) == 0).
					Clicked() {
					a.openHistory()
				}
				if mui.Button(c, "MCP…").Clicked() {
					a.openMCPSettings()
				}
				if !a.Resolving && len(a.Agents) == 0 {
					if mui.Button(c, "Retry discovery").Clicked() {
						a.retryDiscovery()
					}
				}
				if mui.PrimaryButton(c, "Connect").
					Disabled(a.Resolving || len(a.Agents) == 0).
					Clicked() {
					a.Connect()
				}
			}
		})
		a.agentStatusRow(c)
		a.sessionControls(c)
		a.configControls(c)
		if msg := a.errorLine(); msg != "" {
			mui.Text(c, msg).FontSize(12).TextColor(t.Danger).Padding(0, 20, 8, 20).Selectable()
		}
		if len(a.Env.Warnings) > 0 {
			mui.Text(c, strings.Join(a.Env.Warnings, "; ")).
				FontSize(11).
				TextColor(t.Warning).
				Padding(0, 20, 8, 20).
				Selectable()
		}
	})
}

func (a *App) statusLine() string {
	switch {
	case a.Resolving:
		return "Finding agents…"
	case a.Connecting:
		if spec, ok := a.currentAgent(); ok {
			return "Connecting to " + spec.Name + "…"
		}
		return "Connecting…"
	case a.Session != nil:
		name := "agent"
		version := ""
		if a.Info.AgentInfo != nil {
			if a.Info.AgentInfo.Title != "" {
				name = a.Info.AgentInfo.Title
			} else if a.Info.AgentInfo.Name != "" {
				name = a.Info.AgentInfo.Name
			}
			version = a.Info.AgentInfo.Version
		}
		status := "Connected to " + name
		if version != "" {
			status += " " + version
		}
		if a.Info.SessionID == "" {
			return status + " · no active session"
		}
		return status + " · " + a.Info.SessionID
	case a.Env.Summary() != "":
		return a.Env.Summary()
	default:
		return ""
	}
}

func (a *App) errorLine() string {
	switch {
	case a.ResolveError != "":
		return a.ResolveError
	case a.ConnectError != "":
		return a.ConnectError
	case a.WorkspaceError != "":
		return a.WorkspaceError
	default:
		return ""
	}
}

func (a *App) emptyState(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Fill().Center().Gap(8).Children(func() {
		switch {
		case a.Session != nil && a.Info.SessionID == "":
			mui.Text(c, "No active session.").FontSize(16)
			mui.Text(c, "Create a new chat or open one from History.").FontSize(12).TextColor(t.TextMuted)
		case a.Session != nil:
			mui.Text(c, "Connected. Send a message to start.").FontSize(16)
			mui.Text(c, "Agent updates stream into this list.").FontSize(12).TextColor(t.TextMuted)
		case a.Connecting:
			mui.Text(c, "Connecting…").FontSize(14).TextColor(t.TextMuted)
		case a.Resolving:
			mui.Text(c, "Probing pi and Command Code…").FontSize(14).TextColor(t.TextMuted)
		case len(a.Agents) == 0:
			mui.Text(c, "No ACP agent available").FontSize(16)
			mui.Text(c, "Install pi or Command Code, then restart or reconnect.").FontSize(12).TextColor(t.TextMuted)
		default:
			mui.Text(c, "Choose an agent and workspace, then Connect.").FontSize(16)
			mui.Text(c, "The chat appears here.").FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *App) composer(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Gap(8).Padding(12, 20).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		a.planPanel(c)
		a.attachmentPicker(c)
		if a.LastStop != "" {
			mui.Text(c, "Last turn: "+a.LastStop).FontSize(10).TextColor(t.TextMuted)
		}

		mui.Row(c).Gap(8).AlignItems(mui.End).Children(func() {
			if mui.Button(c, "Image…").
				Disabled(!a.canAttach()).
				Clicked() {
				a.chooseImages()
			}
			commands := mui.Button(c, "Commands")
			commands.Disabled(a.canType() == false || len(a.Commands) == 0)
			if commands.Clicked() {
				a.CommandsOpen = !a.CommandsOpen
			}
			mui.Popover(c, commands, &a.CommandsOpen, func() {
				a.commandsPanel(c)
			})

			input := mui.TextArea(c, &a.Input).
				Placeholder("Message").
				Label("Message").
				Lines(1, 6).
				Grow(1).
				Disabled(!a.canType()).
				OnTextKey(func(k mui.TextKey) bool {
					if k.Key == mui.KeyEnter && k.Mods == 0 {
						a.Input = k.Text()
						if a.send() {
							k.SetText("")
						}
						return true
					}
					return false
				})
			_ = input

			if a.Streaming {
				label := "Stop"
				if a.Cancelling {
					label = "Stopping…"
				}
				if mui.PrimaryButton(c, label).Disabled(a.Cancelling).Clicked() {
					a.stop()
				}
			} else if mui.PrimaryButton(c, "Send").
				Disabled(!a.canSend() || !a.hasPrompt()).
				Clicked() {
				a.send()
			}
		})
		hint := "Enter sends · Shift+Enter adds a line"
		if a.Session != nil && a.Info.AgentCapabilities.PromptCapabilities.Image {
			hint += " · up to 4 images"
		}
		mui.Text(c, hint).FontSize(10).TextColor(t.TextMuted)
	})
}

func (a *App) planPanel(c *mui.Context) {
	if len(a.Plan) == 0 {
		return
	}
	t := c.Theme()
	mui.Collapsible(c, "Plan", &a.PlanOpen, func() {
		mui.Column(c).Gap(3).Children(func() {
			for _, entry := range a.Plan {
				color := t.TextMuted
				switch entry.Status {
				case "completed":
					color = t.Success
				case "in_progress":
					color = t.Warning
				case "pending":
					color = t.TextMuted
				}
				text := "• " + entry.Content
				if entry.Status != "" {
					text += " · " + entry.Status
				}
				mui.Text(c, text).FontSize(11).TextColor(color)
			}
		})
	})
}

func (a *App) commandsPanel(c *mui.Context) {
	t := c.Theme()
	mui.Column(c).Width(360).Gap(4).Children(func() {
		mui.Text(c, "Commands").FontSize(12).Bold()
		mui.Scroll(c).MaxHeight(320).Gap(2).Children(func() {
			for _, command := range a.Commands {
				command := command
				label := "/" + command.Name
				button := mui.Button(c, label)
				if button.Clicked() {
					draft := strings.TrimSpace(a.Input)
					a.Input = label
					if command.Input != nil || draft != "" {
						a.Input += " "
					}
					a.Input += draft
					a.CommandsOpen = false
				}
				if command.Description != "" {
					mui.Text(c, truncate(command.Description, 120)).FontSize(10).TextColor(t.TextMuted)
				}
			}
		})
	})
}

func (a *App) canAttach() bool {
	return a.Session != nil && a.Info.SessionID != "" && !a.Connecting && a.SessionBusy == "" &&
		a.Info.AgentCapabilities.PromptCapabilities.Image
}

func (a *App) canType() bool {
	return a.Session != nil && a.Info.SessionID != "" && !a.Connecting && a.SessionBusy == ""
}

func (a *App) canSend() bool {
	return a.canType() && a.ConfigBusy == ""
}

func (a *App) message(c *mui.Context, m *Message) {
	t := c.Theme()
	switch m.Role {
	case RoleUser:
		mui.Row(c).Children(func() {
			mui.Spacer(c)
			mui.Column(c).Gap(6).MaxWidth(520).Children(func() {
				if m.Text != "" {
					mui.Text(c, m.Text).
						Selectable().
						Padding(8, 12).
						Radius(14).
						Background(t.Surface)
				}
				a.messageAttachments(c, m.Attachments)
			})
		})
	case RoleAssistant:
		mui.Column(c).MaxWidth(760).Children(func() {
			if m.Thought != "" {
				mui.Collapsible(c, "Thinking", &m.ShowThought, func() {
					mui.Text(c, m.Thought).
						TextColor(t.TextMuted).
						FontSize(12).
						Selectable()
				})
			}
			if m.Text == "" && !m.Done {
				mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
					mui.Spinner(c)
					mui.Text(c, "Waiting for the agent…").TextColor(t.TextMuted).FontSize(12)
				})
				return
			}
			if m.Done {
				markdown.View(c, m.Text, &a.MD)
			} else {
				markdown.Stream(c, m.Text, &a.MD)
			}
		})
	case RoleTool:
		statusColor := t.TextMuted
		switch m.Status {
		case "completed":
			statusColor = t.Success
		case "failed":
			statusColor = t.Danger
		case "in_progress", "pending":
			statusColor = t.Warning
		}
		mui.Column(c).Gap(3).Padding(8, 10).Radius(8).Background(t.Surface).Children(func() {
			mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
				mui.Text(c, m.Title).Bold().FontSize(12)
				if m.Status != "" {
					mui.Text(c, m.Status).FontSize(11).TextColor(statusColor)
				}
			})
			if m.Detail != "" {
				mui.Text(c, m.Detail).FontSize(11).TextColor(t.TextMuted).Selectable()
			}
		})
	case RoleError:
		mui.Text(c, "⚠ "+m.Text).TextColor(t.Danger).Selectable()
	case RoleSystem:
		mui.Text(c, m.Text).TextColor(t.TextMuted).TextAlign(mui.Center)
	}
}

func (a *App) addMessage(m *Message) *Message {
	a.Messages = append(a.Messages, m)
	return m
}

func (a *App) addError(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.TrimSpace(msg) == "" {
		return
	}
	// Keep the chat readable when an agent crashes repeatedly.
	if n := len(a.Messages); n > 0 {
		last := a.Messages[n-1]
		if last.Role == RoleError && last.Text == msg {
			return
		}
	}
	a.addMessage(&Message{ID: a.nextID(), Role: RoleError, Text: msg, Done: true})
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > 240 {
		text = text[:240] + "…"
	}
	return text
}

func toolDetail(call session.ToolCall) string {
	if text := rawText(call.Content); text != "" {
		return truncate(text, 2000)
	}
	if text := rawText(call.RawOutput); text != "" {
		return truncate(text, 2000)
	}
	return compactJSON(call.RawInput)
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var block acp.ContentBlock
	if json.Unmarshal(raw, &block) == nil && block.Text != "" {
		return block.Text
	}
	var blocks []acp.ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, block := range blocks {
			if block.Type == "text" || block.Type == "content" {
				b.WriteString(block.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	var wrapped []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &wrapped) == nil {
		var b strings.Builder
		for _, item := range wrapped {
			b.WriteString(rawText(item.Content))
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}
