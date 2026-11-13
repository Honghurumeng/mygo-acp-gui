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

// DefaultFontScale is the percentage at which every area starts. Fonts are
// stored as percentages of the app's built-in sizes so the settings file
// stays independent of the layout metrics.
const DefaultFontScale = 100

// Fonts holds the text scale of each UI area as a percentage. A missing
// field means DefaultFontScale, so older files keep their previous sizes.
type Fonts struct {
	// WorkspaceList scales the sidebar's groups, conversations and saved
	// sessions, together with its status area.
	WorkspaceList int `json:"workspaceList,omitempty"`
	// Chat scales the conversation: messages, tool cards and Markdown.
	Chat int `json:"chat,omitempty"`
	// Composer scales the message box and its chips, menus and buttons.
	Composer int `json:"composer,omitempty"`
	// Git scales the Git panel, its history and the diff card.
	Git int `json:"git,omitempty"`
}

// DefaultFonts returns the font scales of a new settings file.
func DefaultFonts() Fonts {
	return Fonts{}.withDefaults()
}

// withDefaults fills every missing scale. A zero cannot be a valid percentage,
// so it means the field was absent in an older file.
func (f Fonts) withDefaults() Fonts {
	if f.WorkspaceList <= 0 {
		f.WorkspaceList = DefaultFontScale
	}
	if f.Chat <= 0 {
		f.Chat = DefaultFontScale
	}
	if f.Composer <= 0 {
		f.Composer = DefaultFontScale
	}
	if f.Git <= 0 {
		f.Git = DefaultFontScale
	}
	return f
}

// Paths are explicit tool overrides. Node, Pi and CommandCode must be
// absolute paths; Shell may be a name resolved from PATH.
type Paths struct {
	Shell       string `json:"shell,omitempty"`
	Node        string `json:"node,omitempty"`
	Pi          string `json:"pi,omitempty"`
	CommandCode string `json:"commandCode,omitempty"`
}

// Selection is the last model-panel choice for one agent: the current value
// of every select config option (model, thinking, ...) plus the session mode.
// A new draft starts from these values, and Connect skips any the agent no
// longer advertises.
type Selection struct {
	ConfigOptions map[string]string `json:"configOptions,omitempty"`
	Mode          string            `json:"mode,omitempty"`
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
	// Appearance is the light/dark choice: system, light or dark. An empty
	// value means system, so older files keep following the desktop.
	Appearance string `json:"appearance,omitempty"`
	// Theme names the colors: default, nord, solarized, catppuccin or claude.
	// An empty value means default.
	Theme string `json:"theme,omitempty"`
	// Notifications enables desktop notifications. It defaults to true so
	// older files without the field keep the previous behavior; the field is
	// written even when false so turning notifications off survives a restart.
	Notifications bool `json:"notifications"`
	// DockBadge enables the icon badge for conversations waiting for the user.
	// Like Notifications, it defaults to true for older files and is written
	// even when false.
	DockBadge bool `json:"dockBadge"`
	// Fonts scales the four text areas. A missing object keeps the default
	// 100% everywhere.
	Fonts Fonts `json:"fonts"`
	// LastSelections maps an agent ID to the config values and session mode
	// last used with it, so a new draft opens with the same model, thinking
	// level and mode instead of the agent's defaults.
	LastSelections map[string]Selection `json:"lastSelections,omitempty"`
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
	return File{
		Version:       CurrentVersion,
		Notifications: true,
		DockBadge:     true,
		Fonts:         DefaultFonts(),
	}
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
	file.Fonts = file.Fonts.withDefaults()
	file.LastSelections = sanitizeSelections(file.LastSelections)
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
	Appearance          string          `json:"appearance"`
	Theme               string          `json:"theme"`
	// Pointers distinguish an older file without the setting (default on)
	// from an explicit false.
	Notifications *bool `json:"notifications"`
	DockBadge     *bool `json:"dockBadge"`
	// A pointer tells an older file without fonts apart from an explicit
	// object whose fields are still defaults.
	Fonts *Fonts `json:"fonts"`
	// Raw keeps one malformed selection from discarding the other fields.
	LastSelections json.RawMessage `json:"lastSelections"`
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
	file.Appearance = strings.TrimSpace(raw.Appearance)
	file.Theme = strings.TrimSpace(raw.Theme)
	if raw.Notifications != nil {
		file.Notifications = *raw.Notifications
	}
	if raw.DockBadge != nil {
		file.DockBadge = *raw.DockBadge
	}
	if raw.Fonts != nil {
		file.Fonts = raw.Fonts.withDefaults()
	}
	file.LastSelections = decodeSelections(raw.LastSelections, &problems)
	return file, errors.Join(problems...)
}

func decodeSelections(raw json.RawMessage, problems *[]error) map[string]Selection {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var selections map[string]Selection
	if err := json.Unmarshal(raw, &selections); err != nil {
		*problems = append(*problems, fmt.Errorf("settings: ignoring lastSelections: %w", err))
		return nil
	}
	return sanitizeSelections(selections)
}

// sanitizeSelections drops empty agent IDs, option IDs and values so a
// generated or hand-edited file cannot make the model panel submit blanks.
func sanitizeSelections(selections map[string]Selection) map[string]Selection {
	if len(selections) == 0 {
		return nil
	}
	clean := make(map[string]Selection, len(selections))
	for agentID, selection := range selections {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		out := Selection{Mode: strings.TrimSpace(selection.Mode)}
		for id, value := range selection.ConfigOptions {
			id = strings.TrimSpace(id)
			value = strings.TrimSpace(value)
			if id == "" || value == "" {
				continue
			}
			if out.ConfigOptions == nil {
				out.ConfigOptions = make(map[string]string, len(selection.ConfigOptions))
			}
			out.ConfigOptions[id] = value
		}
		if len(out.ConfigOptions) == 0 && out.Mode == "" {
			continue
		}
		clean[agentID] = out
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
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
