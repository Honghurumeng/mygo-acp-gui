// Package settings loads and saves the app's preferences file.
//
// The file lives in the app data directory (see
// docs/design/architecture.md) and is written atomically. Load is tolerant:
// a corrupt file, a bad MCP list or a relative path yields warnings and
// defaults instead of blocking startup.
package settings

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

// Paths are explicit tool overrides. Node, Pi and CommandCode must be
// absolute paths; Shell may be a name resolved from PATH.
type Paths struct {
	Shell       string `json:"shell,omitempty"`
	Node        string `json:"node,omitempty"`
	Pi          string `json:"pi,omitempty"`
	CommandCode string `json:"commandCode,omitempty"`
}

// File is the on-disk settings document. Workspaces keeps every known folder
// in sidebar order; RemovedWorkspaces holds the soft-deleted ones, which stay
// in the list so they can be restored later. CollapsedWorkspaces lists the
// groups whose session lists are folded in the sidebar.
type File struct {
	Version             int      `json:"version"`
	LastAgent           string   `json:"lastAgent,omitempty"`
	LastWorkspace       string   `json:"lastWorkspace,omitempty"`
	Workspaces          []string `json:"workspaces,omitempty"`
	RemovedWorkspaces   []string `json:"removedWorkspaces,omitempty"`
	CollapsedWorkspaces []string `json:"collapsedWorkspaces,omitempty"`
	MCPServers          []any    `json:"mcpServers,omitempty"`
	Paths               Paths    `json:"paths,omitempty"`
	// Notifications enables desktop notifications. It defaults to true so
	// older files without the field keep the previous behavior; the field is
	// written even when false so turning notifications off survives a restart.
	Notifications bool `json:"notifications"`
	// DockBadge enables the icon badge for conversations waiting for the user.
	// Like Notifications, it defaults to true for older files and is written
	// even when false.
	DockBadge bool `json:"dockBadge"`
}

// Store reads and writes one settings file. A nil Store or an empty path is
// a no-op, which keeps tests and `-settings` runs free of side effects.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open returns a store for path. The file is not read until Load.
func Open(path string) *Store {
	return &Store{path: strings.TrimSpace(path)}
}

// Path is the settings file location. It is empty when persistence is off.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the file. A missing file is not an error: it returns defaults
// at the current version. Other problems are returned together with whatever
// fields could be decoded, so the caller can warn and continue.
func defaults() File {
	return File{Version: CurrentVersion, Notifications: true, DockBadge: true}
}

func (s *Store) Load() (File, error) {
	if s == nil || s.path == "" {
		return defaults(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := defaults()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file, nil
		}
		return file, fmt.Errorf("settings: read %s: %w", s.path, err)
	}
	return decode(data)
}

// Save writes the file atomically. Parent directories are created as needed
// and the file is mode 0600 because it records local paths.
func (s *Store) Save(file File) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("settings: create data directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("settings: create temporary file: %w", err)
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("settings: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("settings: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("settings: close temporary file: %w", err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("settings: chmod temporary file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("settings: replace %s: %w", s.path, err)
	}
	return nil
}

// rawFile keeps mcpServers undecoded so one malformed list does not discard
// the other fields.
type rawFile struct {
	Version             int             `json:"version"`
	LastAgent           string          `json:"lastAgent"`
	LastWorkspace       string          `json:"lastWorkspace"`
	Workspaces          json.RawMessage `json:"workspaces"`
	RemovedWorkspaces   json.RawMessage `json:"removedWorkspaces"`
	CollapsedWorkspaces json.RawMessage `json:"collapsedWorkspaces"`
	MCPServers          json.RawMessage `json:"mcpServers"`
	Paths               Paths           `json:"paths"`
	// Pointers distinguish an older file without the setting (default on)
	// from an explicit false.
	Notifications *bool `json:"notifications"`
	DockBadge     *bool `json:"dockBadge"`
}

func decode(data []byte) (File, error) {
	file := defaults()
	var raw rawFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return file, fmt.Errorf("settings: invalid JSON: %w", err)
	}

	var problems []error
	if raw.Version > CurrentVersion {
		problems = append(problems, fmt.Errorf("settings: version %d is newer than %d; unknown fields are ignored", raw.Version, CurrentVersion))
	}
	file.LastAgent = strings.TrimSpace(raw.LastAgent)
	file.LastWorkspace = strings.TrimSpace(raw.LastWorkspace)
	if file.LastWorkspace != "" && !filepath.IsAbs(file.LastWorkspace) {
		problems = append(problems, fmt.Errorf("settings: ignoring relative lastWorkspace %q", file.LastWorkspace))
		file.LastWorkspace = ""
	}
	file.Workspaces = decodeWorkspaceList(raw.Workspaces, "workspaces", &problems)
	file.RemovedWorkspaces = decodeWorkspaceList(raw.RemovedWorkspaces, "removedWorkspaces", &problems)
	file.CollapsedWorkspaces = decodeWorkspaceList(raw.CollapsedWorkspaces, "collapsedWorkspaces", &problems)
	if len(raw.MCPServers) > 0 && string(raw.MCPServers) != "null" {
		var servers []any
		if err := json.Unmarshal(raw.MCPServers, &servers); err != nil {
			problems = append(problems, fmt.Errorf("settings: ignoring mcpServers: %w", err))
		} else {
			file.MCPServers = servers
		}
	}
	file.Paths = sanitizePaths(raw.Paths, &problems)
	if raw.Notifications != nil {
		file.Notifications = *raw.Notifications
	}
	if raw.DockBadge != nil {
		file.DockBadge = *raw.DockBadge
	}
	return file, errors.Join(problems...)
}

func decodeWorkspaceList(raw json.RawMessage, name string, problems *[]error) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var workspaces []string
	if err := json.Unmarshal(raw, &workspaces); err != nil {
		*problems = append(*problems, fmt.Errorf("settings: ignoring %s: %w", name, err))
		return nil
	}
	return sanitizeWorkspaces(workspaces, problems)
}

func sanitizeWorkspaces(workspaces []string, problems *[]error) []string {
	seen := make(map[string]bool, len(workspaces))
	clean := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspace = strings.TrimSpace(workspace)
		if workspace == "" {
			continue
		}
		if !filepath.IsAbs(workspace) {
			*problems = append(*problems, fmt.Errorf("settings: ignoring relative workspace %q", workspace))
			continue
		}
		workspace = filepath.Clean(workspace)
		if seen[workspace] {
			continue
		}
		seen[workspace] = true
		clean = append(clean, workspace)
	}
	return clean
}

func sanitizePaths(paths Paths, problems *[]error) Paths {
	clean := func(name, value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		if !filepath.IsAbs(value) {
			*problems = append(*problems, fmt.Errorf("settings: ignoring relative %s path %q", name, value))
			return ""
		}
		return filepath.Clean(value)
	}
	return Paths{
		Shell:       strings.TrimSpace(paths.Shell),
		Node:        clean("node", paths.Node),
		Pi:          clean("pi", paths.Pi),
		CommandCode: clean("commandCode", paths.CommandCode),
	}
}
