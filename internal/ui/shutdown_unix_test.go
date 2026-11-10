//go:build unix

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShutdownKillsProcessGroup(t *testing.T) {
	app, tt := newFakeApp(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	app.Agents[0].Env = append(append([]string(nil), app.Agents[0].Env...),
		"FAKE_AGENT_CHILD_PID_FILE="+pidFile)
	connect(t, app, tt)

	child := waitPID(t, pidFile)
	leader := app.activeChat().Session.PID()
	if !alive(child) || !alive(leader) {
		t.Fatalf("processes not running: leader=%d child=%d", leader, child)
	}

	app.Shutdown()
	waitGone(t, leader)
	waitGone(t, child)
}

func waitPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", path)
	return 0
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d still alive after Shutdown", pid)
}
