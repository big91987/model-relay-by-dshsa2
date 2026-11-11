// Package gateway implements channel selection and upstream request forwarding.
package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

// Usage is the OpenAI token usage object.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// Proxy forwards requests to upstream channels.
type Proxy struct {
	client *http.Client
}

// NewProxy creates a proxy with a streaming-friendly HTTP client.
func NewProxy() *Proxy {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   16,
	}
	return &Proxy{client: &http.Client{Transport: transport}}
}

// SelectChannels returns candidate channels for a model, ordered by priority
// and weighted-random within equal priority. "*" matches any model.
func SelectChannels(channels []store.Channel, model string) []store.Channel {
	candidates := make([]store.Channel, 0, len(channels))
	for _, ch := range channels {
		for _, m := range ch.Models {
			if m == model || m == "*" {
				candidates = append(candidates, ch)
				break
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})
	ordered := make([]store.Channel, 0, len(candidates))
	for i := 0; i < len(candidates); {
		j := i
		for j < len(candidates) && candidates[j].Priority == candidates[i].Priority {
			j++
		}
		group := append([]store.Channel(nil), candidates[i:j]...)
		for len(group) > 0 {
			total := 0
			for _, ch := range group {
				if ch.Weight > 0 {
					total += ch.Weight
				}
			}
			pick := 0
			if total <= 0 {
				pick = rand.Intn(len(group))
			} else {
				n := rand.Intn(total)
				for k, ch := range group {
					w := ch.Weight
					if w <= 0 {
						w = 1
					}
					if n < w {
						pick = k
						break
					}
					n -= w
				}
			}
			ordered = append(ordered, group[pick])
			group = append(group[:pick], group[pick+1:]...)
		}
		i = j
	}
	return ordered
}

// Result reports the outcome of one upstream attempt.
type Result struct {
	StatusCode int
	Usage      Usage
	Started    bool
	Err        error
}

// Forward sends body to the channel and writes the upstream response to w.
// A 5xx or transport error is returned without writing, so the caller may retry.
func (p *Proxy) Forward(ctx context.Context, ch store.Channel, body []byte, stream bool, w http.ResponseWriter) Result {
	endpoint := strings.TrimRight(ch.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{Err: fmt.Errorf("build upstream request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{Err: fmt.Errorf("upstream request: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return Result{StatusCode: resp.StatusCode, Err: fmt.Errorf("upstream status %d", resp.StatusCode)}
	}

	if stream {
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "text/event-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}

		result := Result{StatusCode: resp.StatusCode, Started: true}
		reader := bufio.NewReader(resp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				if _, writeErr := io.WriteString(w, line); writeErr != nil {
					result.Err = writeErr
					return result
				}
				if usage := parseSSEUsage(strings.TrimSpace(line)); usage != nil {
					result.Usage = *usage
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					result.Err = fmt.Errorf("read upstream stream: %w", readErr)
				}
				return result
			}
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{StatusCode: resp.StatusCode, Err: fmt.Errorf("read upstream body: %w", err)}
	}
	var payload struct {
		Usage *Usage `json:"usage"`
	}
	_ = json.Unmarshal(data, &payload)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
	result := Result{StatusCode: resp.StatusCode, Started: true}
	if payload.Usage != nil {
		result.Usage = *payload.Usage
	}
	return result
}

func parseSSEUsage(line string) *Usage {
	if !strings.HasPrefix(line, "data:") {
		return nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return nil
	}
	var chunk struct {
		Usage *Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil
	}
	return chunk.Usage
}

// RewriteModel replaces the top-level model field while preserving all other request fields.
func RewriteModel(body []byte, model string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	payload["model"] = encoded
	return json.Marshal(payload)
}

// ChannelTestResult reports an upstream connectivity check.
type ChannelTestResult struct {
	OK         bool     `json:"ok"`
	StatusCode int      `json:"status_code"`
	LatencyMS  int64    `json:"latency_ms"`
	Models     []string `json:"models"`
	Error      string   `json:"error"`
}

// TestChannel checks GET {base_url}/v1/models with the channel API key.
func (p *Proxy) TestChannel(ctx context.Context, ch store.Channel) ChannelTestResult {
	started := time.Now()
	result := ChannelTestResult{Models: []string{}}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(ch.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.LatencyMS = time.Since(started).Milliseconds()
		result.Error = "build upstream request: " + err.Error()
		return result
	}
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = "upstream request: " + err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Error = "read upstream response: " + err.Error()
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(data))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		result.Error = fmt.Sprintf("upstream returned %d %s", resp.StatusCode, snippet)
		return result
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		result.Error = "invalid models response: " + err.Error()
		return result
	}
	seen := map[string]bool{}
	for _, item := range payload.Data {
		if item.ID != "" && !seen[item.ID] {
			seen[item.ID] = true
			result.Models = append(result.Models, item.ID)
		}
	}
	for _, model := range payload.Models {
		if model != "" && !seen[model] {
			seen[model] = true
			result.Models = append(result.Models, model)
		}
	}
	sort.Strings(result.Models)
	result.OK = true
	return result
}
