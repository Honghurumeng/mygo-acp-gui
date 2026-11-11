// Package archive stores the app's local session index.
//
// The archive is the source of truth for the sidebar's saved session list.
// It links each agent session id to the agent that owns it and keeps local UI
// state that the agent does not know about: whether the session is shown and
// the user's display name.
package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// CurrentVersion is the schema version this build writes.
const CurrentVersion = 1

// Session is one locally archived ACP session. AgentID and SessionID identify
// the agent-side session; Cwd, Title and UpdatedAt mirror the metadata the
// agent reports. ShowName and Show are local-only.
type Session struct {
	AgentID   string `json:"agentId"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	ShowName  string `json:"showName,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Show is false after the user deletes the session. The record is kept
	// so a later agent session/list cannot make it reappear.
	Show bool `json:"show"`
}

// File is the on-disk archive document.
type File struct {
	Version  int       `json:"version"`
	Sessions []Session `json:"sessions"`
}

// Store reads and writes one archive file. A nil Store or an empty path is a
// no-op, which keeps tests and portable runs free of side effects.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open returns a store for path. The file is not read until Load.
func Open(path string) *Store {
	return &Store{path: strings.TrimSpace(path)}
}

// Path is the archive file location. It is empty when persistence is off.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the archive. A missing file is not an error: it returns an empty
// archive at the current version. Invalid individual rows are dropped with a
// warning so one bad record cannot hide the rest.
func (s *Store) Load() (File, error) {
	if s == nil || s.path == "" {
		return File{Version: CurrentVersion}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := File{Version: CurrentVersion}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file, nil
		}
		return file, fmt.Errorf("archive: read %s: %w", s.path, err)
	}
	return decode(data)
}

// Save writes the archive atomically. Parent directories are created as needed
// and the file is mode 0600 because it records local paths and session ids.
func (s *Store) Save(file File) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	if file.Sessions == nil {
		file.Sessions = []Session{}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("archive: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("archive: create data directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".sessions-*.json")
	if err != nil {
		return fmt.Errorf("archive: create temporary file: %w", err)
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("archive: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("archive: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("archive: close temporary file: %w", err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("archive: chmod temporary file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("archive: replace %s: %w", s.path, err)
	}
	return nil
}

type rawFile struct {
	Version  int          `json:"version"`
	Sessions []rawSession `json:"sessions"`
}

type rawSession struct {
	AgentID   string `json:"agentId"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title"`
	ShowName  string `json:"showName"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	// Missing show on an older record means visible.
	Show *bool `json:"show"`
}

func decode(data []byte) (File, error) {
	file := File{Version: CurrentVersion}
	var raw rawFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return file, fmt.Errorf("archive: invalid JSON: %w", err)
	}

	var problems []error
	if raw.Version > CurrentVersion {
		problems = append(problems, fmt.Errorf("archive: version %d is newer than %d; unknown fields are ignored", raw.Version, CurrentVersion))
	}
	seen := make(map[string]bool, len(raw.Sessions))
	for i, item := range raw.Sessions {
		agentID := strings.TrimSpace(item.AgentID)
		sessionID := strings.TrimSpace(item.SessionID)
		if agentID == "" || sessionID == "" {
			problems = append(problems, fmt.Errorf("archive: ignoring session %d without agentId/sessionId", i))
			continue
		}
		key := agentID + "\x00" + sessionID
		if seen[key] {
			problems = append(problems, fmt.Errorf("archive: ignoring duplicate session %s for %s", sessionID, agentID))
			continue
		}
		seen[key] = true

		show := true
		if item.Show != nil {
			show = *item.Show
		}
		record := Session{
			AgentID:   agentID,
			SessionID: sessionID,
			Cwd:       cleanPath(item.Cwd),
			Title:     strings.TrimSpace(item.Title),
			ShowName:  strings.TrimSpace(item.ShowName),
			CreatedAt: strings.TrimSpace(item.CreatedAt),
			UpdatedAt: strings.TrimSpace(item.UpdatedAt),
			Show:      show,
		}
		file.Sessions = append(file.Sessions, record)
	}
	return file, errors.Join(problems...)
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(path)
}
