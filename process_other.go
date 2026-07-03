//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func windowsHideConsole() *syscall.SysProcAttr {
	return nil
}

var killProcessTree = defaultKillProcessTree

func defaultKillProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}

	err := cmd.Process.Kill()
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
