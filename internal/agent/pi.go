package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PiID is the stable id of the vendored pi-acp agent.
const PiID = "pi"

// PiLaunchSpec builds the vendored pi launch spec. The bundle is run by
// the absolute Node path and pi-acp finds pi through the resolved PATH.
func PiLaunchSpec(resourcesDir string, env Environment) (LaunchSpec, error) {
	if resourcesDir == "" {
		return LaunchSpec{}, fmt.Errorf("pi: empty resources directory")
	}
	resourcesDir, err := filepath.Abs(resourcesDir)
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("pi: resources: %w", err)
	}
	bundle := filepath.Join(resourcesDir, "agents", "pi-acp", "pi-acp.bundle.mjs")
	info, err := os.Stat(bundle)
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("pi: vendored bundle: %w", err)
	}
	if info.IsDir() {
		return LaunchSpec{}, fmt.Errorf("pi: vendored bundle is a directory: %s", bundle)
	}
	if env.NodePath == "" {
		return LaunchSpec{}, fmt.Errorf("pi: Node.js 22+ was not found in the login shell PATH")
	}
	if env.NodeMajor > 0 && env.NodeMajor < 22 {
		return LaunchSpec{}, fmt.Errorf("pi: Node.js 22+ required, found %s", env.NodeVersion)
	}
	if env.PiPath == "" {
		return LaunchSpec{}, fmt.Errorf("pi: pi was not found in the login shell PATH")
	}

	baseEnv := env.Env
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	spec := LaunchSpec{
		ID:       PiID,
		Name:     "pi",
		Command:  env.NodePath,
		Args:     []string{bundle},
		Env:      ensurePath(baseEnv, dirsOf(env.NodePath, env.PiPath)...),
		Source:   SourceVendored,
		NodePath: env.NodePath,
		PiPath:   env.PiPath,
		Version:  readTrimmed(filepath.Join(resourcesDir, "agents", "pi-acp", "VERSION")),
	}
	if err := spec.Validate(); err != nil {
		return LaunchSpec{}, err
	}
	spec.LoginHint = spec.CommandLine() + " --terminal-login"
	return spec, nil
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
