package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelMappingRewritesUpstreamModel(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)

	var received atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		received.Store(req.Model)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-map", "object": "chat.completion", "model": req.Model,
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]string{"role": "assistant", "content": "mapped"}, "finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	status, body := request(t, http.MethodPost, app.URL+"/api/channels", admin, map[string]any{
		"name": "mapped", "base_url": upstream.URL, "api_key": "upstream-key",
		"models": []string{"gpt-alias"}, "model_mapping": map[string]string{"gpt-alias": "gpt-upstream"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create mapped channel status = %d body=%v", status, body)
	}
	if mapping, _ := body["model_mapping"].(map[string]any); mapping["gpt-alias"] != "gpt-upstream" {
		t.Fatalf("model_mapping not returned: %v", body)
	}
	key := createToken(t, app.URL, admin, 0)

	status, body = request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
		"model": "gpt-alias", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusOK {
		t.Fatalf("mapped request status = %d body=%v", status, body)
	}
	if got, _ := received.Load().(string); got != "gpt-upstream" {
		t.Fatalf("upstream received model %q, want gpt-upstream", got)
	}
}

func TestChannelConnectivityTest(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	id := createChannel(t, app.URL, admin, "healthy", upstream.URL, 1)

	status, body := request(t, http.MethodPost, fmt.Sprintf("%s/api/channels/%d/test", app.URL, id), admin, nil)
	if status != http.StatusOK {
		t.Fatalf("channel test status = %d body=%v", status, body)
	}
	if body["ok"] != true {
		t.Fatalf("channel test not ok: %v", body)
	}
	if code, _ := body["status_code"].(float64); code != 200 {
		t.Fatalf("channel test status_code = %v", body["status_code"])
	}
	models, _ := body["models"].([]any)
	if len(models) != 1 || models[0] != "gpt-test" {
		t.Fatalf("channel test models = %v", models)
	}

	bad, _ := mockUpstream(t, true)
	defer bad.Close()
	badID := createChannel(t, app.URL, admin, "broken", bad.URL, 1)
	status, body = request(t, http.MethodPost, fmt.Sprintf("%s/api/channels/%d/test", app.URL, badID), admin, nil)
	if status != http.StatusOK {
		t.Fatalf("broken channel test status = %d body=%v", status, body)
	}
	if body["ok"] != false {
		t.Fatalf("broken channel test should fail: %v", body)
	}
	if code, _ := body["status_code"].(float64); code != 500 {
		t.Fatalf("broken channel status_code = %v", body["status_code"])
	}
}

func TestAdminPasswordChange(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)

	status, _ := request(t, http.MethodPut, app.URL+"/api/auth/password", admin, map[string]string{
		"old_password": "wrong", "new_password": "newpass123",
	})
	if status != http.StatusBadRequest && status != http.StatusUnauthorized {
		t.Fatalf("wrong old password status = %d", status)
	}
	status, body := request(t, http.MethodPut, app.URL+"/api/auth/password", admin, map[string]string{
		"old_password": "admin123", "new_password": "newpass123",
	})
	if status != http.StatusOK {
		t.Fatalf("change password status = %d body=%v", status, body)
	}
	if status, _ := request(t, http.MethodPost, app.URL+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "admin123",
	}); status != http.StatusUnauthorized {
		t.Fatalf("old password still works, status = %d", status)
	}
	if status, _ := request(t, http.MethodPost, app.URL+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "newpass123",
	}); status != http.StatusOK {
		t.Fatalf("new password login status = %d", status)
	}
}

func TestDashboardDailyTrend(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	createChannel(t, app.URL, admin, "mock", upstream.URL, 1)
	key := createToken(t, app.URL, admin, 0)

	for _, stream := range []bool{false, true} {
		status, body := request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
			"model": "gpt-test", "stream": stream,
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		})
		if status != http.StatusOK {
			t.Fatalf("request stream=%v status = %d body=%v", stream, status, body)
		}
	}
	waitFor(t, func() bool {
		_, logs := request(t, http.MethodGet, app.URL+"/api/logs", admin, nil)
		items, _ := logs["items"].([]any)
		return len(items) == 2
	})

	_, dashboard := request(t, http.MethodGet, app.URL+"/api/dashboard", admin, nil)
	daily, ok := dashboard["daily"].([]any)
	if !ok || len(daily) != 7 {
		t.Fatalf("daily trend = %v", dashboard["daily"])
	}
	today := time.Now().Format("2006-01-02")
	found := false
	for _, raw := range daily {
		point := raw.(map[string]any)
		if point["date"] == today {
			found = true
			if point["requests"].(float64) != 2 {
				t.Fatalf("today requests = %v", point["requests"])
			}
			if point["tokens"].(float64) != 21 {
				t.Fatalf("today tokens = %v", point["tokens"])
			}
		}
	}
	if !found {
		t.Fatalf("today %s missing from daily trend: %v", today, daily)
	}
}

func TestChannelModelMappingValidation(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	status, body := request(t, http.MethodPost, app.URL+"/api/channels", admin, map[string]any{
		"name": "blank-mapping", "base_url": upstream.URL, "api_key": "upstream-key",
		"models":        []string{"gpt-test"},
		"model_mapping": map[string]string{"  ": " ", "gpt-test": "  "},
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body=%v", status, body)
	}
	mapping, _ := body["model_mapping"].(map[string]any)
	if len(mapping) != 0 {
		t.Fatalf("blank mapping should be dropped: %v", mapping)
	}
}

func TestChannelTestNotFound(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	status, _ := request(t, http.MethodPost, app.URL+"/api/channels/9999/test", admin, nil)
	if status != http.StatusNotFound {
		t.Fatalf("missing channel test status = %d", status)
	}
}
