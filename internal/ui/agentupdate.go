package ui

import (
	"context"
	"strings"
	"time"

	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/agentupdate"
)

const (
	// agentUpdateTimeout bounds one CLI update command. It is longer than a
	// probe because the CLI may download and replace its own installation.
	agentUpdateTimeout = 3 * time.Minute
	// agentUpdateRetryCheck is how long the post-update refresh waits before
	// probing a second time: an update command can return just before its new
	// shim or binary becomes visible.
	agentUpdateRetryCheck = 3 * time.Second
)

// updateAgent starts one builtin CLI's own update command. It only runs from
// the Update button next to a newer version, and only one update runs at a
// time so two CLIs cannot rewrite the same installation at once.
func (a *App) updateAgent(id string) {
	if a.closed.Load() || a.Resolving {
		return
	}
	idx := a.statusIndex(id)
	if idx < 0 {
		return
	}
	status := a.AgentStatuses[idx]
	if status.Updating || strings.TrimSpace(status.LatestVersion) == "" || a.anyAgentUpdating() {
		return
	}
	builtin, ok := agent.BuiltinByID(id)
	if !ok {
		return
	}
	a.AgentStatuses[idx].Updating = true
	a.AgentStatuses[idx].UpdateError = ""
	a.AgentStatuses[idx].UpdateDone = ""

	ctx := a.lifeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, agentUpdateTimeout)
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		defer cancel()
		a.runUpdate(ctx, builtin)
	}()
}

// runUpdate resolves the CLI path again, runs its update command and, on success,
// refreshes only that agent's status so other agents and open sessions keep
// their state. It runs off the UI thread and posts every result back.
func (a *App) runUpdate(ctx context.Context, builtin agent.BuiltinAgent) {
	env := agent.ResolveEnvironment(ctx, a.resolveOpts)
	command := cliPath(env, builtin.ID)
	if command == "" {
		a.postUpdateFailure(builtin.ID, Tf("%s was not found in the login shell PATH", builtin.Name))
		return
	}
	result, err := a.runAgentUpdate(ctx, env.Env, agentupdate.Target{
		ID:      builtin.ID,
		Name:    builtin.Name,
		Command: command,
	})
	if err != nil {
		a.postUpdateFailure(builtin.ID, agentUpdateError(err, result))
		return
	}
	a.refreshAgent(ctx, builtin.ID)
}

// refreshAgent rebuilds one agent's status after its CLI changed, so the row
// shows the version that is now installed instead of the old one. The next
// update check then either clears the notice or reports that the installed
// version is still older than the registry.
func (a *App) refreshAgent(ctx context.Context, id string) {
	env, target, status, ok := a.resolveAgentStatus(ctx, id)
	if !ok {
		a.postUpdateFailure(id, T("the agent is not in the builtin table"))
		return
	}
	if status.Status != AgentReady {
		select {
		case <-ctx.Done():
		case <-time.After(agentUpdateRetryCheck):
		}
		env, target, status, ok = a.resolveAgentStatus(ctx, id)
		if !ok {
			a.postUpdateFailure(id, T("the agent is not in the builtin table"))
			return
		}
	}

	a.Post(func() {
		if a.closed.Load() {
			return
		}
		idx := a.statusIndex(id)
		if idx < 0 {
			return
		}
		status.Updating = false
		status.LatestVersion = ""
		status.UpdateError = ""
		status.UpdateDone = ""
		if status.Status == AgentReady {
			status.UpdateDone = strings.TrimSpace(status.Version)
		}
		a.AgentStatuses[idx] = status
		a.Env = env
		a.applyAgentSpec(target, status.Status == AgentReady)
	})
	if status.Status == AgentReady {
		a.checkAgentUpdates(ctx, []agent.LaunchResult{target})
	}
}

// resolveAgentStatus re-resolves the login environment and probes one agent.
// It is the single-agent version of what discovery does, so an update shows
// the same status text a fresh start would.
func (a *App) resolveAgentStatus(ctx context.Context, id string) (agent.Environment, agent.LaunchResult, AgentStatus, bool) {
	env := agent.ResolveEnvironment(ctx, a.resolveOpts)
	results := agent.BuiltinLaunchResults(a.Resources, env, a.codexProvider())
	for _, result := range results {
		if result.ID == id {
			return env, result, a.probeResult(ctx, result), true
		}
	}
	return env, agent.LaunchResult{}, AgentStatus{}, false
}

// applyAgentSpec replaces one entry of the connectable agent list, or removes
// it when the refreshed CLI can no longer be started.
func (a *App) applyAgentSpec(result agent.LaunchResult, ready bool) {
	index := -1
	for i, spec := range a.Agents {
		if spec.ID == result.ID {
			index = i
			break
		}
	}
	switch {
	case ready && index >= 0:
		a.Agents[index] = result.Spec
	case ready:
		a.Agents = append(a.Agents, result.Spec)
	case index >= 0:
		a.Agents = append(a.Agents[:index], a.Agents[index+1:]...)
		if a.AgentIndex >= len(a.Agents) {
			a.AgentIndex = 0
		}
	}
}

// postUpdateFailure records the reason under the agent's row and leaves the
// newer-version notice in place, so the same button is a retry.
func (a *App) postUpdateFailure(id, message string) {
	message = truncate(strings.TrimSpace(message), 600)
	a.Post(func() {
		if a.closed.Load() {
			return
		}
		idx := a.statusIndex(id)
		if idx < 0 {
			return
		}
		a.AgentStatuses[idx].Updating = false
		a.AgentStatuses[idx].UpdateError = message
	})
}

// agentUpdateError combines the CLI's exit status with the end of its output,
// which is where the update command puts the reason.
func agentUpdateError(err error, result agentupdate.Result) string {
	message := err.Error()
	if plan := strings.TrimSpace(result.Plan.CommandLine()); plan != "" {
		message = plan + ": " + message
	}
	if output := strings.TrimSpace(result.Output); output != "" {
		message += "\n" + output
	}
	return message
}

// anyAgentUpdating serializes updates: concurrent installs can rewrite the
// same shims and package trees, and the CLIs themselves are not safe to run
// side by side.
func (a *App) anyAgentUpdating() bool {
	for _, status := range a.AgentStatuses {
		if status.Updating {
			return true
		}
	}
	return false
}

func (a *App) statusIndex(id string) int {
	for i := range a.AgentStatuses {
		if a.AgentStatuses[i].ID == id {
			return i
		}
	}
	return -1
}

// cliPath returns the installed CLI a builtin's update applies to. For pi and
// codex the launch command is the vendored adapter, so the CLI path comes from
// the resolved environment, not from the spec.
func cliPath(env agent.Environment, id string) string {
	switch id {
	case agent.PiID:
		return strings.TrimSpace(env.PiPath)
	case agent.CommandCodeID:
		return strings.TrimSpace(env.CommandCodePath)
	case agent.CodexID:
		return strings.TrimSpace(env.CodexPath)
	}
	return ""
}
