package agent

import (
	"fmt"
	"os"
)

// CommandCodeID is the stable id of the Command Code CLI agent.
const CommandCodeID = "command-code"

// CommandCodeLaunchSpec builds the launch spec for the user's installed
// Command Code CLI. command-code is a Node CLI (requires Node 22+) and has
// no vendored resources in this app.
func CommandCodeLaunchSpec(env Environment) (LaunchSpec, error) {
	if env.CommandCodePath == "" {
		return LaunchSpec{}, fmt.Errorf("command-code was not found in the login shell PATH")
	}
	if env.NodePath == "" {
		return LaunchSpec{}, fmt.Errorf("command-code needs Node.js 22+, but node was not found in the login shell PATH")
	}
	if env.NodeMajor > 0 && env.NodeMajor < 22 {
		return LaunchSpec{}, fmt.Errorf("command-code needs Node.js 22+, found %s", env.NodeVersion)
	}
	baseEnv := env.Env
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	spec := LaunchSpec{
		ID:        CommandCodeID,
		Name:      "Command Code",
		Command:   env.CommandCodePath,
		Args:      []string{"acp"},
		Env:       ensurePath(baseEnv, dirsOf(env.CommandCodePath)...),
		Source:    SourceBuiltin,
		Version:   env.CommandCodeVersion,
		LoginHint: "command-code login",
	}
	if err := spec.Validate(); err != nil {
		return LaunchSpec{}, err
	}
	return spec, nil
}
