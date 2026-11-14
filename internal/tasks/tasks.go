// Package tasks stores the ordered task list and the Markdown summaries the
// task runner writes into a workspace's tasks folder.
//
// The index lives next to sessions.json in the app data directory. Summaries
// are project files: they are written as tasks/<id>-<slug>.md inside the task's
// workspace so the user can read and commit them with the work they describe.
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// CurrentVersion is the schema version this build writes.
const CurrentVersion = 1

// SummaryDir is the workspace-relative folder that holds task summaries.
const SummaryDir = "tasks"

// Status is the lifecycle of one task. Only pending tasks are editable, and
// only one task is ongoing at a time because the runner is serial.
type Status string

// Task states, written verbatim to tasks.json.
const (
	StatusPending  Status = "pending"
	StatusOngoing  Status = "ongoing"
	StatusComplete Status = "complete"
)

// Valid reports whether s is one of the three task states.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusOngoing, StatusComplete:
		return true
	default:
		return false
	}
}

// Result distinguishes a completed task that passed the checker from one the
// runner gave up on. Both are status complete so they stay in the same list;
// a failure has a summary and a note but no commit.
type Result string

// Task outcomes, written verbatim to tasks.json for complete tasks.
const (
	ResultPass    Result = "pass"
	ResultFailure Result = "failure"
)

// Valid reports whether r is one of the two completed-task outcomes.
func (r Result) Valid() bool {
	return r == ResultPass || r == ResultFailure
}

// Task is one item in the serial queue. The title and text are user input;
// SessionID and SummaryPath are written by the runner after the executor and
// checker have run.
type Task struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Title     string `json:"title"`
	Text      string `json:"text,omitempty"`
	Status    Status `json:"status"`

	CreatedAt   string `json:"createdAt,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`

	// SummaryPath is relative to Workspace (tasks/<id>-<slug>.md). It is
	// empty until the task finishes, either pass or failure.
	SummaryPath string `json:"summaryPath,omitempty"`
	// Result is pass or failure once Status is complete. Pending and ongoing
	// rows leave it empty; older complete rows without a result are pass.
	Result    Result `json:"result,omitempty"`
	AgentID   string `json:"agentId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	// Rounds counts executor turns in the task's current run.
	Rounds int `json:"rounds,omitempty"`
	// Note carries the last checker reason, a stop reason, or a persistence /
	// commit warning shown under the task title.
	Note string `json:"note,omitempty"`
}

// File is the on-disk task index document.
type File struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// Store reads and writes one tasks.json file. A nil Store or an empty path is
// a no-op, which keeps tests and portable runs free of side effects.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open returns a store for path. The file is not read until Load.
func Open(path string) *Store {
	return &Store{path: strings.TrimSpace(path)}
}

// Path is the index location. It is empty when persistence is off.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the index. A missing file is an empty current-version document.
// Rows without an id are dropped; an unknown status becomes pending so a
// future or damaged record cannot pin the queue.
func (s *Store) Load() (File, error) {
	if s == nil || s.path == "" {
		return File{Version: CurrentVersion, Tasks: []Task{}}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := File{Version: CurrentVersion, Tasks: []Task{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file, nil
		}
		return file, fmt.Errorf("tasks: read %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return File{Version: CurrentVersion, Tasks: []Task{}},
			fmt.Errorf("tasks: decode %s: %w", s.path, err)
	}
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	seen := make(map[string]bool, len(file.Tasks))
	out := make([]Task, 0, len(file.Tasks))
	for _, task := range file.Tasks {
		task.ID = strings.TrimSpace(task.ID)
		if task.ID == "" || seen[task.ID] {
			continue
		}
		seen[task.ID] = true
		task.Workspace = strings.TrimSpace(task.Workspace)
		task.Title = strings.TrimSpace(task.Title)
		if task.Title == "" {
			task.Title = "Untitled task"
		}
		if !task.Status.Valid() {
			task.Status = StatusPending
		}
		task.SummaryPath = strings.TrimSpace(task.SummaryPath)
		if task.Status == StatusComplete {
			if !task.Result.Valid() {
				task.Result = ResultPass
			}
		} else {
			task.Result = ""
		}
		out = append(out, task)
	}
	file.Tasks = out
	return file, nil
}

// Save writes the index atomically. Parent directories are created as needed
// and the file is mode 0600 because it records local workspace paths.
func (s *Store) Save(file File) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	if file.Tasks == nil {
		file.Tasks = []Task{}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("tasks: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tasks: create data directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tasks-*.json")
	if err != nil {
		return fmt.Errorf("tasks: create temporary file: %w", err)
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if err := tmp.Chmod(0o600); err != nil {
		remove()
		return fmt.Errorf("tasks: set mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("tasks: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("tasks: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tasks: close: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("tasks: replace: %w", err)
	}
	return nil
}

// NewID returns an opaque task id used in filenames and keys. It avoids a
// dependency on a UUID package and stays stable across restarts.
func NewID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// WriteSummary writes content to <workspace>/tasks/<id>-<slug>.md and returns
// the workspace-relative path with forward slashes. The summary folder is
// created when missing.
func WriteSummary(workspace string, task Task, content string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return "", errors.New("tasks: workspace must be an absolute path")
	}
	id := slug(strings.TrimSpace(task.ID))
	if id == "" || id == "task" {
		id = NewID()
	}
	dir := filepath.Join(workspace, SummaryDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("tasks: create summary folder: %w", err)
	}
	name := id + "-" + slug(task.Title) + ".md"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("tasks: write summary: %w", err)
	}
	return filepath.ToSlash(filepath.Join(SummaryDir, name)), nil
}

// ResolveSummary turns a stored relative summary path into an absolute path
// inside the workspace, or returns "" when the record is outside tasks/.
func ResolveSummary(workspace, relative string) string {
	workspace = strings.TrimSpace(workspace)
	relative = strings.TrimSpace(relative)
	if workspace == "" || !filepath.IsAbs(workspace) || relative == "" {
		return ""
	}
	rel := filepath.FromSlash(relative)
	if filepath.IsAbs(rel) {
		return ""
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) < 2 || parts[0] != SummaryDir {
		return ""
	}
	return filepath.Join(workspace, clean)
}

// ReadSummary reads the stored summary. A missing file is an error so the UI
// can explain that the summary was moved or deleted.
func ReadSummary(workspace, relative string) (string, error) {
	path := ResolveSummary(workspace, relative)
	if path == "" {
		return "", errors.New("tasks: invalid summary path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("tasks: read summary: %w", err)
	}
	return string(data), nil
}

// slug makes a short, filesystem-safe filename part. Letters and digits in
// any script are kept, separators and punctuation collapse into '-'.
func slug(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "task"
	}
	return out
}
