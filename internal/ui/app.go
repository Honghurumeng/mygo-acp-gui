// Package ui is the native MyGo interface. It owns the selected agent and
// workspace, the message list, and the permission dialog. Session callbacks
// never touch this state directly: they post through Window.Update (or a
// queue in tests), and the view only reads on the UI thread.
package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/logs"
	"github.com/reed/acp-gui/internal/session"
	"github.com/reed/acp-gui/internal/settings"
)

// Options configures the app. A non-nil Agents slice skips discovery, which
// lets tests and future catalogs inject launch specs.
type Options struct {
	Version   string
	Resources string
	Agents    []agent.LaunchSpec
	Workspace string
	Resolve   agent.ResolveOptions
	// DataDir enables settings.json persistence; empty keeps everything in
	// memory (tests, portable runs).
	DataDir string
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

	// Workspace is the draft workspace used when no chat tab exists. An
	// open chat carries its own workspace.
	Workspace      string
	WorkspaceError string

	Chats  []*Chat
	Active int

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

	SettingsWarning string

	DiagOpen bool
	DiagChat *Chat

	Permission      *permissionPrompt
	PermissionOpen  bool
	permissionQueue []*permissionPrompt

	MD markdown.Options

	chatsMu      sync.Mutex
	nextChat     int
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

	discoverMu     sync.Mutex
	discoverCancel context.CancelFunc

	injectedAgents bool
	resolveOpts    agent.ResolveOptions

	settings         *settings.Store
	settingsPaths    settings.Paths
	preferredAgentID string

	logDir     string
	logsMu     sync.Mutex
	logs       map[string]*logs.Log
	logsClosed bool

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
		Active:         -1,
		MD:             markdown.Options{},
		sessions:       make(map[*session.Session]struct{}),
		logs:           make(map[string]*logs.Log),
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
	a.loadSettings(opts.DataDir, strings.TrimSpace(opts.Workspace) == "")
	if opts.Agents != nil {
		a.AgentIndex = a.agentIndex(a.preferredAgentID)
	}
	if a.resolveOpts.Shell == "" {
		a.resolveOpts.Shell = firstNonEmpty(os.Getenv("ACP_GUI_SHELL"), a.settingsPaths.Shell)
	}
	if a.resolveOpts.NodePath == "" {
		a.resolveOpts.NodePath = firstNonEmpty(os.Getenv("ACP_GUI_NODE"), a.settingsPaths.Node)
	}
	if a.resolveOpts.PiPath == "" {
		a.resolveOpts.PiPath = firstNonEmpty(os.Getenv("ACP_GUI_PI"), a.settingsPaths.Pi)
	}
	if a.resolveOpts.CommandCodePath == "" {
		a.resolveOpts.CommandCodePath = firstNonEmpty(os.Getenv("ACP_GUI_COMMAND_CODE"), a.settingsPaths.CommandCode)
	}
	return a
}

// loadSettings restores saved preferences. Its warnings never block startup:
// defaults stay in place and the toolbar shows the problem.
func (a *App) loadSettings(dataDir string, workspaceDefault bool) {
	if strings.TrimSpace(dataDir) == "" {
		return
	}
	a.settings = settings.Open(filepath.Join(dataDir, "settings.json"))
	a.logDir = filepath.Join(dataDir, "logs")
	file, err := a.settings.Load()
	if err != nil {
		a.SettingsWarning = err.Error()
	}
	a.settingsPaths = file.Paths
	a.preferredAgentID = file.LastAgent
	if workspaceDefault && file.LastWorkspace != "" {
		a.Workspace = file.LastWorkspace
	}
	if len(file.MCPServers) > 0 {
		a.MCPServers = file.MCPServers
		a.MCPText = prettyJSON(file.MCPServers)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
		a.persistSettings()
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

		for _, chat := range a.chatsSnapshot() {
			chat.cancelConnect()
		}
		a.lifeCancel()
		a.cancelDiscovery()
		for _, s := range sessions {
			_ = s.Close()
		}
		a.cleanup.Wait()
		a.closeLogs()
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

func (a *App) nextChatID() int {
	a.nextChat++
	return a.nextChat
}

// View builds the window.
func (a *App) View(c *mui.Context) {
	chat := a.activeChat()
	if chat != nil {
		chat.list.Key = func(i int) any { return chat.Messages[i].ID }
		chat.list.FollowEnd = true
		chat.list.FollowAnimation = &mui.Smooth
	}

	mui.Column(c).Fill().Children(func() {
		a.toolbar(c)
		a.chatTabs(c)
		if chat == nil {
			mui.Column(c).Grow(1).Children(func() { a.emptyState(c) })
			return
		}
		mui.List(c, &chat.list, len(chat.Messages), func(i int) {
			a.message(c, chat.Messages[i])
		}).Grow(1).Padding(16, 20).Gap(16).Justify(mui.End).Children(func() {
			if len(chat.Messages) == 0 {
				a.emptyState(c)
			}
		})
		a.composer(c)
	})

	a.permissionDialog(c)
	a.historyDialog(c)
	a.settingsDialog(c)
	a.diagnosticsDialog(c)
}

// chatTabs draws the open chats. Each tab keeps its own process, so a
// background chat can stream while another is shown.
func (a *App) chatTabs(c *mui.Context) {
	t := c.Theme()
	var closeChat *Chat
	mui.ScrollHorizontal(c).Padding(0, 20, 6, 20).Children(func() {
		mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
			for i, chat := range a.Chats {
				i, chat := i, chat
				background := t.Background
				textColor := t.TextMuted
				if i == a.Active {
					background = t.Surface
					textColor = t.Text
				}
				tab := mui.ButtonBase(c).Padding(5, 8).Radius(8).Background(background)
				tab.Children(func() {
					mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
						if chat.Streaming {
							mui.Spinner(c)
						} else if chat.Attention {
							mui.Text(c, "!").FontSize(11).TextColor(t.Warning)
						} else if chat.Unread {
							mui.Text(c, "•").FontSize(11).TextColor(t.Accent)
						}
						mui.Text(c, chat.name()).FontSize(12).TextColor(textColor)
					})
				})
				if tab.Clicked() {
					a.selectChat(chat)
				}
				if mui.Button(c, "×").Label("Close " + chat.name()).FontSize(11).Clicked() {
					closeChat = chat
				}
			}
			if mui.Button(c, "+ New").Label("New chat tab").FontSize(12).Clicked() {
				a.newChatTab()
			}
		})
	})
	if closeChat != nil {
		a.removeChat(closeChat)
	}
}

func (a *App) toolbar(c *mui.Context) {
	t := c.Theme()
	chat := a.activeChat()
	locked := chat != nil && (chat.Connected || chat.Connecting)
	mui.Column(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Row(c).Gap(10).AlignItems(mui.Center).Padding(10, 20).Children(func() {
			mui.Text(c, "ACP GUI").FontSize(21).Bold()
			mui.Text(c, "v"+a.Version).FontSize(11).TextColor(t.TextMuted)
			mui.Spacer(c)
			if mui.Button(c, "Logs").Label("Diagnostics").FontSize(11).Clicked() {
				a.openDiagnostics()
			}
			mui.Text(c, a.statusLine()).FontSize(11).TextColor(t.TextMuted)
		})
		mui.Row(c).Gap(8).AlignItems(mui.Center).Padding(0, 20, 10, 20).Children(func() {
			mui.Text(c, "Agent").TextColor(t.TextMuted)
			names := a.agentNames()
			if len(names) == 0 {
				mui.Text(c, "none available").TextColor(t.Danger)
			} else {
				current := a.AgentIndex
				if current < 0 || current >= len(a.Agents) {
					current = 0
					a.AgentIndex = 0
				}
				if chat != nil && chat.Agent.ID != "" {
					current = a.agentIndex(chat.Agent.ID)
				}
				choice := a.Agents[current].Name
				if mui.Select(c, &choice, names).
					Label("Agent").
					Disabled(locked).
					Changed() {
					for i, name := range names {
						if name == choice {
							a.AgentIndex = i
							a.preferredAgentID = a.Agents[i].ID
							if chat != nil {
								chat.Agent = a.Agents[i]
								chat.WorkspaceError = ""
							}
							break
						}
					}
					a.persistSettings()
				}
			}

			mui.Text(c, "Workspace").TextColor(t.TextMuted)
			workspace := &a.Workspace
			if chat != nil {
				workspace = &chat.Workspace
			}
			ws := mui.TextInput(c, workspace).
				Placeholder("Absolute workspace path").
				Label("Workspace").
				Disabled(locked).
				Grow(1)
			if ws.Changed() {
				if chat != nil {
					chat.WorkspaceError = ""
				} else {
					a.WorkspaceError = ""
				}
			}
			if mui.Button(c, "Choose…").
				Disabled(locked).
				Clicked() {
				a.chooseWorkspace()
			}

			switch {
			case chat != nil && chat.Connected:
				if mui.Button(c, "Disconnect").Clicked() {
					a.Disconnect()
				}
			case chat != nil && chat.Connecting:
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
		if chat != nil {
			a.sessionControls(c, chat)
			a.configControls(c, chat)
		}
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
		if a.SettingsWarning != "" {
			mui.Text(c, "Settings: "+a.SettingsWarning).
				FontSize(11).
				TextColor(t.Warning).
				Padding(0, 20, 8, 20).
				Selectable()
		}
	})
}

func (a *App) statusLine() string {
	chat := a.activeChat()
	switch {
	case a.Resolving:
		return "Finding agents…"
	case chat != nil && chat.Connecting:
		return "Connecting to " + chat.agentName() + "…"
	case chat != nil && chat.Session != nil:
		name := "agent"
		version := ""
		if chat.Info.AgentInfo != nil {
			if chat.Info.AgentInfo.Title != "" {
				name = chat.Info.AgentInfo.Title
			} else if chat.Info.AgentInfo.Name != "" {
				name = chat.Info.AgentInfo.Name
			}
			version = chat.Info.AgentInfo.Version
		}
		status := "Connected to " + name
		if version != "" {
			status += " " + version
		}
		if chat.Info.SessionID == "" {
			return status + " · no active session"
		}
		return status + " · " + chat.Info.SessionID
	case a.Env.Summary() != "":
		return a.Env.Summary()
	default:
		return ""
	}
}

func (a *App) errorLine() string {
	if chat := a.activeChat(); chat != nil {
		switch {
		case chat.ConnectError != "":
			return chat.ConnectError
		case chat.WorkspaceError != "":
			return chat.WorkspaceError
		}
	}
	switch {
	case a.ResolveError != "":
		return a.ResolveError
	case a.WorkspaceError != "":
		return a.WorkspaceError
	default:
		return ""
	}
}

func (a *App) emptyState(c *mui.Context) {
	t := c.Theme()
	chat := a.activeChat()
	mui.Column(c).Fill().Center().Gap(8).Children(func() {
		switch {
		case chat != nil && chat.Session != nil && chat.Info.SessionID == "":
			mui.Text(c, "No active session.").FontSize(16)
			mui.Text(c, "Create a new chat or open one from History.").FontSize(12).TextColor(t.TextMuted)
		case chat != nil && chat.Session != nil:
			mui.Text(c, "Connected. Send a message to start.").FontSize(16)
			mui.Text(c, "Agent updates stream into this list.").FontSize(12).TextColor(t.TextMuted)
		case chat != nil && chat.Connecting:
			mui.Text(c, "Connecting…").FontSize(14).TextColor(t.TextMuted)
		case a.Resolving:
			mui.Text(c, "Probing pi and Command Code…").FontSize(14).TextColor(t.TextMuted)
		case len(a.Agents) == 0:
			mui.Text(c, "No ACP agent available").FontSize(16)
			mui.Text(c, "Install pi or Command Code, then restart or reconnect.").FontSize(12).TextColor(t.TextMuted)
		default:
			mui.Text(c, "Choose an agent and workspace, then Connect.").FontSize(16)
			mui.Text(c, "Open another chat with + New; each chat keeps its own agent process.").FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *App) composer(c *mui.Context) {
	chat := a.activeChat()
	if chat == nil {
		return
	}
	t := c.Theme()
	mui.Column(c).Gap(8).Padding(12, 20).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		a.planPanel(c, chat)
		a.attachmentPicker(c, chat)
		if chat.LastStop != "" {
			mui.Text(c, "Last turn: "+chat.LastStop).FontSize(10).TextColor(t.TextMuted)
		}

		mui.Row(c).Gap(8).AlignItems(mui.End).Children(func() {
			if mui.Button(c, "Image…").
				Disabled(!chat.canAttach()).
				Clicked() {
				a.chooseImages()
			}
			commands := mui.Button(c, "Commands")
			commands.Disabled(!chat.canType() || len(chat.Commands) == 0)
			if commands.Clicked() {
				chat.CommandsOpen = !chat.CommandsOpen
			}
			mui.Popover(c, commands, &chat.CommandsOpen, func() {
				a.commandsPanel(c, chat)
			})

			input := mui.TextArea(c, &chat.Input).
				Placeholder("Message").
				Label("Message").
				Lines(1, 6).
				Grow(1).
				Disabled(!chat.canType()).
				OnTextKey(func(k mui.TextKey) bool {
					if k.Key == mui.KeyEnter && k.Mods == 0 {
						chat.Input = k.Text()
						if a.send() {
							k.SetText("")
						}
						return true
					}
					return false
				})
			_ = input

			if chat.Streaming {
				label := "Stop"
				if chat.Cancelling {
					label = "Stopping…"
				}
				if mui.PrimaryButton(c, label).Disabled(chat.Cancelling).Clicked() {
					a.stop()
				}
			} else if mui.PrimaryButton(c, "Send").
				Disabled(!chat.canSend() || !chat.hasPrompt()).
				Clicked() {
				a.send()
			}
		})
		hint := "Enter sends · Shift+Enter adds a line"
		if chat.Session != nil && chat.Info.AgentCapabilities.PromptCapabilities.Image {
			hint += " · up to 4 images"
		}
		mui.Text(c, hint).FontSize(10).TextColor(t.TextMuted)
	})
}

func (a *App) planPanel(c *mui.Context, chat *Chat) {
	if len(chat.Plan) == 0 {
		return
	}
	t := c.Theme()
	mui.Collapsible(c, "Plan", &chat.PlanOpen, func() {
		mui.Column(c).Gap(3).Children(func() {
			for _, entry := range chat.Plan {
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

func (a *App) commandsPanel(c *mui.Context, chat *Chat) {
	t := c.Theme()
	mui.Column(c).Width(360).Gap(4).Children(func() {
		mui.Text(c, "Commands").FontSize(12).Bold()
		mui.Scroll(c).MaxHeight(320).Gap(2).Children(func() {
			for _, command := range chat.Commands {
				command := command
				label := "/" + command.Name
				button := mui.Button(c, label)
				if button.Clicked() {
					draft := strings.TrimSpace(chat.Input)
					chat.Input = label
					if command.Input != nil || draft != "" {
						chat.Input += " "
					}
					chat.Input += draft
					chat.CommandsOpen = false
				}
				if command.Description != "" {
					mui.Text(c, truncate(command.Description, 120)).FontSize(10).TextColor(t.TextMuted)
				}
			}
		})
	})
}

func (c *Chat) canAttach() bool {
	return c.Session != nil && c.Info.SessionID != "" && !c.Connecting && c.Busy == "" &&
		c.Info.AgentCapabilities.PromptCapabilities.Image
}

func (c *Chat) canType() bool {
	return c.Session != nil && c.Info.SessionID != "" && !c.Connecting && c.Busy == ""
}

func (c *Chat) canSend() bool {
	return c.canType() && !c.Streaming && c.ConfigBusy == ""
}

func (c *Chat) hasPrompt() bool {
	return strings.TrimSpace(c.Input) != "" || len(c.Attachments) > 0
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

func (a *App) addMessage(chat *Chat, m *Message) *Message {
	if chat == nil {
		return m
	}
	chat.Messages = append(chat.Messages, m)
	return m
}

func (a *App) addError(chat *Chat, err error) {
	if chat == nil || err == nil {
		return
	}
	msg := err.Error()
	if strings.TrimSpace(msg) == "" {
		return
	}
	// Keep the chat readable when an agent crashes repeatedly.
	if n := len(chat.Messages); n > 0 {
		last := chat.Messages[n-1]
		if last.Role == RoleError && last.Text == msg {
			return
		}
	}
	a.addMessage(chat, &Message{ID: a.nextID(), Role: RoleError, Text: msg, Done: true})
	if chat != a.activeChat() {
		chat.Unread = true
	}
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
