package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// CodexID is the stable id of the vendored codex-acp adapter.
const CodexID = "codex"

// CodexLaunchSpec builds the launch spec for the official codex-acp adapter.
// The adapter runs on the resolved Node path, and CODEX_PATH points it at the
// user's own Codex CLI. As with pi, the app never runs npx or installs the
// upstream agent.
func CodexLaunchSpec(resourcesDir string, env Environment) (LaunchSpec, error) {
	if resourcesDir == "" {
		return LaunchSpec{}, fmt.Errorf("codex: empty resources directory")
	}
	resourcesDir, err := filepath.Abs(resourcesDir)
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("codex: resources: %w", err)
	}
	bundle := filepath.Join(resourcesDir, "agents", "codex-acp", "codex-acp.bundle.mjs")
	info, err := os.Stat(bundle)
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("codex: vendored bundle: %w", err)
	}
	if info.IsDir() {
		return LaunchSpec{}, fmt.Errorf("codex: vendored bundle is a directory: %s", bundle)
	}
	if env.NodePath == "" {
		return LaunchSpec{}, fmt.Errorf("codex: Node.js 22+ was not found in the login shell PATH")
	}
	if env.NodeMajor > 0 && env.NodeMajor < 22 {
		return LaunchSpec{}, fmt.Errorf("codex: Node.js 22+ required, found %s", env.NodeVersion)
	}
	if env.CodexPath == "" {
		return LaunchSpec{}, fmt.Errorf("codex: the Codex CLI was not found in the login shell PATH")
	}

	baseEnv := env.Env
	if baseEnv == nil {
		baseEnv = os.Environ()
	}
	// The vendored bundle deliberately does not ship @openai/codex or its
	// platform binaries. CODEX_PATH makes the adapter spawn the user's
	// installed `codex app-server` instead of its missing npm dependency.
	childEnv := setEnv(ensurePath(baseEnv, dirsOf(env.NodePath, env.CodexPath)...), "CODEX_PATH", env.CodexPath)
	spec := LaunchSpec{
		ID:       CodexID,
		Name:     "Codex",
		Command:  env.NodePath,
		Args:     []string{bundle},
		Env:      childEnv,
		Source:   SourceVendored,
		NodePath: env.NodePath,
		// The UI shows the installed Codex CLI version, not the adapter
		// version baked into the bundle.
		Version:   env.CodexVersion,
		LoginHint: "codex login",
	}
	if err := spec.Validate(); err != nil {
		return LaunchSpec{}, err
	}
	return spec, nil
}
