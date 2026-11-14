package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/git"
	"github.com/reed/acp-gui/internal/session"
	taskstore "github.com/reed/acp-gui/internal/tasks"
)

const (
	// taskMaxRounds bounds one task's executor turns so a checker that keeps
	// rejecting cannot loop forever.
	taskMaxRounds = 8
	// taskCheckTimeout is one checker process: start, prompt and close.
	taskCheckTimeout = 3 * time.Minute
	// taskCommitTimeout covers status plus stage plus commit after a task.
	taskCommitTimeout = 30 * time.Second
	// taskCheckReplyLimit caps the checker transcript kept in memory.
	taskCheckReplyLimit = 256 << 10
	// taskPromptLimit keeps a single prompt comfortably below model limits.
	taskPromptLimit = 24 << 10
	// taskSnapshotTimeout bounds the pre-task work-tree snapshot.
	taskSnapshotTimeout = 30 * time.Second
	// taskDiscardTimeout covers restoring the pre-task snapshot on failure.
	taskDiscardTimeout = 30 * time.Second
)

// taskCheckRound is one executor round as the checker sees it: the last
// paragraph of that round and, for earlier rounds, the feedback the checker
// gave back. Keeping the chain makes the next isolated checker aware of what
// changed since the first attempt.
type taskCheckRound struct {
	Summary  string
	Feedback string
}

// taskRunState is the single serial runner. All fields are UI-thread state;
// background snapshot, checker and finalize goroutines only post results back
// with Gen and TaskID guards.
type taskRunState struct {
	Active    bool
	Queue     bool
	Gen       int
	Workspace string
	TaskID    string
	Title     string
	Phase     string
	Agent     agent.LaunchSpec
	Chat      *Chat
	// CheckerCancel stops a checker still running when the user presses Stop.
	CheckerCancel context.CancelFunc
	// StartCancel stops the pre-task snapshot before the executor connects.
	StartCancel context.CancelFunc
	// Snapshot restores the work tree if the task fails. SnapshotOK is false
	// for workspaces that are not Git repositories or when the snapshot
	// could not be taken; SnapshotErr explains the latter.
	Snapshot    git.WorktreeSnapshot
	SnapshotOK  bool
	SnapshotErr string
	// CheckRounds accumulates the executor summary and checker feedback for
	// every round in this run. Each checker process is new, but the prompt
	// carries this history so it can judge the latest round in context.
	CheckRounds []taskCheckRound
}

// taskRunPhaseText is the short status shown in the panel header and row.
func (a *App) taskRunPhaseText() string {
	if !a.TaskRun.Active {
		return ""
	}
	switch a.TaskRun.Phase {
	case "starting":
		return "starting"
	case "executing":
		return "executing"
	case "checking":
		return "checking"
	case "discarding":
		return "discarding changes & saving summary"
	case "saving", "committing":
		return "saving summary & commit"
	default:
		return ""
	}
}

// startTaskQueue starts the first pending task of the active workspace. The
// runner then advances through the remaining pending tasks in list order.
func (a *App) startTaskQueue() {
	if a.closed.Load() {
		return
	}
	if a.TaskRun.Active {
		a.TaskError = "A task is already running."
		return
	}
	workspace := a.activeTaskWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.TaskError = "Choose a workspace before running tasks."
		return
	}
	if _, ok := a.nextPendingTask(workspace); !ok {
		a.TaskError = ""
		a.TaskNotice = "No pending tasks in this workspace."
		return
	}
	a.TaskError = ""
	a.TaskNotice = ""
	a.TaskRun.Queue = true
	a.GitPanelOpen = true
	a.setRightTab(rightTabTasks)
	a.closeTaskSummary()
	a.startNextTaskIn(workspace)
}

// startNextTaskIn picks the next pending task and prepares its own fresh
// conversation. Queue is read from TaskRun, so completion can chain tasks.
func (a *App) startNextTaskIn(workspace string) {
	if a.closed.Load() || a.TaskRun.Active {
		return
	}
	task, ok := a.nextPendingTask(workspace)
	if !ok {
		if a.TaskRun.Queue {
			a.TaskNotice = "All pending tasks are complete."
		}
		a.TaskRun.Queue = false
		return
	}
	spec, ok := a.taskAgentFor(task)
	if !ok {
		a.TaskError = "No agent is available to run tasks."
		a.TaskRun.Queue = false
		return
	}
	queue := a.TaskRun.Queue
	gen := a.TaskRun.Gen + 1
	a.TaskRun = taskRunState{
		Active:    true,
		Queue:     queue,
		Gen:       gen,
		Workspace: workspace,
		TaskID:    task.ID,
		Title:     task.Title,
		Phase:     "starting",
		Agent:     spec,
	}
	a.startTask(task, spec)
}

func (a *App) taskAgentFor(task taskstore.Task) (agent.LaunchSpec, bool) {
	if id := strings.TrimSpace(task.AgentID); id != "" {
		for _, spec := range a.Agents {
			if spec.ID == id {
				return spec, true
			}
		}
	}
	return a.preferredAgent()
}

// startTask marks the task ongoing, creates a brand-new chat, snapshots the
// work tree for a possible failure, and then connects and sends the first
// executor prompt. The chat is visible like any other conversation, but its
// composer stays locked while the runner owns it.
func (a *App) startTask(task taskstore.Task, spec agent.LaunchSpec) {
	gen := a.TaskRun.Gen
	if i := a.taskIndexByID(task.ID); i >= 0 {
		a.Tasks[i].Status = taskstore.StatusOngoing
		a.Tasks[i].StartedAt = time.Now().UTC().Format(time.RFC3339)
		a.Tasks[i].CompletedAt = ""
		a.Tasks[i].SummaryPath = ""
		a.Tasks[i].Result = ""
		a.Tasks[i].SessionID = ""
		a.Tasks[i].Rounds = 0
		a.Tasks[i].Note = ""
		a.Tasks[i].AgentID = spec.ID
		task = a.Tasks[i]
	}
	a.persistTasks()
	a.TaskError = ""
	a.TaskNotice = ""
	a.TaskRun.Title = task.Title
	a.TaskRun.Phase = "starting"

	chat := a.newChatTabIn(task.Workspace)
	if chat.Agent.ID != spec.ID {
		chat.Agent = spec
		chat.clearDraftConfig()
		a.restoreSelection(chat)
		a.AgentIndex = a.agentIndex(spec.ID)
	}
	chat.TaskTitle = task.Title
	chat.taskActive = true
	chat.onConnect = func(err error) {
		a.taskChatConnected(gen, task.ID, chat, err)
	}
	chat.onTurnEnd = func(turn *turnState, res acp.PromptResult, err error) {
		a.taskTurnEnded(gen, task.ID, chat, turn, res, err)
	}
	chat.Input = taskExecutorPrompt(task)
	chat.pendingSend = true
	a.TaskRun.Chat = chat
	a.snapshotBeforeTask(gen, task.ID, chat)
}

// snapshotBeforeTask records the work tree before the executor can change it.
// The snapshot is taken off the UI thread because a large repository can take
// a moment to scan; the executor does not connect until it is ready. A
// workspace that is not a repository simply runs without rollback support.
func (a *App) snapshotBeforeTask(gen int, taskID string, chat *Chat) {
	ctx, cancel := context.WithTimeout(a.lifeCtx, taskSnapshotTimeout)
	a.TaskRun.StartCancel = cancel
	workspace := chat.Workspace
	go func() {
		snapshot, err := git.SnapshotWorktree(ctx, workspace)
		a.Post(func() {
			cancel()
			if a.closed.Load() || !a.taskRunCurrent(gen, taskID, chat) || a.TaskRun.Phase != "starting" {
				return
			}
			a.TaskRun.StartCancel = nil
			if err != nil {
				a.TaskRun.SnapshotErr = err.Error()
			} else {
				a.TaskRun.Snapshot = snapshot
				a.TaskRun.SnapshotOK = true
			}
			a.connectChat(chat)
		})
	}()
}

func (a *App) taskChatConnected(gen int, taskID string, chat *Chat, err error) {
	if !a.taskRunCurrent(gen, taskID, chat) {
		return
	}
	if err != nil {
		a.failTaskRun("Could not start the task agent: " + err.Error())
		return
	}
	if i := a.taskIndexByID(taskID); i >= 0 {
		a.Tasks[i].SessionID = chat.Info.SessionID
		a.persistTasks()
	}
	if chat.Info.SessionID != "" {
		name := strings.TrimSpace(chat.TaskTitle)
		if name == "" {
			name = chat.name()
		}
		a.setArchivedShowName(chat.Agent.ID, chat.Info.SessionID, name)
	}
	a.TaskRun.Phase = "executing"
}

// taskTurnEnded runs after the executor's turn is fully rendered. It extracts
// the final paragraph, asks the isolated checker, and either continues the
// executor or finishes the task.
func (a *App) taskTurnEnded(gen int, taskID string, chat *Chat, turn *turnState, res acp.PromptResult, err error) {
	if !a.taskRunCurrent(gen, taskID, chat) {
		return
	}
	if a.TaskRun.Phase != "executing" && a.TaskRun.Phase != "starting" {
		return
	}
	if err != nil {
		a.failTaskRun("Agent turn failed: " + err.Error())
		return
	}
	if res.StopReason != "" && res.StopReason != "end_turn" {
		a.failTaskRun("Agent turn stopped: " + res.StopReason)
		return
	}
	index := a.taskIndexByID(taskID)
	if index < 0 {
		a.failTaskRun("The task was removed while it was running.")
		return
	}
	summary := lastAgentParagraph(chat, turn)
	a.Tasks[index].Rounds++
	a.Tasks[index].Note = ""
	a.persistTasks()
	task := a.Tasks[index]
	a.TaskRun.CheckRounds = append(a.TaskRun.CheckRounds, taskCheckRound{Summary: summary})
	a.TaskRun.Phase = "checking"
	a.startTaskCheck(gen, task)
}

// startTaskCheck launches a second, isolated agent process for one judgment.
// It never shares a session with the executor. The prompt carries the task
// text plus every completed round and the feedback that followed it, so the
// new checker can see how the executor responded to earlier criticism.
func (a *App) startTaskCheck(gen int, task taskstore.Task) {
	ctx, cancel := context.WithTimeout(a.lifeCtx, taskCheckTimeout)
	a.TaskRun.CheckerCancel = cancel
	spec := a.TaskRun.Agent
	history := append([]taskCheckRound(nil), a.TaskRun.CheckRounds...)
	agentSummary := taskLatestSummary(history)
	prompt := taskCheckerPrompt(task, history)
	go func() {
		defer cancel()
		reply, err := a.runTaskChecker(ctx, spec, prompt)
		a.Post(func() {
			if a.closed.Load() || !a.taskRunCurrent(gen, task.ID, a.TaskRun.Chat) || a.TaskRun.Phase != "checking" {
				return
			}
			a.TaskRun.CheckerCancel = nil
			if err != nil {
				a.failTaskRun("Checker failed: " + err.Error())
				return
			}
			verdict, parseErr := parseTaskVerdict(reply)
			if parseErr != nil {
				a.failTaskRun("Checker reply: " + parseErr.Error())
				return
			}
			if !verdict.Complete {
				a.continueTask(gen, task.ID, verdict.Reason)
				return
			}
			a.completeTask(gen, task.ID, verdict, agentSummary)
		})
	}()
}

// taskLatestSummary returns the last executor paragraph in the checker
// history, which is the report for the round currently being judged.
func taskLatestSummary(history []taskCheckRound) string {
	if len(history) == 0 {
		return ""
	}
	return strings.TrimSpace(history[len(history)-1].Summary)
}

// runTaskChecker is the isolated checker process. It denies permissions
// because the checker is instructed to use no tools, and it accumulates only
// agent message text, never thoughts or startup notices.
func (a *App) runTaskChecker(ctx context.Context, spec agent.LaunchSpec, prompt string) (string, error) {
	// The checker never needs the project. A throwaway cwd keeps it from
	// loading project context and, more importantly, keeps its agent session
	// out of session/list for the real workspace.
	checkDir, err := os.MkdirTemp("", "acp-gui-task-check-*")
	if err != nil {
		return "", fmt.Errorf("checker workspace: %w", err)
	}
	defer os.RemoveAll(checkDir)

	var mu sync.Mutex
	var reply strings.Builder
	cfg := session.Config{
		Launch:    spec,
		Workspace: checkDir,
		ClientInfo: acp.Implementation{
			Name:    "acp-gui",
			Title:   "ACP GUI",
			Version: a.Version,
		},
		OnUpdate: func(ev session.Event) {
			if ev.Kind != session.UpdateMessageChunk || ev.StartupInfo || ev.Notice != nil || ev.Text == "" {
				return
			}
			mu.Lock()
			if reply.Len() < taskCheckReplyLimit {
				reply.WriteString(clipText(ev.Text, taskCheckReplyLimit-reply.Len()))
			}
			mu.Unlock()
		},
		OnPermission: func(context.Context, session.PermissionRequest) (acp.RequestPermissionResult, error) {
			a.appendAgentLog(spec.ID, []byte("[task-check] permission request denied\n"))
			return acp.CancelledPermission(), nil
		},
		OnStderr: func(chunk string) {
			a.appendAgentLog(spec.ID, []byte(chunk))
		},
		OnProtocolError: func(err error) {
			a.appendAgentLog(spec.ID, []byte("[protocol] "+err.Error()+"\n"))
		},
	}
	a.logSessionStart(spec, checkDir, "check", "")
	s, err := session.Start(ctx, cfg)
	if err != nil {
		return "", err
	}
	if !a.trackSession(s) {
		return "", errors.New("app is shutting down")
	}
	defer a.untrackSession(s)
	defer func() { _ = s.Close() }()
	res, err := s.Prompt(ctx, prompt)
	if err != nil {
		return "", err
	}
	if res.StopReason != "" && res.StopReason != "end_turn" {
		return "", fmt.Errorf("checker turn stopped: %s", res.StopReason)
	}
	mu.Lock()
	out := reply.String()
	mu.Unlock()
	return out, nil
}

// taskVerdict is the checker's strict JSON answer.
type taskVerdict struct {
	Complete bool   `json:"complete"`
	Reason   string `json:"reason"`
	Summary  string `json:"summary"`
}

func parseTaskVerdict(reply string) (taskVerdict, error) {
	text := strings.TrimSpace(reply)
	if strings.HasPrefix(text, "```") {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = strings.TrimSpace(text[newline+1:])
		}
		text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	}
	start := strings.Index(text, "{")
	if start < 0 {
		return taskVerdict{}, fmt.Errorf("no JSON object in reply %q", truncate(singleLine(text), 160))
	}
	var verdict taskVerdict
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&verdict); err != nil {
		return taskVerdict{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if strings.TrimSpace(verdict.Reason) == "" {
		verdict.Reason = "The checker did not give a reason."
	}
	return verdict, nil
}

// continueTask replies in the same executor session with the checker's
// reason. A fresh session is only created for the next task, not per round.
func (a *App) continueTask(gen int, taskID, reason string) {
	if !a.TaskRun.Active || a.TaskRun.Gen != gen || a.TaskRun.TaskID != taskID || a.TaskRun.Phase != "checking" {
		return
	}
	index := a.taskIndexByID(taskID)
	if index < 0 {
		a.failTaskRun("The task was removed while it was running.")
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "The task is not complete yet."
	}
	// The feedback belongs to the round just judged. The next checker prompt
	// carries it with that round's summary.
	if n := len(a.TaskRun.CheckRounds); n > 0 {
		a.TaskRun.CheckRounds[n-1].Feedback = reason
	}
	if a.Tasks[index].Rounds >= taskMaxRounds {
		a.failTaskRun(fmt.Sprintf("Task did not finish after %d rounds: %s", taskMaxRounds, reason))
		return
	}
	a.Tasks[index].Note = "Checker: " + reason
	a.persistTasks()
	chat := a.TaskRun.Chat
	if chat == nil || chat.Session == nil {
		a.failTaskRun("The task conversation is no longer connected.")
		return
	}
	a.TaskRun.Phase = "executing"
	chat.Input = taskContinuePrompt(reason)
	if !a.sendChat(chat) {
		a.failTaskRun("Could not send the continuation prompt.")
	}
}

// completeTask writes the Markdown summary, commits once, marks the task
// complete and advances the queue. The summary file is written before the
// commit so a completed task and its record land in the same commit when the
// workspace is a repository.
func (a *App) completeTask(gen int, taskID string, verdict taskVerdict, agentSummary string) {
	if !a.TaskRun.Active || a.TaskRun.Gen != gen || a.TaskRun.TaskID != taskID || a.TaskRun.Phase != "checking" {
		return
	}
	index := a.taskIndexByID(taskID)
	if index < 0 {
		a.failTaskRun("The task was removed while it was running.")
		return
	}
	task := a.Tasks[index]
	chat := a.TaskRun.Chat
	queue := a.TaskRun.Queue
	workspace := a.TaskRun.Workspace
	completedAt := time.Now().UTC()
	markdown := buildTaskSummaryMarkdown(task, taskstore.ResultPass, verdict, agentSummary, completedAt)
	a.TaskRun.Phase = "saving"

	go func() {
		summaryPath, summaryErr := taskstore.WriteSummary(task.Workspace, task, markdown)
		commitNote, commitErr := commitTaskChanges(a.lifeCtx, task)
		a.Post(func() {
			if a.closed.Load() || !a.TaskRun.Active || a.TaskRun.Gen != gen || a.TaskRun.TaskID != taskID {
				return
			}
			if i := a.taskIndexByID(taskID); i >= 0 {
				a.Tasks[i].Status = taskstore.StatusComplete
				a.Tasks[i].Result = taskstore.ResultPass
				a.Tasks[i].CompletedAt = completedAt.Format(time.RFC3339)
				if summaryPath != "" {
					a.Tasks[i].SummaryPath = summaryPath
				}
				a.Tasks[i].Note = taskCompletionNote(summaryErr, commitErr, commitNote)
			}
			a.persistTasks()
			a.TaskError = ""
			a.TaskNotice = "Task complete: " + task.Title
			if summaryErr != nil {
				a.TaskError = "Summary: " + summaryErr.Error()
			}
			if commitErr != nil {
				a.TaskError = "Commit: " + commitErr.Error()
			}
			if commitNote != "" && commitErr == nil && summaryErr == nil {
				a.TaskNotice += " · " + commitNote
			}
			a.finishTaskChat(chat)
			a.notifyTaskFinished(chat, task, taskstore.ResultPass, verdict.Reason)
			a.TaskRun.Active = false
			a.TaskRun.Phase = ""
			a.TaskRun.Chat = nil
			a.TaskRun.CheckerCancel = nil
			a.TaskRun.StartCancel = nil
			a.TaskRun.CheckRounds = nil
			a.TaskRun.Snapshot = git.WorktreeSnapshot{}
			a.TaskRun.SnapshotOK = false
			a.TaskRun.SnapshotErr = ""
			a.refreshGitAfterTask(task.Workspace)
			if queue {
				a.TaskRun.Queue = true
				a.startNextTaskIn(workspace)
			} else {
				a.TaskRun.Queue = false
			}
		})
	}()
}

// failTaskRun turns the current run into a failed completion. The work the
// executor produced is rolled back to the pre-task snapshot, the summary is
// written after the rollback, and no commit is made.
func (a *App) failTaskRun(reason string) {
	if !a.TaskRun.Active {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "The task failed."
	}
	a.TaskError = reason
	a.TaskNotice = ""
	a.finishTaskFailure(reason)
}

func (a *App) finishTaskFailure(reason string) {
	if !a.TaskRun.Active {
		return
	}
	gen := a.TaskRun.Gen
	taskID := a.TaskRun.TaskID
	queue := a.TaskRun.Queue
	index := a.taskIndexByID(taskID)
	if index < 0 {
		// The task row was removed while it was running; there is nothing to
		// summarize, but the rest of the queue should still run.
		chat := a.TaskRun.Chat
		workspace := a.TaskRun.Workspace
		checkerCancel := a.TaskRun.CheckerCancel
		startCancel := a.TaskRun.StartCancel
		a.TaskRun.Active = false
		a.TaskRun.Phase = ""
		a.TaskRun.Chat = nil
		a.TaskRun.CheckerCancel = nil
		a.TaskRun.StartCancel = nil
		a.TaskRun.CheckRounds = nil
		a.TaskRun.Snapshot = git.WorktreeSnapshot{}
		a.TaskRun.SnapshotOK = false
		a.TaskRun.SnapshotErr = ""
		if checkerCancel != nil {
			checkerCancel()
		}
		if startCancel != nil {
			startCancel()
		}
		if chat != nil {
			a.finishTaskChat(chat)
		}
		if queue {
			a.TaskRun.Queue = true
			a.startNextTaskIn(workspace)
		} else {
			a.TaskRun.Queue = false
		}
		return
	}
	task := a.Tasks[index]
	chat := a.TaskRun.Chat
	workspace := a.TaskRun.Workspace
	snapshot := a.TaskRun.Snapshot
	snapshotOK := a.TaskRun.SnapshotOK
	snapshotErr := a.TaskRun.SnapshotErr
	agentSummary := taskLatestSummary(a.TaskRun.CheckRounds)
	completedAt := time.Now().UTC()
	markdown := buildTaskSummaryMarkdown(task, taskstore.ResultFailure,
		taskVerdict{Reason: reason}, agentSummary, completedAt)
	a.TaskRun.Phase = "discarding"

	go func() {
		discardNote, discardErr := discardTaskSnapshot(a.lifeCtx, snapshot, snapshotOK, snapshotErr)
		summaryPath, summaryErr := taskstore.WriteSummary(task.Workspace, task, markdown)
		a.Post(func() {
			if a.closed.Load() || !a.TaskRun.Active || a.TaskRun.Gen != gen || a.TaskRun.TaskID != taskID {
				return
			}
			if i := a.taskIndexByID(taskID); i >= 0 {
				a.Tasks[i].Status = taskstore.StatusComplete
				a.Tasks[i].Result = taskstore.ResultFailure
				a.Tasks[i].CompletedAt = completedAt.Format(time.RFC3339)
				if summaryPath != "" {
					a.Tasks[i].SummaryPath = summaryPath
				}
				a.Tasks[i].Note = taskFailureNote(reason, discardNote, discardErr, summaryErr)
			}
			a.persistTasks()
			a.TaskNotice = ""
			a.TaskError = "Task failed: " + truncate(singleLine(reason), 200)
			if discardErr != nil {
				a.TaskError += " · Discard: " + singleLine(discardErr.Error())
			}
			if summaryErr != nil {
				a.TaskError += " · Summary: " + singleLine(summaryErr.Error())
			}
			a.finishTaskChat(chat)
			a.notifyTaskFinished(chat, task, taskstore.ResultFailure, reason)
			a.TaskRun.Active = false
			a.TaskRun.Phase = ""
			a.TaskRun.Chat = nil
			a.TaskRun.CheckerCancel = nil
			a.TaskRun.StartCancel = nil
			a.TaskRun.CheckRounds = nil
			a.TaskRun.Snapshot = git.WorktreeSnapshot{}
			a.TaskRun.SnapshotOK = false
			a.TaskRun.SnapshotErr = ""
			a.refreshGitAfterTask(workspace)
			if queue {
				a.TaskRun.Queue = true
				a.startNextTaskIn(workspace)
			} else {
				a.TaskRun.Queue = false
			}
		})
	}()
}

// discardTaskSnapshot restores the pre-task tree. A missing snapshot is not a
// hard error: non-Git workspaces simply keep the changes, with the reason in
// the task note.
func discardTaskSnapshot(ctx context.Context, snapshot git.WorktreeSnapshot, ok bool, snapshotErr string) (string, error) {
	if !ok {
		note := "Task changes were not discarded."
		if reason := strings.TrimSpace(snapshotErr); reason != "" {
			note += " " + singleLine(reason)
		}
		return truncate(note, 200), nil
	}
	callCtx, cancel := context.WithTimeout(ctx, taskDiscardTimeout)
	defer cancel()
	if err := snapshot.Restore(callCtx); err != nil {
		return "", err
	}
	return "Task changes discarded.", nil
}

// notifyTaskFinished announces the whole task, not each executor turn. It is
// silent while the user is already looking at the task conversation.
func (a *App) notifyTaskFinished(chat *Chat, task taskstore.Task, result taskstore.Result, reason string) {
	if !a.canNotify(chat) {
		return
	}
	title := "Task complete"
	body := notificationSummary(chat)
	if result == taskstore.ResultFailure {
		title = "Task failed"
		if reason = strings.TrimSpace(reason); reason != "" {
			body = singleLine(reason)
		}
	}
	a.notifyDesktop(desktopNotification{
		ID:       "acp-gui:task:" + task.ID,
		Title:    title,
		Subtitle: task.Title,
		Body:     body,
		Group:    "acp-gui:task:" + task.ID,
	})
}

func taskCompletionNote(summaryErr, commitErr error, commitNote string) string {
	var parts []string
	if summaryErr != nil {
		parts = append(parts, "Summary could not be saved: "+singleLine(summaryErr.Error()))
	}
	if commitErr != nil {
		parts = append(parts, "Commit failed: "+singleLine(commitErr.Error()))
	}
	if len(parts) == 0 {
		return commitNote
	}
	return truncate(strings.Join(parts, " · "), 160)
}

func taskFailureNote(reason, discardNote string, discardErr, summaryErr error) string {
	parts := []string{singleLine(reason)}
	if discardNote != "" {
		parts = append(parts, discardNote)
	}
	if discardErr != nil {
		parts = append(parts, "Discard failed: "+singleLine(discardErr.Error()))
	}
	if summaryErr != nil {
		parts = append(parts, "Summary could not be saved: "+singleLine(summaryErr.Error()))
	}
	return truncate(strings.Join(parts, " · "), 160)
}

func commitTaskChanges(ctx context.Context, task taskstore.Task) (string, error) {
	if !git.Available() {
		return "Git is not installed; no commit was made.", nil
	}
	callCtx, cancel := context.WithTimeout(ctx, taskCommitTimeout)
	defer cancel()
	root, err := git.RepoRoot(callCtx, task.Workspace)
	switch {
	case errors.Is(err, git.ErrNotRepository):
		return "Not a Git repository; no commit was made.", nil
	case errors.Is(err, git.ErrGitNotFound):
		return "Git is not installed; no commit was made.", nil
	case err != nil:
		return "", err
	}
	committed, err := git.CommitAll(callCtx, root, taskCommitMessage(task))
	if err != nil {
		return "", err
	}
	if !committed {
		return "No changes to commit.", nil
	}
	return "Committed.", nil
}

func taskCommitMessage(task taskstore.Task) string {
	title := singleLine(task.Title)
	if title == "" {
		title = "task " + task.ID
	}
	message := "task: " + title
	runes := []rune(message)
	if len(runes) > 72 {
		message = string(runes[:71]) + "…"
	}
	return message
}

// buildTaskSummaryMarkdown is the persisted record shown by the summary card
// and committed with the workspace.
func buildTaskSummaryMarkdown(task taskstore.Task, result taskstore.Result, verdict taskVerdict, agentSummary string, completed time.Time) string {
	if !result.Valid() {
		result = taskstore.ResultPass
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", task.Title)
	b.WriteString("- Status: complete\n")
	fmt.Fprintf(&b, "- Result: %s\n", result)
	fmt.Fprintf(&b, "- Task ID: `%s`\n", task.ID)
	fmt.Fprintf(&b, "- Workspace: `%s`\n", task.Workspace)
	fmt.Fprintf(&b, "- Completed: %s\n", completed.Format(time.RFC3339))
	if task.SessionID != "" {
		fmt.Fprintf(&b, "- Executor session: `%s`\n", task.SessionID)
	}
	if task.Rounds > 0 {
		fmt.Fprintf(&b, "- Executor rounds: %d\n", task.Rounds)
	}
	b.WriteString("\n## Task\n\n")
	if text := strings.TrimSpace(task.Text); text != "" {
		b.WriteString(text)
	} else {
		b.WriteString("_No details._")
	}
	b.WriteString("\n\n## Summary\n\n")
	summary := strings.TrimSpace(verdict.Summary)
	if summary == "" {
		summary = strings.TrimSpace(agentSummary)
	}
	if summary == "" {
		if result == taskstore.ResultFailure {
			summary = "_The task did not complete; this record contains the failure reason and the executor's last report._"
		} else {
			summary = "_The checker did not produce a summary._"
		}
	}
	b.WriteString(summary)
	b.WriteString("\n")
	if reason := strings.TrimSpace(verdict.Reason); reason != "" {
		b.WriteString("\n## Checker\n\n")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	if final := strings.TrimSpace(agentSummary); final != "" && final != summary {
		b.WriteString("\n## Agent's final report\n\n")
		b.WriteString(final)
		b.WriteString("\n")
	}
	return b.String()
}

func taskExecutorPrompt(task taskstore.Task) string {
	var b strings.Builder
	b.WriteString("TASK_EXECUTION\n\n")
	b.WriteString("You are running one task from the app's serial task queue. Work autonomously in this workspace and make the requested changes.\n\n")
	b.WriteString("Task: ")
	b.WriteString(clipText(task.Title, 2000))
	b.WriteString("\n")
	if text := strings.TrimSpace(task.Text); text != "" {
		b.WriteString("\n")
		b.WriteString(clipText(text, taskPromptLimit))
		b.WriteString("\n")
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Do not create git commits or push; the app commits once after a separate checker accepts the task.\n")
	b.WriteString("- Use the available tools and inspect the workspace yourself.\n")
	b.WriteString("- Do not stop to ask questions unless you are truly blocked; if blocked, explain the blocker.\n")
	b.WriteString("- End your reply with one short paragraph summarizing exactly what you did in this turn. The checker uses your latest paragraph and the earlier round context.\n")
	return b.String()
}

func taskContinuePrompt(reason string) string {
	return "TASK_EXECUTION\n\n" +
		"The completion checker did not accept the task as complete yet.\n" +
		"Checker reason: " + clipText(strings.TrimSpace(reason), 4000) + "\n\n" +
		"Continue the same task. Make the missing changes, then end your reply with one short paragraph summarizing what you did in this turn. Do not create a git commit."
}

func taskCheckerPrompt(task taskstore.Task, history []taskCheckRound) string {
	var b strings.Builder
	b.WriteString("TASK_COMPLETION_CHECK\n\n")
	b.WriteString("You are a strict task completion checker. You have no tools; do not inspect files or run commands.\n\n")
	b.WriteString("TASK TITLE:\n")
	b.WriteString(clipText(task.Title, 2000))
	b.WriteString("\n\nTASK DETAILS:\n")
	if text := strings.TrimSpace(task.Text); text != "" {
		b.WriteString(clipText(text, taskPromptLimit))
	} else {
		b.WriteString("(No details.)")
	}
	b.WriteString("\n\nEXECUTOR ROUNDS (oldest first):\n")
	if len(history) == 0 {
		b.WriteString("(The executor did not write a summary.)\n")
	}
	for i, round := range history {
		fmt.Fprintf(&b, "\nROUND %d EXECUTOR SUMMARY:\n", i+1)
		if summary := strings.TrimSpace(round.Summary); summary != "" {
			b.WriteString(clipText(summary, 4000))
		} else {
			b.WriteString("(The executor did not write a summary for this round.)")
		}
		if feedback := strings.TrimSpace(round.Feedback); feedback != "" {
			fmt.Fprintf(&b, "\n\nCHECKER FEEDBACK AFTER ROUND %d:\n", i+1)
			b.WriteString(clipText(feedback, 4000))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nDecide whether the latest round shows that the task is fully complete. Use the earlier rounds and their feedback to understand what changed, but judge the current state. Base the decision only on the task text and these summaries.\n")
	b.WriteString("Reply with JSON only, without code fences:\n")
	b.WriteString(`{"complete": true, "reason": "one short sentence", "summary": "Markdown summary of what was done; required when complete"}`)
	b.WriteString("\n")
	return b.String()
}

// lastAgentParagraph returns the last non-empty paragraph of the last
// assistant row in this turn that has text. That is the part the checker
// receives; an earlier turn's summary must not leak into this judgment.
func lastAgentParagraph(chat *Chat, turn *turnState) string {
	if chat == nil {
		return ""
	}
	start := 0
	if turn != nil {
		start = turn.startMessage
	}
	if start < 0 || start > len(chat.Messages) {
		start = 0
	}
	for i := len(chat.Messages) - 1; i >= start; i-- {
		message := chat.Messages[i]
		if message.Role != RoleAssistant || strings.TrimSpace(message.Text) == "" {
			continue
		}
		return lastParagraph(message.Text)
	}
	return ""
}

func lastParagraph(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n\n")
	for i := len(parts) - 1; i >= 0; i-- {
		if paragraph := strings.TrimSpace(parts[i]); paragraph != "" {
			return paragraph
		}
	}
	return strings.TrimSpace(text)
}

// notifyTurnEnd delivers one finished prompt to a task chat exactly once.
// applyPromptResult handles normal completion and applyExit handles a process
// that dies mid-turn; a turn must not trigger two checker runs.
func (a *App) notifyTurnEnd(chat *Chat, turn *turnState, res acp.PromptResult, err error) {
	if chat == nil || turn == nil || chat.onTurnEnd == nil || turn.endNotified {
		return
	}
	turn.endNotified = true
	chat.onTurnEnd(turn, res, err)
}

// stopTaskRun is the user's Stop button. Saving and committing are short and
// already past the point where the work is done, so they are allowed to
// finish instead of leaving a half-recorded task.
func (a *App) stopTaskRun(reason string) {
	if !a.TaskRun.Active {
		return
	}
	if a.TaskRun.Phase == "saving" || a.TaskRun.Phase == "committing" || a.TaskRun.Phase == "discarding" {
		return
	}
	a.TaskError = ""
	a.TaskNotice = "Task stopped; it is left pending."
	a.finishTaskRunPending(reason)
}

func (a *App) finishTaskRunPending(note string) {
	if !a.TaskRun.Active {
		return
	}
	taskID := a.TaskRun.TaskID
	chat := a.TaskRun.Chat
	checkerCancel := a.TaskRun.CheckerCancel
	startCancel := a.TaskRun.StartCancel
	a.TaskRun.Active = false
	a.TaskRun.Queue = false
	a.TaskRun.Phase = ""
	a.TaskRun.Chat = nil
	a.TaskRun.CheckerCancel = nil
	a.TaskRun.StartCancel = nil
	a.TaskRun.CheckRounds = nil
	a.TaskRun.Snapshot = git.WorktreeSnapshot{}
	a.TaskRun.SnapshotOK = false
	a.TaskRun.SnapshotErr = ""
	if checkerCancel != nil {
		checkerCancel()
	}
	if startCancel != nil {
		startCancel()
	}
	if i := a.taskIndexByID(taskID); i >= 0 && a.Tasks[i].Status == taskstore.StatusOngoing {
		a.Tasks[i].Status = taskstore.StatusPending
		a.Tasks[i].Note = note
	}
	a.persistTasks()
	if chat != nil {
		a.finishTaskChat(chat)
	}
}

// finishTaskChat releases the executor process but keeps the conversation
// rows so the user can still read what the agent did.
func (a *App) finishTaskChat(chat *Chat) {
	if chat == nil {
		return
	}
	chat.taskActive = false
	chat.onConnect = nil
	chat.onTurnEnd = nil
	chat.pendingSend = false
	chat.cancelConnect()
	chat.Connecting = false
	// A Stop closes the process without a prompt result, so finalize the
	// turn here; the normal completion path is already settled and produces
	// no extra Took row because startedAt was cleared.
	a.cancelPermissionsFor(chat)
	if chat.turn != nil {
		elapsed := a.finishTurnRuntime(chat, chat.turn)
		if chat.turn.assistant != nil {
			chat.turn.assistant.Done = true
		}
		chat.turn = nil
		a.addTurnSummary(chat, elapsed)
	}
	chat.Streaming = false
	chat.Cancelling = false
	if s := chat.Session; s != nil {
		chat.Session = nil
		chat.Connected = false
		chat.Busy = ""
		a.closeInBackground(chat, s)
	}
}

func (a *App) taskRunCurrent(gen int, taskID string, chat *Chat) bool {
	return a.TaskRun.Active && a.TaskRun.Gen == gen && a.TaskRun.TaskID == taskID && a.TaskRun.Chat == chat
}

// refreshGitAfterTask makes a task's commit visible in the Git panel. It only
// touches the panel state when the panel already follows that workspace.
func (a *App) refreshGitAfterTask(workspace string) {
	if a.closed.Load() || workspace == "" {
		return
	}
	if !sameWorkspace(a.activeGitWorkspace(), workspace) {
		if sameWorkspace(a.Git.Workspace, workspace) {
			a.Git.Checked = false
		}
		return
	}
	if a.GitPanelOpen && a.RightTab == rightTabGit {
		a.refreshGitOnFocus()
		return
	}
	if sameWorkspace(a.Git.Workspace, workspace) {
		a.Git.Checked = false
	}
}

// clipText shortens text to max bytes on a rune boundary.
func clipText(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
