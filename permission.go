package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errOperatorSessionInvalid = errors.New("operator session invalid")

type selftestAdminPermissionContextKey struct{}

type adminAccountPermissionResponse struct {
	Status int             `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type adminAccountPermissionData struct {
	Allowed bool `json:"allowed"`
}

func (p *GlobalConfigPlugin) requirePermission(w http.ResponseWriter, r *http.Request, permission string, invalidCode, deniedCode, failureCode int, deniedMsg string) bool {
	return p.requirePermissionForToken(w, r, r.URL.Query().Get("operator_session_token"), permission, invalidCode, deniedCode, failureCode, deniedMsg)
}

func (p *GlobalConfigPlugin) requirePermissionForToken(w http.ResponseWriter, r *http.Request, token, permission string, invalidCode, deniedCode, failureCode int, deniedMsg string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		if hasAPIKey(r) {
			return true
		}
		writeJSON(w, invalidCode, nil, "操作人会话无效或已过期")
		return false
	}
	allowed, err := p.checkOperatorPermission(r.Context(), r, token, permission)
	if errors.Is(err, errOperatorSessionInvalid) {
		writeJSON(w, invalidCode, nil, "操作人会话无效或已过期")
		return false
	}
	if err != nil {
		writeJSON(w, failureCode, nil, "权限校验失败")
		return false
	}
	if !allowed {
		writeJSON(w, deniedCode, nil, deniedMsg)
		return false
	}
	return true
}

func hasAPIKey(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-API-Key")) != ""
}

func (p *GlobalConfigPlugin) checkOperatorPermission(ctx context.Context, r *http.Request, token, permission string) (bool, error) {
	body := map[string]string{"session_token": token, "permission_code": permission}
	var resp adminAccountPermissionResponse
	if err := p.doAdminAccountRequest(ctx, r, http.MethodPost, "/api/admin-account/check-permission", body, &resp); err != nil {
		return false, err
	}
	switch resp.Status {
	case 0:
		var data adminAccountPermissionData
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			return false, err
		}
		return data.Allowed, nil
	case 2271, 2272, 2274:
		return false, errOperatorSessionInvalid
	case 2273:
		return false, errors.New("admin_account rejected permission code")
	default:
		return false, errors.New("admin_account permission check failed")
	}
}

func (p *GlobalConfigPlugin) doAdminAccountRequest(ctx context.Context, r *http.Request, method, path string, body any, out any) error {
	if ctx != nil {
		if mock, ok := ctx.Value(selftestAdminPermissionContextKey{}).(string); ok && method == http.MethodPost && path == "/api/admin-account/check-permission" {
			data, err := json.Marshal(adminAccountPermissionData{Allowed: mock == "allowed"})
			if err != nil {
				return err
			}
			resp, err := json.Marshal(adminAccountPermissionResponse{Status: 0, Data: data})
			if err != nil {
				return err
			}
			return json.Unmarshal(resp, out)
		}
	} else {
		ctx = context.Background()
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, p.runtimeURL(r, path), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.adminAPIKey) != "" {
		req.Header.Set("X-API-Key", p.adminAPIKey)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("admin_account api http status failed")
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (p *GlobalConfigPlugin) runtimeURL(r *http.Request, path string) string {
	host := strings.TrimSpace(p.runtimeAddr)
	if host == "" && r != nil {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" || host == "example.com" {
		host = "127.0.0.1:8080"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	parsed, err := url.Parse(host)
	if err == nil && parsed.Port() == "" {
		if _, _, splitErr := net.SplitHostPort(parsed.Host); splitErr != nil {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), "8080")
			host = parsed.String()
		}
	}
	return strings.TrimRight(host, "/") + path
}
