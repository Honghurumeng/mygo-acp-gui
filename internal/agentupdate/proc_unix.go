//go:build unix

package agentupdate

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcess puts the package manager in its own process group, so a
// timeout can stop the tar/git children an install spawns as one tree.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	if err := syscall.Kill(-proc.Pid, syscall.SIGKILL); err != nil {
		return proc.Kill()
	}
	return nil
}
