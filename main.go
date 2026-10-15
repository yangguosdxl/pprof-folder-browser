package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFiles embed.FS

type appState struct {
	mu            sync.Mutex
	compareMu     sync.Mutex
	tabs          []*browserTab
	nextTabNumber int
	selectDir     dirSelector
	storagePath   string
}

// dirSelector 封装系统目录选择能力，测试中会替换为不会弹窗的实现。
type dirSelector func(context.Context) (string, error)

var errDirSelectionCanceled = errors.New("已取消选择目录")

const (
	defaultListenAddr  = "0.0.0.0:18080"
	listenAddrEnv      = "PPROF_FOLDER_BROWSER_ADDR"
	statePathEnv       = "PPROF_FOLDER_BROWSER_STATE"
	stateDirName       = "pprof-folder-browser"
	stateFileName      = "state.json"
	stateVersion       = 1
	pprofListenHost    = "0.0.0.0"
	pprofFallbackHost  = "127.0.0.1"
	defaultTabNameBase = "页签"
	diffResultDirName  = "diff-result"
)

type browserTab struct {
	ID        string                   `json:"id"`
	Name      string                   `json:"name"`
	CreatedAt time.Time                `json:"createdAt"`
	Dirs      []string                 `json:"-"`
	Profiles  []profileFile            `json:"-"`
	Sessions  map[string]*pprofSession `json:"-"`
}

type tabSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type profileFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type pprofSession struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profileId"`
	Path      string    `json:"path"`
	URL       string    `json:"url"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
	Cmd       *exec.Cmd `json:"-"`
	Cancel    func()    `json:"-"`
}

type addDirRequest struct {
	Path string `json:"path"`
}

type createTabRequest struct {
	Name string `json:"name"`
}

type renameTabRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type openProfileRequest struct {
	ID string `json:"id"`
}

type compareProfilesRequest struct {
	FirstTabID  string `json:"firstTabId"`
	SecondTabID string `json:"secondTabId"`
}

type compareProfilesResponse struct {
	OutputDir string        `json:"outputDir"`
	Compared  int           `json:"compared"`
	Files     []string      `json:"files"`
	Warnings  []string      `json:"warnings"`
	Tab       tabSummary    `json:"tab"`
	Profiles  []profileFile `json:"profiles"`
}

type profileComparison struct {
	First      profileFile
	Second     profileFile
	OutputName string
}

type persistedAppState struct {
	Version       int            `json:"version"`
	NextTabNumber int            `json:"nextTabNumber"`
	Tabs          []persistedTab `json:"tabs"`
}

type persistedTab struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"createdAt,omitempty"`
	Dirs      []string      `json:"dirs"`
	Profiles  []profileFile `json:"profiles"`
}

type apiError struct {
	Error string `json:"error"`
}

var (
	errNoMatchingProfiles = errors.New("两个页签中没有可比较的同名 pprof 文件，请先扫描并处理重复文件")
	runPprofDiff          = defaultRunPprofDiff
)

func main() {
	state := newPersistentAppState()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tabs", state.handleListTabs)
	mux.HandleFunc("POST /api/tabs", state.handleCreateTab)
	mux.HandleFunc("PATCH /api/tabs", state.handleRenameTab)
	mux.HandleFunc("DELETE /api/tabs", state.handleDeleteTab)
	mux.HandleFunc("GET /api/dirs", state.handleListDirs)
	mux.HandleFunc("POST /api/dirs", state.handleAddDir)
	mux.HandleFunc("DELETE /api/dirs", state.handleRemoveDir)
	mux.HandleFunc("POST /api/select-dir", state.handleSelectDir)
	mux.HandleFunc("POST /api/scan", state.handleScan)
	mux.HandleFunc("GET /api/profiles", state.handleProfiles)
	mux.HandleFunc("POST /api/compare", state.handleCompareProfiles)
	mux.HandleFunc("POST /api/open", state.handleOpenProfile)
	mux.HandleFunc("GET /api/sessions", state.handleSessions)
	mux.HandleFunc("DELETE /api/sessions", state.handleClearSessions)
	mux.Handle("/", staticHandler())

	addr := listenAddr()
	log.Printf("pprof 文件浏览器已启动：http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequest(mux)))
}

func staticHandler() http.Handler {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	return http.FileServer(http.FS(sub))
}

func newAppState() *appState {
	defaultTab := newBrowserTab("tab-1", defaultTabNameBase+" 1")
	return &appState{
		tabs:          []*browserTab{defaultTab},
		nextTabNumber: 2,
		selectDir:     selectDirDialog,
	}
}

func newPersistentAppState() *appState {
	statePath, err := appStatePath()
	if err != nil {
		log.Printf("状态文件路径不可用，将仅使用内存状态：%v", err)
		return newAppState()
	}

	state, err := newAppStateWithStorage(statePath)
	if err != nil {
		log.Printf("读取状态文件失败，将使用空状态：%s：%v", statePath, err)
	}
	return state
}

func newAppStateWithStorage(path string) (*appState, error) {
	state := newAppState()
	state.storagePath = strings.TrimSpace(path)
	if state.storagePath == "" {
		return state, nil
	}

	persisted, err := readPersistedState(state.storagePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}

	restored := appStateFromPersisted(persisted)
	restored.storagePath = state.storagePath
	return restored, nil
}

func appStatePath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(statePathEnv)); path != "" {
		return path, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, stateDirName, stateFileName), nil
}

func readPersistedState(path string) (persistedAppState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedAppState{}, err
	}

	var state persistedAppState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedAppState{}, err
	}
	return state, nil
}

func appStateFromPersisted(persisted persistedAppState) *appState {
	state := newAppState()
	tabs := make([]*browserTab, 0, len(persisted.Tabs))
	seenIDs := make(map[string]struct{}, len(persisted.Tabs))
	maxTabNumber := 0

	// 旧版本没有加入时间，按原有文件顺序补齐递增时间，保持兼容且顺序稳定。
	legacyBase := time.Now().Add(-time.Duration(len(persisted.Tabs)) * time.Nanosecond)
	for index, persistedTab := range persisted.Tabs {
		id := strings.TrimSpace(persistedTab.ID)
		if id == "" {
			continue
		}
		if _, exists := seenIDs[id]; exists {
			continue
		}
		seenIDs[id] = struct{}{}

		name := strings.TrimSpace(persistedTab.Name)
		if name == "" {
			name = id
		}

		tab := newBrowserTab(id, name)
		tab.CreatedAt = persistedTab.CreatedAt
		if tab.CreatedAt.IsZero() {
			tab.CreatedAt = legacyBase.Add(time.Duration(index) * time.Nanosecond)
		}
		tab.Dirs = cleanPersistedStrings(persistedTab.Dirs)
		tab.Profiles = cleanPersistedProfiles(persistedTab.Profiles)
		tabs = append(tabs, tab)

		if number, ok := parseTabNumber(id); ok && number > maxTabNumber {
			maxTabNumber = number
		}
	}

	if len(tabs) == 0 {
		return state
	}

	state.tabs = tabs
	state.nextTabNumber = persisted.NextTabNumber
	if state.nextTabNumber <= maxTabNumber {
		state.nextTabNumber = maxTabNumber + 1
	}
	if state.nextTabNumber < 2 {
		state.nextTabNumber = 2
	}
	return state
}

func cleanPersistedStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cleanPersistedProfiles(profiles []profileFile) []profileFile {
	cleaned := make([]profileFile, 0, len(profiles))
	for _, profile := range profiles {
		profile.Path = strings.TrimSpace(profile.Path)
		profile.Dir = strings.TrimSpace(profile.Dir)
		if profile.Path == "" || profile.Dir == "" {
			continue
		}
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			profile.ID = stableID(profile.Path)
		}
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			profile.Name = filepath.Base(profile.Path)
		}
		cleaned = append(cleaned, profile)
	}
	return cleaned
}

func parseTabNumber(id string) (int, bool) {
	value, ok := strings.CutPrefix(id, "tab-")
	if !ok {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return number, true
}

func (s *appState) saveLocked() error {
	if s.storagePath == "" {
		return nil
	}
	return writePersistedState(s.storagePath, s.persistedLocked())
}

func (s *appState) persistedLocked() persistedAppState {
	tabs := make([]persistedTab, 0, len(s.tabs))
	for _, tab := range s.tabs {
		tabs = append(tabs, persistedTab{
			ID:        tab.ID,
			Name:      tab.Name,
			CreatedAt: tab.CreatedAt,
			Dirs:      cloneStrings(tab.Dirs),
			Profiles:  cloneProfiles(tab.Profiles),
		})
	}
	return persistedAppState{
		Version:       stateVersion,
		NextTabNumber: s.nextTabNumber,
		Tabs:          tabs,
	}
}

func writePersistedState(path string, state persistedAppState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func newBrowserTab(id string, name string) *browserTab {
	return &browserTab{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now(),
		Dirs:      []string{},
		Profiles:  []profileFile{},
		Sessions:  make(map[string]*pprofSession),
	}
}

func listenAddr() string {
	addr := strings.TrimSpace(os.Getenv(listenAddrEnv))
	if addr == "" {
		return defaultListenAddr
	}
	return addr
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

func (s *appState) handleListTabs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	activeTabID := ""
	if len(s.tabs) > 0 {
		activeTabID = newestTab(s.tabs).ID
	}
	tabs := cloneTabs(s.tabs)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"tabs":        tabs,
		"activeTabId": activeTabID,
	})
}

func (s *appState) handleCreateTab(w http.ResponseWriter, r *http.Request) {
	var req createTabRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
			return
		}
	}

	s.mu.Lock()
	tabNumber := s.nextTabNumber
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s %d", defaultTabNameBase, tabNumber)
	}
	tab := newBrowserTab(fmt.Sprintf("tab-%d", tabNumber), name)
	s.nextTabNumber++
	s.tabs = append(s.tabs, tab)
	responseTab := cloneTab(tab)
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"tab": responseTab})
}

func (s *appState) handleRenameTab(w http.ResponseWriter, r *http.Request) {
	var req renameTabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "缺少页签 ID")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "页签名称不能为空")
		return
	}

	s.mu.Lock()
	tab, ok := s.findTabLocked(req.ID)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	tab.Name = req.Name
	responseTab := cloneTab(tab)
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"tab": responseTab})
}

func (s *appState) handleDeleteTab(w http.ResponseWriter, r *http.Request) {
	tabID := strings.TrimSpace(r.URL.Query().Get("tabId"))
	if tabID == "" {
		writeError(w, http.StatusBadRequest, "缺少页签 ID")
		return
	}

	s.mu.Lock()
	if len(s.tabs) <= 1 {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "至少保留一个页签")
		return
	}

	index := -1
	for i, tab := range s.tabs {
		if tab.ID == tabID {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}

	deletedTab := s.tabs[index]
	sessions := make([]*pprofSession, 0, len(deletedTab.Sessions))
	for _, session := range deletedTab.Sessions {
		sessions = append(sessions, session)
	}

	previousTabs := s.tabs
	nextTabs := make([]*browserTab, 0, len(s.tabs)-1)
	nextTabs = append(nextTabs, s.tabs[:index]...)
	nextTabs = append(nextTabs, s.tabs[index+1:]...)
	s.tabs = nextTabs

	activeTabID := ""
	if len(s.tabs) > 0 {
		nextIndex := index
		if nextIndex >= len(s.tabs) {
			nextIndex = len(s.tabs) - 1
		}
		activeTabID = s.tabs[nextIndex].ID
	}
	tabs := cloneTabs(s.tabs)

	if err := s.saveLocked(); err != nil {
		s.tabs = previousTabs
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	for _, session := range sessions {
		stopPprofSession(session)
	}

	log.Printf("已删除页签 %s，清除 %d 个 pprof 进程", tabID, len(sessions))
	writeJSON(w, http.StatusOK, map[string]any{
		"tabs":        tabs,
		"activeTabId": activeTabID,
		"cleared":     len(sessions),
	})
}

func (s *appState) handleListDirs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	dirs := cloneStrings(tab.Dirs)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"dirs": dirs})
}

func (s *appState) handleAddDir(w http.ResponseWriter, r *http.Request) {
	var req addDirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}

	dir, err := normalizeDir(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	for _, existing := range tab.Dirs {
		if strings.EqualFold(existing, dir) {
			dirs := cloneStrings(tab.Dirs)
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"dirs": dirs})
			return
		}
	}
	tab.Dirs = append(tab.Dirs, dir)
	sort.Strings(tab.Dirs)
	dirs := cloneStrings(tab.Dirs)
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"dirs": dirs})
}

func (s *appState) handleRemoveDir(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	normalized, err := filepath.Abs(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "目录路径无效")
		return
	}

	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	next := tab.Dirs[:0]
	for _, existing := range tab.Dirs {
		if !strings.EqualFold(existing, normalized) {
			next = append(next, existing)
		}
	}
	tab.Dirs = next
	tab.Profiles = filterProfilesByDirs(tab.Profiles, tab.Dirs)
	dirs := cloneStrings(tab.Dirs)
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"dirs": dirs})
}

func (s *appState) handleSelectDir(w http.ResponseWriter, r *http.Request) {
	selector := s.selectDir
	if selector == nil {
		selector = selectDirDialog
	}

	log.Print("正在打开系统目录选择窗口")
	dir, err := selector(r.Context())
	if errors.Is(err, errDirSelectionCanceled) {
		log.Print("已取消选择目录")
		writeJSON(w, http.StatusOK, map[string]any{"path": "", "canceled": true})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("打开目录选择窗口失败：%v", err))
		return
	}

	normalized, err := normalizeDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("已选择目录：%s", normalized)
	writeJSON(w, http.StatusOK, map[string]any{"path": normalized, "canceled": false})
}

func (s *appState) handleScan(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	tabID := tab.ID
	dirs := append([]string(nil), tab.Dirs...)
	s.mu.Unlock()

	profiles, err := scanProfiles(dirs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	tab, ok = s.findTabLocked(tabID)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	tab.Profiles = profiles
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("保存状态失败：%v", err))
		return
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles, "count": len(profiles)})
}

func (s *appState) handleProfiles(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	profiles := cloneProfiles(tab.Profiles)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *appState) handleCompareProfiles(w http.ResponseWriter, r *http.Request) {
	var req compareProfilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	req.FirstTabID = strings.TrimSpace(req.FirstTabID)
	req.SecondTabID = strings.TrimSpace(req.SecondTabID)
	if req.FirstTabID == "" || req.SecondTabID == "" {
		writeError(w, http.StatusBadRequest, "请选择两个页签进行比较")
		return
	}
	if req.FirstTabID == req.SecondTabID {
		writeError(w, http.StatusBadRequest, "不能与同一个页签比较")
		return
	}

	// Keep output replacement and result-tab creation ordered when requests overlap.
	s.compareMu.Lock()
	defer s.compareMu.Unlock()

	s.mu.Lock()
	firstTab, firstOK := s.findTabLocked(req.FirstTabID)
	secondTab, secondOK := s.findTabLocked(req.SecondTabID)
	if !firstOK || !secondOK {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "要比较的页签不存在")
		return
	}
	firstName := firstTab.Name
	firstProfiles := cloneProfiles(firstTab.Profiles)
	secondName := secondTab.Name
	secondProfiles := cloneProfiles(secondTab.Profiles)
	s.mu.Unlock()

	result, err := compareTabProfiles(r.Context(), firstName, firstProfiles, secondName, secondProfiles)
	for _, warning := range result.Warnings {
		log.Printf("pprof 差分警告：%s", warning)
	}
	if err != nil {
		if errors.Is(err, errNoMatchingProfiles) {
			message := err.Error()
			if len(result.Warnings) > 0 {
				message += "；" + strings.Join(result.Warnings, "；")
			}
			writeError(w, http.StatusBadRequest, message)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	profiles, err := scanProfiles([]string{result.OutputDir})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("扫描差分结果失败：%v", err))
		return
	}
	resultTab, err := s.createResultTab(comparisonFolderName(firstName, secondName), result.OutputDir, profiles)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("创建差分结果页签失败：%v", err))
		return
	}
	result.Tab = resultTab
	result.Profiles = profiles

	writeJSON(w, http.StatusCreated, result)
}

func (s *appState) createResultTab(name string, outputDir string, profiles []profileFile) (tabSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tabNumber := s.nextTabNumber
	tab := newBrowserTab(fmt.Sprintf("tab-%d", tabNumber), name)
	tab.Dirs = []string{outputDir}
	tab.Profiles = cloneProfiles(profiles)
	s.nextTabNumber++
	s.tabs = append(s.tabs, tab)
	if err := s.saveLocked(); err != nil {
		s.tabs = s.tabs[:len(s.tabs)-1]
		s.nextTabNumber = tabNumber
		return tabSummary{}, err
	}
	return cloneTab(tab), nil
}

func (s *appState) handleOpenProfile(w http.ResponseWriter, r *http.Request) {
	var req openProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "缺少 profile ID")
		return
	}

	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	tabID := tab.ID
	profile, ok := findProfile(tab.Profiles, req.ID)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "没有找到这个 profile 文件，请重新扫描")
		return
	}
	for _, session := range tab.Sessions {
		if session.ProfileID == profile.ID {
			url := sessionURLForHost(session, publicHostFromRequest(r))
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"url": url, "reused": true})
			return
		}
	}
	s.mu.Unlock()

	session, err := startPprof(profile, publicHostFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	tab, ok = s.findTabLocked(tabID)
	if !ok {
		s.mu.Unlock()
		stopPprofSession(session)
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	tab.Sessions[session.ID] = session
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"url": session.URL, "reused": false})
}

func (s *appState) handleSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	tab, ok := s.findTabByRequestLocked(r)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	sessions := make([]*pprofSession, 0, len(tab.Sessions))
	urlHost := publicHostFromRequest(r)
	for id, session := range tab.Sessions {
		if session.Cmd != nil && session.Cmd.ProcessState != nil && session.Cmd.ProcessState.Exited() {
			delete(tab.Sessions, id)
			continue
		}
		sessions = append(sessions, cloneSessionForHost(session, urlHost))
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *appState) handleClearSessions(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("tabId")) == "" {
		cleared := s.clearSessions()
		writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared})
		return
	}
	cleared, ok := s.clearSessionsForTab(r.URL.Query().Get("tabId"))
	if !ok {
		writeError(w, http.StatusNotFound, "页签不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared})
}

func (s *appState) clearSessionsForTab(tabID string) (int, bool) {
	s.mu.Lock()
	tab, ok := s.findTabLocked(tabID)
	if !ok {
		s.mu.Unlock()
		return 0, false
	}
	sessions := make([]*pprofSession, 0, len(tab.Sessions))
	for _, session := range tab.Sessions {
		sessions = append(sessions, session)
	}
	tab.Sessions = make(map[string]*pprofSession)
	s.mu.Unlock()

	for _, session := range sessions {
		stopPprofSession(session)
	}

	log.Printf("已清除 %d 个 pprof 进程", len(sessions))
	return len(sessions), true
}

func stopPprofSession(session *pprofSession) {
	if session == nil {
		return
	}
	if err := killProcessTree(session.Cmd); err != nil {
		log.Printf("结束 pprof 进程失败：%s，错误：%v", session.Path, err)
	}
	if session.Cancel != nil {
		session.Cancel()
	}
}

func (s *appState) clearSessions() int {
	total := 0
	s.mu.Lock()
	tabIDs := make([]string, 0, len(s.tabs))
	for _, tab := range s.tabs {
		tabIDs = append(tabIDs, tab.ID)
	}
	s.mu.Unlock()

	for _, tabID := range tabIDs {
		cleared, ok := s.clearSessionsForTab(tabID)
		if ok {
			total += cleared
		}
	}
	return total
}

func (s *appState) findTabByRequestLocked(r *http.Request) (*browserTab, bool) {
	return s.findTabLocked(r.URL.Query().Get("tabId"))
}

func (s *appState) findTabLocked(id string) (*browserTab, bool) {
	if id == "" {
		if len(s.tabs) == 0 {
			return nil, false
		}
		return s.tabs[0], true
	}
	for _, tab := range s.tabs {
		if tab.ID == id {
			return tab, true
		}
	}
	return nil, false
}

func findProfile(profiles []profileFile, id string) (profileFile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return profileFile{}, false
}

func compareTabProfiles(ctx context.Context, firstTabName string, firstProfiles []profileFile, secondTabName string, secondProfiles []profileFile) (compareProfilesResponse, error) {
	comparisons, warnings := matchingProfileComparisons(firstTabName, firstProfiles, secondTabName, secondProfiles)
	result := compareProfilesResponse{Warnings: warnings}
	if len(comparisons) == 0 {
		return result, errNoMatchingProfiles
	}

	outputDir, err := comparisonOutputDir(firstTabName, secondTabName)
	if err != nil {
		return result, fmt.Errorf("确定差分结果目录失败：%w", err)
	}
	parentDir := filepath.Dir(outputDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return result, fmt.Errorf("创建差分结果父目录失败：%w", err)
	}
	temporaryDir, err := os.MkdirTemp(parentDir, ".pprof-diff-")
	if err != nil {
		return result, fmt.Errorf("创建差分临时目录失败：%w", err)
	}
	defer func() {
		if temporaryDir != "" {
			_ = os.RemoveAll(temporaryDir)
		}
	}()

	result.OutputDir = outputDir
	result.Files = make([]string, 0, len(comparisons))
	for _, comparison := range comparisons {
		outputPath := filepath.Join(temporaryDir, comparison.OutputName)
		if err := runPprofDiff(ctx, comparison.First.Path, comparison.Second.Path, outputPath); err != nil {
			return result, fmt.Errorf("比较 %q 失败：%w", comparison.OutputName, err)
		}
		result.Files = append(result.Files, filepath.Join(outputDir, comparison.OutputName))
	}

	if err := replaceOutputDirectory(temporaryDir, outputDir); err != nil {
		return result, fmt.Errorf("替换差分结果目录失败：%w", err)
	}
	temporaryDir = ""
	result.Compared = len(comparisons)
	return result, nil
}

func matchingProfileComparisons(firstTabName string, firstProfiles []profileFile, secondTabName string, secondProfiles []profileFile) ([]profileComparison, []string) {
	firstByName := profilesByName(firstProfiles)
	secondByName := profilesByName(secondProfiles)
	keys := make(map[string]struct{}, len(firstByName)+len(secondByName))
	for key := range firstByName {
		keys[key] = struct{}{}
	}
	for key := range secondByName {
		keys[key] = struct{}{}
	}

	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)

	comparisons := make([]profileComparison, 0)
	warnings := make([]string, 0)
	usedOutputNames := make(map[string]struct{})
	for _, name := range names {
		firstMatches := firstByName[name]
		secondMatches := secondByName[name]
		displayName := comparisonDisplayName(firstMatches, secondMatches)
		if len(firstMatches) > 1 {
			warnings = append(warnings, fmt.Sprintf("文件 %q 在页签 %q 中有 %d 份，已跳过", displayName, firstTabName, len(firstMatches)))
		}
		if len(secondMatches) > 1 {
			warnings = append(warnings, fmt.Sprintf("文件 %q 在页签 %q 中有 %d 份，已跳过", displayName, secondTabName, len(secondMatches)))
		}
		if len(firstMatches) != 1 || len(secondMatches) != 1 {
			continue
		}

		outputName := uniqueComparisonOutputName(safePathComponent(firstMatches[0].Name), usedOutputNames)
		comparisons = append(comparisons, profileComparison{
			First:      firstMatches[0],
			Second:     secondMatches[0],
			OutputName: outputName,
		})
	}
	return comparisons, warnings
}

func profilesByName(profiles []profileFile) map[string][]profileFile {
	byName := make(map[string][]profileFile)
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			name = filepath.Base(profile.Path)
		}
		if name == "" || name == "." {
			continue
		}
		key := strings.ToLower(name)
		byName[key] = append(byName[key], profile)
	}
	return byName
}

func comparisonDisplayName(firstMatches []profileFile, secondMatches []profileFile) string {
	if len(firstMatches) > 0 {
		return firstMatches[0].Name
	}
	if len(secondMatches) > 0 {
		return secondMatches[0].Name
	}
	return "pprof"
}

func comparisonOutputDir(firstTabName string, secondTabName string) (string, error) {
	root, err := filepath.Abs(diffResultDirName)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, comparisonFolderName(firstTabName, secondTabName)), nil
}

func comparisonFolderName(firstTabName string, secondTabName string) string {
	return safePathComponent(firstTabName) + " vs " + safePathComponent(secondTabName)
}

func safePathComponent(value string) string {
	const invalidPathCharacters = `<>:"/\\|?*`

	var builder strings.Builder
	for _, char := range strings.TrimSpace(value) {
		if char < 32 || strings.ContainsRune(invalidPathCharacters, char) {
			builder.WriteByte('_')
			continue
		}
		builder.WriteRune(char)
	}
	cleaned := strings.Trim(builder.String(), ". ")
	if cleaned == "" {
		cleaned = "unnamed"
	}
	if isWindowsReservedName(cleaned) {
		cleaned += "_"
	}

	const maxComponentRunes = 100
	runes := []rune(cleaned)
	if len(runes) > maxComponentRunes {
		cleaned = string(runes[:maxComponentRunes])
	}
	return cleaned
}

func isWindowsReservedName(value string) bool {
	name := strings.ToUpper(strings.TrimSuffix(value, filepath.Ext(value)))
	switch name {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func uniqueComparisonOutputName(name string, used map[string]struct{}) string {
	if name == "" {
		name = "pprof"
	}
	candidate := name
	for suffix := 2; ; suffix++ {
		key := strings.ToLower(candidate)
		if _, exists := used[key]; !exists {
			used[key] = struct{}{}
			return candidate
		}
		extension := filepath.Ext(name)
		stem := strings.TrimSuffix(name, extension)
		candidate = fmt.Sprintf("%s__%d%s", stem, suffix, extension)
	}
}

func replaceOutputDirectory(temporaryDir string, outputDir string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return err
	}
	return os.Rename(temporaryDir, outputDir)
}

func defaultRunPprofDiff(ctx context.Context, baseProfilePath string, targetProfilePath string, outputPath string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("没有找到 go 命令，请确认 Go 已安装并在 PATH 中")
	}

	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", pprofDiffCommandArgs(baseProfilePath, targetProfilePath)...)
	cmd.Stdout = output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = windowsHideConsole()
	}
	runErr := cmd.Run()
	closeErr := output.Close()
	if runErr != nil {
		_ = os.Remove(outputPath)
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return fmt.Errorf("go tool pprof: %w: %s", runErr, message)
		}
		return fmt.Errorf("go tool pprof: %w", runErr)
	}
	if closeErr != nil {
		_ = os.Remove(outputPath)
		return closeErr
	}
	return nil
}

func pprofDiffCommandArgs(baseProfilePath string, targetProfilePath string) []string {
	return []string{"tool", "pprof", "-proto", "-diff_base=" + baseProfilePath, targetProfilePath}
}

func normalizeDir(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("目录不能为空")
	}
	dir, err := filepath.Abs(trimmed)
	if err != nil {
		return "", errors.New("目录路径无效")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("目录不可访问：%v", err)
	}
	if !info.IsDir() {
		return "", errors.New("路径不是目录")
	}
	return dir, nil
}

func scanProfiles(dirs []string) ([]profileFile, error) {
	profiles := make([]profileFile, 0)
	for _, root := range dirs {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				log.Printf("跳过无法访问的路径：%s，错误：%v", path, walkErr)
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !looksLikeProfile(path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				log.Printf("读取文件信息失败：%s，错误：%v", path, err)
				return nil
			}
			profiles = append(profiles, profileFile{
				ID:       stableID(path),
				Name:     filepath.Base(path),
				Path:     path,
				Dir:      root,
				Size:     info.Size(),
				Modified: info.ModTime().Format(time.RFC3339),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("扫描目录失败：%s：%v", root, err)
		}
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Modified == profiles[j].Modified {
			return strings.ToLower(profiles[i].Path) < strings.ToLower(profiles[j].Path)
		}
		return profiles[i].Modified > profiles[j].Modified
	})
	return profiles, nil
}

func looksLikeProfile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pprof" {
		return true
	}
	if strings.HasSuffix(name, ".pb.gz") || strings.HasSuffix(name, ".prof") {
		return true
	}
	// Go pprof 的 profile 文件常见命名没有固定扩展名，这里保守匹配常见关键词。
	keywords := []string{"profile", "heap", "allocs", "goroutine", "threadcreate", "block", "mutex", "trace"}
	for _, keyword := range keywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}
	return false
}

func filterProfilesByDirs(profiles []profileFile, dirs []string) []profileFile {
	filtered := profiles[:0]
	for _, profile := range profiles {
		for _, dir := range dirs {
			if strings.EqualFold(profile.Dir, dir) {
				filtered = append(filtered, profile)
				break
			}
		}
	}
	return filtered
}

func cloneTab(tab *browserTab) tabSummary {
	return tabSummary{ID: tab.ID, Name: tab.Name, CreatedAt: tab.CreatedAt}
}

func cloneTabs(tabs []*browserTab) []tabSummary {
	sorted := append([]*browserTab(nil), tabs...)
	sortTabsByCreatedAt(sorted)
	cloned := make([]tabSummary, len(sorted))
	for i, tab := range sorted {
		cloned[i] = cloneTab(tab)
	}
	return cloned
}

func newestTab(tabs []*browserTab) *browserTab {
	if len(tabs) == 0 {
		return nil
	}
	newest := tabs[0]
	for _, tab := range tabs[1:] {
		if tab.CreatedAt.After(newest.CreatedAt) {
			newest = tab
		}
	}
	return newest
}

func sortTabsByCreatedAt(tabs []*browserTab) {
	sort.SliceStable(tabs, func(i, j int) bool {
		if tabs[i].CreatedAt.Equal(tabs[j].CreatedAt) {
			return tabs[i].ID > tabs[j].ID
		}
		return tabs[i].CreatedAt.After(tabs[j].CreatedAt)
	})
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneProfiles(values []profileFile) []profileFile {
	cloned := make([]profileFile, len(values))
	copy(cloned, values)
	return cloned
}

func cloneSessionForHost(session *pprofSession, urlHost string) *pprofSession {
	cloned := *session
	cloned.URL = sessionURLForHost(session, urlHost)
	return &cloned
}

func sessionURLForHost(session *pprofSession, urlHost string) string {
	if session.Port == 0 {
		return session.URL
	}
	return pprofWebURL(urlHost, session.Port)
}

func startPprof(profile profileFile, urlHost string) (*pprofSession, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return nil, errors.New("没有找到 go 命令，请确认 Go 已安装并在 PATH 中")
	}
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("分配本地端口失败：%v", err)
	}

	url := pprofWebURL(urlHost, port)
	ctx, cancel := context.WithCancel(context.Background())
	args := pprofCommandArgs(port, profile.Path)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = windowsHideConsole()
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("启动 go tool pprof 失败：%v", err)
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("pprof 进程已关闭：%s", profile.Path)
				return
			}
			log.Printf("pprof 进程退出：%s，错误：%v", profile.Path, err)
		}
	}()

	// pprof Web UI 启动需要一点时间，前端打开新窗口后浏览器会自动等待。
	sessionID := stableID(profile.Path + "|" + strconv.Itoa(port) + "|" + time.Now().Format(time.RFC3339Nano))
	return &pprofSession{
		ID:        sessionID,
		ProfileID: profile.ID,
		Path:      profile.Path,
		URL:       url,
		Port:      port,
		StartedAt: time.Now(),
		Cmd:       cmd,
		Cancel:    cancel,
	}, nil
}

func pprofCommandArgs(port int, profilePath string) []string {
	// pprof 默认会自动打开浏览器；前端已经负责 window.open，这里关闭自动打开以避免重复页面。
	args := []string{"tool", "pprof", "-http=" + net.JoinHostPort(pprofListenHost, strconv.Itoa(port)), "-no_browser"}
	name := strings.ToLower(filepath.Base(profilePath))
	if name == "unityobjects_instance_memory.pprof" || name == "unityobjects_instance_count.pprof" || name == "unityobjects_resources.pprof" {
		// Web UI 未提供标签伪栈开关：启动时按资源身份分组，真实调用栈及 unkdown 继续保留。
		// resourceKey 对未映射资源也有稳定记录身份，避免丢失资源路径为空的样本。
		args = append(args, "-tagroot=resourceKey")
	}
	return append(args, profilePath)
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(pprofListenHost, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("无法解析 TCP 端口")
	}
	return addr.Port, nil
}

func pprofWebURL(urlHost string, port int) string {
	host := strings.TrimSpace(urlHost)
	if host == "" {
		host = pprofFallbackHost
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func publicHostFromRequest(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return pprofFallbackHost
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return pprofFallbackHost
	}
	return host
}

func stableID(value string) string {
	sum := sha1.Sum([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("写入 JSON 响应失败：%v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}
