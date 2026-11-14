// Package agent resolves the executables and launch descriptions of the
// agents this app can start.
//
// The GUI starts vendored pi-acp / codex-acp itself, with an absolute Node path. It
// never relies on the PATH a Finder-launched app inherits, and never runs
// npm/npx at startup.
package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Source says where a launch spec came from.
type Source string

const (
	// SourceVendored is a bundle shipped inside the app, such as pi-acp or codex-acp.
	SourceVendored Source = "vendored"
	// SourceBuiltin is a hand-maintained app agent.
	SourceBuiltin Source = "builtin"
	// SourceUser is an agent the user added.
	SourceUser Source = "user"
)

// LaunchSpec is everything needed to spawn an ACP agent.
//
// Command is always an absolute path. Env, when non-nil, is the complete
// environment for the child, usually the user's login-shell environment.
type LaunchSpec struct {
	ID      string
	Name    string
	Command string
	Args    []string
	Env     []string
	Source  Source

	// NodePath and PiPath are informational for vendored agents. They are
	// also used by the UI to explain what was resolved.
	NodePath string
	PiPath   string
	Version  string

	// LoginHint is a copy-paste command for the user's terminal when the
	// agent reports that authentication is required. The app never runs it.
	LoginHint string
}

// Validate checks the fields required to start the process.
func (s LaunchSpec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("agent: empty id")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("agent %s: empty name", s.ID)
	}
	if !filepath.IsAbs(s.Command) {
		return fmt.Errorf("agent %s: command must be absolute: %q", s.ID, s.Command)
	}
	if err := checkExecutable(s.Command); err != nil {
		return fmt.Errorf("agent %s: %w", s.ID, err)
	}
	for _, arg := range s.Args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("agent %s: argument contains NUL", s.ID)
		}
	}
	return nil
}

// CommandLine renders the launch for diagnostics and login hints.
func (s LaunchSpec) CommandLine() string {
	parts := make([]string, 0, len(s.Args)+1)
	parts = append(parts, quoteArg(s.Command))
	for _, arg := range s.Args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ValidateWorkspace checks and canonicalizes a workspace directory. ACP's
// session/new requires an absolute cwd, and the agent needs to be able to
// write there, so both are checked before a process is spawned.
func ValidateWorkspace(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("workspace is empty")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("workspace must be an absolute path: %q", path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", path)
	}

	// Creating a temporary file catches the common read-only/permission
	// case without running a platform-specific access check.
	f, err := os.CreateTemp(path, ".acp-gui-write-check-*")
	if err != nil {
		return "", fmt.Errorf("workspace is not writable: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("workspace: remove write check: %w", err)
	}
	return path, nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("executable is a directory: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("not executable: %s", path)
	}
	return nil
}
