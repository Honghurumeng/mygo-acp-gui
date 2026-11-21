// Package agentupdate runs a builtin CLI's own update subcommand.
//
// Nothing runs on its own: an update happens only after the user clicks the
// Update button next to a newer version. Each CLI knows how it was installed,
// so the app does not guess a package manager: `pi update`, `command-code
// update` and `codex update` update themselves in place and keep working
// whether they came from npm, pnpm, bun, Homebrew or a standalone installer.
package agentupdate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds one update run. The CLI may download and replace its
// own installation, so the button gets more time than a probe.
const DefaultTimeout = 3 * time.Minute

// defaultOutput caps how much of the CLI's output is kept. The tail is the
// useful part: update commands end with the error that mattered.
const defaultOutput = 8 << 10

// Target is one installed CLI.
type Target struct {
	ID   string
	Name string
	// Command is the absolute path of the installed CLI, as discovery
	// resolved it.
	Command string
}

// Plan is the command an update would run.
type Plan struct {
	Executable string
	Args       []string
}

// CommandLine renders the plan for a tooltip or an error message.
func (p Plan) CommandLine() string {
	parts := make([]string, 0, len(p.Args)+1)
	parts = append(parts, quoteArg(p.Executable))
	for _, arg := range p.Args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\n\"'$`\\") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// PlanUpdate returns the CLI's own update command. An empty or relative path
// is a discovery bug, not something to run.
func PlanUpdate(cli string) (Plan, error) {
	cli = strings.TrimSpace(cli)
	if cli == "" || !filepath.IsAbs(cli) {
		return Plan{}, fmt.Errorf("invalid CLI path %q", cli)
	}
	return Plan{Executable: cli, Args: []string{"update"}}, nil
}

// Result is what an update ran and what it printed.
type Result struct {
	Plan   Plan
	Output string
}

// Update runs the CLI's update subcommand and returns the captured output.
// env is the login-shell environment, which gives the CLI the same PATH,
// HOME and installer hints it would see in a terminal.
func (r Runner) Update(ctx context.Context, env []string, target Target) (Result, error) {
	plan, err := PlanUpdate(target.Command)
	if err != nil {
		return Result{}, err
	}
	result := Result{Plan: plan}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	limit := r.MaxOutput
	if limit <= 0 {
		limit = defaultOutput
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := &tailBuffer{max: limit}
	cmd := exec.CommandContext(runCtx, plan.Executable, plan.Args...)
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	configureProcess(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd.Process) }
	cmd.WaitDelay = 5 * time.Second

	err = cmd.Run()
	result.Output = strings.TrimSpace(out.String())
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("update timed out after %s", timeout)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return result, errors.New("update was cancelled")
		}
		return result, err
	}
	return result, nil
}

// Runner runs one update command. The zero value uses DefaultTimeout and
// keeps the last 8 KiB of output.
type Runner struct {
	Timeout   time.Duration
	MaxOutput int
}

// tailBuffer keeps only the last max bytes written, so a chatty update command
// cannot grow the app's memory or the status tooltip without bound.
type tailBuffer struct {
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.max <= 0 {
		return n, nil
	}
	if n >= b.max {
		b.buf = append(b.buf[:0], p[n-b.max:]...)
		return n, nil
	}
	if overflow := len(b.buf) + n - b.max; overflow > 0 {
		b.buf = b.buf[overflow:]
	}
	b.buf = append(b.buf, p...)
	return n, nil
}

func (b *tailBuffer) String() string {
	return string(b.buf)
}
