package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (p *GlobalConfigPlugin) handleConfigList(w http.ResponseWriter, r *http.Request) {
	if !p.requirePermission(w, r, "global_config.view", 2301, 2302, 2305, "操作人无查看全局配置权限") {
		return
	}
	page, pageSize, ok := parseConfigPage(r)
	if !ok {
		writeJSON(w, 2303, nil, "分页参数不合法")
		return
	}
	moduleName := strings.TrimSpace(r.URL.Query().Get("module_name"))
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if (moduleName != "" && !moduleNameRE.MatchString(moduleName)) || utf8.RuneCountInString(keyword) > 64 {
		writeJSON(w, 2304, nil, "模块或关键词参数不合法")
		return
	}
	items, total, err := p.listConfigs(r.Context(), moduleName, strings.ToLower(keyword), page, pageSize)
	if err != nil {
		writeJSON(w, 2305, nil, "查询配置列表失败")
		return
	}
	writeJSON(w, 0, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize}, "")
}

func (p *GlobalConfigPlugin) handleConfigDetail(w http.ResponseWriter, r *http.Request) {
	if !p.requirePermission(w, r, "global_config.view", 2311, 2312, 2315, "操作人无查看全局配置权限") {
		return
	}
	configKey := strings.TrimSpace(r.URL.Query().Get("config_key"))
	if !configKeyRE.MatchString(configKey) {
		writeJSON(w, 2313, nil, "配置键名缺失或格式不合法")
		return
	}
	item, ok, err := p.getConfig(r.Context(), configKey)
	if err != nil {
		writeJSON(w, 2315, nil, "查询配置详情失败")
		return
	}
	if !ok {
		writeJSON(w, 2314, nil, "配置项不存在")
		return
	}
	writeJSON(w, 0, configDetailResponse(item), "")
}

func (p *GlobalConfigPlugin) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperatorSessionToken string          `json:"operator_session_token"`
		ConfigKey            string          `json:"config_key"`
		CurrentValue         json.RawMessage `json:"current_value"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, 2325, nil, "配置值缺失或与类型不匹配")
		return
	}
	if !p.requirePermissionForToken(w, r, req.OperatorSessionToken, "global_config.edit", 2321, 2322, 2326, "操作人无编辑全局配置权限") {
		return
	}
	req.ConfigKey = strings.TrimSpace(req.ConfigKey)
	if !configKeyRE.MatchString(req.ConfigKey) {
		writeJSON(w, 2323, nil, "配置键名缺失或格式不合法")
		return
	}
	item, ok, err := p.getConfig(r.Context(), req.ConfigKey)
	if err != nil {
		writeJSON(w, 2326, nil, "更新配置失败")
		return
	}
	if !ok {
		writeJSON(w, 2324, nil, "配置项不存在")
		return
	}
	value, ok := decodeConfigValue(req.CurrentValue, item.ValueType)
	if !ok {
		writeJSON(w, 2325, nil, "配置值缺失或与类型不匹配")
		return
	}
	updated, err := p.updateConfigValue(r.Context(), req.ConfigKey, value)
	if errors.Is(err, errConfigNotFound) {
		writeJSON(w, 2324, nil, "配置项不存在")
		return
	}
	if err != nil {
		writeJSON(w, 2326, nil, "更新配置失败")
		return
	}
	writeJSON(w, 0, map[string]any{
		"config_key":    updated.ConfigKey,
		"value_type":    updated.ValueType,
		"current_value": updated.CurrentValue,
		"updated_at":    updated.UpdatedAt.Format(time.RFC3339),
	}, "")
}

func (p *GlobalConfigPlugin) handleConfigResetDefault(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperatorSessionToken string `json:"operator_session_token"`
		ConfigKey            string `json:"config_key"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSON(w, 2333, nil, "配置键名缺失或格式不合法")
		return
	}
	if !p.requirePermissionForToken(w, r, req.OperatorSessionToken, "global_config.reset", 2331, 2332, 2335, "操作人无恢复默认值权限") {
		return
	}
	req.ConfigKey = strings.TrimSpace(req.ConfigKey)
	if !configKeyRE.MatchString(req.ConfigKey) {
		writeJSON(w, 2333, nil, "配置键名缺失或格式不合法")
		return
	}
	updated, err := p.resetConfigDefault(r.Context(), req.ConfigKey)
	if errors.Is(err, errConfigNotFound) {
		writeJSON(w, 2334, nil, "配置项不存在")
		return
	}
	if err != nil {
		writeJSON(w, 2335, nil, "恢复默认值失败")
		return
	}
	writeJSON(w, 0, map[string]any{
		"config_key":    updated.ConfigKey,
		"value_type":    updated.ValueType,
		"current_value": updated.CurrentValue,
		"default_value": updated.DefaultValue,
		"updated_at":    updated.UpdatedAt.Format(time.RFC3339),
	}, "")
}

func parseConfigPage(r *http.Request) (int, int, bool) {
	page, err1 := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, err2 := strconv.Atoi(r.URL.Query().Get("page_size"))
	if err1 != nil || err2 != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		return 0, 0, false
	}
	return page, pageSize, true
}

func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	return dec.Decode(dst)
}
