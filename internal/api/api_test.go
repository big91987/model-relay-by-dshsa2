package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/big91987/model-relay-by-dshsa2/internal/config"
	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

func newTestApp(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		Port:          "0",
		DatabasePath:  filepath.Join(dir, "test.db"),
		AdminUsername: "admin",
		AdminPassword: "admin123",
		WebDist:       filepath.Join(dir, "web"),
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	server, err := New(cfg, st)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func request(t *testing.T, method, url, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

func login(t *testing.T, base string) string {
	t.Helper()
	status, body := request(t, http.MethodPost, base+"/api/auth/login", "", map[string]string{"username": "admin", "password": "admin123"})
	if status != http.StatusOK {
		t.Fatalf("login status = %d body=%v", status, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("login returned no token: %v", body)
	}
	return token
}

func createChannel(t *testing.T, base, adminToken, name, upstreamURL string, priority int) int64 {
	t.Helper()
	status, body := request(t, http.MethodPost, base+"/api/channels", adminToken, map[string]any{
		"name": name, "base_url": upstreamURL, "api_key": "upstream-key",
		"models": []string{"gpt-test"}, "priority": priority, "weight": 1, "status": 1,
	})
	if status != http.StatusCreated {
		t.Fatalf("create channel status = %d body=%v", status, body)
	}
	return int64(body["id"].(float64))
}

func createToken(t *testing.T, base, adminToken string, quota int64) string {
	t.Helper()
	status, body := request(t, http.MethodPost, base+"/api/tokens", adminToken, map[string]any{"name": "test-key", "quota": quota})
	if status != http.StatusCreated {
		t.Fatalf("create token status = %d body=%v", status, body)
	}
	key, _ := body["key"].(string)
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("token key = %q", key)
	}
	return key
}

func mockUpstream(t *testing.T, fail bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			http.Error(w, "bad upstream auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &req)
		if fail {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
			return
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "data: {\"id\":\"chunk-1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"chunk-2\",\"choices\":[{\"delta\":{\"content\":\" world\"}}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "model": req.Model,
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]string{"role": "assistant", "content": "hello"}, "finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12},
		})
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			http.Error(w, "bad upstream auth", http.StatusUnauthorized)
			return
		}
		if fail {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]string{{"id": "gpt-test", "object": "model"}},
		})
	})
	return httptest.NewServer(mux), &calls
}

func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestAdminAuthAndChannelCRUD(t *testing.T) {
	app, _ := newTestApp(t)
	if status, _ := request(t, http.MethodPost, app.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "wrong"}); status != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", status)
	}
	admin := login(t, app.URL)
	if status, _ := request(t, http.MethodGet, app.URL+"/api/channels", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("unauthorized channels status = %d", status)
	}

	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	id := createChannel(t, app.URL, admin, "mock", upstream.URL, 5)

	status, body := request(t, http.MethodGet, app.URL+"/api/channels", admin, nil)
	if status != http.StatusOK {
		t.Fatalf("list channels status = %d body=%v", status, body)
	}
	if total := int(body["total"].(float64)); total != 1 {
		t.Fatalf("channel total = %d", total)
	}
	items := body["items"].([]any)
	first := items[0].(map[string]any)
	if first["api_key"] != nil {
		t.Fatalf("api_key leaked in channel response: %v", first)
	}
	if masked, _ := first["api_key_masked"].(string); strings.Contains(masked, "upstream-key") {
		t.Fatalf("masked key leaks secret: %q", masked)
	}

	status, body = request(t, http.MethodPut, fmt.Sprintf("%s/api/channels/%d", app.URL, id), admin, map[string]any{
		"name": "mock-renamed", "priority": 9, "models": []string{"gpt-test", "gpt-other"},
	})
	if status != http.StatusOK {
		t.Fatalf("update channel status = %d body=%v", status, body)
	}
	if body["name"] != "mock-renamed" {
		t.Fatalf("renamed channel = %v", body["name"])
	}
	if models, _ := body["models"].([]any); len(models) != 2 {
		t.Fatalf("models = %v", models)
	}

	if status, _ := request(t, http.MethodDelete, fmt.Sprintf("%s/api/channels/%d", app.URL, id), admin, nil); status != http.StatusNoContent {
		t.Fatalf("delete channel status = %d", status)
	}
}

func TestNonStreamProxyLogsAndUsage(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	createChannel(t, app.URL, admin, "mock", upstream.URL, 1)
	key := createToken(t, app.URL, admin, 0)

	status, body := request(t, http.MethodGet, app.URL+"/v1/models", key, nil)
	if status != http.StatusOK {
		t.Fatalf("models status = %d body=%v", status, body)
	}
	data := body["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["id"] != "gpt-test" {
		t.Fatalf("models = %v", data)
	}

	status, body = request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
		"model": "gpt-test", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusOK {
		t.Fatalf("chat status = %d body=%v", status, body)
	}
	choices := body["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("content = %v", message["content"])
	}

	waitFor(t, func() bool {
		_, logs := request(t, http.MethodGet, app.URL+"/api/logs", admin, nil)
		items, _ := logs["items"].([]any)
		if len(items) == 0 {
			return false
		}
		entry := items[0].(map[string]any)
		return entry["total_tokens"].(float64) == 12 && entry["status_code"].(float64) == 200
	})
	_, tokens := request(t, http.MethodGet, app.URL+"/api/tokens", admin, nil)
	first := tokens["items"].([]any)[0].(map[string]any)
	if first["used_quota"].(float64) != 12 {
		t.Fatalf("used_quota = %v", first["used_quota"])
	}
}

func TestStreamProxy(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	createChannel(t, app.URL, admin, "mock", upstream.URL, 1)
	key := createToken(t, app.URL, admin, 0)

	payload, _ := json.Marshal(map[string]any{
		"model": "gpt-test", "stream": true, "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req, _ := http.NewRequest(http.MethodPost, app.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	for _, want := range []string{"data:", "hello", "[DONE]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %q: %q", want, text)
		}
	}

	waitFor(t, func() bool {
		_, logs := request(t, http.MethodGet, app.URL+"/api/logs?status=success", admin, nil)
		items, _ := logs["items"].([]any)
		if len(items) == 0 {
			return false
		}
		entry := items[0].(map[string]any)
		return entry["stream"] == true && entry["total_tokens"].(float64) == 9
	})
}

func TestFailover(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	bad, badCalls := mockUpstream(t, true)
	defer bad.Close()
	good, _ := mockUpstream(t, false)
	defer good.Close()
	createChannel(t, app.URL, admin, "bad", bad.URL, 10)
	createChannel(t, app.URL, admin, "good", good.URL, 1)
	key := createToken(t, app.URL, admin, 0)

	status, body := request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
		"model": "gpt-test", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusOK {
		t.Fatalf("failover status = %d body=%v", status, body)
	}
	if badCalls.Load() == 0 {
		t.Fatal("bad channel was not attempted")
	}
	waitFor(t, func() bool {
		_, logs := request(t, http.MethodGet, app.URL+"/api/logs", admin, nil)
		items, _ := logs["items"].([]any)
		if len(items) == 0 {
			return false
		}
		return items[0].(map[string]any)["channel_name"] == "good"
	})
}

func TestInvalidTokenAndQuota(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	upstream, _ := mockUpstream(t, false)
	defer upstream.Close()
	createChannel(t, app.URL, admin, "mock", upstream.URL, 1)

	status, _ := request(t, http.MethodPost, app.URL+"/v1/chat/completions", "sk-does-not-exist", map[string]any{
		"model": "gpt-test", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d", status)
	}

	key := createToken(t, app.URL, admin, 1)
	status, _ = request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
		"model": "gpt-test", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusOK {
		t.Fatalf("first quota request status = %d", status)
	}
	waitFor(t, func() bool {
		_, tokens := request(t, http.MethodGet, app.URL+"/api/tokens", admin, nil)
		first := tokens["items"].([]any)[0].(map[string]any)
		return first["used_quota"].(float64) == 12
	})
	status, body := request(t, http.MethodPost, app.URL+"/v1/chat/completions", key, map[string]any{
		"model": "gpt-test", "messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("quota exceeded status = %d body=%v", status, body)
	}
	errorBody := body["error"].(map[string]any)
	if errorBody["code"] != "insufficient_quota" {
		t.Fatalf("quota error code = %v", errorBody["code"])
	}
}

func TestTokenKeyReturnedOnce(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	_, created := request(t, http.MethodPost, app.URL+"/api/tokens", admin, map[string]any{"name": "k1", "quota": 0})
	key, _ := created["key"].(string)
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("created key = %q", key)
	}
	_, listed := request(t, http.MethodGet, app.URL+"/api/tokens", admin, nil)
	item := listed["items"].([]any)[0].(map[string]any)
	if _, exists := item["key"]; exists {
		t.Fatalf("key field repeated in list response: %v", item)
	}
	if item["key_prefix"] == "" {
		t.Fatalf("missing key_prefix: %v", item)
	}
}

func TestDashboard(t *testing.T) {
	app, _ := newTestApp(t)
	admin := login(t, app.URL)
	_, body := request(t, http.MethodGet, app.URL+"/api/dashboard", admin, nil)
	if _, ok := body["requests"].(map[string]any); !ok {
		t.Fatalf("dashboard requests shape = %v", body["requests"])
	}
	if _, ok := body["recent_logs"].([]any); !ok {
		t.Fatalf("dashboard recent_logs shape = %v", body["recent_logs"])
	}
}
