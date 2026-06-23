package main

import (
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

//go:embed web/*
var webFiles embed.FS

type appState struct {
	mu        sync.Mutex
	dirs      []string
	profiles  []profileFile
	sessions  map[string]*pprofSession
	selectDir dirSelector
}

// dirSelector 封装系统目录选择能力，测试中会替换为不会弹窗的实现。
type dirSelector func(context.Context) (string, error)

var errDirSelectionCanceled = errors.New("已取消选择目录")

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

type openProfileRequest struct {
	ID string `json:"id"`
}

type apiError struct {
	Error string `json:"error"`
}

func main() {
	state := newAppState()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/dirs", state.handleListDirs)
	mux.HandleFunc("POST /api/dirs", state.handleAddDir)
	mux.HandleFunc("DELETE /api/dirs", state.handleRemoveDir)
	mux.HandleFunc("POST /api/select-dir", state.handleSelectDir)
	mux.HandleFunc("POST /api/scan", state.handleScan)
	mux.HandleFunc("GET /api/profiles", state.handleProfiles)
	mux.HandleFunc("POST /api/open", state.handleOpenProfile)
	mux.HandleFunc("GET /api/sessions", state.handleSessions)
	mux.Handle("/", staticHandler())

	addr := "127.0.0.1:18080"
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
	return &appState{
		dirs:      []string{},
		profiles:  []profileFile{},
		sessions:  make(map[string]*pprofSession),
		selectDir: selectDirDialog,
	}
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

func (s *appState) handleListDirs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"dirs": cloneStrings(s.dirs)})
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
	defer s.mu.Unlock()
	for _, existing := range s.dirs {
		if strings.EqualFold(existing, dir) {
			writeJSON(w, http.StatusOK, map[string]any{"dirs": cloneStrings(s.dirs)})
			return
		}
	}
	s.dirs = append(s.dirs, dir)
	sort.Strings(s.dirs)
	writeJSON(w, http.StatusCreated, map[string]any{"dirs": cloneStrings(s.dirs)})
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
	defer s.mu.Unlock()
	next := s.dirs[:0]
	for _, existing := range s.dirs {
		if !strings.EqualFold(existing, normalized) {
			next = append(next, existing)
		}
	}
	s.dirs = next
	s.profiles = filterProfilesByDirs(s.profiles, s.dirs)
	writeJSON(w, http.StatusOK, map[string]any{"dirs": cloneStrings(s.dirs)})
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
	dirs := append([]string(nil), s.dirs...)
	s.mu.Unlock()

	profiles, err := scanProfiles(dirs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	s.profiles = profiles
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles, "count": len(profiles)})
}

func (s *appState) handleProfiles(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"profiles": cloneProfiles(s.profiles)})
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
	profile, ok := s.findProfileLocked(req.ID)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "没有找到这个 profile 文件，请重新扫描")
		return
	}
	for _, session := range s.sessions {
		if session.ProfileID == profile.ID {
			url := session.URL
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"url": url, "reused": true})
			return
		}
	}
	s.mu.Unlock()

	session, err := startPprof(profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"url": session.URL, "reused": false})
}

func (s *appState) handleSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := make([]*pprofSession, 0, len(s.sessions))
	for id, session := range s.sessions {
		if session.Cmd.ProcessState != nil && session.Cmd.ProcessState.Exited() {
			delete(s.sessions, id)
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *appState) findProfileLocked(id string) (profileFile, bool) {
	for _, profile := range s.profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return profileFile{}, false
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

func startPprof(profile profileFile) (*pprofSession, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return nil, errors.New("没有找到 go 命令，请确认 Go 已安装并在 PATH 中")
	}
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("分配本地端口失败：%v", err)
	}

	url := "http://127.0.0.1:" + strconv.Itoa(port)
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"tool", "pprof", "-http=127.0.0.1:" + strconv.Itoa(port), profile.Path}
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

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
