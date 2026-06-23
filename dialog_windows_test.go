//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWindows目录选择脚本使用资源管理器样式弹窗(t *testing.T) {
	script := windowsOpenFolderDialogScript()

	checks := []string{
		"FileOpenDialog",
		"FOS_PICKFOLDERS",
		`dialog.SetTitle("Open Folder")`,
		`dialog.SetOkButtonLabel("Select folder")`,
		`dialog.SetFileNameLabel("文件夹:")`,
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("目录选择脚本缺少 %q", check)
		}
	}
}

func TestWindows目录选择脚本中的CSharp代码可编译(t *testing.T) {
	source, ok := extractPickerCSharpSource(windowsOpenFolderDialogScript())
	if !ok {
		t.Fatal("没有从目录选择脚本中提取到 C# 代码")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", "Add-Type -TypeDefinition $env:PICKER_CSHARP")
	cmd.Env = append(os.Environ(), "PICKER_CSHARP="+source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("C# 目录选择代码编译失败：%v\n%s", err, output)
	}
}

func extractPickerCSharpSource(script string) (string, bool) {
	startMarker := "Add-Type -TypeDefinition @\""
	start := strings.Index(script, startMarker)
	if start == -1 {
		return "", false
	}
	start += len(startMarker)

	end := strings.Index(script[start:], "\"@")
	if end == -1 {
		return "", false
	}
	return script[start : start+end], true
}
