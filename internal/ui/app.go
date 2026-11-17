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

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/archive"
	"github.com/reed/acp-gui/internal/codexprovider"
	"github.com/reed/acp-gui/internal/logs"
	"github.com/reed/acp-gui/internal/modelconfig"
	"github.com/reed/acp-gui/internal/session"
	"github.com/reed/acp-gui/internal/settings"
	taskstore "github.com/reed/acp-gui/internal/tasks"
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
	// ProbeAgent checks one builtin agent during discovery. Nil uses
	// session.Probe; tests can replace it to drive the status machine
	// without spawning a process.
	ProbeAgent func(context.Context, agent.LaunchSpec) error
	// ProbeConfig reads an agent's session config options during discovery
	// and when the draft model panel needs a refresh. Nil uses
	// session.ProbeConfig; tests can replace it.
	ProbeConfig func(context.Context, agent.LaunchSpec) (session.Info, error)
	// ProbeModels reads and caches config options for the models that are
	// missing from the per-model cache. Nil uses session.ProbeConfigModels,
	// or ProbeConfig when that is injected; tests can replace it.
	ProbeModels func(context.Context, agent.LaunchSpec, session.ModelProbe) (session.Info, error)
	// ReadClipboardImage returns the clipboard image as PNG, or nil. Nil
	// uses mygo.Clipboard.ReadImage; tests inject a fixed image.
	ReadClipboardImage func() []byte
	// OpenImages opens the native multi-image picker. Nil uses
	// mygo.Dialog.Open; tests inject paths without a window.
	OpenImages func() ([]string, error)
	// FileIndex lists workspace-relative files for @ mentions. Nil uses the
	// built-in workspace walker; tests inject a small deterministic list.
	FileIndex func(string) ([]string, error)
	// RevealItem shows a file or folder in the system file manager. Nil uses
	// mygo.Shell.ShowItemInFolder; tests replace it to capture the path.
	RevealItem func(string)
}

// MessageRole is how a chat row is drawn.
type MessageRole int

const (
	RoleUser MessageRole = iota
	RoleAssistant
	RoleTool
	RoleSystem
	RoleError
	// RoleTurn is the muted runtime caption that closes a completed turn.
	RoleTurn
	// RoleInbound is a left-aligned message sent to this client by another
	// agent session, such as the task assigned to a subagent.
	RoleInbound
	// RoleSubagent is the announcement card for one agent-managed child
	// session. Its transcript rows keep their normal roles and use SubagentID
	// so they remain searchable and replayable in the parent chat.
	RoleSubagent
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
	NoticeKind  string
	NoticeLevel string
	// SubagentID and SubagentName identify child-session output. They stay
	// empty for the root conversation.
	SubagentID   string
	SubagentName string
	// DetailOpen opens a tool card or subagent transcript. Details stay hidden
	// until it is true; cards start collapsed.
	DetailOpen  bool
	Attachments []Attachment
	// FileRefs mirror @ mentions from the prompt, so the user's row lists
	// the files the message refers to.
	FileRefs []FileRef
	// SentAt is when a user prompt was sent. Replayed history has no
	// timestamps, so it stays zero unless the local archive recorded one.
	SentAt time.Time
	// Elapsed is the runtime shown in a RoleTurn summary row. It is kept as
	// a duration rather than text so the label can be formatted at draw time.
	Elapsed time.Duration
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
	Version     string
	Status      string
	Detail      string
	InstallHint string
}

// AgentModeState is the session mode list cached from a config probe.
type AgentModeState struct {
	Modes         []acp.Mode
	CurrentModeID string
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
	// number is the user-turn ordinal this prompt belongs to, and startedAt
	// is when it was sent. Both stay zero for replayed history.
	number    int
	startedAt time.Time
	// startMessage is the first chat row of this turn, so a task runner reads
	// only the summary the current turn produced.
	startMessage int
	// endNotified keeps a task runner from receiving the same turn's end
	// twice when both the prompt result and the process exit arrive.
	endNotified bool
}

// Subagent is one child session announced by the agent. Its output shares the
// parent chat list but keeps independent streaming and tool-call state, so a
// child tool cannot close the parent's current assistant row.
type Subagent struct {
	ID          string
	ParentID    string
	Title       string
	Description string
	State       string
	StopReason  string
	CWD         string
	CanCancel   bool
	Card        *Message

	turn     *turnState
	incoming *Message
}

// App is the root UI state.
type App struct {
	Version   string
	Resources string
	// DataDir is the persistent app directory, empty for in-memory runs. It
	// is also where the generated Codex model catalog is written.
	DataDir       string
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
	// AgentModes caches the session modes returned by the same throwaway
	// session/new, so the mode chip is visible in a draft instead of only
	// after the first connect. Keyed by agent ID.
	AgentModes map[string]AgentModeState
	// AgentModelConfigs caches, per agent and model value, the config
	// options the agent returned for that model. ACP only reports thinking
	// levels for a session's current model, so this is what lets a draft
	// show another model's levels without connecting. It is persisted in
	// model-config.json, and a later startup sweeps only new models.
	AgentModelConfigs map[string]map[string]modelconfig.Model
	Env               agent.Environment
	Resolving         bool
	ResolveError      string

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
	// RightTab selects the right column: "git", "tasks" or "files". The
	// panel width and open/closed state are shared, so tabs never stack.
	RightTab string
	Git      gitPanelState
	Files    filePanelState

	// Tasks is the ordered task index. It is loaded before the first frame
	// and persisted in <data>/tasks.json; summaries go to the task
	// workspace's tasks/ folder. TaskRun owns the single serial runner.
	Tasks      []taskstore.Task
	TaskRun    taskRunState
	taskStore  *taskstore.Store
	TaskError  string
	TaskNotice string

	// Task editor and delete confirmation are tied to the workspace they
	// were opened in, so switching groups cannot save into another folder.
	TaskEditorOpen    bool
	TaskEditID        string
	TaskEditWorkspace string
	TaskEditTitle     string
	TaskEditText      string
	TaskEditorError   string
	TaskDeleteOpen    bool
	TaskDeleteID      string

	// TaskSummary is the floating Markdown card opened from a complete task.
	TaskSummaryOpen   bool
	TaskSummaryTaskID string
	TaskSummaryTitle  string
	TaskSummaryPath   string
	TaskSummaryBody   string
	TaskSummaryBusy   bool
	TaskSummaryErr    string
	TaskSummaryGen    int
	// TaskSummaryMode shows the summary as rendered Markdown, an editor or
	// an all-additions diff in the shared viewer card.
	TaskSummaryMode    viewerMode
	TaskSummarySaving  bool
	TaskSummarySaveErr string
	TaskSummaryNotice  string

	Chats  []*Chat
	Active int

	RenameOpen      bool
	RenameChat      *Chat
	RenameSaved     bool
	RenameAgentID   string
	RenameSessionID string
	RenameText      string

	// SettingsOpen shows the settings panel. SettingsSection is the selected
	// category in its left column.
	SettingsOpen    bool
	SettingsSection string
	// CodexProvider is the saved custom provider for Codex. CodexProviderDraft
	// is the panel's editable copy; CodexProviderModelsText is its one-model-
	// per-line editor and CodexProviderEfforts holds the checked reasoning
	// levels. The context and compact fields stay as text while edited so an
	// incomplete number is visible instead of being silently replaced. Saving
	// validates and applies the draft to new Codex processes.
	CodexProvider                   codexprovider.Config
	CodexProviderDraft              codexprovider.Config
	CodexProviderModelsText         string
	CodexProviderContextWindowText  string
	CodexProviderCompactPercentText string
	CodexProviderEfforts            map[string]bool
	CodexProviderNotice             string
	// Appearance is the light/dark choice (system, light or dark). Theme
	// names the colors (default, nord, solarized, catppuccin, claude, green,
	// green-soft, vscode, everforest, one-dark or tokyo-night). Both are
	// persisted in settings.json and applied in View.
	Appearance string
	Theme      string
	// Fonts scales each text area: the workspace list, the conversation, the
	// composer and the Git panel. It is persisted in settings.json.
	Fonts settings.Fonts
	// ViewerWidth is the floating file viewer's width after the user drags its
	// left edge. Zero follows the window; the value is persisted.
	ViewerWidth float32
	// NotificationsEnabled gates desktop notifications. It defaults to on and
	// is persisted in settings.json.
	NotificationsEnabled bool
	// NotificationTestNotice is the result of the test button, shown under it.
	NotificationTestNotice string
	// DockBadgeEnabled gates the Dock icon badge. It defaults to on and is
	// persisted in settings.json.
	DockBadgeEnabled bool
	// dockBadgeCount is the badge last sent to the platform; -1 means none
	// was sent yet. Comparing against it keeps UI updates from calling the
	// platform when the count did not change.
	dockBadgeCount int

	SavedLoading  bool
	SavedError    string
	SavedSessions []savedSession
	ShowDeleted   bool
	SavedMenuOpen string
	// savedVersions keeps the latest session/list generation per agent, so a
	// slow result cannot overwrite a newer one for the same agent.
	savedVersions map[string]int
	// savedSynced records the agents whose session/list was imported once
	// after they connected, replacing the old manual refresh button.
	savedSynced map[string]bool

	// Archived is the local session index. SavedSessions is the UI view of
	// it, sorted for the sidebar. archive is nil when DataDir is empty.
	Archived []archive.Session
	archive  *archive.Store

	SettingsWarning string

	// ImagePreview is the attachment shown enlarged by imagePreviewDialog.
	ImagePreview *Attachment
	ImageOpen    bool

	Permission      *permissionPrompt
	PermissionOpen  bool
	permissionQueue []*permissionPrompt

	MD markdown.Options
	// TaskMD renders task summaries with the right panel's font scale. It is
	// a separate Options value because markdown.Options contains a sync.Map
	// and must not be copied.
	TaskMD markdown.Options

	chatsMu  sync.Mutex
	nextChat int
	// win is the open window, nil while the app runs in the background.
	win     *mygo.Window
	postMu  sync.Mutex
	pending []func()
	// updateWin is the last window, kept after it closed: MyGo runs
	// Window.Update closures on the UI thread without drawing a closed
	// window, so background updates keep landing while no window is open.
	// It is nil until the first window exists (tests).
	updateWin    *mygo.Window
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
	// discoveryStarted keeps a reopened window from probing the agents
	// again; discovery runs once per app, not once per window.
	discoveryStarted   bool
	resolveOpts        agent.ResolveOptions
	probeAgent         func(context.Context, agent.LaunchSpec) error
	probeConfig        func(context.Context, agent.LaunchSpec) (session.Info, error)
	probeModels        func(context.Context, agent.LaunchSpec, session.ModelProbe) (session.Info, error)
	readClipboardImage func() []byte
	openImages         func() ([]string, error)
	fileIndexer        func(string) ([]string, error)
	revealItem         func(string)
	// notify shows a desktop notification. Nil uses mygo (see notify.go);
	// tests replace it to capture what the app would show.
	notify func(desktopNotification)
	// setDockBadge shows n on the Dock icon. Nil uses mygo (see badge.go);
	// tests replace it to capture the count.
	setDockBadge func(int)
	// isFocused overrides the window's focus state in tests; nil asks the
	// window.
	isFocused func() bool

	// agentConfigAt and agentConfigBusy throttle config probes. Both are
	// UI-thread state; background probes only post their result back.
	agentConfigAt   map[string]time.Time
	agentConfigBusy map[string]bool

	settings         *settings.Store
	settingsPaths    settings.Paths
	preferredAgentID string
	// lastSelections remembers the model-panel values and session mode per
	// agent, so a new draft starts where the last conversation left off. It
	// is loaded from settings.json and rewritten by rememberSelection.
	lastSelections map[string]settings.Selection
	// modelConfig persists AgentModelConfigs; nil when there is no data
	// directory.
	modelConfig *modelconfig.Store

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
		DataDir:            strings.TrimSpace(opts.DataDir),
		Workspace:          workspace,
		Active:             -1,
		SidebarOpen:        true,
		GitPanelOpen:       true,
		RightTab:           rightTabGit,
		Git:                gitPanelState{ChangesHeight: defaultGitChangesHeight},
		Files:              newFilePanelState(""),
		MD:                 markdown.Options{},
		sessions:           make(map[*session.Session]struct{}),
		logs:               make(map[string]*logs.Log),
		resolveOpts:        opts.Resolve,
		probeAgent:         opts.ProbeAgent,
		injectedAgents:     opts.Agents != nil,
		probeConfig:        session.ProbeConfig,
		probeModels:        session.ProbeConfigModels,
		Workspaces:         []string{},
		AgentConfigs:       make(map[string][]acp.ConfigOption),
		AgentModes:         make(map[string]AgentModeState),
		AgentModelConfigs:  make(map[string]map[string]modelconfig.Model),
		agentConfigAt:      make(map[string]time.Time),
		agentConfigBusy:    make(map[string]bool),
		WorkspaceCollapsed: make(map[string]bool),
		WorkspaceExpanded:  make(map[string]bool),
		RemovedWorkspaces:  make(map[string]bool),
		savedSynced:        make(map[string]bool),
		lastSelections:     make(map[string]settings.Selection),

		Appearance:           AppearanceSystem,
		Theme:                ThemeDefault,
		Fonts:                defaultFontScales(),
		NotificationsEnabled: true,
		DockBadgeEnabled:     true,
		dockBadgeCount:       -1,
	}
	if opts.Agents != nil {
		a.Agents = append([]agent.LaunchSpec(nil), opts.Agents...)
		a.Resolving = false
		for _, spec := range a.Agents {
			a.AgentStatuses = append(a.AgentStatuses, AgentStatus{ID: spec.ID, Name: spec.Name, Version: spec.Version, Status: AgentReady})
		}
	} else {
		a.Resolving = true
	}
	if a.probeAgent == nil {
		a.probeAgent = func(ctx context.Context, spec agent.LaunchSpec) error {
			_, err := session.Probe(ctx, spec)
			return err
		}
	}
	if opts.ProbeConfig != nil {
		a.probeConfig = opts.ProbeConfig
		// An injected config probe is the whole probe: tests that replace it
		// do not expect a second, real agent process for the model sweep.
		a.probeModels = nil
	}
	if opts.ProbeModels != nil {
		a.probeModels = opts.ProbeModels
	}
	a.readClipboardImage = opts.ReadClipboardImage
	if a.readClipboardImage == nil {
		a.readClipboardImage = mygo.Clipboard.ReadImage
	}
	a.openImages = opts.OpenImages
	a.fileIndexer = opts.FileIndex
	if a.fileIndexer == nil {
		a.fileIndexer = scanWorkspaceFiles
	}
	a.revealItem = opts.RevealItem
	if a.revealItem == nil {
		a.revealItem = func(path string) { mygo.Shell.ShowItemInFolder(path) }
	}
	a.loadSettings(opts.DataDir, strings.TrimSpace(opts.Workspace) == "")
	a.loadSessionArchive(opts.DataDir)
	a.loadTaskStore(opts.DataDir)
	a.loadModelConfigs(opts.DataDir)
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
	if a.resolveOpts.CodexPath == "" {
		a.resolveOpts.CodexPath = firstNonEmpty(os.Getenv("ACP_GUI_CODEX"), a.settingsPaths.Codex)
	}
	// Start with a draft conversation so the compose box and its controls are
	// visible; the first message connects it.
	// The initial draft expands its group. Capture the persisted state first
	// and re-apply it, so a collapsed workspace stays collapsed across
	// restarts.
	collapsed := append([]string(nil), a.collapsedWorkspaceList()...)
	a.newChatTabIn(a.defaultWorkspace())
	for _, workspace := range collapsed {
		a.WorkspaceCollapsed[workspace] = true
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
	a.CodexProvider = file.CodexProvider
	if err := a.refreshCodexCatalog(); err != nil {
		a.SettingsWarning = strings.TrimSpace(strings.Join([]string{a.SettingsWarning, err.Error()}, "\n"))
	}
	if len(file.LastSelections) > 0 {
		a.lastSelections = file.LastSelections
	}
	a.Appearance = normalizeAppearance(file.Appearance)
	a.Theme = normalizeTheme(file.Theme)
	a.Fonts = normalizeFontScales(file.Fonts)
	a.ViewerWidth = normalizeViewerWidth(file.ViewerWidth)
	a.NotificationsEnabled = file.Notifications
	a.DockBadgeEnabled = file.DockBadge
	for _, workspace := range file.Workspaces {
		a.ensureWorkspace(workspace)
	}
	for _, workspace := range file.RemovedWorkspaces {
		// Keep the record addressable so "Show removed" can restore it.
		a.ensureWorkspace(workspace)
		a.markWorkspaceRemoved(workspace)
	}
	if a.WorkspaceCollapsed == nil {
		a.WorkspaceCollapsed = make(map[string]bool)
	}
	for _, workspace := range file.CollapsedWorkspaces {
		a.WorkspaceCollapsed[workspace] = true
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
// main thread after the window exists. Updates posted while no window was
// open apply before the new window draws its first frame.
func (a *App) Attach(win *mygo.Window) {
	a.postMu.Lock()
	a.win = win
	a.updateWin = win
	// Probe the agents once: a reopened window keeps the discovery results.
	discover := !a.injectedAgents && !a.discoveryStarted
	a.discoveryStarted = true
	pending := a.pending
	a.pending = nil
	a.postMu.Unlock()
	for _, fn := range pending {
		fn()
	}
	win.OnFocus(func() {
		a.clearActiveUnread()
		a.refreshGitOnFocus()
		a.refreshFilesOnFocus()
	})
	// Repaint the badge for this run: a count cached before the first
	// window would otherwise look unchanged and never reach the platform.
	// Reopening the window shows the active chat, so what finished while it
	// was closed is read again.
	a.dockBadgeCount = -1
	a.clearActiveUnread()
	a.updateDockBadge()
	if discover {
		a.startDiscovery()
	}
}

// Detach forgets win after it closed, so dialogs no longer reach a destroyed
// window. Updates keep posting through it (see updateWin): the app and its
// agents run on in the background until the app quits.
func (a *App) Detach(win *mygo.Window) {
	a.postMu.Lock()
	if a.win == win {
		a.win = nil
	}
	a.postMu.Unlock()
}

func (a *App) window() *mygo.Window {
	a.postMu.Lock()
	defer a.postMu.Unlock()
	return a.win
}

// Post runs fn on the UI thread and redraws, or only runs it while the app
// has no window. Before the first window exists (tests), fn is queued for
// Flush.
func (a *App) Post(fn func()) {
	if fn == nil || a.closed.Load() {
		return
	}
	a.postMu.Lock()
	win := a.win
	if win == nil {
		// No window: land the update through the closed one without a
		// redraw, or queue it until the first window exists (tests).
		win = a.updateWin
	}
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
// It is safe to call from OnWillQuit; closing a window does not call it.
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
// conversation with its composer, the collapsible right column, and the rail
// of tab icons at the window's right edge.
func (a *App) View(c *mui.Context) {
	a.applyTheme(c)
	// Markdown headings and code are sized in ems of this body size, so one
	// value scales the whole message while keeping its hierarchy.
	a.MD.Theme = &markdown.Theme{FontSize: a.chatBodyFont(c)}
	a.TaskMD.Theme = &markdown.Theme{FontSize: a.gitFont(c.Theme().FontSize)}
	a.syncWorkspaces()
	chat := a.activeChat()
	var rows []*Message
	if chat != nil {
		rows = conversationRows(chat)
		chat.list.Key = func(i int) any { return rows[i].ID }
		chat.list.FollowEnd = true
		chat.list.FollowAnimation = &mui.Smooth
	}

	mui.Row(c).Fill().Children(func() {
		if a.SidebarOpen {
			a.sidebar(c)
		}
		a.conversation(c, chat, rows)
		if a.GitPanelOpen {
			switch a.RightTab {
			case rightTabTasks:
				a.taskSummaryPopover(c, a.taskPanel(c))
			case rightTabFiles:
				a.fileViewerPopover(c, a.filePanel(c))
			default:
				// The diff floats over the conversation with its right edge on
				// the panel's left edge, so the panel stays visible and usable.
				a.gitDiffPopover(c, a.gitPanel(c))
			}
		}
		a.rightRail(c)
	})

	a.gitDiscardDialog(c)
	if chat != nil {
		a.jumpDialog(c, chat)
	}
	a.permissionDialog(c)
	a.settingsDialog(c)
	a.renameDialog(c)
	a.taskEditorDialog(c)
	a.taskDeleteDialog(c)
	a.fileCreateDialog(c)
	a.fileRenameDialog(c)
	a.fileDeleteDialog(c)
	a.imagePreviewDialog(c)
}

// conversation is the middle column: its bar, messages and composer.
func (a *App) conversation(c *mui.Context, chat *Chat, rows []*Message) {
	mui.Column(c).Grow(1).FillHeight().Background(c.Theme().Background).Children(func() {
		a.header(c)
		a.banner(c)
		if chat == nil {
			mui.Column(c).Grow(1).Children(func() { a.emptyState(c) })
			return
		}
		mui.Box(c).Grow(1).Children(func() {
			mui.List(c, &chat.list, len(rows), func(i int) {
				a.message(c, chat, rows[i])
			}).Fill().Padding(16, 20).Gap(16).Justify(mui.End).Children(func() {
				if len(rows) == 0 {
					a.emptyState(c)
				}
			})
			a.jumpToLatestButton(c, chat)
			a.turnTimer(c, chat)
		})
		if a.commandOpen(chat) {
			a.commandPalette(c, chat)
		}
		a.composer(c)
	})
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
		if quietButton(c, toggle).Label(tip).FontSize(15).Clicked() {
			a.SidebarOpen = !a.SidebarOpen
		}
		mui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			mui.Row(c).Gap(6).AlignItems(mui.Center).MinWidth(0).Children(func() {
				if chat != nil {
					agentBadge(c, chat.Agent.ID, 13)
				}
				// Let layout cut the title to the room the actions leave, so it
				// ellipsizes by width instead of at a fixed rune count.
				mui.Text(c, title).FontSize(15).Bold().SingleLine().MinWidth(0)
			})
			sub := a.statusLine()
			if chat != nil && strings.TrimSpace(chat.Workspace) != "" {
				if sub != "" {
					sub += " · "
				}
				sub += filepath.Base(chat.Workspace)
			}
			if sub != "" {
				mutedText(c, sub, 10).SingleLine().MinWidth(0)
			}
		})
		a.headerActions(c, chat)
	})
}

func (a *App) headerActions(c *mui.Context, chat *Chat) {
	if chat == nil {
		return
	}
	action := func(label, tip string) mui.Element {
		return quietButton(c, label).Label(tip).FontSize(11)
	}
	if chat.Connecting {
		mutedText(c, "Connecting…", 11)
		if action("Cancel", "Cancel connecting").Clicked() {
			if chat.taskActive {
				a.stopTaskRun("Stopped while starting.")
			} else {
				a.Disconnect()
			}
		}
		return
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
		name := chat.agentName()
		version := strings.TrimSpace(chat.Agent.Version)
		if chat.Info.AgentInfo != nil {
			if !agent.IsBuiltinID(chat.Agent.ID) {
				// Custom agents report their own display name. Builtins use
				// their CLI name and version instead: the adapter's
				// agentInfo.version is not the installed CLI version.
				if chat.Info.AgentInfo.Title != "" {
					name = chat.Info.AgentInfo.Title
				} else if chat.Info.AgentInfo.Name != "" {
					name = chat.Info.AgentInfo.Name
				}
				version = chat.Info.AgentInfo.Version
			} else if version == "" && chat.Agent.ID == agent.CommandCodeID {
				version = chat.Info.AgentInfo.Version
			}
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
		centered(c, func() {
			busyLine(c, "Connecting…")
		})
	case a.Resolving:
		centered(c, func() {
			busyLine(c, "Probing pi, Command Code and Codex…")
		})
	default:
		glyph, title, text := emptyStateCopy(chat, len(a.Agents))
		centered(c, func() {
			emptyState(c, glyph, title, text)
		})
	}
}

// emptyStateCopy names the empty list's icon, title and body.
func emptyStateCopy(chat *Chat, agents int) (glyph *mui.SVG, title, text string) {
	switch {
	case chat != nil && chat.Session != nil && chat.Info.SessionID == "":
		return messageSquareIcon, "No active session.", "Create a new chat or open a saved session from the sidebar."
	case chat != nil && chat.Session != nil:
		return messagesSquareIcon, "Connected. Send a message to start.", "Agent updates stream into this list."
	case agents == 0:
		return unplugIcon, "No ACP agent available", "Install pi, Command Code or Codex, then refresh discovery in the sidebar."
	case chat != nil:
		return sparklesIcon, "Start the conversation.", "Type below and press Enter; the app connects on the first message."
	default:
		return messageSquareIcon, "New conversation", "Create one from the sidebar and type a message."
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
				mui.Text(c, text).FontSize(a.chatFont(11)).TextColor(color)
			}
		})
	})
}

// chatMaxWidth is the measure of a conversation row: replies (including the
// thinking block), tool cards and notices stop at the same width instead of
// stretching across a wide window.
const chatMaxWidth float32 = 960

// conversationRows groups child-session transcript rows under their subagent
// card. Collapsed cards keep the parent conversation focused on the overall
// turn; expanding one inserts that child's task, replies and tools directly
// below its announcement. Unknown children still fall back to flat rows so a
// late or malformed update is never dropped.
func conversationRows(chat *Chat) []*Message {
	if chat == nil {
		return nil
	}
	cards := make(map[*Message]bool)
	children := make(map[string][]*Message)
	for _, message := range chat.Messages {
		if message == nil {
			continue
		}
		if message.Role == RoleSubagent {
			cards[message] = true
		} else if message.SubagentID != "" {
			children[message.SubagentID] = append(children[message.SubagentID], message)
		}
	}
	rows := make([]*Message, 0, len(chat.Messages))
	for _, message := range chat.Messages {
		if message == nil {
			continue
		}
		if message.Role == RoleSubagent {
			rows = append(rows, message)
			if message.DetailOpen && message.SubagentID != "" {
				rows = append(rows, children[message.SubagentID]...)
			}
			continue
		}
		if message.SubagentID != "" {
			if sub := chat.subagent(message.SubagentID); sub != nil && cards[sub.Card] {
				// The child's card owns these rows and inserts them when open.
				continue
			}
		}
		rows = append(rows, message)
	}
	return rows
}

func (a *App) message(c *mui.Context, chat *Chat, m *Message) {
	t := c.Theme()
	switch m.Role {
	case RoleUser:
		mui.Row(c).Children(func() {
			mui.Spacer(c)
			mui.Column(c).Gap(6).MaxWidth(520).Children(func() {
				if m.Text != "" {
					mui.Text(c, m.Text).
						FontSize(a.chatBodyFont(c)).
						Selectable().
						Padding(8, 12).
						Radius(14).
						Background(t.Surface)
				}
				a.messageFiles(c, m.FileRefs)
				a.messageAttachments(c, m.Attachments)
			})
		})
	case RoleAssistant:
		mui.Column(c).MaxWidth(chatMaxWidth).Children(func() {
			a.subagentLabel(c, m)
			if m.Thought != "" {
				mui.Collapsible(c, "Thinking", &m.ShowThought, func() {
					mutedText(c, m.Thought, a.chatFont(12)).Selectable()
				})
			}
			if m.Text == "" && !m.Done {
				mui.Row(c).Gap(8).AlignItems(mui.Center).Children(func() {
					mui.Spinner(c)
					mutedText(c, "Waiting for the agent…", a.chatFont(12))
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
		a.toolMessage(c, m)
	case RoleSubagent:
		a.subagentMessage(c, chat, m)
	case RoleError:
		mui.Text(c, "⚠ "+m.Text).MaxWidth(chatMaxWidth).FontSize(a.chatBodyFont(c)).
			TextColor(t.Danger).Selectable()
	case RoleSystem:
		color := t.TextMuted
		switch m.NoticeLevel {
		case "warning":
			color = t.Warning
		case "error":
			color = t.Danger
		}
		mui.Text(c, m.Text).MaxWidth(chatMaxWidth).FontSize(a.chatBodyFont(c)).
			TextColor(color).TextAlign(mui.Center)
	case RoleInbound:
		mui.Row(c).Children(func() {
			mui.Column(c).Gap(6).MaxWidth(720).Padding(8, 12).Radius(12).
				Background(t.Surface).Children(func() {
				a.subagentLabel(c, m)
				if m.Text != "" {
					mui.Text(c, m.Text).FontSize(a.chatBodyFont(c)).Selectable()
				}
				a.messageFiles(c, m.FileRefs)
				a.messageAttachments(c, m.Attachments)
			})
			mui.Spacer(c)
		})
	case RoleTurn:
		if m.Elapsed > 0 {
			mui.Row(c).MaxWidth(chatMaxWidth).Children(func() {
				mui.Spacer(c)
				mutedText(c, "Took "+formatElapsed(m.Elapsed), a.chatFont(9)).Shrink(0).Tooltip("Runtime of this turn")
			})
		}
	}
}

// subagentMessage is the persistent announcement for one child session. It is
// deliberately separate from a tool card: the title names the worker, the
// status is user-facing, and session/workspace metadata stays collapsed.
func (a *App) subagentMessage(c *mui.Context, chat *Chat, m *Message) {
	t := c.Theme()
	status := subagentStatusLabel(m.Status)
	statusColor := t.TextMuted
	switch m.Status {
	case "in_progress", "pending", "starting":
		statusColor = t.Warning
	case "completed", "idle":
		statusColor = t.Success
	case "failed":
		statusColor = t.Danger
	case "disconnected":
		statusColor = t.Warning
	}
	sub := (*Subagent)(nil)
	if chat != nil {
		sub = chat.subagent(m.SubagentID)
	}
	title := strings.TrimPrefix(m.Title, "Subagent · ")
	if title == "" {
		title = "Agent"
	}
	root := mui.Column(c).MaxWidth(chatMaxWidth).Padding(10).Radius(10).
		Background(t.Surface).Border(1, t.Border).
		Role(mui.RoleGroup).Label(subagentCardLabel(m.Title, status))
	root.Children(func() {
		// Build the collapsible inside the card subtree. Building it before
		// root.Children attaches the trigger to the conversation row instead.
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Label("Toggle subagent details: " + m.Title).
			FillWidth().Gap(10).AlignItems(mui.Center).Children(func() {
			mui.Icon(c, gitBranchIcon).Size(15, 15).Shrink(0).TextColor(statusColor)
			mui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				sectionLabel(c, "SUBAGENT")
				mui.Text(c, title).FontSize(a.chatFont(12)).Bold().SingleLine().MinWidth(0)
			})
			mui.Text(c, status).FontSize(a.chatFont(10)).TextColor(statusColor).
				Background(statusColor.Alpha(0.1)).Padding(2, 7).Radius(999).Shrink(0)
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			mutedText(c, mark, a.chatFont(10)).Width(10).Shrink(0)
		})
		if m.Text != "" {
			mutedText(c, m.Text, a.chatFont(11)).SingleLine().MinWidth(0)
		}
		if reply := lastSubagentReply(chat, m.SubagentID); reply != "" {
			mui.Row(c).Gap(5).AlignItems(mui.Center).MinWidth(0).Children(func() {
				sectionLabel(c, "RESULT")
				mui.Text(c, reply).FontSize(a.chatFont(11)).Grow(1).MinWidth(0).SingleLine()
			})
		}
		parts.Panel(func() {
			mui.Column(c).Gap(3).PaddingStart(25).Children(func() {
				if m.Detail != "" {
					mutedText(c, m.Detail, a.chatFont(10)).Selectable()
				}
				if sub != nil && sub.ID != "" {
					mutedText(c, "Session: "+sub.ID, a.chatFont(10)).Selectable().Tooltip("ACP child session ID")
				}
			})
		})
	})
}

func subagentCardLabel(title, status string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Subagent"
	}
	return title + ", status " + status
}

func lastSubagentReply(chat *Chat, subagentID string) string {
	if chat == nil || subagentID == "" {
		return ""
	}
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		message := chat.Messages[i]
		if message == nil || message.Role != RoleAssistant || message.SubagentID != subagentID {
			continue
		}
		text := singleLine(message.Text)
		if text != "" {
			text = strings.NewReplacer("`", "", "**", "", "__", "", "~~", "").Replace(text)
			return truncate(text, 240)
		}
	}
	return ""
}

// subagentLabel identifies child-session output without taking over the main
// assistant identity. The full id stays in the tooltip for debugging.
func (a *App) subagentLabel(c *mui.Context, m *Message) {
	if m == nil || strings.TrimSpace(m.SubagentName) == "" {
		return
	}
	label := "Subagent · " + singleLine(m.SubagentName)
	if m.SubagentID != "" {
		labelElement := mutedText(c, label, a.chatFont(10)).SingleLine()
		labelElement.Tooltip(m.SubagentID)
		return
	}
	mutedText(c, label, a.chatFont(10)).SingleLine()
}

// toolMessage draws one tool call as a card that starts collapsed: its header
// keeps the command to one line, and the output is hidden. A click on the
// header opens the full command and the output below it; another click closes
// them again.
func (a *App) toolMessage(c *mui.Context, m *Message) {
	t := c.Theme()
	status := toolStatusLabel(m.Status)
	statusColor := t.TextMuted
	switch m.Status {
	case "completed":
		statusColor = t.Success
	case "failed":
		statusColor = t.Danger
	case "in_progress", "pending":
		statusColor = t.Warning
	}
	if strings.TrimSpace(m.Detail) == "" {
		// Orchestration calls such as wait_agent and close_agent have no
		// user-facing payload. Draw them as a compact event instead of an
		// expandable card whose panel would otherwise be empty.
		surfaceCard(c, mui.Row(c).Gap(6).MaxWidth(chatMaxWidth).Padding(8, 10).AlignItems(mui.Center)).Children(func() {
			a.subagentLabel(c, m)
			mutedText(c, "•", a.chatFont(12)).Width(8).Shrink(0)
			mui.Text(c, m.Title).Bold().FontSize(a.chatFont(12)).Grow(1).MinWidth(0).SingleLine()
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		return
	}
	surfaceCard(c, mui.Column(c).Gap(3).MaxWidth(chatMaxWidth).Padding(8, 10)).Children(func() {
		a.subagentLabel(c, m)
		// Created inside the card: nodes join their parent as they are built,
		// so a trigger built before it would sit outside the card's surface.
		parts := mui.CollapsibleBase(c, &m.DetailOpen)
		parts.Trigger.Grow(1).Gap(6).AlignItems(mui.Center).
			Label("Toggle tool output: " + m.Title).Children(func() {
			mark := "▸"
			if m.DetailOpen {
				mark = "▾"
			}
			// A fixed arrow width keeps the panel aligned with the command.
			mutedText(c, mark, a.chatFont(10)).Width(10)
			title := mui.Text(c, m.Title).Bold().FontSize(a.chatFont(12)).Grow(1)
			if !m.DetailOpen {
				// Long commands stay one line until the card is opened.
				title.SingleLine()
			}
			if status != "" {
				mui.Text(c, status).FontSize(a.chatFont(11)).TextColor(statusColor).Shrink(0)
			}
		})
		parts.Panel(func() {
			if m.Detail != "" {
				mutedText(c, m.Detail, a.chatFont(11)).Selectable()
			}
		}).PaddingStart(16)
	})
}

func toolStatusLabel(status string) string {
	normalized := strings.TrimSpace(status)
	switch strings.ToLower(normalized) {
	case "":
		return ""
	case "pending":
		return "Pending"
	case "in_progress", "running":
		return "In progress"
	case "completed", "complete":
		return "Completed"
	case "failed", "error":
		return "Failed"
	case "cancelled", "canceled":
		return "Cancelled"
	default:
		return strings.ToUpper(normalized[:1]) + normalized[1:]
	}
}

func (a *App) addMessage(chat *Chat, m *Message) *Message {
	if chat == nil {
		return m
	}
	chat.Messages = append(chat.Messages, m)
	return m
}

// newTurnSummary builds the muted runtime row that closes one turn. It is a
// real message so replay can place it at the same boundary as a live turn.
func (a *App) newTurnSummary(elapsed time.Duration) *Message {
	if elapsed <= 0 {
		return nil
	}
	return &Message{ID: a.nextID(), Role: RoleTurn, Elapsed: elapsed, Done: true}
}

func (a *App) addTurnSummary(chat *Chat, elapsed time.Duration) *Message {
	message := a.newTurnSummary(elapsed)
	if message == nil {
		return nil
	}
	return a.addMessage(chat, message)
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
	a.markChatUnread(chat)
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
	if _, ok := subagentControlLabel(call); ok {
		return ""
	}
	if text := rawText(call.Content); text != "" {
		return truncate(text, 2000)
	}
	if text := rawText(call.RawOutput); text != "" {
		return truncate(text, 2000)
	}
	return compactJSON(call.RawInput)
}

// applyToolPresentation preserves partial tool updates while keeping an
// orchestration event readable once it has been recognized. Later updates may
// omit the payload that identified the call, so they must not reintroduce its
// raw title or JSON detail.
func applyToolPresentation(message *Message, call session.ToolCall) {
	if message == nil {
		return
	}
	if label, ok := subagentControlLabel(call); ok {
		message.Title = label
		message.Detail = ""
		return
	}
	if _, established := subagentControlLabel(session.ToolCall{Title: message.Title}); established {
		return
	}
	if call.Title != "" {
		message.Title = call.Title
	}
	if detail := toolDetail(call); detail != "" {
		message.Detail = detail
	}
}

func subagentControlLabel(call session.ToolCall) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(call.Title))
	key = strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	switch key {
	case "spawn", "spawnagent", "spawnsubagent":
		return "Spawn subagent", true
	case "sendinput", "sendmessagetosubagent":
		return "Send message to subagent", true
	case "followup", "followuptask", "continuesubagent":
		return "Continue subagent", true
	case "waitagent", "waitforsubagent":
		return "Wait for subagent", true
	case "listagents", "listsubagents":
		return "List subagents", true
	case "resumeagent", "resumesubagent":
		return "Resume subagent", true
	case "closeagent", "closesubagent":
		return "Close subagent", true
	}
	if key == "wait" && hasSubagentPayload(call) {
		return "Wait for subagent", true
	}
	return "", false
}

func hasSubagentPayload(call session.ToolCall) bool {
	payload := strings.ToLower(string(call.RawInput) + string(call.Content) + string(call.RawOutput))
	for _, marker := range []string{"agentsstates", "receiverthreadids", "senderthreadid", "agentid"} {
		if strings.Contains(payload, marker) {
			return true
		}
	}
	return false
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
