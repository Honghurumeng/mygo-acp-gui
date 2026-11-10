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

// Connect validates the active chat's workspace and starts initialize plus
// session/new, load or resume in a goroutine. Other chats keep running.
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
			return
		}
		spec = selected
		chat.Agent = selected
	}
	workspace, err := agent.ValidateWorkspace(chat.Workspace)
	if err != nil {
		chat.WorkspaceError = err.Error()
		chat.clearPendingSession()
		return
	}
	chat.Workspace = workspace
	chat.WorkspaceError = ""
	chat.ConnectError = ""
	chat.Connecting = true
	chat.Busy = "connect"
	chat.Unread = false

	action := chat.pendingSessionAction
	sessionID := chat.pendingSessionID
	title := chat.pendingSessionTitle
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
	switch action {
	case "load":
		cfg.LoadSessionID = sessionID
	case "resume":
		cfg.ResumeSessionID = sessionID
	}

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
		cfg.OnExit = func(exit session.Exit) {
			// The process is already gone and its descendants have been
			// signalled; it no longer needs shutdown tracking.
			a.untrackSession(exit.Session)
			a.Post(func() {
				a.applyExit(chat, exit)
			})
		}
		s, err := session.Start(ctx, cfg)
		if err != nil {
			a.Post(func() {
				if a.closed.Load() || !a.connectIsCurrent(chat, gen) {
					return
				}
				chat.Connecting = false
				chat.Busy = ""
				chat.ConnectError = connectError(err, spec)
				a.markAgentAuth(spec.ID, err)
				chat.clearConnect(gen)
			})
			return
		}
		if !a.trackSession(s) {
			return
		}
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
			chat.Connecting = false
			chat.Connected = true
			chat.ConnectError = ""
			chat.Busy = ""
			chat.Unread = false
			a.resetConversation(chat)
			chat.replaying = action == "load"
			if title != "" {
				chat.Title = title
			}
			a.markAgentReady(spec.ID)
			chat.clearConnect(gen)

			// Flush history and available_commands_update buffered during
			// initialize/session setup after the UI state is installed.
			s.Activate()
			if chat.replaying {
				a.Post(func() { a.finishReplay(chat) })
			}
		})
	}()
}

func (a *App) connectIsCurrent(chat *Chat, gen int) bool {
	return chat != nil && !chat.Closed && a.chatIndex(chat) >= 0 && chat.connectCurrent(gen) && !a.closed.Load()
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
	chat.Info = session.Info{}
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
	chat.CommandsOpen = false
	chat.Attachments = nil
	chat.Plan = nil
	chat.PlanOpen = false
	chat.Usage = nil
	chat.Title = ""
	chat.Unread = false
	chat.replaying = false
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
	s := chat.Session
	if s == nil {
		return false
	}
	chat.Input = ""
	chat.Attachments = nil
	chat.ConnectError = ""

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
	if chat == nil || chat.Session == nil || chat.ConfigBusy != "" || chat.Connecting || chat.Streaming || chat.Busy != "" {
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
	a.loadHistoryMode(sessionID, false, title, cwd)
}

// resumeHistory attaches a saved session without replaying history.
func (a *App) resumeHistory(sessionID, title, cwd string) {
	a.loadHistoryMode(sessionID, true, title, cwd)
}

func (a *App) loadHistoryMode(sessionID string, resume bool, title, cwd string) {
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
	chat.replaying = !resume
	action := "load"
	if resume {
		action = "resume"
	}
	chat.Busy = action
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
			if cwd != "" {
				chat.Workspace = cwd
				a.Workspace = cwd
			}
			if newInfo.Title != "" {
				chat.Title = newInfo.Title
			} else if title != "" {
				chat.Title = title
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
	switch ev.Kind {
	case session.UpdateCommands:
		chat.Commands = append([]acp.AvailableCommand(nil), ev.Commands...)
		return
	case session.UpdateConfigOption:
		if len(ev.ConfigOptions) > 0 {
			chat.Info.ConfigOptions = ev.ConfigOptions
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
			chat.Title = ev.Title
			chat.Info.Title = ev.Title
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
	chat.Session = nil
	chat.Connected = false
	chat.Connecting = false
	chat.Busy = ""
	chat.Streaming = false
	chat.Cancelling = false
	chat.Info = session.Info{}
	chat.replaying = false
	chat.CommandsOpen = false
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
