package main

import (
	"encoding/json"
	"strings"
	"time"
)

func decodeConfigValue(raw json.RawMessage, valueType string) (any, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	switch valueType {
	case "string":
		s, ok := v.(string)
		return s, ok
	case "number":
		n, ok := v.(json.Number)
		if !ok {
			return nil, false
		}
		f, err := n.Float64()
		return f, err == nil
	case "boolean":
		b, ok := v.(bool)
		return b, ok
	case "json":
		return normalizeJSONValue(v), true
	default:
		return nil, false
	}
}

func normalizeJSONValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
	case []any:
		for i := range x {
			x[i] = normalizeJSONValue(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalizeJSONValue(x[k])
		}
	}
	return v
}

func configListResponse(item globalConfigItem) map[string]any {
	return map[string]any{
		"config_key":    item.ConfigKey,
		"value_type":    item.ValueType,
		"current_value": item.CurrentValue,
		"default_value": item.DefaultValue,
		"description":   item.Description,
		"module_name":   item.ModuleName,
		"updated_at":    item.UpdatedAt.Format(time.RFC3339),
	}
}

func configDetailResponse(item globalConfigItem) map[string]any {
	resp := configListResponse(item)
	resp["created_at"] = item.CreatedAt.Format(time.RFC3339)
	return resp
}
