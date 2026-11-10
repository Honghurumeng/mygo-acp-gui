//go:build unix

package session

import (
	"os/exec"
	"syscall"
)

// configureProcess puts the agent in its own process group so that it and
// every child it spawns (pi, tools, login wrappers) can be cleaned up as
// one tree.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(pid int, force bool) error {
	if pid <= 0 {
		return nil
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-pid, sig)
}
