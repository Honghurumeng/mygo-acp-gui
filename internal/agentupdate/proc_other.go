//go:build !unix

package agentupdate

import (
	"os"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}

func killProcessGroup(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	return proc.Kill()
}
