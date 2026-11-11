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
	"time"

	cryfeedback "github.com/ELY-Open-Source/CrychicUI/feedback"
	crylayout "github.com/ELY-Open-Source/CrychicUI/layout"
	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/archive"
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
	// ProbeConfig reads an agent's session config options during discovery
	// and when the draft model panel needs a refresh. Nil uses
	// session.ProbeConfig; tests can replace it.
	ProbeConfig func(context.Context, agent.LaunchSpec) (session.Info, error)
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
	// AgentConfigs caches each ready agent's session config options (the
	// model/thinking selects behind the composer chip) from a throwaway
	// session/new during discovery, so a draft can pick them before it
	// connects. Keyed by agent ID. The cache is refreshed from live sessions
	// and by reopening the model panel; ACP has no provider-readiness call,
	// so the agent's current model list is the only availability signal.
	AgentConfigs map[string][]acp.ConfigOption
	Env          agent.Environment
	Resolving    bool
	ResolveError string

	// Workspace is the draft workspace used when no chat exists. An open
	// chat carries its own workspace.
	Workspace      string
	WorkspaceError string

	// Workspaces is the ordered list of folders shown in the sidebar.
	// Collapsed and Expanded hold the sidebar state of each workspace.
	// RemovedWorkspaces soft-deletes groups: they stay in the list (and in
	// settings.json) but are hidden until they are shown again.
	Workspaces            []string
	WorkspaceCollapsed    map[string]bool
	WorkspaceExpanded     map[string]bool
	RemovedWorkspaces     map[string]bool
	ShowRemovedWorkspaces bool
	WorkspaceMenuOpen     string
	SidebarOpen           bool
	GitPanelOpen          bool
	Git                   gitPanelState

	Chats  []*Chat
	Active int

	RenameOpen      bool
	RenameChat      *Chat
	RenameSaved     bool
	RenameAgentID   string
	RenameSessionID string
	RenameText      string

	MCPText      string
	MCPServers   []any
	MCPError     string
	SettingsOpen bool

	SavedLoading  bool
	SavedError    string
	SavedSessions []savedSession
	ShowDeleted   bool
	SavedMenuOpen string
	savedVersion  int
	// savedSynced records the agents whose session/list was imported once
	// after they connected, replacing the old manual refresh button.
	savedSynced map[string]bool

	// Archived is the local session index. SavedSessions is the UI view of
	// it, sorted for the sidebar. archive is nil when DataDir is empty.
	Archived []archive.Session
	archive  *archive.Store

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
	probeConfig    func(context.Context, agent.LaunchSpec) (session.Info, error)

	// agentConfigAt and agentConfigBusy throttle config probes. Both are
	// UI-thread state; background probes only post their result back.
	agentConfigAt   map[string]time.Time
	agentConfigBusy map[string]bool

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
		Version:            version,
		lifeCtx:            lifeCtx,
		lifeCancel:         lifeCancel,
		Resources:          opts.Resources,
		Workspace:          workspace,
		Active:             -1,
		SidebarOpen:        true,
		GitPanelOpen:       true,
		Git:                gitPanelState{ChangesHeight: defaultGitChangesHeight},
		MD:                 markdown.Options{},
		sessions:           make(map[*session.Session]struct{}),
		logs:               make(map[string]*logs.Log),
		resolveOpts:        opts.Resolve,
		injectedAgents:     opts.Agents != nil,
		probeConfig:        session.ProbeConfig,
		Workspaces:         []string{},
		AgentConfigs:       make(map[string][]acp.ConfigOption),
		agentConfigAt:      make(map[string]time.Time),
		agentConfigBusy:    make(map[string]bool),
		WorkspaceCollapsed: make(map[string]bool),
		WorkspaceExpanded:  make(map[string]bool),
		RemovedWorkspaces:  make(map[string]bool),
		savedSynced:        make(map[string]bool),
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
	if opts.ProbeConfig != nil {
		a.probeConfig = opts.ProbeConfig
	}
	a.MCPServers = []any{}
	a.MCPText = "[]"
	a.loadSettings(opts.DataDir, strings.TrimSpace(opts.Workspace) == "")
	a.loadSessionArchive(opts.DataDir)
	a.ensureWorkspace(a.Workspace)
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
	// Start with a draft conversation so the compose box and its controls are
	// visible; the first message connects it.
	a.newChatTabIn(a.defaultWorkspace())
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
	for _, workspace := range file.Workspaces {
		a.ensureWorkspace(workspace)
	}
	for _, workspace := range file.RemovedWorkspaces {
		// Keep the record addressable so "Show removed" can restore it.
		a.ensureWorkspace(workspace)
		a.markWorkspaceRemoved(workspace)
	}
	if workspaceDefault && file.LastWorkspace != "" {
		a.Workspace = file.LastWorkspace
	}
	// The draft must not target a removed group, otherwise new sessions would
	// land in a workspace the sidebar does not show.
	if workspaceDefault && a.workspaceRemoved(a.Workspace) {
		a.Workspace = a.firstVisibleWorkspace()
		if a.Workspace == "" {
			if home, err := os.UserHomeDir(); err == nil {
				a.Workspace = home
			}
		}
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

func (a *App) nextID() int {
	a.next++
	return a.next
}

func (a *App) nextChatID() int {
	a.nextChat++
	return a.nextChat
}

// View builds the window: a collapsible workspace sidebar, a title bar, the
// conversation with its composer, and the collapsible Git panel.
func (a *App) View(c *mui.Context) {
	a.syncWorkspaces()
	chat := a.activeChat()
	if chat != nil {
		chat.list.Key = func(i int) any { return chat.Messages[i].ID }
		chat.list.FollowEnd = true
		chat.list.FollowAnimation = &mui.Smooth
	}

	mui.Row(c).Fill().Children(func() {
		if a.SidebarOpen {
			a.sidebar(c)
		}
		mui.Column(c).Grow(1).FillHeight().Background(c.Theme().Background).Children(func() {
			a.header(c)
			a.banner(c)
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
			if a.commandOpen(chat) {
				a.commandPalette(c, chat)
			}
			a.composer(c)
		})
		if a.GitPanelOpen {
			a.gitPanel(c)
		}
	})

	a.gitDiffDialog(c)
	a.permissionDialog(c)
	a.settingsDialog(c)
	a.diagnosticsDialog(c)
	a.renameDialog(c)
}

// header is the conversation bar: sidebar toggle, title and the session
// actions. Agent discovery details stay in the sidebar footer.
func (a *App) header(c *mui.Context) {
	t := c.Theme()
	chat := a.activeChat()
	title := "New conversation"
	if chat != nil {
		title = chat.name()
	}
	mui.Row(c).Height(60).Gap(8).Padding(0, 16).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		toggle, tip := "≡", "Show sidebar"
		if a.SidebarOpen {
			toggle, tip = "‹", "Hide sidebar"
		}
		if mui.Button(c, toggle).Label(tip).FontSize(15).Clicked() {
			a.SidebarOpen = !a.SidebarOpen
		}
		mui.Column(c).Gap(1).Grow(1).Children(func() {
			mui.Row(c).Gap(6).AlignItems(mui.Center).Children(func() {
				if chat != nil {
					agentBadge(c, chat.Agent.ID, 13)
				}
				mui.Text(c, truncate(title, 34)).FontSize(15).Bold().SingleLine()
			})
			sub := a.statusLine()
			if chat != nil && strings.TrimSpace(chat.Workspace) != "" {
				if sub != "" {
					sub += " · "
				}
				sub += filepath.Base(chat.Workspace)
			}
			if sub != "" {
				mui.Text(c, truncate(sub, 140)).FontSize(10).TextColor(t.TextMuted).SingleLine()
			}
		})
		a.headerActions(c, chat)
	})
}

func (a *App) headerActions(c *mui.Context, chat *Chat) {
	t := c.Theme()
	action := func(label, tip string) mui.Element {
		return mui.Button(c, label).Label(tip).FontSize(11)
	}
	gitLabel := "Show Git panel"
	var gitButton mui.Element
	if a.GitPanelOpen {
		gitLabel = "Hide Git panel"
		gitButton = mui.PrimaryButton(c, "Git")
	} else {
		gitButton = mui.Button(c, "Git")
	}
	gitButton.Label(gitLabel).FontSize(11)
	if gitButton.Clicked() {
		a.GitPanelOpen = !a.GitPanelOpen
	}
	if chat == nil {
		return
	}
	if chat.Connecting {
		mui.Text(c, "Connecting…").FontSize(11).TextColor(t.TextMuted)
		if action("Cancel", "Cancel connecting").Clicked() {
			a.Disconnect()
		}
		return
	}
	if action("Logs", "Diagnostics").Clicked() {
		a.openDiagnostics()
	}
	if action("MCP…", "MCP servers").Clicked() {
		a.openMCPSettings()
	}
}

// banner shows errors and warnings right below the title bar.
func (a *App) banner(c *mui.Context) {
	t := c.Theme()
	var lines []string
	if msg := a.errorLine(); msg != "" {
		lines = append(lines, msg)
	}
	if len(a.Env.Warnings) > 0 {
		lines = append(lines, strings.Join(a.Env.Warnings, "; "))
	}
	if a.SettingsWarning != "" {
		lines = append(lines, "Settings: "+a.SettingsWarning)
	}
	if len(lines) == 0 {
		return
	}
	mui.Column(c).Gap(3).Padding(6, 16).Children(func() {
		color := t.Warning
		if a.errorLine() != "" {
			color = t.Danger
		}
		for _, line := range lines {
			mui.Text(c, line).FontSize(11).TextColor(color).Selectable()
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
	chat := a.activeChat()
	switch {
	case chat != nil && chat.Connecting:
		crylayout.Centered(c, func() {
			cryfeedback.Busy(c, true, "Connecting…", "Connected")
		})
	case a.Resolving:
		crylayout.Centered(c, func() {
			cryfeedback.Busy(c, true, "Probing pi and Command Code…", "Agents ready")
		})
	default:
		glyph, title, text := emptyStateCopy(chat, len(a.Agents))
		crylayout.Centered(c, func() {
			cryfeedback.EmptyState(c, glyph, title, text, "")
		})
	}
}

// emptyStateCopy names the empty list's icon, title and body.
func emptyStateCopy(chat *Chat, agents int) (glyph, title, text string) {
	switch {
	case chat != nil && chat.Session != nil && chat.Info.SessionID == "":
		return "message-square", "No active session.", "Create a new chat or open a saved session from the sidebar."
	case chat != nil && chat.Session != nil:
		return "messages-square", "Connected. Send a message to start.", "Agent updates stream into this list."
	case agents == 0:
		return "unplug", "No ACP agent available", "Install pi or Command Code, then refresh discovery in the sidebar."
	case chat != nil:
		return "sparkles", "Start the conversation.", "Type below and press Enter; the app connects on the first message."
	default:
		return "message-square", "New conversation", "Create one from the sidebar and type a message."
	}
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
