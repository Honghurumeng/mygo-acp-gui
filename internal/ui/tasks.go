package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	taskstore "github.com/reed/acp-gui/internal/tasks"
)

const taskFileName = "tasks.json"

// activeTaskWorkspace is the folder the task panel belongs to. It follows the
// active conversation exactly like the Git panel does.
func (a *App) activeTaskWorkspace() string {
	return a.activeGitWorkspace()
}

// loadTaskStore reads the serial queue before the first frame. A task left
// ongoing by a crash or quit is returned to pending: no executor survives a
// restart, so it can be run again instead of blocking the queue forever.
func (a *App) loadTaskStore(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir != "" {
		a.taskStore = taskstore.Open(filepath.Join(dataDir, taskFileName))
	}
	if a.taskStore == nil {
		return
	}
	file, err := a.taskStore.Load()
	if err != nil {
		a.TaskError = err.Error()
	}
	a.Tasks = append([]taskstore.Task(nil), file.Tasks...)
	changed := false
	for i := range a.Tasks {
		if a.Tasks[i].Status != taskstore.StatusOngoing {
			continue
		}
		a.Tasks[i].Status = taskstore.StatusPending
		a.Tasks[i].Note = "Interrupted when the app quit; ready to run again."
		changed = true
	}
	if changed {
		a.persistTasks()
	}
}

// persistTasks writes the queue order and every task's state. It is called
// after each edit, reorder and status transition, so a crash loses at most the
// turn that was in flight.
func (a *App) persistTasks() {
	if a.taskStore == nil {
		return
	}
	file := taskstore.File{
		Version: taskstore.CurrentVersion,
		Tasks:   append([]taskstore.Task(nil), a.Tasks...),
	}
	if err := a.taskStore.Save(file); err != nil {
		a.TaskError = err.Error()
	}
}

func (a *App) taskIndexByID(id string) int {
	id = strings.TrimSpace(id)
	for i := range a.Tasks {
		if a.Tasks[i].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) taskByID(id string) *taskstore.Task {
	if i := a.taskIndexByID(id); i >= 0 {
		return &a.Tasks[i]
	}
	return nil
}

// tasksForWorkspace returns the queue slice in stored order.
func (a *App) tasksForWorkspace(workspace string) []taskstore.Task {
	var out []taskstore.Task
	for _, task := range a.Tasks {
		if sameWorkspace(task.Workspace, workspace) {
			out = append(out, task)
		}
	}
	return out
}

// nextPendingTask picks the first pending task of a workspace. Complete and
// ongoing rows keep their place in the list but are skipped.
func (a *App) nextPendingTask(workspace string) (taskstore.Task, bool) {
	for _, task := range a.Tasks {
		if task.Status == taskstore.StatusPending && sameWorkspace(task.Workspace, workspace) {
			return task, true
		}
	}
	return taskstore.Task{}, false
}

func (a *App) taskCounts(workspace string) (pending, ongoing, complete int) {
	for _, task := range a.Tasks {
		if !sameWorkspace(task.Workspace, workspace) {
			continue
		}
		switch task.Status {
		case taskstore.StatusPending:
			pending++
		case taskstore.StatusOngoing:
			ongoing++
		case taskstore.StatusComplete:
			complete++
		}
	}
	return pending, ongoing, complete
}

// taskPanel is the Tasks tab's right column. It mirrors gitPanel's width and
// returns the column as the summary card's anchor.
func (a *App) taskPanel(c *mui.Context) mui.Element {
	t := c.Theme()
	panel := mui.Column(c).Width(gitPanelWidth).FillHeight().
		Background(t.Surface).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	panel.Children(func() {
		a.taskPanelHeader(c)
		a.taskPanelBody(c)
	})
	return panel
}

func (a *App) taskPanelHeader(c *mui.Context) {
	t := c.Theme()
	workspace := a.activeTaskWorkspace()
	pending, ongoing, complete := a.taskCounts(workspace)
	line := "No workspace"
	if workspace != "" {
		line = filepath.Base(workspace)
		if line == "" || line == "." || line == string(filepath.Separator) {
			line = workspace
		}
		line += fmt.Sprintf(" · %d pending · %d ongoing", pending, ongoing)
		if complete > 0 {
			line += fmt.Sprintf(" · %d complete", complete)
		}
	}
	if a.TaskRun.Active && !sameWorkspace(a.TaskRun.Workspace, workspace) {
		line = "Running " + truncate(a.TaskRun.Title, 28) + " in " + filepath.Base(a.TaskRun.Workspace)
	}
	mui.Row(c).Height(54).Gap(6).Padding(8, 12).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			mui.Row(c).Gap(5).AlignItems(mui.Center).Children(func() {
				a.rightPanelName(c, "Tasks")
				if a.TaskRun.Active {
					mui.Spinner(c)
				}
				mui.Spacer(c)
				if quietButton(c, "＋").Label("New task").FontSize(a.gitFont(12)).Clicked() {
					a.openNewTask()
				}
			})
			if phase := a.taskRunPhaseText(); phase != "" {
				line += " · " + phase
			}
			caption := a.gitCaption(c, line).SingleLine().MinWidth(0)
			caption.Tooltip(workspace)
		})
	})
}

func (a *App) taskPanelBody(c *mui.Context) {
	workspace := a.activeTaskWorkspace()
	a.taskFeedback(c)
	a.taskActions(c, workspace)
	mui.Scroll(c).Grow(1).Children(func() {
		mui.Column(c).Gap(3).Padding(5, 12, 10, 12).Children(func() {
			tasks := a.tasksForWorkspace(workspace)
			if len(tasks) == 0 {
				mui.Column(c).Gap(3).Padding(6, 2).Children(func() {
					mutedText(c, "No tasks yet.", a.gitFont(11))
					mutedText(c, "Create a task and run the pending queue. Tasks run one at a time in list order.", a.gitFont(9))
				})
				return
			}
			for _, task := range tasks {
				a.taskRow(c, task)
			}
		})
	})
}

func (a *App) taskActions(c *mui.Context, workspace string) {
	pending, _, _ := a.taskCounts(workspace)
	busy := a.TaskRun.Active
	mui.Row(c).Gap(6).AlignItems(mui.Center).Padding(4, 12, 6, 12).Children(func() {
		run := primaryButton(c, "Run pending").FontSize(a.gitFont(10))
		run.Disabled(busy || pending == 0 || workspace == "" || !filepath.IsAbs(workspace))
		run.Label("Run pending tasks").Tooltip("Run pending tasks in list order")
		if run.Clicked() && !busy && pending > 0 {
			a.startTaskQueue()
		}
		phase := a.TaskRun.Phase
		canStop := busy && phase != "saving" && phase != "committing" && phase != "discarding"
		stop := dangerButton(c, "Stop").FontSize(a.gitFont(10))
		stop.Label("Stop task queue").Tooltip("Stop after the current step and leave the task pending")
		stop.Disabled(!canStop)
		if stop.Clicked() && canStop {
			a.stopTaskRun("Stopped by user.")
		}
		if busy {
			a.gitCaption(c, "serial queue")
		}
	})
}

func (a *App) taskFeedback(c *mui.Context) {
	t := c.Theme()
	if a.TaskError == "" && a.TaskNotice == "" {
		return
	}
	mui.Column(c).Gap(2).Padding(3, 12, 2, 12).Children(func() {
		if a.TaskError != "" {
			message := truncate(singleLine(a.TaskError), 240)
			mui.Text(c, "⚠ "+message).FontSize(a.gitFont(9)).TextColor(t.Danger).
				Selectable().Tooltip(a.TaskError)
		}
		if a.TaskNotice != "" {
			mui.Text(c, a.TaskNotice).FontSize(a.gitFont(9)).TextColor(t.Success)
		}
	})
}

func (a *App) taskRow(c *mui.Context, task taskstore.Task) {
	t := c.Theme()
	row := mui.Row(c).Gap(3).AlignItems(mui.Center).Padding(3, 5).Radius(8).
		Background(t.Background)
	row.Children(func() {
		switch task.Status {
		case taskstore.StatusOngoing:
			mui.Spinner(c)
		case taskstore.StatusComplete:
			mark, color := "✓", t.Success
			if task.Result == taskstore.ResultFailure {
				mark, color = "✗", t.Danger
			}
			mui.Text(c, mark).FontSize(a.gitFont(11)).TextColor(color)
		default:
			mui.Text(c, "○").FontSize(a.gitFont(11)).TextColor(t.TextMuted)
		}

		item := mui.ButtonBase(c).Grow(1).MinWidth(0).Gap(1).
			Padding(1, 2).Radius(6).Label("Task " + task.Title)
		item.Children(func() {
			title := mui.Text(c, truncate(task.Title, 46)).FontSize(a.gitFont(10)).
				Bold().SingleLine().MinWidth(0)
			if description := strings.TrimSpace(task.Text); description != "" {
				title.Tooltip(description)
			}
			status := a.gitCaption(c, a.taskStatusLine(task)).SingleLine().MinWidth(0)
			if task.Note != "" {
				status.Tooltip(task.Note)
			}
		})
		if item.Clicked() {
			switch task.Status {
			case taskstore.StatusComplete:
				a.openTaskSummary(task.ID)
			case taskstore.StatusPending:
				a.openEditTask(task.ID)
			default:
				if chat := a.TaskRun.Chat; chat != nil && a.TaskRun.TaskID == task.ID {
					a.selectChat(chat)
				}
			}
		}

		switch task.Status {
		case taskstore.StatusPending:
			a.taskIconButton(c, "↑", "Move task up", "Move task up", func() { a.moveTask(task.ID, -1) })
			a.taskIconButton(c, "↓", "Move task down", "Move task down", func() { a.moveTask(task.ID, 1) })
			a.taskIconButton(c, "✎", "Edit task", "Edit task", func() { a.openEditTask(task.ID) })
		case taskstore.StatusComplete:
			view := quietButton(c, "View").FontSize(a.gitFont(9)).Label("View task summary")
			if view.Clicked() {
				a.openTaskSummary(task.ID)
			}
		}
		if task.Status != taskstore.StatusOngoing {
			a.taskIconButton(c, "✕", "Delete task", "Delete task", func() { a.requestDeleteTask(task.ID) })
		}
	})
}

// taskIconButton is a compact row action whose accessible name explains both
// the direction and the task kind through the caller's label.
func (a *App) taskIconButton(c *mui.Context, glyph, label, tip string, fn func()) {
	button := quietButton(c, glyph).FontSize(a.gitFont(10)).Label(label).Tooltip(tip)
	if button.Clicked() {
		fn()
	}
}

func (a *App) taskStatusLine(task taskstore.Task) string {
	line := string(task.Status)
	switch task.Status {
	case taskstore.StatusOngoing:
		if task.Rounds > 0 {
			line += fmt.Sprintf(" · round %d", task.Rounds)
		}
		if task.ID == a.TaskRun.TaskID {
			if phase := a.taskRunPhaseText(); phase != "" {
				line += " · " + phase
			}
		}
	case taskstore.StatusComplete:
		line += " · " + taskResultLabel(task)
		if label := taskTimeLabel(task.CompletedAt); label != "" {
			line += " · " + label
		}
	}
	if note := singleLine(task.Note); note != "" {
		line += " · " + truncate(note, 54)
	}
	return line
}

func taskResultLabel(task taskstore.Task) string {
	if task.Result == taskstore.ResultFailure {
		return "failure"
	}
	return "pass"
}

func taskTimeLabel(value string) string {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	at = at.Local()
	if at.Year() == time.Now().Year() {
		return at.Format("Jan 2 15:04")
	}
	return at.Format("Jan 2, 2006 15:04")
}

// openNewTask starts the editor for the active workspace.
func (a *App) openNewTask() {
	workspace := a.activeTaskWorkspace()
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.TaskError = "Choose a workspace before adding a task."
		return
	}
	a.TaskEditorOpen = true
	a.TaskEditID = ""
	a.TaskEditWorkspace = workspace
	a.TaskEditTitle = ""
	a.TaskEditText = ""
	a.TaskEditorError = ""
}

// openEditTask edits a task that has not started yet.
func (a *App) openEditTask(id string) {
	task := a.taskByID(id)
	if task == nil {
		return
	}
	if task.Status != taskstore.StatusPending {
		a.TaskError = "Only pending tasks can be edited."
		return
	}
	a.TaskEditorOpen = true
	a.TaskEditID = task.ID
	a.TaskEditWorkspace = task.Workspace
	a.TaskEditTitle = task.Title
	a.TaskEditText = task.Text
	a.TaskEditorError = ""
}

func (a *App) taskEditorDialog(c *mui.Context) {
	if !a.TaskEditorOpen {
		return
	}
	t := c.Theme()
	heading := "New task"
	if a.TaskEditID != "" {
		heading = "Edit task"
	}
	mui.Modal(c, &a.TaskEditorOpen, func() {
		mui.Column(c).Gap(9).Width(460).Children(func() {
			mui.Text(c, heading).FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, "Tasks run serially in list order. Pending tasks can be edited, reordered or deleted.").
				FontSize(a.workspaceFont(10)).TextColor(t.TextMuted)
			mui.TextInput(c, &a.TaskEditTitle).
				Label("Task title").
				Placeholder("What should the agent do?")
			mui.TextAreaBase(c, &a.TaskEditText).
				Label("Task details").
				Placeholder("Context, files, acceptance criteria…").
				Lines(6, 12).
				Padding(6, 8).
				FontSize(a.workspaceFont(11)).
				Radius(t.Radius).
				Border(1, t.Border).
				Background(t.Background)
			if a.TaskEditorError != "" {
				mui.Text(c, "⚠ "+a.TaskEditorError).FontSize(a.workspaceFont(10)).TextColor(t.Danger)
			}
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.closeTaskEditor()
				}
				if primaryButton(c, "Save task").Clicked() {
					a.saveTaskEditor()
				}
			})
		})
	})
	if !a.TaskEditorOpen {
		a.closeTaskEditor()
	}
}

func (a *App) closeTaskEditor() {
	a.TaskEditorOpen = false
	a.TaskEditID = ""
	a.TaskEditWorkspace = ""
	a.TaskEditTitle = ""
	a.TaskEditText = ""
	a.TaskEditorError = ""
}

func (a *App) saveTaskEditor() {
	title := strings.TrimSpace(a.TaskEditTitle)
	text := strings.TrimSpace(a.TaskEditText)
	if title == "" {
		a.TaskEditorError = "A task needs a title."
		return
	}
	workspace := strings.TrimSpace(a.TaskEditWorkspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		a.TaskEditorError = "The task workspace is no longer available."
		return
	}
	if a.TaskEditID == "" {
		a.Tasks = append(a.Tasks, taskstore.Task{
			ID:        taskstore.NewID(),
			Workspace: workspace,
			Title:     title,
			Text:      text,
			Status:    taskstore.StatusPending,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		a.TaskNotice = "Task added."
	} else {
		task := a.taskByID(a.TaskEditID)
		if task == nil || !sameWorkspace(task.Workspace, workspace) {
			a.TaskEditorError = "This task is no longer available."
			return
		}
		if task.Status != taskstore.StatusPending {
			a.TaskEditorError = "Only pending tasks can be edited."
			return
		}
		task.Title = title
		task.Text = text
		a.TaskNotice = "Task updated."
	}
	a.TaskError = ""
	a.persistTasks()
	a.closeTaskEditor()
}

// requestDeleteTask asks before removing a task row. The Markdown summary is
// left on disk: it is a project file and may already be committed.
func (a *App) requestDeleteTask(id string) {
	task := a.taskByID(id)
	if task == nil {
		return
	}
	if task.Status == taskstore.StatusOngoing {
		a.TaskError = "Stop the running task before deleting it."
		return
	}
	a.TaskDeleteOpen = true
	a.TaskDeleteID = task.ID
}

func (a *App) taskDeleteDialog(c *mui.Context) {
	if !a.TaskDeleteOpen {
		return
	}
	t := c.Theme()
	task := a.taskByID(a.TaskDeleteID)
	if task == nil {
		a.closeTaskDelete()
		return
	}
	mui.Modal(c, &a.TaskDeleteOpen, func() {
		mui.Column(c).Gap(10).Width(380).Children(func() {
			mui.Text(c, "Delete task?").FontSize(a.workspaceFont(16)).Bold()
			mui.Text(c, task.Title).FontSize(a.workspaceFont(12)).Bold()
			hint := "The task is removed from the queue. Its Markdown summary, if one exists, stays in the workspace tasks folder."
			if task.Status != taskstore.StatusComplete {
				hint = "The task is removed from the queue. This cannot be undone."
			}
			mui.Text(c, hint).FontSize(a.workspaceFont(10)).TextColor(t.TextMuted)
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Cancel").Clicked() {
					a.closeTaskDelete()
				}
				if dangerButton(c, "Delete this task").Clicked() {
					a.deleteTask(task.ID)
				}
			})
		})
	})
	if !a.TaskDeleteOpen {
		a.closeTaskDelete()
	}
}

func (a *App) closeTaskDelete() {
	a.TaskDeleteOpen = false
	a.TaskDeleteID = ""
}

func (a *App) deleteTask(id string) {
	index := a.taskIndexByID(id)
	if index < 0 {
		a.closeTaskDelete()
		return
	}
	task := a.Tasks[index]
	if task.Status == taskstore.StatusOngoing {
		a.TaskError = "Stop the running task before deleting it."
		a.closeTaskDelete()
		return
	}
	a.Tasks = append(a.Tasks[:index], a.Tasks[index+1:]...)
	if a.TaskSummaryTaskID == id {
		a.closeTaskSummary()
	}
	a.TaskError = ""
	a.TaskNotice = "Task deleted."
	a.persistTasks()
	a.closeTaskDelete()
}

// moveTask swaps a task with its neighbor in the workspace view. The global
// slice order is swapped at the same two positions, so tasks of other
// workspaces keep their place. A running task cannot be moved.
func (a *App) moveTask(id string, delta int) {
	workspace := a.activeTaskWorkspace()
	group := a.tasksForWorkspace(workspace)
	at := -1
	for i := range group {
		if group[i].ID == id {
			at = i
			break
		}
	}
	if at < 0 || at+delta < 0 || at+delta >= len(group) {
		return
	}
	if group[at].Status == taskstore.StatusOngoing {
		return
	}
	from := a.taskIndexByID(group[at].ID)
	to := a.taskIndexByID(group[at+delta].ID)
	if from < 0 || to < 0 || from == to {
		return
	}
	a.Tasks[from], a.Tasks[to] = a.Tasks[to], a.Tasks[from]
	a.TaskError = ""
	a.TaskNotice = "Task order updated."
	a.persistTasks()
}

// openTaskSummary reads a completed task's Markdown file off the UI thread.
func (a *App) openTaskSummary(id string) {
	task := a.taskByID(id)
	if task == nil {
		return
	}
	a.TaskSummaryOpen = true
	a.TaskSummaryTaskID = task.ID
	a.TaskSummaryTitle = task.Title
	a.TaskSummaryPath = task.SummaryPath
	a.TaskSummaryBody = ""
	a.TaskSummaryErr = ""
	a.TaskSummaryBusy = true
	a.TaskSummaryDiff = false
	a.TaskSummaryGen++
	gen := a.TaskSummaryGen
	if strings.TrimSpace(task.SummaryPath) == "" {
		a.TaskSummaryBusy = false
		a.TaskSummaryErr = "No summary was saved for this task."
		return
	}
	workspace := task.Workspace
	go func() {
		body, err := taskstore.ReadSummary(workspace, task.SummaryPath)
		a.Post(func() {
			if a.closed.Load() || a.TaskSummaryGen != gen ||
				!a.TaskSummaryOpen || a.TaskSummaryTaskID != task.ID {
				return
			}
			a.TaskSummaryBusy = false
			if err != nil {
				a.TaskSummaryErr = err.Error()
				return
			}
			a.TaskSummaryBody = body
		})
	}()
}

func (a *App) closeTaskSummary() {
	a.TaskSummaryOpen = false
	a.TaskSummaryGen++
	a.TaskSummaryTaskID = ""
	a.TaskSummaryTitle = ""
	a.TaskSummaryPath = ""
	a.TaskSummaryBody = ""
	a.TaskSummaryBusy = false
	a.TaskSummaryErr = ""
	a.TaskSummaryDiff = false
}

// taskSummaryPopover floats the saved Markdown beside the task panel, using
// the same anchored card and responsive width as the Git diff viewer.
func (a *App) taskSummaryPopover(c *mui.Context, anchor mui.Element) {
	if !a.TaskSummaryOpen {
		return
	}
	task := a.taskByID(a.TaskSummaryTaskID)
	if task == nil || !sameWorkspace(task.Workspace, a.activeTaskWorkspace()) {
		a.closeTaskSummary()
		return
	}
	mui.PopoverBase(c, anchor, &a.TaskSummaryOpen, func(panel mui.Element) {
		a.taskSummaryCard(c, panel, anchor)
	})
	if !a.TaskSummaryOpen {
		a.closeTaskSummary()
	}
}

func (a *App) taskSummaryCard(c *mui.Context, panel, anchor mui.Element) {
	t := c.Theme()
	task := a.taskByID(a.TaskSummaryTaskID)
	if task == nil {
		return
	}
	windowW, _ := c.Size()
	width := gitDiffCardWidth(windowW)
	panel.Width(width).HeightPercent(80).Radius(10).Clip().
		Background(t.Background).Border(1, t.Border).
		Shadow(0, 12, 32, 0, mui.RGBA(0, 0, 0, 0.24)).
		AttachTo(anchor, mui.AnchorStart, mui.AnchorEnd).
		MarginEnd(10).AttachEdgeMargin(12)
	view := mui.Local(panel, "summary-view", func() taskSummaryDiffView { return taskSummaryDiffView{} })
	panel.Children(func() {
		mui.Row(c).Height(48).Gap(6).Padding(0, 12).AlignItems(mui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			line := "Task summary · " + a.TaskSummaryTitle
			if task.Result == taskstore.ResultFailure {
				line = "Task failure · " + a.TaskSummaryTitle
			}
			mui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				mui.Text(c, line).FontSize(a.gitFont(11)).
					Bold().SingleLine().MinWidth(0).Tooltip(a.TaskSummaryPath)
				if a.TaskSummaryPath != "" {
					a.gitCaption(c, a.TaskSummaryPath).SingleLine().MinWidth(0)
				}
			})
			mode := "Diff"
			if a.TaskSummaryDiff {
				mode = "Markdown"
			}
			if quietButton(c, mode).Label("Toggle task summary diff view").FontSize(a.gitFont(9)).
				Tooltip("Show the summary as a Markdown diff").Clicked() {
				a.TaskSummaryDiff = !a.TaskSummaryDiff
			}
			if quietButton(c, "✕").Label("Close").FontSize(a.gitFont(9)).Tooltip("Close the summary").Clicked() {
				a.closeTaskSummary()
			}
		})
		switch {
		case a.TaskSummaryBusy:
			centered(c, func() { busyLine(c, "Reading summary…") })
		case a.TaskSummaryErr != "":
			mui.Column(c).Grow(1).Padding(12, 12).Children(func() {
				dangerAlert(c, "Cannot show this summary", a.TaskSummaryErr)
			})
		case strings.TrimSpace(a.TaskSummaryBody) == "":
			centered(c, func() {
				emptyState(c, gitCompareIcon, "Summary is empty", "The checker saved no Markdown for this task.")
			})
		default:
			if a.TaskSummaryDiff {
				a.taskSummaryDiffBody(c, view)
				return
			}
			mui.Scroll(c).Grow(1).Children(func() {
				mui.Column(c).Padding(14, 18).Children(func() {
					markdown.View(c, a.TaskSummaryBody, &a.TaskMD)
				})
			})
		}
	})
}

// taskSummaryDiffView holds the summary presented like a Git diff: the whole
// Markdown file is the new side and the old side is empty, so every line is an
// addition. The viewer keeps its scroll position while the card is open.
type taskSummaryDiffView struct {
	new   string
	state *editor.DiffState
	font  float32
}

func (a *App) taskSummaryDiffBody(c *mui.Context, view *taskSummaryDiffView) {
	fontSize := a.gitFont(11)
	if view.state == nil || view.new != a.TaskSummaryBody || view.font != fontSize {
		view.new = a.TaskSummaryBody
		view.font = fontSize
		view.state = &editor.DiffState{
			Old:        editor.NewBuffer(""),
			New:        editor.NewBuffer(view.new),
			Layout:     editor.DiffUnified,
			Wrap:       true,
			Font:       mui.Font{Size: fontSize},
			LineHeight: 1.4,
		}
	}
	editor.DiffView(c, view.state).Grow(1).FillWidth()
}
