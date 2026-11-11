package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultShellTimeout = 5 * time.Second
	defaultToolTimeout  = 3 * time.Second
)

// ResolveOptions configures login-shell and explicit path resolution.
type ResolveOptions struct {
	// Shell is the login shell to ask, usually $SHELL. Empty picks zsh,
	// bash or sh.
	Shell string
	// NodePath, PiPath and CommandCodePath override the shell's answers.
	// They must be absolute paths.
	NodePath        string
	PiPath          string
	CommandCodePath string
	// Env is the base environment. Nil inherits the app's environment.
	Env []string
	// Timeout bounds each shell call. Zero uses 5 seconds.
	Timeout time.Duration
}

// Environment is the resolved login environment plus the tools the builtin
// launch specs need.
type Environment struct {
	Shell              string
	Env                []string
	Path               string
	NodePath           string
	PiPath             string
	CommandCodePath    string
	NodeVersion        string
	PiVersion          string
	CommandCodeVersion string
	NodeMajor          int
	Warnings           []string
}

// Summary is a short line for the UI.
func (e Environment) Summary() string {
	if e.NodePath == "" && e.PiPath == "" && e.NodeVersion == "" && e.PiVersion == "" &&
		e.CommandCodePath == "" && e.CommandCodeVersion == "" {
		return ""
	}
	var parts []string
	if e.NodePath == "" {
		parts = append(parts, "Node not found")
	} else if e.NodeVersion != "" {
		parts = append(parts, "Node "+e.NodeVersion)
	} else {
		parts = append(parts, "Node "+e.NodePath)
	}
	if e.PiPath == "" {
		parts = append(parts, "pi not found")
	} else if e.PiVersion != "" {
		parts = append(parts, "pi "+e.PiVersion)
	} else {
		parts = append(parts, "pi "+e.PiPath)
	}
	if e.CommandCodePath != "" {
		if e.CommandCodeVersion != "" {
			parts = append(parts, "command-code "+e.CommandCodeVersion)
		} else {
			parts = append(parts, "command-code "+e.CommandCodePath)
		}
	}
	return strings.Join(parts, " · ")
}

// ResolveEnvironment asks the user's login shell for its environment and
// for node/pi/command-code, so a Finder-launched app sees the same PATH as
// a terminal. If PATH comes only from an interactive rc file (common with
// nvm and custom npm prefixes), it retries with an interactive login shell,
// then checks common install locations. Explicit absolute overrides win.
func ResolveEnvironment(ctx context.Context, opts ResolveOptions) Environment {
	base := append([]string(nil), opts.Env...)
	if base == nil {
		base = os.Environ()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}

	env := Environment{Env: base}
	if runtime.GOOS == "windows" {
		env.Path = envValue(base, "PATH")
		env.NodePath = findExecutable("node", env.Path)
		env.PiPath = findExecutable("pi", env.Path)
		env.CommandCodePath = findExecutable("command-code", env.Path)
	} else {
		shell, warning := resolveShell(opts.Shell)
		env.Shell = shell
		if warning != "" {
			env.Warnings = append(env.Warnings, warning)
		}
		if shell != "" {
			result := shellEnvDump(ctx, shell, base, timeout, "-lc")
			if result.node == "" || result.pi == "" || result.commandCode == "" {
				// Some PATHs live only in .zshrc/.bashrc, which a
				// non-interactive login shell does not source.
				alt := shellEnvDump(ctx, shell, base, timeout, "-ilc")
				useAlternative := (result.node == "" && alt.node != "") ||
					(result.pi == "" && alt.pi != "") ||
					(result.commandCode == "" && alt.commandCode != "")
				if useAlternative {
					if result.node == "" {
						result.node = alt.node
					}
					if result.pi == "" {
						result.pi = alt.pi
					}
					if result.commandCode == "" {
						result.commandCode = alt.commandCode
					}
					if alt.env != nil {
						result.env = alt.env
					}
					result.warning = alt.warning
				}
			}
			if result.env != nil {
				env.Env = result.env
			}
			if result.warning != "" {
				env.Warnings = append(env.Warnings, result.warning)
			}
			env.NodePath = result.node
			env.PiPath = result.pi
			env.CommandCodePath = result.commandCode
		}
		env.Path = envValue(env.Env, "PATH")
		if env.NodePath == "" {
			env.NodePath = fallbackTool(ctx, "node", env.Env)
		}
		if env.PiPath == "" {
			env.PiPath = fallbackTool(ctx, "pi", env.Env)
		}
		if env.CommandCodePath == "" {
			env.CommandCodePath = fallbackTool(ctx, "command-code", env.Env)
		}
	}

	if opts.NodePath != "" {
		path, err := absoluteExecutable(opts.NodePath, "node")
		if err != nil {
			warning := err.Error()
			if env.NodePath != "" {
				warning += "; using " + env.NodePath
			}
			env.Warnings = append(env.Warnings, warning)
		} else {
			env.NodePath = path
		}
	}
	if opts.PiPath != "" {
		path, err := absoluteExecutable(opts.PiPath, "pi")
		if err != nil {
			warning := err.Error()
			if env.PiPath != "" {
				warning += "; using " + env.PiPath
			}
			env.Warnings = append(env.Warnings, warning)
		} else {
			env.PiPath = path
		}
	}
	if opts.CommandCodePath != "" {
		path, err := absoluteExecutable(opts.CommandCodePath, "command-code")
		if err != nil {
			warning := err.Error()
			if env.CommandCodePath != "" {
				warning += "; using " + env.CommandCodePath
			}
			env.Warnings = append(env.Warnings, warning)
		} else {
			env.CommandCodePath = path
		}
	}

	env.Env = ensurePath(env.Env, dirsOf(env.NodePath, env.PiPath, env.CommandCodePath)...)
	env.Path = envValue(env.Env, "PATH")

	if env.NodePath != "" {
		version, err := toolVersion(ctx, env.NodePath, env.Env)
		if err != nil {
			env.Warnings = append(env.Warnings, "node --version: "+err.Error())
		} else {
			env.NodeVersion = version
			env.NodeMajor = majorVersion(version)
		}
	}
	if env.PiPath != "" {
		version, err := toolVersion(ctx, env.PiPath, env.Env)
		if err != nil {
			env.Warnings = append(env.Warnings, "pi --version: "+err.Error())
		} else {
			env.PiVersion = version
		}
	}
	if env.CommandCodePath != "" {
		version, err := toolVersion(ctx, env.CommandCodePath, env.Env)
		if err != nil {
			env.Warnings = append(env.Warnings, "command-code --version: "+err.Error())
		} else {
			env.CommandCodeVersion = version
		}
	}
	return env
}

func resolveShell(requested string) (shell, warning string) {
	shell = requested
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		for _, candidate := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
			if fileExists(candidate) {
				shell = candidate
				break
			}
		}
	}
	if shell == "" {
		return "", "no login shell found"
	}
	if !filepath.IsAbs(shell) {
		if path, err := exec.LookPath(shell); err == nil {
			shell = path
		} else {
			return shell, fmt.Sprintf("login shell %q: %v", shell, err)
		}
	}
	if err := checkExecutable(shell); err != nil {
		return shell, "login shell: " + err.Error()
	}
	return shell, ""
}

type shellDump struct {
	env         []string
	node        string
	pi          string
	commandCode string
	warning     string
}

// shellEnvDump dumps the shell's environment as NUL-separated entries, then
// asks command -v where node, pi and command-code are. The dump goes to a
// temporary file so that noisy shell startup scripts cannot corrupt it.
func shellEnvDump(ctx context.Context, shell string, base []string, timeout time.Duration, flags ...string) shellDump {
	f, err := os.CreateTemp("", "acp-gui-env-*")
	if err != nil {
		return shellDump{warning: "environment: " + err.Error()}
	}
	name := f.Name()
	_ = f.Close()
	defer os.Remove(name)

	script := `env -0 > "$ACP_GUI_ENV_FILE"
printf '\001NODE=%s\0' "$(command -v node 2>/dev/null)" >> "$ACP_GUI_ENV_FILE"
printf '\001PI=%s\0' "$(command -v pi 2>/dev/null)" >> "$ACP_GUI_ENV_FILE"
printf '\001COMMAND_CODE=%s\0' "$(command -v command-code 2>/dev/null)" >> "$ACP_GUI_ENV_FILE"
`
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append(append([]string(nil), flags...), script)
	cmd := exec.CommandContext(runCtx, shell, args...)
	cmd.Env = append(append([]string(nil), base...), "ACP_GUI_ENV_FILE="+name)
	runErr := cmd.Run()

	data, readErr := os.ReadFile(name)
	if readErr != nil || len(data) == 0 {
		msg := "login shell produced no environment"
		if runErr != nil {
			msg += ": " + runErr.Error()
		}
		return shellDump{warning: msg}
	}
	env, node, pi, commandCode := parseEnvDump(data)
	result := shellDump{env: env, node: node, pi: pi, commandCode: commandCode}
	if runCtx.Err() != nil {
		result.warning = "login shell timed out"
	}
	if len(env) == 0 {
		result.warning = "login shell produced no environment entries"
	}
	return result
}

// parseEnvDump reads the NUL-separated output. Entries starting with 0x01
// are our markers; everything else is an environment entry. Shell startup
// noise before the first NUL would be ignored safely.
func parseEnvDump(data []byte) (env []string, node, pi, commandCode string) {
	const marker = 0x01
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		if entry[0] == marker {
			key, value, ok := bytes.Cut(entry[1:], []byte("="))
			if !ok {
				continue
			}
			switch string(key) {
			case "NODE":
				node = string(value)
			case "PI":
				pi = string(value)
			case "COMMAND_CODE":
				commandCode = string(value)
			}
			continue
		}
		if bytes.IndexByte(entry, '=') <= 0 {
			continue
		}
		text := string(entry)
		if strings.HasPrefix(text, "ACP_GUI_ENV_FILE=") {
			continue
		}
		env = append(env, text)
	}
	return env, absoluteOrEmpty(node), absoluteOrEmpty(pi), absoluteOrEmpty(commandCode)
}

func absoluteOrEmpty(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	return filepath.Clean(path)
}

func absoluteExecutable(path, name string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s path must be absolute: %q", name, path)
	}
	if err := checkExecutable(path); err != nil {
		return "", fmt.Errorf("%s path: %w", name, err)
	}
	return filepath.Clean(path), nil
}

func toolVersion(ctx context.Context, path string, env []string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, "--version")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "", errors.New("empty version output")
	}
	return line, nil
}

func majorVersion(version string) int {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if i := strings.IndexAny(version, ".-+ "); i >= 0 {
		version = version[:i]
	}
	n, _ := strconv.Atoi(version)
	return n
}

// fallbackTool looks in the shell PATH, npm's global bin and the locations
// package managers commonly use when their PATH setup is only interactive.
func fallbackTool(ctx context.Context, name string, env []string) string {
	if path := findExecutable(name, envValue(env, "PATH")); path != "" {
		return path
	}
	home := envValue(env, "HOME")
	var dirs []string
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, "Library", "pnpm"),
			filepath.Join(home, ".local", "share", "pnpm"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".asdf", "shims"),
		)
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	if name == "pi" || name == "command-code" {
		if bin := npmGlobalBin(ctx, env); bin != "" {
			dirs = append([]string{bin}, dirs...)
		}
	}
	for _, dir := range dirs {
		for _, candidate := range executableNames(dir, name) {
			if err := checkExecutable(candidate); err == nil {
				return filepath.Clean(candidate)
			}
		}
	}
	return ""
}

func npmGlobalBin(ctx context.Context, env []string) string {
	npm := findExecutable("npm", envValue(env, "PATH"))
	if npm == "" {
		return ""
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, npm, "prefix", "-g")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	prefix := strings.TrimSpace(string(out))
	if prefix == "" || !filepath.IsAbs(prefix) {
		return ""
	}
	if runtime.GOOS == "windows" {
		return prefix
	}
	return filepath.Join(prefix, "bin")
}

func executableNames(dir, name string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, name)}
	}
	return []string{
		filepath.Join(dir, name+".exe"),
		filepath.Join(dir, name+".cmd"),
		filepath.Join(dir, name+".bat"),
		filepath.Join(dir, name),
	}
}

func findExecutable(name, path string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, candidate := range executableNames(dir, name) {
			if err := checkExecutable(candidate); err == nil {
				return filepath.Clean(candidate)
			}
		}
	}
	return ""
}

func envValue(env []string, key string) string {
	prefix := key + "="
	value := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = entry[len(prefix):]
		}
	}
	return value
}

// ensurePath prepends dirs that are not already in PATH, preserving all
// other environment entries. dirsOf skips empty entries.
func ensurePath(env []string, dirs ...string) []string {
	path := envValue(env, "PATH")
	current := filepath.SplitList(path)
	seen := make(map[string]bool, len(current)+len(dirs))
	for _, dir := range current {
		seen[dir] = true
	}
	var add []string
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		add = append(add, dir)
	}
	if len(add) == 0 {
		return env
	}
	if len(current) > 0 {
		add = append(add, current...)
	}
	return setEnv(env, "PATH", strings.Join(add, string(os.PathListSeparator)))
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

func dirsOf(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			out = append(out, filepath.Dir(path))
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
