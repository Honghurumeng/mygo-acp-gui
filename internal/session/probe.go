package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

// ModelProbe configures the optional model sweep of ProbeConfigModels.
type ModelProbe struct {
	// Known lists model values whose config options are already cached. They
	// are not switch-tested again, which makes a later startup compare the
	// model list against the cache and stop when nothing is new.
	Known map[string]bool
	// Ready receives the session/new result before the sweep starts, so the
	// caller can show the model list while the other models are still being
	// read. It runs on the probe goroutine.
	Ready func(info Info)
	// Report receives the config options the agent returned after switching
	// to a model. It runs on the probe goroutine, once per new model, before
	// the probe returns.
	Report func(model string, options []acp.ConfigOption)
}

// Probe starts the agent, performs initialize only, and shuts it down. It
// is the discovery check: a valid ACP agent is one that answers initialize.
// The caller controls the timeout through ctx.
func Probe(ctx context.Context, launch agent.LaunchSpec) (Info, error) {
	return probe(ctx, launch, true, nil)
}

// ProbeConfig is Probe plus a throwaway session/new so callers can read the
// agent's config options (provider, model, thinking) before the first real
// connect. A failing session/new is not an error: agents that need a login
// still probe cleanly, they just report no options. The caller controls the
// timeout of both round trips through ctx.
func ProbeConfig(ctx context.Context, launch agent.LaunchSpec) (Info, error) {
	return probe(ctx, launch, false, nil)
}

// ProbeConfigModels is ProbeConfig plus a sweep of the models that are not in
// probe.Known. ACP reports reasoning levels only for a session's current
// model, and switching models through session/set_config_option is the only
// way to read another model's list, so the same throwaway session walks the
// model select and reports each answer. A failing switch is skipped: the next
// startup retries that model instead of failing discovery.
func ProbeConfigModels(ctx context.Context, launch agent.LaunchSpec, probeModel ModelProbe) (Info, error) {
	return probe(ctx, launch, false, &probeModel)
}

// sessionNewProbeTimeout bounds the throwaway session/new so a stuck agent
// cannot eat the whole probe budget. Model switches have their own bound.
const sessionNewProbeTimeout = 12 * time.Second

// modelProbeTimeout bounds one model switch. A slow switch only loses that
// model: the sweep moves on and the next startup retries it.
const modelProbeTimeout = 5 * time.Second

func probe(ctx context.Context, launch agent.LaunchSpec, initializeOnly bool, models *ModelProbe) (Info, error) {
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
	if !initializeOnly {
		// Best effort: the session is only used to read configOptions.
		newCtx, cancelNew := context.WithTimeout(ctx, sessionNewProbeTimeout)
		state, sessionErr := s.NewSession(newCtx, dir, []any{})
		cancelNew()
		if sessionErr == nil {
			info = state
			if models != nil {
				probeModelConfigs(ctx, s, info, *models)
			}
		}
	}
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

// probeModelConfigs switches the throwaway session through every model that
// the cache does not know yet, reporting each model's config options. The
// sweep stops at the caller's deadline; models it did not reach are simply
// still missing from the cache and are retried on the next startup.
func probeModelConfigs(ctx context.Context, s *Session, info Info, probeModel ModelProbe) {
	if probeModel.Ready != nil {
		probeModel.Ready(info)
	}
	model := acp.ModelOption(info.ConfigOptions)
	if model == nil || probeModel.Report == nil {
		return
	}
	for _, choice := range model.Options {
		if choice.Value == "" || probeModel.Known[choice.Value] {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, modelProbeTimeout)
		options, err := s.SetConfigOption(callCtx, model.ID, choice.Value)
		cancel()
		if err != nil || len(options) == 0 {
			continue
		}
		probeModel.Report(choice.Value, options)
	}
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
