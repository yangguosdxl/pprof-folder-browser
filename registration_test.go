package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func Test幂等登记并发及重启后复用同一页签(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := newAppStateWithStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			response := httptest.NewRecorder()
			state.handleCreateTab(response, httptest.NewRequest(http.MethodPut, "/api/tabs",
				strings.NewReader(`{"name":"测试登记","registrationKey":"same-artifact"}`)))
			if response.Code != http.StatusOK && response.Code != http.StatusCreated {
				t.Errorf("登记失败：%d %s", response.Code, response.Body.String())
			}
		}()
	}
	workers.Wait()
	if len(state.tabs) != 2 || state.nextTabNumber != 3 {
		t.Fatalf("并发重复创建：页签=%d，下一编号=%d", len(state.tabs), state.nextTabNumber)
	}
	restored, err := newAppStateWithStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	restored.handleCreateTab(response, httptest.NewRequest(http.MethodPut, "/api/tabs",
		strings.NewReader(`{"name":"响应丢失后重试","registrationKey":"same-artifact"}`)))
	var result tabResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Tab.ID != "tab-2" || len(restored.tabs) != 2 {
		t.Fatalf("重启后未复用原页签：%d %s", response.Code, response.Body.String())
	}
}

func Test幂等登记必须给标识且保存失败不留下半成品(t *testing.T) {
	state := newAppState()
	missing := httptest.NewRecorder()
	state.handleCreateTab(missing, httptest.NewRequest(http.MethodPut, "/api/tabs",
		strings.NewReader(`{"name":"没有标识"}`)))
	if missing.Code != http.StatusBadRequest || len(state.tabs) != 1 {
		t.Fatal("缺少登记标识仍然创建了页签")
	}
	blocked := filepath.Join(t.TempDir(), "普通文件")
	if err := os.WriteFile(blocked, []byte("阻止创建目录"), 0600); err != nil {
		t.Fatal(err)
	}
	state.storagePath = filepath.Join(blocked, "state.json")
	failed := httptest.NewRecorder()
	state.handleCreateTab(failed, httptest.NewRequest(http.MethodPut, "/api/tabs",
		strings.NewReader(`{"name":"保存失败","registrationKey":"key"}`)))
	if failed.Code != http.StatusInternalServerError || len(state.tabs) != 1 || state.nextTabNumber != 2 {
		t.Fatalf("保存失败后存在半成品：%d，页签=%d", failed.Code, len(state.tabs))
	}
}

func TestVersionEndpointReturnsAppVersion(t *testing.T) {
	response := httptest.NewRecorder()
	handleVersion(response, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["version"] != appVersion || appVersion == "" {
		t.Fatalf("版本 = %q，期望 %q", body["version"], appVersion)
	}
}
