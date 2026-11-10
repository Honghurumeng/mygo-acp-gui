package ui

import (
	"context"
	"errors"
	"fmt"
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
			default:
				statuses[i].Status = AgentNotACP
				statuses[i].Detail = truncate(err.Error(), 600)
			}
		}(i, result)
	}
	wg.Wait()

	a.Post(func() {
		if a.closed.Load() {
			return
		}
		selected := ""
		if a.AgentIndex >= 0 && a.AgentIndex < len(a.Agents) {
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
	})
}

func (a *App) retryDiscovery() {
	if a.Resolving || a.injectedAgents || a.closed.Load() {
		return
	}
	a.Resolving = true
	a.ResolveError = ""
	for i := range a.AgentStatuses {
		a.AgentStatuses[i].Status = AgentProbing
		a.AgentStatuses[i].Detail = ""
	}
	a.startDiscovery()
}

// Connect validates the workspace and starts initialize plus session/new,
// load or resume in a goroutine. Its callbacks only post updates to the UI
// thread.
func (a *App) Connect() {
	if a.closed.Load() || a.Connecting || a.Session != nil {
		return
	}
	spec, ok := a.currentAgent()
	if !ok {
		a.ConnectError = "No agent selected."
		a.clearPendingSession()
		return
	}
	workspace, err := agent.ValidateWorkspace(a.Workspace)
	if err != nil {
		a.WorkspaceError = err.Error()
		a.clearPendingSession()
		return
	}
	a.Workspace = workspace
	a.WorkspaceError = ""
	a.ConnectError = ""
	a.Connecting = true
	a.SessionBusy = "connect"

	action := a.pendingSessionAction
	sessionID := a.pendingSessionID
	title := a.pendingSessionTitle
	a.pendingSessionAction = ""
	a.pendingSessionID = ""
	a.pendingSessionTitle = ""
	cfg := session.Config{
		Launch:     spec,
		Workspace:  workspace,
		MCPServers: a.MCPServers,
		ClientInfo: acp.Implementation{
			Name:    "acp-gui",
			Title:   "ACP GUI",
			Version: a.Version,
		},
		DeferUpdates: true,
	}
	switch action {
	case "load":
		cfg.LoadSessionID = sessionID
	case "resume":
		cfg.ResumeSessionID = sessionID
	}

	a.connectMu.Lock()
	a.connectGen++
	gen := a.connectGen
	ctx, cancel := context.WithCancel(context.Background())
	a.connectCancel = cancel
	a.connectMu.Unlock()

	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		defer cancel()

		cfg.OnUpdate = func(ev session.Event) {
			a.Post(func() {
				if a.connectIsCurrent(gen) {
					a.applyUpdate(ev)
				}
			})
		}
		cfg.OnPermission = a.askPermission
		cfg.OnExit = func(exit session.Exit) {
			// The process is already gone and its descendants have been
			// signalled; it no longer needs shutdown tracking.
			a.untrackSession(exit.Session)
			a.Post(func() {
				if a.Session == exit.Session {
					a.applyExit(exit)
					return
				}
				if a.connectIsCurrent(gen) {
					a.applyExit(exit)
				}
			})
		}
		s, err := session.Start(ctx, cfg)
		if err != nil {
			a.Post(func() {
				if a.closed.Load() || !a.connectIsCurrent(gen) {
					return
				}
				a.Connecting = false
				a.SessionBusy = ""
				a.ConnectError = connectError(err, spec)
				a.markAgentAuth(spec.ID, err)
				a.connectMu.Lock()
				if a.connectGen == gen {
					a.connectCancel = nil
				}
				a.connectMu.Unlock()
			})
			return
		}
		if !a.trackSession(s) {
			return
		}
		a.Post(func() {
			if a.closed.Load() || !a.connectIsCurrent(gen) {
				a.untrackSession(s)
				go func() { _ = s.Close() }()
				return
			}
			select {
			case <-s.Done():
				// The agent exited in the small window between the session
				// handshake returning and this update running.
				a.Connecting = false
				a.SessionBusy = ""
				a.ConnectError = "The agent exited immediately after creating the session."
				return
			default:
			}
			a.Session = s
			a.Info = s.Info()
			a.Connecting = false
			a.Connected = true
			a.ConnectError = ""
			a.SessionBusy = ""
			a.resetConversation()
			a.replaying = action == "load"
			if title != "" {
				a.Title = title
			}
			a.markAgentReady(spec.ID)
			a.connectMu.Lock()
			if a.connectGen == gen {
				a.connectCancel = nil
			}
			a.connectMu.Unlock()

			// Flush history and available_commands_update buffered during
			// initialize/session setup after the UI state is installed.
			s.Activate()
			if a.replaying {
				a.Post(a.finishReplay)
			}
		})
	}()
}

func (a *App) clearPendingSession() {
	a.pendingSessionID = ""
	a.pendingSessionAction = ""
	a.pendingSessionTitle = ""
}

func (a *App) connectIsCurrent(gen int) bool {
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	return a.connectGen == gen && !a.closed.Load()
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

// Disconnect detaches immediately and cleans the process group in the
// background so the window does not freeze on the grace period.
func (a *App) Disconnect() {
	a.connectMu.Lock()
	a.connectGen++
	cancel := a.connectCancel
	a.connectCancel = nil
	a.connectMu.Unlock()
	if cancel != nil {
		cancel()
	}

	s := a.Session
	a.Session = nil
	a.Connected = false
	a.Connecting = false
	a.SessionBusy = ""
	a.Streaming = false
	a.Cancelling = false
	a.Info = session.Info{}
	a.resetConversation()
	a.Permission = nil
	a.PermissionOpen = false
	if s != nil {
		a.closeInBackground(s)
	}
}

func (a *App) closeInBackground(s *session.Session) {
	go func() {
		if err := s.Close(); err != nil {
			a.Post(func() {
				if !a.closed.Load() {
					a.addError(err)
				}
			})
		}
		a.untrackSession(s)
	}()
}

func (a *App) resetConversation() {
	if a.Permission != nil {
		a.finishPermission(a.Permission, acp.CancelledPermission())
	}
	if a.turn != nil && a.turn.assistant != nil {
		a.turn.assistant.Done = true
	}
	a.Messages = nil
	a.turn = nil
	a.Streaming = false
	a.Cancelling = false
	a.ConfigBusy = ""
	a.LastStop = ""
	a.Commands = nil
	a.CommandsOpen = false
	a.Attachments = nil
	a.Plan = nil
	a.PlanOpen = false
	a.Usage = nil
	a.Title = ""
	a.replaying = false
}

func (a *App) send() bool {
	if !a.canSend() {
		return false
	}
	text := strings.TrimSpace(a.Input)
	attachments := append([]Attachment(nil), a.Attachments...)
	if text == "" && len(attachments) == 0 {
		return false
	}
	s := a.Session
	a.Input = ""
	a.Attachments = nil
	a.ConnectError = ""

	a.addMessage(&Message{
		ID:          a.nextID(),
		Role:        RoleUser,
		Text:        text,
		Done:        true,
		Attachments: attachments,
	})
	assistant := a.addMessage(&Message{
		ID:   a.nextID(),
		Role: RoleAssistant,
	})
	turn := &turnState{assistant: assistant, tools: make(map[string]*Message)}
	a.turn = turn
	a.Streaming = true
	a.Cancelling = false
	a.LastStop = ""

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
			a.applyPromptResult(s, turn, res, err)
		})
	}()
	return true
}

func (a *App) hasPrompt() bool {
	return strings.TrimSpace(a.Input) != "" || len(a.Attachments) > 0
}

func (a *App) stop() {
	if a.Session == nil || !a.Streaming || a.Cancelling {
		return
	}
	s := a.Session
	a.Cancelling = true
	go func() {
		err := s.Cancel()
		a.Post(func() {
			if a.Session != s {
				return
			}
			if err != nil {
				a.Cancelling = false
				a.addError(err)
			}
		})
	}()
}

func (a *App) setConfigOption(id, value string) {
	s := a.Session
	if s == nil || a.ConfigBusy != "" || a.Connecting || a.Streaming || a.SessionBusy != "" {
		return
	}
	a.ConfigBusy = id
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		options, err := s.SetConfigOption(ctx, id, value)
		a.Post(func() {
			if a.Session != s {
				return
			}
			a.ConfigBusy = ""
			if len(options) > 0 {
				a.Info.ConfigOptions = options
			}
			a.Info = s.Info()
			if err != nil {
				a.addError(err)
			}
		})
	}()
}

func (a *App) setMode(modeID string) {
	s := a.Session
	if s == nil || a.ConfigBusy != "" || a.Connecting || a.Streaming || a.SessionBusy != "" {
		return
	}
	a.ConfigBusy = "mode"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := s.SetMode(ctx, modeID)
		a.Post(func() {
			if a.Session != s {
				return
			}
			a.ConfigBusy = ""
			a.Info = s.Info()
			if err == nil {
				a.Info.CurrentModeID = modeID
			} else {
				a.addError(err)
			}
		})
	}()
}

func (a *App) newChat() {
	s := a.Session
	if s == nil || a.SessionBusy != "" || a.Streaming {
		return
	}
	oldID := s.Info().SessionID
	snapshot := a.conversationSnapshot()
	a.resetConversation()
	a.SessionBusy = "new"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := s.NewSession(ctx, a.Workspace, a.MCPServers)
		if err == nil && oldID != "" && oldID != info.SessionID {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.CloseSessionID(closeCtx, oldID)
			closeCancel()
		}
		a.Post(func() {
			a.SessionBusy = ""
			if a.Session != s {
				return
			}
			if err != nil {
				a.restoreConversation(snapshot)
				a.addError(err)
				return
			}
			a.Info = info
			a.Title = info.Title
		})
	}()
}

func (a *App) closeActiveSession() {
	s := a.Session
	if s == nil || a.SessionBusy != "" || s.Info().SessionID == "" {
		return
	}
	a.SessionBusy = "close"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.CloseSession(ctx)
		a.Post(func() {
			a.SessionBusy = ""
			if a.Session != s {
				return
			}
			if err != nil {
				a.addError(err)
				return
			}
			a.Info = s.Info()
			a.resetConversation()
		})
	}()
}

// loadHistory opens a saved session in the live process, replaying history.
func (a *App) loadHistory(sessionID, title, cwd string) {
	a.loadHistoryMode(sessionID, false, title, cwd)
}

// resumeHistory attaches a saved session without replaying history.
func (a *App) resumeHistory(sessionID, title, cwd string) {
	a.loadHistoryMode(sessionID, true, title, cwd)
}

func (a *App) loadHistoryMode(sessionID string, resume bool, title, cwd string) {
	s := a.Session
	if s == nil || a.SessionBusy != "" || a.Streaming {
		return
	}
	if cwd != "" {
		validated, err := agent.ValidateWorkspace(cwd)
		if err != nil {
			a.addError(err)
			return
		}
		cwd = validated
	}
	info := s.Info()
	oldID := info.SessionID
	snapshot := a.conversationSnapshot()
	a.resetConversation()
	a.Info.SessionID = sessionID
	if title != "" {
		a.Title = title
	}
	a.replaying = !resume
	action := "load"
	if resume {
		action = "resume"
	}
	a.SessionBusy = action
	a.HistoryOpen = false

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var newInfo session.Info
		var err error
		if resume {
			newInfo, err = s.ResumeSessionAt(ctx, sessionID, cwd)
		} else {
			newInfo, err = s.LoadSessionAt(ctx, sessionID, cwd)
		}
		if err == nil && oldID != "" && oldID != sessionID {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.CloseSessionID(closeCtx, oldID)
			closeCancel()
		}
		a.Post(func() {
			a.SessionBusy = ""
			if a.Session != s {
				return
			}
			if err != nil {
				a.restoreConversation(snapshot)
				a.addError(err)
				return
			}
			a.Info = newInfo
			if cwd != "" {
				a.Workspace = cwd
			}
			if newInfo.Title != "" {
				a.Title = newInfo.Title
			} else if title != "" {
				a.Title = title
			}
			if a.replaying {
				a.finishReplay()
			}
		})
	}()
}

type conversationState struct {
	messages    []*Message
	turn        *turnState
	info        session.Info
	title       string
	commands    []acp.AvailableCommand
	plan        []session.PlanEntry
	planOpen    bool
	usage       *acp.Usage
	attachments []Attachment
	lastStop    string
}

func (a *App) conversationSnapshot() conversationState {
	return conversationState{
		messages:    a.Messages,
		turn:        a.turn,
		info:        a.Info,
		title:       a.Title,
		commands:    a.Commands,
		plan:        a.Plan,
		planOpen:    a.PlanOpen,
		usage:       a.Usage,
		attachments: a.Attachments,
		lastStop:    a.LastStop,
	}
}

func (a *App) restoreConversation(state conversationState) {
	a.Messages = state.messages
	a.turn = state.turn
	a.Info = state.info
	a.Title = state.title
	a.Commands = state.commands
	a.Plan = state.plan
	a.PlanOpen = state.planOpen
	a.Usage = state.usage
	a.Attachments = state.attachments
	a.LastStop = state.lastStop
	a.replaying = false
	a.Streaming = false
	a.Cancelling = false
}

func (a *App) finishReplay() {
	if !a.replaying {
		return
	}
	for _, message := range a.Messages {
		message.Done = true
	}
	a.turn = nil
	a.replaying = false
}

func (a *App) applyUpdate(ev session.Event) {
	if a.Session != ev.Session {
		return
	}
	if a.Info.SessionID == "" && a.SessionBusy == "" {
		return
	}
	// Ignore late updates from a session we already switched away from.
	// A new chat does not know its id before session/new answers, so it
	// accepts everything until then; old sessions are idle at that point.
	if a.SessionBusy != "new" && ev.SessionID != "" && a.Info.SessionID != "" && ev.SessionID != a.Info.SessionID {
		return
	}
	switch ev.Kind {
	case session.UpdateCommands:
		a.Commands = append([]acp.AvailableCommand(nil), ev.Commands...)
		return
	case session.UpdateConfigOption:
		if len(ev.ConfigOptions) > 0 {
			a.Info.ConfigOptions = ev.ConfigOptions
		}
		a.ConfigBusy = ""
		return
	case session.UpdateCurrentMode:
		if ev.CurrentModeID != "" {
			a.Info.CurrentModeID = ev.CurrentModeID
		}
		a.ConfigBusy = ""
		return
	case session.UpdateSessionInfo:
		if ev.Title != "" {
			a.Title = ev.Title
			a.Info.Title = ev.Title
		}
		return
	case session.UpdateUsage:
		a.Usage = ev.Usage
		return
	case session.UpdatePlan:
		hadPlan := len(a.Plan) > 0
		a.Plan = append([]session.PlanEntry(nil), ev.Plan...)
		if len(a.Plan) == 0 {
			a.PlanOpen = false
		} else if !hadPlan {
			a.PlanOpen = true
		}
		return
	}

	if ev.Kind == session.UpdateMessageChunk || ev.Kind == session.UpdateUserChunk || ev.Kind == session.UpdateThoughtChunk {
		if ev.Text == "" {
			return
		}
		if a.replaying {
			role := RoleAssistant
			if ev.Kind == session.UpdateUserChunk {
				role = RoleUser
			}
			msg := a.replayMessage(role)
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
		turn = a.ensureTurn(true)
		turn.assistant.Done = !a.Streaming
		turn.assistant.Text += ev.Text
	case session.UpdateUserChunk:
		a.replayMessage(RoleUser).Text += ev.Text
	case session.UpdateThoughtChunk:
		turn = a.ensureTurn(true)
		turn.assistant.Thought += ev.Text
	case session.UpdateToolCall, session.UpdateToolCallDone:
		turn = a.ensureTurn(false)
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
			a.addMessage(msg)
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

func (a *App) ensureTurn(needAssistant bool) *turnState {
	if a.turn == nil {
		a.turn = &turnState{tools: make(map[string]*Message)}
	}
	if needAssistant && a.turn.assistant == nil {
		assistant := &Message{ID: a.nextID(), Role: RoleAssistant, Done: !a.Streaming}
		a.addMessage(assistant)
		a.turn.assistant = assistant
	}
	return a.turn
}

// replayMessage appends to the current row of role, or starts a new row.
func (a *App) replayMessage(role MessageRole) *Message {
	if n := len(a.Messages); n > 0 {
		last := a.Messages[n-1]
		if last.Role == role && !last.Done {
			return last
		}
		last.Done = true
	}
	message := &Message{ID: a.nextID(), Role: role}
	a.addMessage(message)
	return message
}

func (a *App) applyPromptResult(s *session.Session, turn *turnState, res acp.PromptResult, err error) {
	if a.Session != s || a.turn != turn {
		return
	}
	if turn.assistant != nil {
		turn.assistant.Done = true
	}
	a.Streaming = false
	a.Cancelling = false
	if a.Permission != nil {
		// A turn may end while a permission request is still waiting (for
		// example after an agent-side cancel). Do not leave a dead dialog.
		a.finishPermission(a.Permission, acp.CancelledPermission())
	}
	if res.StopReason != "" {
		a.LastStop = res.StopReason
	}
	if err != nil && !errors.Is(err, acp.ErrClosed) {
		a.addError(err)
	}
}

func (a *App) applyExit(exit session.Exit) {
	if a.Session != exit.Session {
		return
	}
	a.Session = nil
	a.Connected = false
	a.Connecting = false
	a.SessionBusy = ""
	a.Streaming = false
	a.Cancelling = false
	a.Permission = nil
	a.PermissionOpen = false
	a.Info = session.Info{}
	a.resetConversation()
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
		a.addError(errors.New(msg))
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
