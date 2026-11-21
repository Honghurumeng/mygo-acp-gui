package agentupdate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeCLI writes a script that records its arguments and prints output, so a
// test can check exactly what the update action ran.
func fakeCLI(t *testing.T, body string) (cli, logPath string) {
	t.Helper()
	dir := t.TempDir()
	cli = filepath.Join(dir, "pi")
	logPath = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > '" + logPath + "'\n" + body + "\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return cli, logPath
}

func TestPlanUpdateRunsTheCLIsOwnUpdateCommand(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "pi")
	plan, err := PlanUpdate(cli)
	if err != nil {
		t.Fatalf("PlanUpdate: %v", err)
	}
	if plan.Executable != cli {
		t.Errorf("executable = %q, want %q", plan.Executable, cli)
	}
	if got := strings.Join(plan.Args, " "); got != "update" {
		t.Errorf("args = %q, want update", got)
	}
	if got, want := plan.CommandLine(), cli+" update"; got != want {
		t.Errorf("CommandLine = %q, want %q", got, want)
	}
	for _, bad := range []string{"", "  ", "pi", "bin/pi"} {
		if _, err := PlanUpdate(bad); err == nil {
			t.Errorf("PlanUpdate accepted %q", bad)
		}
	}
}

func TestUpdateCapturesOutputAndTheLoginShellEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI uses a shebang")
	}
	cli, logPath := fakeCLI(t, "printf 'updating pi\\nupdate done: %s\\n' \"$HOME\"")
	result, err := (Runner{}).Update(context.Background(), []string{
		"HOME=/tmp/pi-home", "PATH=/usr/bin:/bin",
	}, Target{ID: "pi", Name: "pi", Command: cli})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(result.Output, "update done: /tmp/pi-home") {
		t.Errorf("output = %q, want the inherited HOME", result.Output)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(args); got != "update" {
		t.Errorf("recorded args = %q, want update", got)
	}
}

func TestUpdateKeepsTheTailOfLongOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI uses a shebang")
	}
	cli, _ := fakeCLI(t, "printf 'first\\n'; printf 'last\\n'")
	result, err := (Runner{MaxOutput: 8}).Update(context.Background(), nil, Target{Command: cli})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.Output != "st\nlast" {
		t.Errorf("output = %q, want only the tail", result.Output)
	}
	if len(result.Output) > 8 {
		t.Errorf("output = %q (%d bytes), want at most 8", result.Output, len(result.Output))
	}
}

func TestUpdateReportsFailureAndStopsOnTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI uses a shebang")
	}
	t.Run("exit status", func(t *testing.T) {
		cli, _ := fakeCLI(t, "printf 'update failed: permission denied\\n' >&2; exit 3")
		result, err := (Runner{}).Update(context.Background(), nil, Target{Command: cli})
		if err == nil {
			t.Fatal("Update succeeded, want the exit status")
		}
		if !strings.Contains(result.Output, "permission denied") {
			t.Errorf("output = %q, want stderr", result.Output)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		cli, _ := fakeCLI(t, "exec /bin/sleep 10")
		started := time.Now()
		_, err := (Runner{Timeout: 150 * time.Millisecond}).Update(context.Background(), nil, Target{Command: cli})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("error = %v, want a timeout", err)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Errorf("timeout took %s", elapsed)
		}
	})
}

func TestUpdateRejectsAnEmptyCommand(t *testing.T) {
	_, err := (Runner{}).Update(context.Background(), nil, Target{})
	if err == nil || !strings.Contains(err.Error(), "invalid CLI path") {
		t.Fatalf("error = %v, want an invalid path error", err)
	}
}
