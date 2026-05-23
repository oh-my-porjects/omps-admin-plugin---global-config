package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	errConfigNotFound = errors.New("config not found")
	configKeyRE       = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	moduleNameRE      = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
)

type globalConfigItem struct {
	ID           string
	ConfigKey    string
	ValueType    string
	CurrentValue any
	DefaultValue any
	Description  string
	ModuleName   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (p *GlobalConfigPlugin) ensureSchema(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	_, err := p.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS global_configs_items (
			id TEXT NOT NULL,
			config_key TEXT PRIMARY KEY,
			value_type TEXT NOT NULL CHECK (value_type IN ('string', 'number', 'boolean', 'json')),
			current_value JSONB NOT NULL,
			default_value JSONB NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			module_name TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return err
	}
	return p.seedDefaultConfigs(ctx)
}

func (p *GlobalConfigPlugin) seedDefaultConfigs(ctx context.Context) error {
	for _, item := range defaultConfigItems(time.Now().UTC()) {
		currentRaw, err := json.Marshal(item.CurrentValue)
		if err != nil {
			return err
		}
		defaultRaw, err := json.Marshal(item.DefaultValue)
		if err != nil {
			return err
		}
		if _, err := p.db.ExecContext(ctx, `
			INSERT INTO global_configs_items
				(id, config_key, value_type, current_value, default_value, description, module_name, created_at, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, now(), now())
			ON CONFLICT (config_key) DO UPDATE SET
				value_type = EXCLUDED.value_type,
				default_value = EXCLUDED.default_value,
				description = EXCLUDED.description,
				module_name = EXCLUDED.module_name,
				updated_at = global_configs_items.updated_at`,
			item.ID, item.ConfigKey, item.ValueType, string(currentRaw), string(defaultRaw), item.Description, item.ModuleName); err != nil {
			return err
		}
	}
	return nil
}

func (p *GlobalConfigPlugin) listConfigs(ctx context.Context, moduleName, keyword string, page, pageSize int) ([]map[string]any, int, error) {
	if p.db != nil {
		where, args := "TRUE", []any{}
		if moduleName != "" {
			args = append(args, moduleName)
			where += " AND module_name=$" + strconv.Itoa(len(args))
		}
		if keyword != "" {
			args = append(args, "%"+keyword+"%")
			where += " AND (lower(config_key) LIKE $" + strconv.Itoa(len(args)) + " OR lower(description) LIKE $" + strconv.Itoa(len(args)) + ")"
		}
		var total int
		if err := p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM global_configs_items WHERE "+where, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
		args = append(args, pageSize, (page-1)*pageSize)
		rows, err := p.db.QueryContext(ctx, `
			SELECT id::text, config_key, value_type, current_value::text, default_value::text, description, module_name, created_at, updated_at
			FROM global_configs_items WHERE `+where+` ORDER BY module_name, config_key LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			item, err := scanConfigItem(rows)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, configListResponse(item))
		}
		return items, total, rows.Err()
	}
	p.ensureMemoryStore()
	p.mu.Lock()
	defer p.mu.Unlock()
	all := make([]globalConfigItem, 0, len(p.items))
	for _, item := range p.items {
		if moduleName != "" && item.ModuleName != moduleName {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(item.ConfigKey+" "+item.Description), keyword) {
			continue
		}
		all = append(all, item)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].ModuleName == all[j].ModuleName {
			return all[i].ConfigKey < all[j].ConfigKey
		}
		return all[i].ModuleName < all[j].ModuleName
	})
	total := len(all)
	start := (page - 1) * pageSize
	if start > total {
		return []map[string]any{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	resp := make([]map[string]any, 0, end-start)
	for _, item := range all[start:end] {
		resp = append(resp, configListResponse(item))
	}
	return resp, total, nil
}

func (p *GlobalConfigPlugin) ensureMemoryStore() {
	if p.items != nil {
		return
	}
	now := time.Now().UTC()
	p.items = defaultConfigItems(now)
}

func defaultConfigItems(now time.Time) map[string]globalConfigItem {
	return map[string]globalConfigItem{
		"global_config.feature_enabled": {
			ID:           "global_config.feature_enabled",
			ConfigKey:    "global_config.feature_enabled",
			ValueType:    "boolean",
			CurrentValue: true,
			DefaultValue: true,
			Description:  "全局配置功能开关",
			ModuleName:   "global_config",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"global_config.max_page_size": {
			ID:           "global_config.max_page_size",
			ConfigKey:    "global_config.max_page_size",
			ValueType:    "number",
			CurrentValue: float64(100),
			DefaultValue: float64(100),
			Description:  "配置列表最大分页大小",
			ModuleName:   "global_config",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"global_config.ui_theme": {
			ID:           "global_config.ui_theme",
			ConfigKey:    "global_config.ui_theme",
			ValueType:    "string",
			CurrentValue: "system",
			DefaultValue: "system",
			Description:  "后台配置界面默认主题",
			ModuleName:   "global_config",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.account_min_length": {
			ID:           "user.account_min_length",
			ConfigKey:    "user.account_min_length",
			ValueType:    "number",
			CurrentValue: float64(4),
			DefaultValue: float64(4),
			Description:  "用户账号最小长度",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.account_max_length": {
			ID:           "user.account_max_length",
			ConfigKey:    "user.account_max_length",
			ValueType:    "number",
			CurrentValue: float64(32),
			DefaultValue: float64(32),
			Description:  "用户账号最大长度",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.password_min_length": {
			ID:           "user.password_min_length",
			ConfigKey:    "user.password_min_length",
			ValueType:    "number",
			CurrentValue: float64(8),
			DefaultValue: float64(8),
			Description:  "用户密码最小长度",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.password_max_length": {
			ID:           "user.password_max_length",
			ConfigKey:    "user.password_max_length",
			ValueType:    "number",
			CurrentValue: float64(32),
			DefaultValue: float64(32),
			Description:  "用户密码最大长度",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.allow_register": {
			ID:           "user.allow_register",
			ConfigKey:    "user.allow_register",
			ValueType:    "boolean",
			CurrentValue: true,
			DefaultValue: true,
			Description:  "是否允许新用户注册",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.access_token_ttl_seconds": {
			ID:           "user.access_token_ttl_seconds",
			ConfigKey:    "user.access_token_ttl_seconds",
			ValueType:    "number",
			CurrentValue: float64(3600),
			DefaultValue: float64(3600),
			Description:  "用户 access token 有效期秒数",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.refresh_token_ttl_seconds": {
			ID:           "user.refresh_token_ttl_seconds",
			ConfigKey:    "user.refresh_token_ttl_seconds",
			ValueType:    "number",
			CurrentValue: float64(10800),
			DefaultValue: float64(10800),
			Description:  "用户 refresh token 有效期秒数",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		"user.token_refresh_threshold_seconds": {
			ID:           "user.token_refresh_threshold_seconds",
			ConfigKey:    "user.token_refresh_threshold_seconds",
			ValueType:    "number",
			CurrentValue: float64(300),
			DefaultValue: float64(300),
			Description:  "WebSocket 自动续期触发阈值秒数",
			ModuleName:   "user",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
}

func (p *GlobalConfigPlugin) getConfig(ctx context.Context, configKey string) (globalConfigItem, bool, error) {
	if p.db != nil {
		row := p.db.QueryRowContext(ctx, `
			SELECT id::text, config_key, value_type, current_value::text, default_value::text, description, module_name, created_at, updated_at
			FROM global_configs_items WHERE config_key=$1`, configKey)
		item, err := scanConfigItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return globalConfigItem{}, false, nil
		}
		return item, err == nil, err
	}
	p.ensureMemoryStore()
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.items[configKey]
	return item, ok, nil
}

func (p *GlobalConfigPlugin) updateConfigValue(ctx context.Context, configKey string, value any) (globalConfigItem, error) {
	if p.db != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return globalConfigItem{}, err
		}
		row := p.db.QueryRowContext(ctx, `
			UPDATE global_configs_items SET current_value=$2::jsonb, updated_at=now()
			WHERE config_key=$1
			RETURNING id::text, config_key, value_type, current_value::text, default_value::text, description, module_name, created_at, updated_at`,
			configKey, string(raw))
		item, err := scanConfigItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return globalConfigItem{}, errConfigNotFound
		}
		return item, err
	}
	p.ensureMemoryStore()
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.items[configKey]
	if !ok {
		return globalConfigItem{}, errConfigNotFound
	}
	item.CurrentValue = value
	item.UpdatedAt = time.Now().UTC()
	p.items[configKey] = item
	return item, nil
}

func (p *GlobalConfigPlugin) resetConfigDefault(ctx context.Context, configKey string) (globalConfigItem, error) {
	if p.db != nil {
		row := p.db.QueryRowContext(ctx, `
			UPDATE global_configs_items SET current_value=default_value, updated_at=now()
			WHERE config_key=$1
			RETURNING id::text, config_key, value_type, current_value::text, default_value::text, description, module_name, created_at, updated_at`,
			configKey)
		item, err := scanConfigItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return globalConfigItem{}, errConfigNotFound
		}
		return item, err
	}
	p.ensureMemoryStore()
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.items[configKey]
	if !ok {
		return globalConfigItem{}, errConfigNotFound
	}
	item.CurrentValue = item.DefaultValue
	item.UpdatedAt = time.Now().UTC()
	p.items[configKey] = item
	return item, nil
}

type configScanner interface {
	Scan(dest ...any) error
}

func scanConfigItem(scanner configScanner) (globalConfigItem, error) {
	var item globalConfigItem
	var currentRaw, defaultRaw string
	if err := scanner.Scan(&item.ID, &item.ConfigKey, &item.ValueType, &currentRaw, &defaultRaw, &item.Description, &item.ModuleName, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return globalConfigItem{}, err
	}
	if err := json.Unmarshal([]byte(currentRaw), &item.CurrentValue); err != nil {
		return globalConfigItem{}, err
	}
	if err := json.Unmarshal([]byte(defaultRaw), &item.DefaultValue); err != nil {
		return globalConfigItem{}, err
	}
	return item, nil
}
