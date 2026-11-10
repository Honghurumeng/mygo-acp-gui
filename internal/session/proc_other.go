//go:build !unix && !windows

package session

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}

func signalGroup(pid int, force bool) error {
	if pid <= 0 {
		return nil
	}
	return nil
}
