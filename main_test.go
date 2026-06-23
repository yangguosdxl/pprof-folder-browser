package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type selectDirResponse struct {
	Path     string `json:"path"`
	Canceled bool   `json:"canceled"`
}

type clearSessionsResponse struct {
	Cleared int `json:"cleared"`
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

func Test清除所有Pprof会话会取消进程并清空列表(t *testing.T) {
	state := newAppState()
	canceled := 0
	state.sessions["one"] = &pprofSession{
		ID:     "one",
		Cmd:    &exec.Cmd{},
		Cancel: func() { canceled++ },
	}
	state.sessions["two"] = &pprofSession{
		ID:     "two",
		Cmd:    &exec.Cmd{},
		Cancel: func() { canceled++ },
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/sessions", nil)
	state.handleClearSessions(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d，响应：%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response clearSessionsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("解析清除会话响应失败：%v", err)
	}
	if response.Cleared != 2 {
		t.Fatalf("cleared = %d，期望 2", response.Cleared)
	}
	if canceled != 2 {
		t.Fatalf("取消次数 = %d，期望 2", canceled)
	}
	if len(state.sessions) != 0 {
		t.Fatalf("剩余会话数 = %d，期望 0", len(state.sessions))
	}
}

func Test清除Pprof会话接口链路可刷新为空列表(t *testing.T) {
	state := newAppState()
	state.sessions["active"] = &pprofSession{
		ID:     "active",
		URL:    "http://127.0.0.1:12345",
		Cmd:    &exec.Cmd{},
		Cancel: func() {},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/sessions", state.handleClearSessions)
	mux.HandleFunc("GET /api/sessions", state.handleSessions)
	server := httptest.NewServer(mux)
	defer server.Close()

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions", nil)
	if err != nil {
		t.Fatalf("创建清除会话请求失败：%v", err)
	}
	clearResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("请求清除会话接口失败：%v", err)
	}
	defer clearResponse.Body.Close()
	if clearResponse.StatusCode != http.StatusOK {
		t.Fatalf("清除会话状态码 = %d，期望 %d", clearResponse.StatusCode, http.StatusOK)
	}

	var cleared clearSessionsResponse
	if err := json.NewDecoder(clearResponse.Body).Decode(&cleared); err != nil {
		t.Fatalf("解析清除会话响应失败：%v", err)
	}
	if cleared.Cleared != 1 {
		t.Fatalf("cleared = %d，期望 1", cleared.Cleared)
	}

	listResponse, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("请求会话列表接口失败：%v", err)
	}
	defer listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("会话列表状态码 = %d，期望 %d", listResponse.StatusCode, http.StatusOK)
	}

	var listed struct {
		Sessions []pprofSession `json:"sessions"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("解析会话列表响应失败：%v", err)
	}
	if len(listed.Sessions) != 0 {
		t.Fatalf("会话列表数量 = %d，期望 0", len(listed.Sessions))
	}
}

func TestPprof启动参数禁用自动打开浏览器(t *testing.T) {
	args := pprofCommandArgs(38080, `D:\profiles\heap.pprof`)

	expected := []string{
		"tool",
		"pprof",
		"-http=127.0.0.1:38080",
		"-no_browser",
		`D:\profiles\heap.pprof`,
	}
	if strings.Join(args, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("pprof 启动参数 = %#v，期望 %#v", args, expected)
	}
}
