//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func windowsHideConsole() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

var killProcessTree = defaultKillProcessTree

func defaultKillProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}

	pid := cmd.Process.Pid
	taskkill := exec.Command("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(pid))
	taskkill.SysProcAttr = windowsHideConsole()
	output, err := taskkill.CombinedOutput()
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}

	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("taskkill PID %d: %w", pid, err)
	}
	return fmt.Errorf("taskkill PID %d: %w: %s", pid, err, message)
}
