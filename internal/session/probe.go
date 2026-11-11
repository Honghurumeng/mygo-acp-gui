package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

// Probe starts the agent, performs initialize only, and shuts it down. It
// is the discovery check: a valid ACP agent is one that answers initialize.
// The caller controls the timeout through ctx.
func Probe(ctx context.Context, launch agent.LaunchSpec) (Info, error) {
	dir, err := os.MkdirTemp("", "acp-gui-probe-*")
	if err != nil {
		return Info{}, fmt.Errorf("probe: workspace: %w", err)
	}
	defer os.RemoveAll(dir)

	var (
		protocolMu  sync.Mutex
		protocolErr error
	)
	s, err := Start(ctx, Config{
		Launch:         launch,
		Workspace:      dir,
		InitializeOnly: true,
		DeferUpdates:   true,
		OnProtocolError: func(err error) {
			protocolMu.Lock()
			if protocolErr == nil {
				protocolErr = err
			}
			protocolMu.Unlock()
		},
	})
	if err != nil {
		protocolMu.Lock()
		clean := protocolErr
		protocolMu.Unlock()
		if clean != nil {
			return Info{}, fmt.Errorf("probe: stdout is not clean ACP JSON: %w (startup: %v)", clean, err)
		}
		return Info{}, err
	}
	info := s.Info()
	closeErr := s.Close()
	protocolMu.Lock()
	clean := protocolErr
	protocolMu.Unlock()
	if clean != nil {
		return Info{}, fmt.Errorf("probe: stdout is not clean ACP JSON: %w", clean)
	}
	if closeErr != nil {
		return Info{}, closeErr
	}
	return info, nil
}

// ListSessions starts a short-lived agent process and asks it for saved
// sessions. cwd filters to one workspace; empty lists all workspaces.
func ListSessions(ctx context.Context, launch agent.LaunchSpec, cwd string) ([]acp.SessionSummary, error) {
	return ListSessionsIn(ctx, launch, cwd, cwd)
}

// ListSessionsIn is ListSessions with a separate process directory. Some
// agents report the process cwd on sessions they have no stored cwd for, so
// the UI passes its current workspace while still asking for all sessions.
func ListSessionsIn(ctx context.Context, launch agent.LaunchSpec, dir, cwd string) ([]acp.SessionSummary, error) {
	filter := ""
	if filepath.IsAbs(cwd) {
		filter = filepath.Clean(cwd)
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filter
	} else {
		dir = filepath.Clean(dir)
	}
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		tmp, err := os.MkdirTemp("", "acp-gui-list-*")
		if err != nil {
			return nil, fmt.Errorf("list sessions: workspace: %w", err)
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}

	s, err := Start(ctx, Config{
		Launch:         launch,
		Workspace:      dir,
		InitializeOnly: true,
		DeferUpdates:   true,
	})
	if err != nil {
		return nil, err
	}
	sessions, listErr := s.ListSessions(ctx, filter)
	closeErr := s.Close()
	if listErr != nil {
		return nil, listErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return sessions, nil
}
