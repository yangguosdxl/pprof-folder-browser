package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

type tabsResponse struct {
	Tabs        []tabSummary `json:"tabs"`
	ActiveTabID string       `json:"activeTabId"`
}

type tabResponse struct {
	Tab tabSummary `json:"tab"`
}

type dirsResponse struct {
	Dirs []string `json:"dirs"`
}

type profilesResponse struct {
	Profiles []profileFile `json:"profiles"`
	Count    int           `json:"count"`
}

func Test默认会创建一个页签(t *testing.T) {
	state := newAppState()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
	state.handleListTabs(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d，响应：%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response tabsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("解析页签列表响应失败：%v", err)
	}
	if len(response.Tabs) != 1 {
		t.Fatalf("页签数量 = %d，期望 1", len(response.Tabs))
	}
	if response.ActiveTabID != response.Tabs[0].ID {
		t.Fatalf("activeTabId = %q，期望 %q", response.ActiveTabID, response.Tabs[0].ID)
	}
	if response.Tabs[0].Name != "页签 1" {
		t.Fatalf("默认页签名称 = %q，期望 %q", response.Tabs[0].Name, "页签 1")
	}
}

func Test默认监听零地址(t *testing.T) {
	t.Setenv(listenAddrEnv, "")

	if listenAddr() != "0.0.0.0:18080" {
		t.Fatalf("listenAddr = %q，期望 %q", listenAddr(), "0.0.0.0:18080")
	}
}

func Test监听地址支持环境变量覆盖(t *testing.T) {
	t.Setenv(listenAddrEnv, "0.0.0.0:18081")

	if listenAddr() != "0.0.0.0:18081" {
		t.Fatalf("listenAddr = %q，期望 %q", listenAddr(), "0.0.0.0:18081")
	}
}

func Test可以新增页签并保存改名到服务器状态(t *testing.T) {
	state := newAppState()

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tabs", strings.NewReader(`{}`))
	state.handleCreateTab(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("创建页签状态码 = %d，期望 %d，响应：%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var created tabResponse
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("解析创建页签响应失败：%v", err)
	}
	if created.Tab.Name != "页签 2" {
		t.Fatalf("新增页签名称 = %q，期望 %q", created.Tab.Name, "页签 2")
	}

	body, err := json.Marshal(renameTabRequest{ID: created.Tab.ID, Name: "线上 CPU"})
	if err != nil {
		t.Fatalf("序列化改名请求失败：%v", err)
	}
	renameRecorder := httptest.NewRecorder()
	renameRequest := httptest.NewRequest(http.MethodPatch, "/api/tabs", bytes.NewReader(body))
	state.handleRenameTab(renameRecorder, renameRequest)

	if renameRecorder.Code != http.StatusOK {
		t.Fatalf("改名状态码 = %d，期望 %d，响应：%s", renameRecorder.Code, http.StatusOK, renameRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/tabs", nil)
	state.handleListTabs(listRecorder, listRequest)

	var listed tabsResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&listed); err != nil {
		t.Fatalf("解析页签列表响应失败：%v", err)
	}
	if len(listed.Tabs) != 2 {
		t.Fatalf("页签数量 = %d，期望 2", len(listed.Tabs))
	}
	if listed.Tabs[1].Name != "线上 CPU" {
		t.Fatalf("服务端保存的页签名 = %q，期望 %q", listed.Tabs[1].Name, "线上 CPU")
	}
}

func Test状态文件会在重启后恢复页签目录和扫描结果(t *testing.T) {
	storagePath := filepath.Join(t.TempDir(), "state.json")
	profileDir := t.TempDir()
	profilePath := filepath.Join(profileDir, "heap.pprof")
	if err := os.WriteFile(profilePath, []byte("profile"), 0o644); err != nil {
		t.Fatalf("创建测试 profile 文件失败：%v", err)
	}

	state, err := newAppStateWithStorage(storagePath)
	if err != nil {
		t.Fatalf("创建带持久化的状态失败：%v", err)
	}

	addBody, err := json.Marshal(addDirRequest{Path: profileDir})
	if err != nil {
		t.Fatalf("序列化添加目录请求失败：%v", err)
	}
	addRecorder := httptest.NewRecorder()
	state.handleAddDir(addRecorder, httptest.NewRequest(http.MethodPost, "/api/dirs", bytes.NewReader(addBody)))
	if addRecorder.Code != http.StatusCreated {
		t.Fatalf("添加目录状态码 = %d，期望 %d，响应：%s", addRecorder.Code, http.StatusCreated, addRecorder.Body.String())
	}

	scanRecorder := httptest.NewRecorder()
	state.handleScan(scanRecorder, httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader(`{}`)))
	if scanRecorder.Code != http.StatusOK {
		t.Fatalf("扫描状态码 = %d，期望 %d，响应：%s", scanRecorder.Code, http.StatusOK, scanRecorder.Body.String())
	}

	createRecorder := httptest.NewRecorder()
	state.handleCreateTab(createRecorder, httptest.NewRequest(http.MethodPost, "/api/tabs", strings.NewReader(`{}`)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("创建页签状态码 = %d，期望 %d，响应：%s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	var created tabResponse
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("解析创建页签响应失败：%v", err)
	}

	renameBody, err := json.Marshal(renameTabRequest{ID: created.Tab.ID, Name: "重启后保留"})
	if err != nil {
		t.Fatalf("序列化改名请求失败：%v", err)
	}
	renameRecorder := httptest.NewRecorder()
	state.handleRenameTab(renameRecorder, httptest.NewRequest(http.MethodPatch, "/api/tabs", bytes.NewReader(renameBody)))
	if renameRecorder.Code != http.StatusOK {
		t.Fatalf("改名状态码 = %d，期望 %d，响应：%s", renameRecorder.Code, http.StatusOK, renameRecorder.Body.String())
	}

	restored, err := newAppStateWithStorage(storagePath)
	if err != nil {
		t.Fatalf("读取持久化状态失败：%v", err)
	}

	listTabs := httptest.NewRecorder()
	restored.handleListTabs(listTabs, httptest.NewRequest(http.MethodGet, "/api/tabs", nil))
	var tabs tabsResponse
	if err := json.NewDecoder(listTabs.Body).Decode(&tabs); err != nil {
		t.Fatalf("解析恢复后的页签列表失败：%v", err)
	}
	if len(tabs.Tabs) != 2 {
		t.Fatalf("恢复后的页签数量 = %d，期望 2", len(tabs.Tabs))
	}
	if tabs.Tabs[1].Name != "重启后保留" {
		t.Fatalf("恢复后的第二个页签名 = %q，期望 %q", tabs.Tabs[1].Name, "重启后保留")
	}

	listDirs := httptest.NewRecorder()
	restored.handleListDirs(listDirs, httptest.NewRequest(http.MethodGet, "/api/dirs", nil))
	var dirs dirsResponse
	if err := json.NewDecoder(listDirs.Body).Decode(&dirs); err != nil {
		t.Fatalf("解析恢复后的目录列表失败：%v", err)
	}
	expectedDir, err := filepath.Abs(profileDir)
	if err != nil {
		t.Fatalf("计算测试目录绝对路径失败：%v", err)
	}
	if len(dirs.Dirs) != 1 || dirs.Dirs[0] != expectedDir {
		t.Fatalf("恢复后的目录 = %#v，期望只包含 %q", dirs.Dirs, expectedDir)
	}

	listProfiles := httptest.NewRecorder()
	restored.handleProfiles(listProfiles, httptest.NewRequest(http.MethodGet, "/api/profiles", nil))
	var profiles profilesResponse
	if err := json.NewDecoder(listProfiles.Body).Decode(&profiles); err != nil {
		t.Fatalf("解析恢复后的 profile 列表失败：%v", err)
	}
	expectedProfilePath, err := filepath.Abs(profilePath)
	if err != nil {
		t.Fatalf("计算测试 profile 绝对路径失败：%v", err)
	}
	if len(profiles.Profiles) != 1 || profiles.Profiles[0].Path != expectedProfilePath {
		t.Fatalf("恢复后的 profiles = %#v，期望只包含 %q", profiles.Profiles, expectedProfilePath)
	}

	nextTab := httptest.NewRecorder()
	restored.handleCreateTab(nextTab, httptest.NewRequest(http.MethodPost, "/api/tabs", strings.NewReader(`{}`)))
	var next tabResponse
	if err := json.NewDecoder(nextTab.Body).Decode(&next); err != nil {
		t.Fatalf("解析恢复后新建页签响应失败：%v", err)
	}
	if next.Tab.ID != "tab-3" {
		t.Fatalf("恢复后新建页签 ID = %q，期望 %q", next.Tab.ID, "tab-3")
	}
}

func Test不同页签目录列表相互隔离(t *testing.T) {
	state := newAppState()
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tabs", strings.NewReader(`{}`))
	state.handleCreateTab(createRecorder, createRequest)
	var created tabResponse
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("解析创建页签响应失败：%v", err)
	}

	firstBody, err := json.Marshal(addDirRequest{Path: firstDir})
	if err != nil {
		t.Fatalf("序列化第一个目录请求失败：%v", err)
	}
	firstAdd := httptest.NewRecorder()
	state.handleAddDir(firstAdd, httptest.NewRequest(http.MethodPost, "/api/dirs", bytes.NewReader(firstBody)))
	if firstAdd.Code != http.StatusCreated {
		t.Fatalf("默认页签添加目录状态码 = %d，期望 %d，响应：%s", firstAdd.Code, http.StatusCreated, firstAdd.Body.String())
	}

	secondBody, err := json.Marshal(addDirRequest{Path: secondDir})
	if err != nil {
		t.Fatalf("序列化第二个目录请求失败：%v", err)
	}
	secondAdd := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/dirs?tabId="+created.Tab.ID, bytes.NewReader(secondBody))
	state.handleAddDir(secondAdd, secondRequest)
	if secondAdd.Code != http.StatusCreated {
		t.Fatalf("第二页签添加目录状态码 = %d，期望 %d，响应：%s", secondAdd.Code, http.StatusCreated, secondAdd.Body.String())
	}

	defaultList := httptest.NewRecorder()
	state.handleListDirs(defaultList, httptest.NewRequest(http.MethodGet, "/api/dirs", nil))
	var defaultDirs struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.NewDecoder(defaultList.Body).Decode(&defaultDirs); err != nil {
		t.Fatalf("解析默认页签目录响应失败：%v", err)
	}

	secondList := httptest.NewRecorder()
	state.handleListDirs(secondList, httptest.NewRequest(http.MethodGet, "/api/dirs?tabId="+created.Tab.ID, nil))
	var secondDirs struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.NewDecoder(secondList.Body).Decode(&secondDirs); err != nil {
		t.Fatalf("解析第二页签目录响应失败：%v", err)
	}

	expectedFirst, err := filepath.Abs(firstDir)
	if err != nil {
		t.Fatalf("计算第一个目录绝对路径失败：%v", err)
	}
	expectedSecond, err := filepath.Abs(secondDir)
	if err != nil {
		t.Fatalf("计算第二个目录绝对路径失败：%v", err)
	}
	if len(defaultDirs.Dirs) != 1 || defaultDirs.Dirs[0] != expectedFirst {
		t.Fatalf("默认页签目录 = %#v，期望只包含 %q", defaultDirs.Dirs, expectedFirst)
	}
	if len(secondDirs.Dirs) != 1 || secondDirs.Dirs[0] != expectedSecond {
		t.Fatalf("第二页签目录 = %#v，期望只包含 %q", secondDirs.Dirs, expectedSecond)
	}
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
	state.tabs[0].Sessions["one"] = &pprofSession{
		ID:     "one",
		Cmd:    &exec.Cmd{},
		Cancel: func() { canceled++ },
	}
	state.tabs[0].Sessions["two"] = &pprofSession{
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
	if len(state.tabs[0].Sessions) != 0 {
		t.Fatalf("剩余会话数 = %d，期望 0", len(state.tabs[0].Sessions))
	}
}

func Test清除Pprof会话接口链路可刷新为空列表(t *testing.T) {
	state := newAppState()
	state.tabs[0].Sessions["active"] = &pprofSession{
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
		"-http=0.0.0.0:38080",
		"-no_browser",
		`D:\profiles\heap.pprof`,
	}
	if strings.Join(args, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("pprof 启动参数 = %#v，期望 %#v", args, expected)
	}
}

func TestPprof页面URL使用当前请求Host(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://192.168.1.10:18080/api/sessions", nil)
	url := pprofWebURL(publicHostFromRequest(request), 38080)

	if url != "http://192.168.1.10:38080" {
		t.Fatalf("pprof URL = %q，期望 %q", url, "http://192.168.1.10:38080")
	}
}

func TestPprof页面URL遇到零地址时回退本机地址(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	request.Host = "0.0.0.0:18080"
	url := pprofWebURL(publicHostFromRequest(request), 38080)

	if url != "http://127.0.0.1:38080" {
		t.Fatalf("pprof URL = %q，期望 %q", url, "http://127.0.0.1:38080")
	}
}
