//go:build windows

package main

import "syscall"

func windowsHideConsole() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
