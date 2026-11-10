//go:build unix

package session

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

func TestCloseKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	s := startFake(t, []string{"FAKE_AGENT_CHILD_PID_FILE=" + pidFile}, Config{})

	child := waitPIDFile(t, pidFile)
	leader := s.PID()
	if !processAlive(child) {
		t.Fatalf("fake agent child %d is not running", child)
	}
	if !processAlive(leader) {
		t.Fatalf("agent leader %d is not running", leader)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitDead(t, leader)
	waitDead(t, child)
	waitGroupDead(t, leader)
}

func waitPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("child pid file %s: contents %q", path, b)
	return 0
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive after Close", pid)
}

func waitGroupDead(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process group %d is still alive after Close", pgid)
}
