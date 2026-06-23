package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type selectDirResponse struct {
	Path     string `json:"path"`
	Canceled bool   `json:"canceled"`
}

func Test选择目录接口成功返回规范化路径(t *testing.T) {
	dir := t.TempDir()
	state := newAppState()
	state.selectDir = func(context.Context) (string, error) {
		return dir, nil
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/select-dir", nil)
	state.handleSelectDir(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d，响应：%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response selectDirResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}

	expected, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("计算期望路径失败：%v", err)
	}
	if response.Path != expected {
		t.Fatalf("path = %q，期望 %q", response.Path, expected)
	}
	if response.Canceled {
		t.Fatal("canceled = true，期望 false")
	}
}

func Test选择目录接口取消选择不会报错(t *testing.T) {
	state := newAppState()
	state.selectDir = func(context.Context) (string, error) {
		return "", errDirSelectionCanceled
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/select-dir", nil)
	state.handleSelectDir(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d，响应：%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response selectDirResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if response.Path != "" {
		t.Fatalf("path = %q，期望空字符串", response.Path)
	}
	if !response.Canceled {
		t.Fatal("canceled = false，期望 true")
	}
}

func Test选择目录接口返回选择器错误(t *testing.T) {
	state := newAppState()
	state.selectDir = func(context.Context) (string, error) {
		return "", errors.New("窗口创建失败")
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/select-dir", nil)
	state.handleSelectDir(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 %d，响应：%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "打开目录选择窗口失败") {
		t.Fatalf("错误响应未包含期望提示：%s", recorder.Body.String())
	}
}

func Test选择目录后可以继续添加目录(t *testing.T) {
	chosenDir := t.TempDir()
	state := newAppState()
	state.selectDir = func(context.Context) (string, error) {
		return chosenDir, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/select-dir", state.handleSelectDir)
	mux.HandleFunc("POST /api/dirs", state.handleAddDir)
	server := httptest.NewServer(mux)
	defer server.Close()

	selectResponse, err := http.Post(server.URL+"/api/select-dir", "application/json", nil)
	if err != nil {
		t.Fatalf("请求选择目录接口失败：%v", err)
	}
	defer selectResponse.Body.Close()
	if selectResponse.StatusCode != http.StatusOK {
		t.Fatalf("选择目录状态码 = %d，期望 %d", selectResponse.StatusCode, http.StatusOK)
	}

	var selected selectDirResponse
	if err := json.NewDecoder(selectResponse.Body).Decode(&selected); err != nil {
		t.Fatalf("解析选择目录响应失败：%v", err)
	}

	body, err := json.Marshal(addDirRequest{Path: selected.Path})
	if err != nil {
		t.Fatalf("序列化添加目录请求失败：%v", err)
	}
	addResponse, err := http.Post(server.URL+"/api/dirs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("请求添加目录接口失败：%v", err)
	}
	defer addResponse.Body.Close()
	if addResponse.StatusCode != http.StatusCreated {
		t.Fatalf("添加目录状态码 = %d，期望 %d", addResponse.StatusCode, http.StatusCreated)
	}

	var added struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.NewDecoder(addResponse.Body).Decode(&added); err != nil {
		t.Fatalf("解析添加目录响应失败：%v", err)
	}
	if len(added.Dirs) != 1 || added.Dirs[0] != selected.Path {
		t.Fatalf("目录列表 = %#v，期望只包含 %q", added.Dirs, selected.Path)
	}
}
