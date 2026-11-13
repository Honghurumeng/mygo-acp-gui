// Package modelconfig persists the ACP config options an agent reports for
// each model.
//
// ACP only reveals reasoning levels for a session's current model:
// session/new returns the current model's thought_level, and
// session/set_config_option with another model returns that model's list.
// There is no call that lists every model's levels, so the app sweeps the
// model list once, stores each answer here, and later startups only probe the
// models that are missing from this cache.
package modelconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/reed/acp-gui/internal/acp"
)

// CurrentVersion is the schema version this build writes.
const CurrentVersion = 1

// Model is the config option list an agent returned right after switching to
// one model. It includes the model select itself, so rendering a draft for
// this model needs no merging heuristics.
type Model struct {
	ConfigOptions []acp.ConfigOption `json:"configOptions"`
	// ProbedAt records when the sweep ran. It is informational today; it
	// leaves room for a future TTL without another schema change.
	ProbedAt string `json:"probedAt,omitempty"`
}

// File is the on-disk cache: agent ID -> model value -> options.
type File struct {
	Version int                         `json:"version"`
	Agents  map[string]map[string]Model `json:"agents"`
}

// Store reads and writes one cache file. A nil Store or an empty path is a
// no-op, which keeps tests and portable runs free of side effects.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open returns a store for path. The file is not read until Load.
func Open(path string) *Store {
	return &Store{path: strings.TrimSpace(path)}
}

// Path is the cache file location. It is empty when persistence is off.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the cache. A missing file is not an error: it returns an empty
// cache. A corrupt file also returns an empty cache plus an error, so the
// caller can start over instead of failing startup.
func (s *Store) Load() (File, error) {
	file := File{Version: CurrentVersion, Agents: map[string]map[string]Model{}}
	if s == nil || s.path == "" {
		return file, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return file, nil
		}
		return file, fmt.Errorf("model config: read %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return File{Version: CurrentVersion, Agents: map[string]map[string]Model{}},
			fmt.Errorf("model config: invalid JSON: %w", err)
	}
	if file.Agents == nil {
		file.Agents = map[string]map[string]Model{}
	}
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	for agentID, models := range file.Agents {
		if agentID == "" || len(models) == 0 {
			delete(file.Agents, agentID)
			continue
		}
		for model, entry := range models {
			if model == "" || len(entry.ConfigOptions) == 0 {
				delete(models, model)
			}
		}
		if len(models) == 0 {
			delete(file.Agents, agentID)
		}
	}
	return file, nil
}

// Save writes the cache atomically. Parent directories are created as needed
// and the file is mode 0600 because it records the user's model choices.
func (s *Store) Save(file File) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if file.Version <= 0 {
		file.Version = CurrentVersion
	}
	if file.Agents == nil {
		file.Agents = map[string]map[string]Model{}
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("model config: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("model config: create data directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".model-config-*.json")
	if err != nil {
		return fmt.Errorf("model config: create temporary file: %w", err)
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("model config: write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("model config: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("model config: close temporary file: %w", err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("model config: chmod temporary file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("model config: replace %s: %w", s.path, err)
	}
	return nil
}
