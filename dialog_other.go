//go:build !windows

package main

import (
	"context"
	"errors"
)

func selectDirDialog(context.Context) (string, error) {
	return "", errors.New("当前平台暂不支持系统目录选择窗口")
}
