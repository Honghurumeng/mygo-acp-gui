package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	taskstore "github.com/reed/acp-gui/internal/tasks"
)

func TestTaskPanelCreateEditReorderDeleteAndPersist(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	app := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen || app.RightTab != rightTabTasks {
		t.Fatalf("task tab did not open: open=%v tab=%q", app.GitPanelOpen, app.RightTab)
	}
	if !tt.HasText("No tasks yet.") {
		t.Fatalf("empty task panel missing; texts: %q", tt.Texts())
	}
	// The Git / Tasks switch lives in the conversation header; the panel
	// itself only names the tab that is showing.
	for _, hidden := range []string{"Git right panel", "Task right panel"} {
		if _, ok := tt.Find(hidden); ok {
			t.Fatalf("panel still has an internal tab button %q", hidden)
		}
	}

	if err := tt.Click("Show Git panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen || app.RightTab != rightTabGit {
		t.Fatalf("Git tab did not take over the right column: open=%v tab=%q", app.GitPanelOpen, app.RightTab)
	}
	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.GitPanelOpen || app.RightTab != rightTabTasks {
		t.Fatalf("Tasks tab did not return: open=%v tab=%q", app.GitPanelOpen, app.RightTab)
	}

	addTask := func(title, text string) {
		t.Helper()
		app.openNewTask()
		app.TaskEditTitle = title
		app.TaskEditText = text
		tt.Frame()
		if err := tt.Click("Save task"); err != nil {
			t.Logf("texts: %q", tt.Texts())
			t.Fatal(err)
		}
		tt.Frame()
	}
	addTask("First task", "do the first thing")
	addTask("Second task", "do the second thing")
	addTask("Third task", "do the third thing")
	if len(app.Tasks) != 3 {
		t.Fatalf("tasks = %+v", app.Tasks)
	}
	if !tt.HasText("First task") || !tt.HasText("pending") {
		t.Fatalf("task rows missing; texts: %q", tt.Texts())
	}

	first := app.Tasks[0].ID
	app.moveTask(first, 1)
	if app.Tasks[0].Title != "Second task" || app.Tasks[1].Title != "First task" {
		t.Fatalf("order after move = %+v", app.Tasks)
	}

	app.openEditTask(first)
	app.TaskEditTitle = "First task renamed"
	tt.Frame()
	if err := tt.Click("Save task"); err != nil {
		t.Fatal(err)
	}
	if task := app.taskByID(first); task == nil || task.Title != "First task renamed" {
		t.Fatalf("edited task = %+v", task)
	}

	second := app.Tasks[0].ID
	app.requestDeleteTask(second)
	tt.Frame()
	if err := tt.Click("Delete this task"); err != nil {
		t.Fatal(err)
	}
	if len(app.Tasks) != 2 || app.taskByID(second) != nil {
		t.Fatalf("tasks after delete = %+v", app.Tasks)
	}

	store := taskstore.Open(filepath.Join(dataDir, taskFileName))
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tasks) != 2 || loaded.Tasks[0].Title != "First task renamed" {
		t.Fatalf("persisted tasks = %+v", loaded.Tasks)
	}

	restarted := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() {
		restarted.Shutdown()
		restarted.Flush()
	})
	if len(restarted.Tasks) != 2 || restarted.Tasks[0].Title != "First task renamed" {
		t.Fatalf("restarted tasks = %+v", restarted.Tasks)
	}
}

func TestTaskRunnerChecksContinuesAndCommits(t *testing.T) {
	workspace := initGitPanelRepo(t)
	t.Setenv("FAKE_TASK_CHECK_STATE", filepath.Join(t.TempDir(), "task-check-state"))
	sessionLog := filepath.Join(t.TempDir(), "task-sessions.log")
	t.Setenv("FAKE_AGENT_SESSION_LOG", sessionLog)
	app, tt := newFakeAppIn(t, workspace)

	addTask := func(title, text string) {
		t.Helper()
		app.openNewTask()
		app.TaskEditTitle = title
		app.TaskEditText = text
		app.saveTaskEditor()
	}
	addTask("First feature", "/eventually /writefile first-task.txt add the first feature")
	addTask("Second feature", "/writefile second-task.txt add the second feature")
	if len(app.Tasks) != 2 {
		t.Fatalf("tasks = %+v", app.Tasks)
	}

	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Run pending tasks"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		pending, ongoing, complete := app.taskCounts(workspace)
		return complete == 2 && pending == 0 && ongoing == 0 && !app.TaskRun.Active && !app.TaskRun.Queue
	})

	tasks := app.tasksForWorkspace(workspace)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v", app.Tasks)
	}
	if tasks[0].Status != taskstore.StatusComplete || tasks[1].Status != taskstore.StatusComplete {
		t.Fatalf("statuses = %q, %q", tasks[0].Status, tasks[1].Status)
	}
	if tasks[0].Result != taskstore.ResultPass || tasks[1].Result != taskstore.ResultPass {
		t.Fatalf("results = %q, %q; want pass", tasks[0].Result, tasks[1].Result)
	}
	if tasks[0].Rounds != 2 {
		t.Errorf("first task rounds = %d, want 2 (one rejected check)", tasks[0].Rounds)
	}
	if tasks[1].Rounds != 1 {
		t.Errorf("second task rounds = %d, want 1", tasks[1].Rounds)
	}
	if tasks[0].SessionID == "" || tasks[1].SessionID == "" {
		t.Errorf("executor sessions = %q, %q; want both recorded", tasks[0].SessionID, tasks[1].SessionID)
	}
	for _, task := range tasks {
		if task.SummaryPath == "" {
			t.Fatalf("task %q has no summary path", task.Title)
		}
		body, err := taskstore.ReadSummary(workspace, task.SummaryPath)
		if err != nil {
			t.Fatalf("read summary %q: %v", task.SummaryPath, err)
		}
		if !strings.Contains(body, task.Title) || !strings.Contains(body, "## Summary") {
			t.Errorf("summary for %q = %q", task.Title, body)
		}
	}

	// The runner commits once per completed task and never pushes.
	count := gitCommitCount(t, workspace)
	if count != 2 {
		t.Fatalf("commit count = %d, want 2", count)
	}

	// The executor chats stay readable after completion, but their sessions
	// are released and the task title remains visible in the sidebar.
	taskChats := 0
	for _, chat := range app.Chats {
		if chat.TaskTitle == "" {
			continue
		}
		taskChats++
		if chat.taskActive {
			t.Errorf("task chat %q is still locked", chat.TaskTitle)
		}
	}
	if taskChats != 2 {
		t.Errorf("task chats = %d, want 2", taskChats)
	}

	app.openTaskSummary(tasks[0].ID)
	waitFor(t, app, tt, func() bool {
		return app.TaskSummaryOpen && !app.TaskSummaryBusy && app.TaskSummaryErr == "" && app.TaskSummaryBody != ""
	})
	if !tt.HasText("Task summary") || !tt.HasText("Task complete") {
		t.Errorf("summary card missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Show diff"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.TaskSummaryMode != viewerDiff {
		t.Fatal("summary diff toggle did not switch to the diff view")
	}
	if err := tt.Click("Show file content"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.TaskSummaryMode != viewerView {
		t.Fatal("summary view toggle did not switch back to Markdown")
	}
	if err := tt.Click("Edit file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.TaskSummaryMode != viewerEdit {
		t.Fatal("summary edit toggle did not switch to the editor")
	}
	app.saveTaskSummary("# Replaced summary\n")
	waitFor(t, app, tt, func() bool {
		return !app.TaskSummarySaving && app.TaskSummaryBody == "# Replaced summary\n"
	})
	body, err := taskstore.ReadSummary(tasks[0].Workspace, tasks[0].SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "# Replaced summary") {
		t.Fatalf("saved summary = %q", body)
	}
	if !tt.HasText("Saved.") {
		t.Errorf("summary saved notice missing; texts: %q", tt.Texts())
	}

	store := taskstore.Open(filepath.Join(filepath.Dir(app.taskStore.Path()), taskFileName))
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tasks) != 2 || loaded.Tasks[0].Status != taskstore.StatusComplete {
		t.Fatalf("persisted tasks = %+v", loaded.Tasks)
	}

	// The successful tasks changed files in the workspace and each task made
	// exactly one commit with its own file, not just the summary Markdown.
	for _, name := range []string{"first-task.txt", "second-task.txt"} {
		data, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || string(data) != "task result\n" {
			t.Errorf("successful task file %s = %q, err = %v", name, data, err)
		}
	}
	messages := strings.Split(strings.TrimSpace(gitPanelOutput(t, workspace, "log", "--format=%s")), "\n")
	if len(messages) != 2 || messages[0] != "task: Second feature" || messages[1] != "task: First feature" {
		t.Fatalf("task commit messages = %q", messages)
	}
	if files := gitPanelOutput(t, workspace, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "second-task.txt") {
		t.Errorf("second task commit does not contain its file: %q", files)
	}
	if files := gitPanelOutput(t, workspace, "show", "--name-only", "--format=", "HEAD~1"); !strings.Contains(files, "first-task.txt") {
		t.Errorf("first task commit does not contain its file: %q", files)
	}

	// The executor continues the same session for round two, new tasks get a
	// new process, and every checker round is a fresh process outside the
	// task workspace. Prompt start/finish lines also prove the queue never
	// runs two task sessions at once.
	lines := readTaskSessionLog(t, sessionLog)
	exec, check := classifyTaskSessionLog(t, lines)
	if len(exec) != 3 {
		t.Fatalf("executor prompts = %d, want 3 (2 rounds + 1): %+v", len(exec), exec)
	}
	if exec[0].pid != exec[1].pid || exec[1].pid == exec[2].pid {
		t.Errorf("executor processes = %d,%d,%d; want task one reused, task two new", exec[0].pid, exec[1].pid, exec[2].pid)
	}
	if len(check) != 3 {
		t.Fatalf("checker prompts = %d, want 3: %+v", len(check), check)
	}
	checkerPIDs := map[int]bool{}
	for _, item := range check {
		if item.pid == exec[0].pid || item.pid == exec[2].pid {
			t.Errorf("checker reused an executor process: %+v", item)
		}
		if checkerPIDs[item.pid] {
			t.Errorf("checker process %d was reused; want one per round", item.pid)
		}
		checkerPIDs[item.pid] = true
	}
	cwd := map[int]string{}
	for _, item := range lines {
		if item.kind == "new" {
			cwd[item.pid] = item.detail
		}
	}
	if canonicalWorkspace(cwd[exec[0].pid]) != canonicalWorkspace(workspace) {
		t.Errorf("executor cwd = %q, want %q", cwd[exec[0].pid], workspace)
	}
	for pid := range checkerPIDs {
		if cwd[pid] == "" || canonicalWorkspace(cwd[pid]) == canonicalWorkspace(workspace) {
			t.Errorf("checker %d cwd = %q, want a throwaway workspace", pid, cwd[pid])
		}
	}
	assertSerialTaskPrompts(t, lines)
}

func TestTaskRunnerStopLeavesTaskPending(t *testing.T) {
	workspace := t.TempDir()
	app, tt := newFakeAppIn(t, workspace)
	app.openNewTask()
	app.TaskEditTitle = "Long task"
	app.TaskEditText = "/slow do the long thing"
	app.saveTaskEditor()

	app.startTaskQueue()
	waitFor(t, app, tt, func() bool {
		chat := app.TaskRun.Chat
		return app.TaskRun.Active && app.TaskRun.Phase == "executing" && chat != nil && chat.Streaming
	})
	app.stopTaskRun("Stopped by user.")
	waitFor(t, app, tt, func() bool { return !app.TaskRun.Active && !app.TaskRun.Queue })

	tasks := app.tasksForWorkspace(workspace)
	if len(tasks) != 1 || tasks[0].Status != taskstore.StatusPending {
		t.Fatalf("tasks after stop = %+v", tasks)
	}
	if !strings.Contains(tasks[0].Note, "Stopped") {
		t.Errorf("stop note = %q", tasks[0].Note)
	}
	if app.TaskNotice == "" {
		t.Error("stop notice missing")
	}
	for _, chat := range app.Chats {
		if chat.TaskTitle != "" && chat.Streaming {
			t.Errorf("stopped task chat %q is still streaming", chat.TaskTitle)
		}
	}
}

func TestTaskFailureDiscardsWorkAndMarksFailure(t *testing.T) {
	workspace := initGitPanelRepo(t)
	writeGitPanelFile(t, workspace, "base.txt", "base\n")
	runGitPanelCommand(t, workspace, "add", ".")
	runGitPanelCommand(t, workspace, "commit", "-q", "-m", "base")
	// This untracked file exists before the task and must survive the rollback.
	writeGitPanelFile(t, workspace, "preexisting.txt", "keep\n")

	app, tt := newFakeAppIn(t, workspace)
	app.openNewTask()
	app.TaskEditTitle = "Refused task"
	app.TaskEditText = "/refuse /writefile task-output.txt"
	app.saveTaskEditor()

	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Run pending tasks"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		tasks := app.tasksForWorkspace(workspace)
		return len(tasks) == 1 && tasks[0].Status == taskstore.StatusComplete &&
			tasks[0].Result == taskstore.ResultFailure && !app.TaskRun.Active && !app.TaskRun.Queue
	})

	task := app.tasksForWorkspace(workspace)[0]
	if task.SummaryPath == "" {
		t.Fatal("failed task has no summary path")
	}
	if !strings.Contains(task.Note, "discarded") {
		t.Errorf("failure note = %q", task.Note)
	}
	body, err := taskstore.ReadSummary(workspace, task.SummaryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- Result: failure", "## Checker", "refusal"} {
		if !strings.Contains(body, want) {
			t.Errorf("failure summary missing %q:\n%s", want, body)
		}
	}
	// The executor created this file, but a failed task rolls it back.
	if _, err := os.Stat(filepath.Join(workspace, "task-output.txt")); !os.IsNotExist(err) {
		t.Errorf("task output survived the failed run (err = %v)", err)
	}
	// A pre-existing untracked file is part of the snapshot and comes back.
	data, err := os.ReadFile(filepath.Join(workspace, "preexisting.txt"))
	if err != nil || string(data) != "keep\n" {
		t.Errorf("pre-existing file = %q, err = %v", data, err)
	}
	if count := gitCommitCount(t, workspace); count != 1 {
		t.Fatalf("failed task made a commit: count = %d", count)
	}
	if !tt.HasText("failure") {
		t.Errorf("failure marker missing from the complete list; texts: %q", tt.Texts())
	}

	loaded, err := taskstore.Open(filepath.Join(filepath.Dir(app.taskStore.Path()), taskFileName)).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].Result != taskstore.ResultFailure {
		t.Fatalf("persisted failed task = %+v", loaded.Tasks)
	}
}

func TestTaskFailureKeepsQueueRunning(t *testing.T) {
	workspace := t.TempDir()
	app, tt := newFakeAppIn(t, workspace)

	app.openNewTask()
	app.TaskEditTitle = "Refused task"
	app.TaskEditText = "/refuse first task"
	app.saveTaskEditor()
	app.openNewTask()
	app.TaskEditTitle = "Following task"
	app.TaskEditText = "finish the following task"
	app.saveTaskEditor()

	app.startTaskQueue()
	waitFor(t, app, tt, func() bool {
		pending, ongoing, complete := app.taskCounts(workspace)
		return len(app.tasksForWorkspace(workspace)) == 2 && pending == 0 && ongoing == 0 &&
			complete == 2 && !app.TaskRun.Active && !app.TaskRun.Queue
	})

	tasks := app.tasksForWorkspace(workspace)
	if tasks[0].Result != taskstore.ResultFailure {
		t.Fatalf("first task result = %q, want failure", tasks[0].Result)
	}
	if tasks[1].Result != taskstore.ResultPass || tasks[1].SummaryPath == "" {
		t.Fatalf("second task = %+v, want a completed pass with a summary", tasks[1])
	}
}

func TestTaskCheckerPromptCarriesEarlierRounds(t *testing.T) {
	prompt := taskCheckerPrompt(taskstore.Task{
		ID: "id", Title: "Fix it", Text: "details",
	}, []taskCheckRound{
		{Summary: "first round summary", Feedback: "add the missing test"},
		{Summary: "second round summary"},
	})
	for _, want := range []string{
		"first round summary",
		"add the missing test",
		"second round summary",
		"ROUND 1 EXECUTOR SUMMARY",
		"CHECKER FEEDBACK AFTER ROUND 1",
		"ROUND 2 EXECUTOR SUMMARY",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("checker prompt missing %q:\n%s", want, prompt)
		}
	}
	first := strings.Index(prompt, "first round summary")
	feedback := strings.Index(prompt, "add the missing test")
	second := strings.Index(prompt, "second round summary")
	if !(first >= 0 && first < feedback && feedback < second) {
		t.Errorf("checker history order = first %d, feedback %d, second %d", first, feedback, second)
	}
}

func TestParseTaskVerdictAndSummaryHelpers(t *testing.T) {
	verdict, err := parseTaskVerdict("```json\n{\"complete\":true,\"reason\":\"done\",\"summary\":\"# Done\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Complete || verdict.Reason != "done" || verdict.Summary != "# Done" {
		t.Fatalf("verdict = %+v", verdict)
	}
	if _, err := parseTaskVerdict("not json"); err == nil {
		t.Fatal("invalid verdict was accepted")
	}
	if got := lastParagraph("first\n\nsecond\n\nthird\n"); got != "third" {
		t.Fatalf("last paragraph = %q", got)
	}
	task := taskstore.Task{ID: "id", Title: strings.Repeat("long ", 30), Workspace: "/tmp/ws"}
	if message := taskCommitMessage(task); len([]rune(message)) > 72 {
		t.Fatalf("commit message = %q", message)
	}
	markdown := buildTaskSummaryMarkdown(taskstore.Task{
		ID: "id", Title: "Do it", Workspace: "/tmp/ws", Text: "details", Rounds: 2,
	}, taskstore.ResultPass, verdict, "agent report", time.Now())
	for _, want := range []string{"# Do it", "## Task", "details", "## Summary", "# Done", "## Agent's final report", "agent report"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("summary missing %q:\n%s", want, markdown)
		}
	}
	failure := buildTaskSummaryMarkdown(taskstore.Task{
		ID: "id", Title: "Do it", Workspace: "/tmp/ws", Rounds: 8,
	}, taskstore.ResultFailure, taskVerdict{Reason: "It never passed."}, "", time.Now())
	for _, want := range []string{"- Result: failure", "## Checker", "It never passed."} {
		if !strings.Contains(failure, want) {
			t.Errorf("failure summary missing %q:\n%s", want, failure)
		}
	}
}

func gitCommitCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-list: %v", err)
	}
	count := 0
	for _, r := range strings.TrimSpace(string(out)) {
		if r < '0' || r > '9' {
			t.Fatalf("git rev-list output = %q", out)
		}
		count = count*10 + int(r-'0')
	}
	return count
}

func TestTaskStoreOngoingBecomesPendingOnLoad(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	store := taskstore.Open(filepath.Join(dataDir, taskFileName))
	if err := store.Save(taskstore.File{Version: taskstore.CurrentVersion, Tasks: []taskstore.Task{{
		ID: "x", Workspace: workspace, Title: "Interrupted", Status: taskstore.StatusOngoing,
	}}}); err != nil {
		t.Fatal(err)
	}
	app := New(Options{Version: "test", Workspace: workspace, DataDir: dataDir, Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() { app.Shutdown(); app.Flush() })
	if len(app.Tasks) != 1 || app.Tasks[0].Status != taskstore.StatusPending {
		t.Fatalf("tasks = %+v", app.Tasks)
	}
	if !strings.Contains(app.Tasks[0].Note, "Interrupted") {
		t.Fatalf("note = %q", app.Tasks[0].Note)
	}
}

type taskSessionEntry struct {
	pid    int
	kind   string
	detail string
}

// readTaskSessionLog parses the fake agent's FAKE_AGENT_SESSION_LOG lines.
func readTaskSessionLog(t *testing.T, path string) []taskSessionEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	var out []taskSessionEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 5)
		if len(fields) != 5 {
			t.Fatalf("malformed session log line: %q", line)
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("session log pid %q: %v", fields[1], err)
		}
		var detail string
		if err := json.Unmarshal([]byte(fields[4]), &detail); err != nil {
			t.Fatalf("session log detail %q: %v", fields[4], err)
		}
		out = append(out, taskSessionEntry{pid: pid, kind: fields[2], detail: detail})
	}
	return out
}

func classifyTaskSessionLog(t *testing.T, lines []taskSessionEntry) (executor, checker []taskSessionEntry) {
	t.Helper()
	for _, line := range lines {
		if line.kind != "prompt" {
			continue
		}
		switch {
		case strings.HasPrefix(line.detail, "TASK_EXECUTION"):
			executor = append(executor, line)
		case strings.HasPrefix(line.detail, "TASK_COMPLETION_CHECK"):
			checker = append(checker, line)
		}
	}
	return executor, checker
}

// assertSerialTaskPrompts fails when a prompt starts before the previous task
// prompt has finished. session/new lines are ignored; only turns overlap.
func assertSerialTaskPrompts(t *testing.T, lines []taskSessionEntry) {
	t.Helper()
	active := map[int]bool{}
	for _, line := range lines {
		switch line.kind {
		case "prompt":
			if len(active) > 0 {
				t.Errorf("prompt from pid %d overlaps active prompts %v", line.pid, active)
			}
			active[line.pid] = true
		case "finish":
			delete(active, line.pid)
		}
	}
	if len(active) != 0 {
		t.Errorf("prompts left active: %v", active)
	}
}

func TestTaskPanelButtonsCreateEditReorderDelete(t *testing.T) {
	workspace := t.TempDir()
	app := New(Options{Version: "test", Workspace: workspace, DataDir: t.TempDir(), Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	if err := tt.Click("Show Tasks panel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("New task"); err != nil {
		t.Fatal(err)
	}
	app.TaskEditTitle = "First button task"
	app.TaskEditText = "created from the panel"
	tt.Frame()
	if err := tt.Click("Save task"); err != nil {
		t.Fatal(err)
	}
	if len(app.tasksForWorkspace(workspace)) != 1 {
		t.Fatalf("tasks after New task = %+v", app.Tasks)
	}
	first := app.tasksForWorkspace(workspace)[0].ID

	tt.Frame()
	if err := tt.Click("Edit task"); err != nil {
		t.Fatal(err)
	}
	if !app.TaskEditorOpen || app.TaskEditID != first {
		t.Fatalf("Edit task opened id=%q open=%v", app.TaskEditID, app.TaskEditorOpen)
	}
	app.TaskEditTitle = "Renamed from the panel"
	tt.Frame()
	if err := tt.Click("Save task"); err != nil {
		t.Fatal(err)
	}
	if task := app.taskByID(first); task == nil || task.Title != "Renamed from the panel" {
		t.Fatalf("edited task = %+v", task)
	}

	if err := tt.Click("New task"); err != nil {
		t.Fatal(err)
	}
	app.TaskEditTitle = "Second button task"
	app.TaskEditText = "another task"
	tt.Frame()
	if err := tt.Click("Save task"); err != nil {
		t.Fatal(err)
	}
	if len(app.tasksForWorkspace(workspace)) != 2 {
		t.Fatalf("tasks after second create = %+v", app.Tasks)
	}

	tt.Frame()
	if err := tt.Click("Move task down"); err != nil {
		t.Fatal(err)
	}
	tasks := app.tasksForWorkspace(workspace)
	if tasks[0].Title != "Second button task" || tasks[1].Title != "Renamed from the panel" {
		t.Fatalf("order after move down = %+v", tasks)
	}

	tt.Frame()
	if err := tt.Click("Delete task"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Delete this task"); err != nil {
		t.Fatal(err)
	}
	if left := app.tasksForWorkspace(workspace); len(left) != 1 || left[0].Title != "Renamed from the panel" {
		t.Fatalf("tasks after delete = %+v", left)
	}
}
