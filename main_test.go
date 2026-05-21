package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPluginName(t *testing.T) {
	name := Plugin.Name()
	if name == "" {
		t.Fatal("Name() 不应为空")
	}
}

func TestPluginInit(t *testing.T) {
	ctx := PluginContext{
		Logger: slog.Default(),
	}
	if err := Plugin.Init(ctx); err != nil {
		t.Errorf("Init 失败: %v", err)
	}
}

func TestPluginShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	if err := Plugin.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown(ctx) 失败: %v", err)
	}
}

func TestHandleConfigListAcceptsProjectAdminSession(t *testing.T) {
	p := &GlobalConfigPlugin{}
	req := httptest.NewRequest(http.MethodGet, "/api/global_config/config/list?page=1&page_size=20", nil)
	req.Header.Set("X-Admin-Session-Token", "project-session")
	req.Header.Set("X-Admin-Role", "admin")
	rec := httptest.NewRecorder()

	p.handleConfigList(rec, req)

	var resp struct {
		Status int             `json:"status"`
		Data   json.RawMessage `json:"data"`
		Msg    string          `json:"msg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, rec.Body.String())
	}
	if resp.Status != 0 {
		t.Fatalf("status=%d msg=%s body=%s", resp.Status, resp.Msg, rec.Body.String())
	}
}

func TestRequirePermissionRejectsOperatorForConfigMutation(t *testing.T) {
	p := &GlobalConfigPlugin{}
	req := httptest.NewRequest(http.MethodPost, "/api/global_config/config/update", nil)
	req.Header.Set("X-Admin-Session-Token", "project-session")
	req.Header.Set("X-Admin-Role", "operator")
	rec := httptest.NewRecorder()

	if p.requirePermissionForToken(rec, req, "", "global_config.edit", 2321, 2322, 2326, "denied") {
		t.Fatal("operator project session should not edit global config")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected http status %d", rec.Code)
	}
}
