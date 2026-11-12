package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

// discover resolves the login shell environment, builds the builtin launch
// specs and probes each one with initialize. One missing agent must not hide
// the other.
func (a *App) startDiscovery() {
	if a.closed.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	a.discoverMu.Lock()
	if a.discoverCancel != nil {
		a.discoverCancel()
	}
	a.discoverCancel = cancel
	a.discoverMu.Unlock()
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		defer cancel()
		a.discover(ctx)
	}()
}

func (a *App) cancelDiscovery() {
	a.discoverMu.Lock()
	cancel := a.discoverCancel
	a.discoverCancel = nil
	a.discoverMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) discover(ctx context.Context) {
	env := agent.ResolveEnvironment(ctx, a.resolveOpts)
	results := agent.BuiltinLaunchResults(a.Resources, env)
	statuses := make([]AgentStatus, len(results))

	var wg sync.WaitGroup
	for i, result := range results {
		statuses[i] = AgentStatus{
			ID:          result.ID,
			Name:        result.Name,
			InstallHint: result.InstallHint,
			Status:      AgentProbing,
		}
		if result.Err != nil {
			statuses[i].Status = AgentMissing
			statuses[i].Detail = result.Err.Error()
			continue
		}
		wg.Add(1)
		go func(i int, result agent.LaunchResult) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			info, err := session.Probe(probeCtx, result.Spec)
			switch {
			case err == nil:
				statuses[i].Status = AgentReady
				if info.AgentInfo != nil {
					statuses[i].Detail = strings.TrimSpace(info.AgentInfo.Name + " " + info.AgentInfo.Version)
				}
			case errors.Is(err, context.DeadlineExceeded):
				statuses[i].Status = AgentError
				statuses[i].Detail = "initialize probe timed out after 5s"
				a.appendAgentLog(result.ID, []byte("[probe] timeout after 5s\n"))
			default:
				statuses[i].Status = AgentNotACP
				statuses[i].Detail = truncate(err.Error(), 600)
				a.appendAgentLog(result.ID, []byte("[probe] "+err.Error()+"\n"))
			}
		}(i, result)
	}
	wg.Wait()

	a.Post(func() {
		if a.closed.Load() {
			return
		}
		selected := a.preferredAgentID
		if selected == "" && a.AgentIndex >= 0 && a.AgentIndex < len(a.Agents) {
			selected = a.Agents[a.AgentIndex].ID
		}
		a.Resolving = false
		a.Env = env
		a.AgentStatuses = statuses
		a.Agents = nil
		a.ResolveError = ""
		var details []string
		for i, result := range results {
			if statuses[i].Status == AgentReady {
				a.Agents = append(a.Agents, result.Spec)
			} else {
				details = append(details, fmt.Sprintf("%s: %s (%s)", result.Name, statuses[i].Status, statuses[i].Detail))
			}
		}
		a.AgentIndex = 0
		for i, spec := range a.Agents {
			if spec.ID == selected {
				a.AgentIndex = i
				break
			}
		}
		if len(a.Agents) == 0 {
			a.ResolveError = strings.Join(details, "\n")
			if a.ResolveError == "" {
				a.ResolveError = "No builtin ACP agent is available."
			}
		}
		// Drafts created before discovery finished can now pick the default
		// builtin agent.
		for _, chat := range a.chatsSnapshot() {
			if chat.Session == nil && chat.Agent.ID == "" && len(a.Agents) > 0 {
				chat.Agent = a.Agents[a.AgentIndex]
			}
		}
		// Second pass: read each ready agent's config options so the composer
		// can offer provider, model and thinking without a connect.
		a.probeAgentConfigs()
		// Import every ready agent's saved sessions in the background. The
		// sidebar draws from the archive, so a workspace added later in this
		// run still shows history from agents that were never connected.
		for _, spec := range a.Agents {
			a.syncSavedSessionsOnce(spec.ID)
		}
	})
}

// probeAgentConfig starts one throwaway session per ready agent to cache its
// config options. It runs after the status check has already been published,
// so it never delays startup and cannot change an agent's status.
func (a *App) probeAgentConfigs() {
	for _, spec := range a.Agents {
		a.probeAgentConfig(spec, true)
	}
}

const (
	agentConfigProbeTimeout = 12 * time.Second
	// A draft panel re-reads the agent's model list when it opens, but not
	// more often than this; each read spawns an agent + session/new.
	agentConfigRefreshAfter = 30 * time.Second
	// With an empty cache (for example after login was still missing during
	// discovery), retry sooner so the panel can populate without a restart.
	agentConfigRetryAfter = 5 * time.Second
)

// probeAgentConfig refreshes one agent's cached config options in the
// background. ACP has no separate provider-readiness method: the model
// select in session/new is the agent's own list of usable models, so
// re-reading it is how stale or unauthenticated providers disappear. A
// failed read keeps the previous cache instead of showing an empty panel.
// All map access is on the UI thread; the goroutine only posts its result.
func (a *App) probeAgentConfig(spec agent.LaunchSpec, force bool) {
	if a.closed.Load() || spec.ID == "" || a.probeConfig == nil {
		return
	}
	if a.agentConfigBusy == nil {
		a.agentConfigBusy = make(map[string]bool)
	}
	if a.agentConfigAt == nil {
		a.agentConfigAt = make(map[string]time.Time)
	}
	if a.agentConfigBusy[spec.ID] {
		return
	}
	if !force {
		wait := agentConfigRefreshAfter
		if len(a.AgentConfigs[spec.ID]) == 0 {
			wait = agentConfigRetryAfter
		}
		if at, ok := a.agentConfigAt[spec.ID]; ok && time.Since(at) < wait {
			return
		}
	}
	a.agentConfigBusy[spec.ID] = true
	ctx, cancel := context.WithTimeout(a.lifeCtx, agentConfigProbeTimeout)
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		defer cancel()
		info, err := a.probeConfig(ctx, spec)
		// ProbeConfig ignores a failed session/new; an empty SessionID means
		// the agent did not actually return a session, so keep old options.
		fresh := err == nil && info.SessionID != ""
		options := copyConfigOptions(info.ConfigOptions)
		a.Post(func() {
			delete(a.agentConfigBusy, spec.ID)
			if a.closed.Load() {
				return
			}
			// Throttle failed attempts too, so a broken agent is not spawned
			// every time the panel opens.
			a.agentConfigAt[spec.ID] = time.Now()
			if !fresh {
				return
			}
			a.AgentConfigs[spec.ID] = options
		})
	}()
}

// refreshAgentConfig re-reads the provider/model list behind a draft. A
// connected chat reads its live session instead.
func (a *App) refreshAgentConfig(chat *Chat) {
	if chat == nil || chat.Session != nil || chat.Connecting {
		return
	}
	spec := chat.Agent
	if spec.ID == "" {
		var ok bool
		spec, ok = a.preferredAgent()
		if !ok {
			return
		}
	}
	a.probeAgentConfig(spec, false)
}

// cacheAgentConfigs keeps the draft cache in step with a live session, whose
// configOptions came from a real session/new and therefore already reflect
// the agent's current model availability.
func (a *App) cacheAgentConfigs(agentID string, options []acp.ConfigOption) {
	if agentID == "" || a.closed.Load() {
		return
	}
	if a.AgentConfigs == nil {
		a.AgentConfigs = make(map[string][]acp.ConfigOption)
	}
	if a.agentConfigAt == nil {
		a.agentConfigAt = make(map[string]time.Time)
	}
	a.AgentConfigs[agentID] = copyConfigOptions(options)
	a.agentConfigAt[agentID] = time.Now()
}

func copyConfigOptions(options []acp.ConfigOption) []acp.ConfigOption {
	if len(options) == 0 {
		return nil
	}
	out := make([]acp.ConfigOption, len(options))
	for i, option := range options {
		out[i] = option
		out[i].Options = append([]acp.ConfigOptionValue(nil), option.Options...)
	}
	return out
}

func (a *App) retryDiscovery() {
	if a.Resolving || a.injectedAgents || a.closed.Load() {
		return
	}
	a.Resolving = true
	a.ResolveError = ""
	a.AgentConfigs = make(map[string][]acp.ConfigOption)
	for i := range a.AgentStatuses {
		a.AgentStatuses[i].Status = AgentProbing
		a.AgentStatuses[i].Detail = ""
	}
	a.startDiscovery()
}

// Connect validates the active chat's workspace and starts initialize plus
// session/new or session/load in a goroutine. Other chats keep running.
func (a *App) Connect() {
	if a.closed.Load() {
		return
	}
	chat := a.activeChat()
	if chat == nil {
		chat = a.ensureChat()
	}
	if chat.Connecting || chat.Session != nil {
		return
	}
	spec := chat.Agent
	if spec.ID == "" {
		selected, ok := a.preferredAgent()
		if !ok {
			chat.ConnectError = "No agent selected."
			chat.clearPendingSession()
			chat.pendingSend = false
			return
		}
		spec = selected
		chat.Agent = selected
	}
	// Use the current catalog entry, not a copy made when the draft was
	// created, so discovery refreshes and test-injected paths apply.
	for _, current := range a.Agents {
		if current.ID == spec.ID {
			spec = current
			chat.Agent = current
			break
		}
	}
	workspace, err := agent.ValidateWorkspace(chat.Workspace)
	if err != nil {
		chat.WorkspaceError = err.Error()
		chat.clearPendingSession()
		chat.pendingSend = false
		return
	}
	chat.Workspace = workspace
	chat.WorkspaceError = ""
	chat.ConnectError = ""
	chat.gitWorkspace = ""
	chat.Connecting = true
	chat.Busy = "connect"
	chat.Unread = false
	chat.Stderr = ""
	chat.StderrBytes = 0
	a.ensureWorkspace(workspace)
	a.WorkspaceCollapsed[workspace] = false
	a.persistSettings()

	action := chat.pendingSessionAction
	sessionID := chat.pendingSessionID
	title := chat.pendingSessionTitle
	pendingConfig := chat.draftConfig()
	chat.clearPendingSession()
	cfg := session.Config{
		Launch:     spec,
		Workspace:  workspace,
		MCPServers: chat.MCPServers,
		ClientInfo: acp.Implementation{
			Name:    "acp-gui",
			Title:   "ACP GUI",
			Version: a.Version,
		},
		DeferUpdates: true,
	}
	if action == "load" {
		cfg.LoadSessionID = sessionID
	}
	a.logSessionStart(spec, workspace, action, sessionID)

	ctx, gen, cancel := chat.beginConnect()
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		defer cancel()

		cfg.OnUpdate = func(ev session.Event) {
			a.Post(func() {
				if a.connectIsCurrent(chat, gen) {
					a.applyUpdate(chat, ev)
				}
			})
		}
		cfg.OnPermission = func(ctx context.Context, req session.PermissionRequest) (acp.RequestPermissionResult, error) {
			return a.askPermission(chat, ctx, req)
		}
		cfg.OnStderr = func(chunk string) {
			a.appendAgentLog(spec.ID, []byte(chunk))
			a.Post(func() {
				if a.connectIsCurrent(chat, gen) {
					a.appendStderr(chat, chunk)
				}
			})
		}
		cfg.OnProtocolError = func(err error) {
			line := "[protocol] " + err.Error() + "\n"
			a.appendAgentLog(spec.ID, []byte(line))
			a.Post(func() {
				if a.connectIsCurrent(chat, gen) {
					a.appendStderr(chat, line)
				}
			})
		}
		cfg.OnExit = func(exit session.Exit) {
			// The process is already gone and its descendants have been
			// signalled; it no longer needs shutdown tracking.
			a.appendAgentLog(spec.ID, []byte(fmt.Sprintf("[exit] expected=%t err=%v\n\n", exit.Expected, exit.Err)))
			a.untrackSession(exit.Session)
			a.Post(func() {
				a.applyExit(chat, exit)
			})
		}
		s, err := session.Start(ctx, cfg)
		if err != nil {
			a.appendAgentLog(spec.ID, []byte("connect failed: "+err.Error()+"\n"))
			a.Post(func() {
				if a.closed.Load() || !a.connectIsCurrent(chat, gen) {
					return
				}
				chat.Connecting = false
				chat.Busy = ""
				chat.ConnectError = connectError(err, spec)
				chat.pendingSend = false
				a.markAgentAuth(spec.ID, err)
				chat.clearConnect(gen)
			})
			return
		}
		if !a.trackSession(s) {
			return
		}
		a.applyDraftConfig(ctx, s, spec.ID, pendingConfig)
		a.Post(func() {
			if a.closed.Load() || !a.connectIsCurrent(chat, gen) {
				a.untrackSession(s)
				go func() { _ = s.Close() }()
				return
			}
			select {
			case <-s.Done():
				// The agent exited in the small window between the session
				// handshake returning and this update running.
				chat.Connecting = false
				chat.Busy = ""
				chat.ConnectError = "The agent exited immediately after creating the session."
				return
			default:
			}
			chat.Session = s
			chat.Info = s.Info()
			a.cacheAgentConfigs(spec.ID, chat.Info.ConfigOptions)
			chat.PendingConfig = nil
			chat.Connecting = false
			chat.Connected = true
			chat.ConnectError = ""
			chat.Busy = ""
			chat.Unread = false
			pendingSend := chat.pendingSend
			queuedAttachments := append([]Attachment(nil), chat.Attachments...)
			a.resetConversation(chat)
			if pendingSend {
				chat.Attachments = queuedAttachments
			}
			chat.pendingSend = pendingSend
			chat.replaying = action == "load"
			if title != "" {
				chat.Title = title
			}
			a.rememberArchivedSession(chat)
			if name := a.archivedShowName(chat.Agent.ID, chat.Info.SessionID); name != "" {
				chat.Title = name
			}
			a.markAgentReady(spec.ID)
			chat.clearConnect(gen)

			// Flush history and available_commands_update buffered during
			// initialize/session setup after the UI state is installed.
			s.Activate()
			a.syncSavedSessionsOnce(spec.ID)
			if chat.replaying {
				a.Post(func() { a.finishReplay(chat) })
			}
			if pendingSend {
				chat.pendingSend = false
				a.send()
			}
		})
	}()
}

func (a *App) connectIsCurrent(chat *Chat, gen int) bool {
	return chat != nil && !chat.Closed && a.chatIndex(chat) >= 0 && chat.connectCurrent(gen) && !a.closed.Load()
}

// applyDraftConfig pushes the choices made in a draft (model, thinking, …)
// onto the freshly created session, before the first prompt is sent, so the
// turn uses what the composer showed. It runs on the connect goroutine.
func (a *App) applyDraftConfig(ctx context.Context, s *session.Session, agentID string, pending map[string]string) {
	if len(pending) == 0 {
		return
	}
	info := s.Info()
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		// The model decides which thinking values exist.
		if (ids[i] == "model") != (ids[j] == "model") {
			return ids[i] == "model"
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		value := pending[id]
		option := info.ConfigOption(id)
		if option == nil || option.CurrentValue == value || !configHasChoice(*option, value) {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err := s.SetConfigOption(callCtx, id, value)
		cancel()
		if err != nil {
			a.appendAgentLog(agentID, []byte("[config] "+id+"="+value+": "+err.Error()+"\n"))
			continue
		}
		info = s.Info()
	}
}

func (a *App) markAgentAuth(id string, err error) {
	if session.IsAuthRequired(err) {
		a.setAgentStatus(id, AgentNeedsAuth, err.Error())
	} else {
		a.setAgentStatus(id, AgentError, err.Error())
	}
}

func (a *App) markAgentReady(id string) {
	a.setAgentStatus(id, AgentReady, "")
}

func (a *App) setAgentStatus(id, status, detail string) {
	for i := range a.AgentStatuses {
		if a.AgentStatuses[i].ID == id {
			a.AgentStatuses[i].Status = status
			a.AgentStatuses[i].Detail = detail
			return
		}
	}
}

// Disconnect closes the active chat's process but keeps its tab as a draft.
func (a *App) Disconnect() {
	chat := a.activeChat()
	if chat == nil {
		return
	}
	chat.cancelConnect()
	chat.Connecting = false
	chat.Busy = ""
	if s := chat.Session; s != nil {
		chat.Session = nil
		chat.Connected = false
		a.closeInBackground(chat, s)
	}
	chat.Connected = false
	if chat.Info.SessionID != "" {
		a.cacheAgentConfigs(chat.Agent.ID, chat.Info.ConfigOptions)
	}
	chat.seedDraftConfig()
	chat.Info = session.Info{}
	chat.pendingSend = false
	a.resetConversation(chat)
}

func (a *App) closeInBackground(chat *Chat, s *session.Session) {
	go func() {
		if err := s.Close(); err != nil {
			a.Post(func() {
				if !a.closed.Load() && chat != nil && !chat.Closed {
					a.addError(chat, err)
				}
			})
		}
		a.untrackSession(s)
	}()
}

// resetConversation clears one chat's transcript and ACP view state.
func (a *App) resetConversation(chat *Chat) {
	if chat == nil {
		return
	}
	a.cancelPermissionsFor(chat)
	if chat.turn != nil && chat.turn.assistant != nil {
		chat.turn.assistant.Done = true
	}
	chat.Messages = nil
	chat.turn = nil
	chat.Streaming = false
	chat.Cancelling = false
	chat.ConfigBusy = ""
	chat.LastStop = ""
	chat.Commands = nil
	chat.Attachments = nil
	chat.Plan = nil
	chat.PlanOpen = false
	chat.Usage = nil
	chat.TurnUsage = nil
	chat.Title = ""
	chat.Unread = false
	chat.replaying = false
	chat.ModelOpen = false
	chat.ModeOpen = false
	chat.MenuOpen = false
	chat.JumpOpen = false
	chat.ModelProvider = ""
}

func (a *App) send() bool {
	chat := a.activeChat()
	if chat == nil || !chat.canSend() {
		return false
	}
	text := strings.TrimSpace(chat.Input)
	attachments := append([]Attachment(nil), chat.Attachments...)
	if text == "" && len(attachments) == 0 {
		return false
	}
	if chat.Session == nil {
		// The first message connects the draft and is then sent by the
		// successful connect callback.
		if !chat.Connecting {
			chat.pendingSend = true
			a.Connect()
		}
		if !chat.Connecting {
			chat.pendingSend = false
		}
		return false
	}
	s := chat.Session
	if s == nil {
		return false
	}
	if len(attachments) > 0 && !chat.Info.AgentCapabilities.PromptCapabilities.Image {
		a.addError(chat, attachmentError("This agent does not support image attachments."))
		return false
	}
	chat.Input = ""
	chat.Attachments = nil
	chat.ConnectError = ""
	chat.TurnUsage = nil

	a.addMessage(chat, &Message{
		ID:          a.nextID(),
		Role:        RoleUser,
		Text:        text,
		Done:        true,
		Attachments: attachments,
	})
	assistant := a.addMessage(chat, &Message{
		ID:   a.nextID(),
		Role: RoleAssistant,
	})
	turn := &turnState{assistant: assistant, tools: make(map[string]*Message)}
	chat.turn = turn
	chat.Streaming = true
	chat.Cancelling = false
	chat.LastStop = ""

	blocks := make([]acp.ContentBlock, 0, len(attachments)+1)
	if text != "" {
		blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})
	}
	for _, attachment := range attachments {
		blocks = append(blocks, acp.ContentBlock{
			Type:     "image",
			MimeType: attachment.MimeType,
			Data:     attachment.Data,
			Name:     attachment.Name,
		})
	}

	go func() {
		res, err := s.PromptContent(context.Background(), blocks)
		a.Post(func() {
			a.applyPromptResult(chat, turn, res, err)
		})
	}()
	return true
}

func (a *App) stop() {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || !chat.Streaming || chat.Cancelling {
		return
	}
	s := chat.Session
	chat.Cancelling = true
	go func() {
		err := s.Cancel()
		a.Post(func() {
			if chat.Session != s {
				return
			}
			if err != nil {
				chat.Cancelling = false
				a.addError(chat, err)
			}
		})
	}()
}

func (a *App) setConfigOption(id, value string) {
	chat := a.activeChat()
	if chat == nil || chat.ConfigBusy != "" || chat.Connecting || chat.Streaming || chat.Busy != "" {
		return
	}
	if chat.Session == nil {
		// Draft: there is no session yet, so remember the choice and let
		// Connect push it after session/new.
		if chat.PendingConfig == nil {
			chat.PendingConfig = make(map[string]string)
		}
		chat.PendingConfig[id] = value
		return
	}
	s := chat.Session
	chat.ConfigBusy = id
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		options, err := s.SetConfigOption(ctx, id, value)
		a.Post(func() {
			if chat.Session != s {
				return
			}
			chat.ConfigBusy = ""
			if len(options) > 0 {
				chat.Info.ConfigOptions = options
			}
			chat.Info = s.Info()
			a.cacheAgentConfigs(chat.Agent.ID, chat.Info.ConfigOptions)
			if err != nil {
				a.addError(chat, err)
			}
		})
	}()
}

func (a *App) setMode(modeID string) {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || chat.ConfigBusy != "" || chat.Connecting || chat.Streaming || chat.Busy != "" {
		return
	}
	s := chat.Session
	chat.ConfigBusy = "mode"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := s.SetMode(ctx, modeID)
		a.Post(func() {
			if chat.Session != s {
				return
			}
			chat.ConfigBusy = ""
			chat.Info = s.Info()
			if err == nil {
				chat.Info.CurrentModeID = modeID
			} else {
				a.addError(chat, err)
			}
		})
	}()
}

// newSession creates a new ACP session in the active chat's agent process,
// leaving other chats untouched.
func (a *App) newSession() {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || chat.Busy != "" || chat.Streaming {
		return
	}
	s := chat.Session
	oldID := s.Info().SessionID
	snapshot := chat.snapshot()
	a.resetConversation(chat)
	chat.Busy = "new"
	workspace := chat.Workspace
	mcpServers := append([]any(nil), chat.MCPServers...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := s.NewSession(ctx, workspace, mcpServers)
		if err == nil && oldID != "" && oldID != info.SessionID {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.CloseSessionID(closeCtx, oldID)
			closeCancel()
		}
		a.Post(func() {
			chat.Busy = ""
			if chat.Session != s {
				return
			}
			if err != nil {
				chat.restore(snapshot)
				a.addError(chat, err)
				return
			}
			chat.Info = info
			chat.Title = info.Title
			a.cacheAgentConfigs(chat.Agent.ID, info.ConfigOptions)
			a.rememberArchivedSession(chat)
		})
	}()
}

func (a *App) closeActiveSession() {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || chat.Busy != "" || chat.Info.SessionID == "" {
		return
	}
	s := chat.Session
	chat.Busy = "close"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.CloseSession(ctx)
		a.Post(func() {
			chat.Busy = ""
			if chat.Session != s {
				return
			}
			if err != nil {
				a.addError(chat, err)
				return
			}
			chat.Info = s.Info()
			a.resetConversation(chat)
		})
	}()
}

// loadHistory opens a saved session in the active chat, replaying history.
func (a *App) loadHistory(sessionID, title, cwd string) {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || chat.Busy != "" || chat.Streaming {
		return
	}
	s := chat.Session
	if cwd != "" {
		validated, err := agent.ValidateWorkspace(cwd)
		if err != nil {
			a.addError(chat, err)
			return
		}
		cwd = validated
	}
	info := s.Info()
	oldID := info.SessionID
	snapshot := chat.snapshot()
	a.resetConversation(chat)
	chat.Info.SessionID = sessionID
	if title != "" {
		chat.Title = title
	}
	chat.replaying = true
	chat.Busy = "load"

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		newInfo, err := s.LoadSessionAt(ctx, sessionID, cwd)
		if err == nil && oldID != "" && oldID != sessionID {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.CloseSessionID(closeCtx, oldID)
			closeCancel()
		}
		a.Post(func() {
			chat.Busy = ""
			if chat.Session != s {
				return
			}
			if err != nil {
				chat.restore(snapshot)
				a.addError(chat, err)
				return
			}
			chat.Info = newInfo
			a.cacheAgentConfigs(chat.Agent.ID, newInfo.ConfigOptions)
			if cwd != "" {
				workspace := a.workspacePath(cwd)
				chat.Workspace = workspace
				a.Workspace = workspace
				a.ensureWorkspace(workspace)
				a.WorkspaceCollapsed[workspace] = false
			}
			if newInfo.Title != "" {
				chat.Title = newInfo.Title
			} else if title != "" {
				chat.Title = title
			}
			a.rememberArchivedSession(chat)
			if name := a.archivedShowName(chat.Agent.ID, chat.Info.SessionID); name != "" {
				chat.Title = name
			}
			if chat.replaying {
				a.finishReplay(chat)
			}
		})
	}()
}

func (a *App) finishReplay(chat *Chat) {
	if chat == nil || !chat.replaying {
		return
	}
	for _, message := range chat.Messages {
		message.Done = true
	}
	chat.turn = nil
	chat.replaying = false
}

func (a *App) applyUpdate(chat *Chat, ev session.Event) {
	if chat == nil || chat.Closed || chat.Session != ev.Session {
		return
	}
	if chat.Info.SessionID == "" && chat.Busy == "" {
		return
	}
	// Ignore late updates from a session we already switched away from.
	// A new chat does not know its id before session/new answers, so it
	// accepts everything until then; old sessions are idle at that point.
	if chat.Busy != "new" && ev.SessionID != "" && chat.Info.SessionID != "" && ev.SessionID != chat.Info.SessionID {
		return
	}
	if chat != a.activeChat() {
		chat.Unread = true
	}
	if ev.Kind == session.UpdateMessageChunk {
		if ev.StartupInfo {
			// pi-acp's prelude (pi version, extensions, skills, ...) is not
			// part of the model's answer. The status bar already shows what
			// matters; never append it to the assistant message.
			return
		}
		if ev.Notice != nil {
			a.addNotice(chat, ev.Text, ev.Notice)
			return
		}
	}
	switch ev.Kind {
	case session.UpdateCommands:
		chat.Commands = append([]acp.AvailableCommand(nil), ev.Commands...)
		return
	case session.UpdateConfigOption:
		if len(ev.ConfigOptions) > 0 {
			chat.Info.ConfigOptions = ev.ConfigOptions
			a.cacheAgentConfigs(chat.Agent.ID, ev.ConfigOptions)
		}
		chat.ConfigBusy = ""
		return
	case session.UpdateCurrentMode:
		if ev.CurrentModeID != "" {
			chat.Info.CurrentModeID = ev.CurrentModeID
		}
		chat.ConfigBusy = ""
		return
	case session.UpdateSessionInfo:
		if ev.Title != "" {
			chat.Info.Title = ev.Title
			a.setArchivedTitle(chat, ev.Title)
			if name := a.archivedShowName(chat.Agent.ID, chat.Info.SessionID); name != "" {
				chat.Title = name
			} else {
				chat.Title = ev.Title
			}
		}
		return
	case session.UpdateUsage:
		chat.Usage = ev.Usage
		return
	case session.UpdatePlan:
		hadPlan := len(chat.Plan) > 0
		chat.Plan = append([]session.PlanEntry(nil), ev.Plan...)
		if len(chat.Plan) == 0 {
			chat.PlanOpen = false
		} else if !hadPlan {
			chat.PlanOpen = true
		}
		return
	}

	if ev.Kind == session.UpdateMessageChunk || ev.Kind == session.UpdateUserChunk || ev.Kind == session.UpdateThoughtChunk {
		if ev.Text == "" {
			return
		}
		if chat.replaying {
			role := RoleAssistant
			if ev.Kind == session.UpdateUserChunk {
				role = RoleUser
			}
			msg := a.replayMessage(chat, role)
			if ev.Kind == session.UpdateThoughtChunk {
				msg.Thought += ev.Text
			} else {
				msg.Text += ev.Text
			}
			return
		}
	}

	var turn *turnState
	switch ev.Kind {
	case session.UpdateMessageChunk:
		turn = a.ensureTurn(chat, true)
		turn.assistant.Done = !chat.Streaming
		turn.assistant.Text += ev.Text
	case session.UpdateUserChunk:
		a.replayMessage(chat, RoleUser).Text += ev.Text
	case session.UpdateThoughtChunk:
		turn = a.ensureTurn(chat, true)
		turn.assistant.Thought += ev.Text
	case session.UpdateToolCall, session.UpdateToolCallDone:
		turn = a.ensureTurn(chat, false)
		if ev.ToolCall == nil {
			return
		}
		call := *ev.ToolCall
		msg := turn.tools[call.ToolCallID]
		if call.ToolCallID == "" {
			msg = nil
		}
		created := false
		if msg == nil {
			created = true
			msg = &Message{
				ID:         a.nextID(),
				Role:       RoleTool,
				Done:       true,
				ToolCallID: call.ToolCallID,
			}
			turn.tools[call.ToolCallID] = msg
			a.addMessage(chat, msg)
		}
		if call.Title != "" {
			msg.Title = call.Title
		}
		if msg.Title == "" {
			msg.Title = "Tool call"
		}
		if call.Status != "" {
			msg.Status = call.Status
		}
		if detail := toolDetail(call); detail != "" {
			msg.Detail = detail
		}
		// Text that follows a new tool call belongs to a new assistant row,
		// after the tool card, not back above it.
		if created {
			if turn.assistant != nil {
				turn.assistant.Done = true
			}
			turn.assistant = nil
		}
	}
}

// addNotice inserts adapter status text (retry, compaction, queue, ...) as a
// centered system row. The current assistant row is closed so continuation
// text after the notice starts a new row instead of appending above it.
func (a *App) addNotice(chat *Chat, text string, notice *session.Notice) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	kind, level := notice.Kind, notice.Level
	if level == "" {
		level = "info"
	}
	a.closeAssistantForNotice(chat)
	switch kind {
	case "queue", "auto_retry", "auto_compaction":
		if n := len(chat.Messages); n > 0 {
			last := chat.Messages[n-1]
			if last.Role == RoleSystem && last.NoticeKind == kind {
				last.Text = text
				last.NoticeLevel = level
				return
			}
		}
	}
	a.addMessage(chat, &Message{
		ID:          a.nextID(),
		Role:        RoleSystem,
		Text:        text,
		Done:        true,
		NoticeKind:  kind,
		NoticeLevel: level,
	})
}

// closeAssistantForNotice ends the current assistant row. An eager empty row
// (created when the prompt was sent) is removed instead of leaving a blank
// bubble above the status text.
func (a *App) closeAssistantForNotice(chat *Chat) {
	if chat.turn == nil || chat.turn.assistant == nil {
		return
	}
	message := chat.turn.assistant
	chat.turn.assistant = nil
	if message.Text != "" || message.Thought != "" {
		message.Done = true
		return
	}
	for i, candidate := range chat.Messages {
		if candidate == message {
			chat.Messages = append(chat.Messages[:i], chat.Messages[i+1:]...)
			return
		}
	}
}

func (a *App) ensureTurn(chat *Chat, needAssistant bool) *turnState {
	if chat.turn == nil {
		chat.turn = &turnState{tools: make(map[string]*Message)}
	}
	if needAssistant && chat.turn.assistant == nil {
		assistant := &Message{ID: a.nextID(), Role: RoleAssistant, Done: !chat.Streaming}
		a.addMessage(chat, assistant)
		chat.turn.assistant = assistant
	}
	return chat.turn
}

// replayMessage appends to the current row of role, or starts a new row.
func (a *App) replayMessage(chat *Chat, role MessageRole) *Message {
	if n := len(chat.Messages); n > 0 {
		last := chat.Messages[n-1]
		if last.Role == role && !last.Done {
			return last
		}
		last.Done = true
	}
	message := &Message{ID: a.nextID(), Role: role}
	a.addMessage(chat, message)
	return message
}

func (a *App) applyPromptResult(chat *Chat, turn *turnState, res acp.PromptResult, err error) {
	if chat == nil || chat.Session == nil || chat.turn != turn {
		return
	}
	if turn.assistant != nil {
		turn.assistant.Done = true
	}
	chat.Streaming = false
	chat.Cancelling = false
	// A turn may end while a permission request is still waiting (for
	// example after an agent-side cancel). Do not leave a dead dialog.
	a.cancelPermissionsFor(chat)
	if res.StopReason != "" {
		chat.LastStop = res.StopReason
	}
	if usage, ok := res.Tokens(); ok {
		chat.TurnUsage = &usage
	}
	a.touchArchivedSession(chat)
	if err != nil && !errors.Is(err, acp.ErrClosed) {
		a.addError(chat, err)
	}
	if chat != a.activeChat() {
		chat.Unread = true
	}
}

func (a *App) applyExit(chat *Chat, exit session.Exit) {
	if chat == nil || chat.Closed || chat.Session != exit.Session {
		return
	}
	if exit.Err != nil {
		a.appendStderr(chat, fmt.Sprintf("\n[agent exited: %v]\n", exit.Err))
	} else {
		a.appendStderr(chat, "\n[agent exited]\n")
	}
	chat.Session = nil
	chat.Connected = false
	chat.Connecting = false
	chat.Busy = ""
	chat.Streaming = false
	chat.Cancelling = false
	chat.Info = session.Info{}
	chat.replaying = false
	if chat.turn != nil {
		if chat.turn.assistant != nil {
			chat.turn.assistant.Done = true
		}
		chat.turn = nil
	}
	a.cancelPermissionsFor(chat)
	if !exit.Expected {
		msg := "Agent exited unexpectedly."
		if exit.Err != nil {
			msg += " " + exit.Err.Error()
		}
		if tail := strings.TrimSpace(exit.Stderr); tail != "" {
			if len(tail) > 2000 {
				tail = tail[len(tail)-2000:]
			}
			msg += "\n" + tail
		}
		a.addError(chat, errors.New(msg))
	}
	if chat != a.activeChat() {
		chat.Unread = true
	}
}

func connectError(err error, spec agent.LaunchSpec) string {
	var auth *session.AuthRequiredError
	if errors.As(err, &auth) {
		var b strings.Builder
		b.WriteString("Authentication required.")
		for _, method := range auth.Methods {
			b.WriteString("\n• ")
			if method.Name != "" {
				b.WriteString(method.Name)
			} else {
				b.WriteString(method.ID)
			}
			if method.Description != "" {
				b.WriteString(" — ")
				b.WriteString(method.Description)
			}
		}
		if spec.LoginHint != "" {
			b.WriteString("\nRun in a terminal, then reconnect:\n")
			b.WriteString(spec.LoginHint)
		}
		return b.String()
	}
	return err.Error()
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}
