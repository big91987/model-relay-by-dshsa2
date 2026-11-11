package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/big91987/model-relay-by-dshsa2/internal/auth"
	"github.com/big91987/model-relay-by-dshsa2/internal/gateway"
	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

func (s *Server) gatewayToken(w http.ResponseWriter, r *http.Request) (*store.Token, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeOpenAIError(w, http.StatusUnauthorized, "missing api key", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	key := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if !strings.HasPrefix(key, "sk-") {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid api key", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	token, err := s.store.GetTokenByHash(auth.HashAPIKey(key))
	if errors.Is(err, sql.ErrNoRows) {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid api key", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "cannot verify api key", "server_error", "internal_error")
		return nil, false
	}
	if token.Status != 1 {
		writeOpenAIError(w, http.StatusUnauthorized, "api key is disabled", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	if token.Quota > 0 && token.UsedQuota >= token.Quota {
		writeOpenAIError(w, http.StatusTooManyRequests, "token quota exceeded", "insufficient_quota", "insufficient_quota")
		return nil, false
	}
	return token, true
}

func (s *Server) handleGatewayModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.gatewayToken(w, r); !ok {
		return
	}
	models, err := s.aggregateModels()
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "cannot list models", "server_error", "internal_error")
		return
	}
	data := make([]map[string]string, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]string{"id": model, "object": "model", "owned_by": "gateway"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	token, ok := s.gatewayToken(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "cannot read request body", "invalid_request_error", "invalid_request")
		return
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request_error", "invalid_request")
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "model_required")
		return
	}

	channels, err := s.store.EnabledChannels()
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "cannot load channels", "server_error", "internal_error")
		return
	}
	candidates := gateway.SelectChannels(channels, req.Model)
	if len(candidates) == 0 {
		writeOpenAIError(w, http.StatusNotFound, "model not found: "+req.Model, "invalid_request_error", "model_not_found")
		return
	}
	attempts := candidates
	if len(attempts) > 3 {
		attempts = attempts[:3]
	}

	started := time.Now()
	var lastErr error
	for _, channel := range attempts {
		attemptBody := body
		if mapped := channel.ModelMapping[req.Model]; mapped != "" && mapped != req.Model {
			rewritten, rewriteErr := gateway.RewriteModel(body, mapped)
			if rewriteErr != nil {
				lastErr = rewriteErr
				continue
			}
			attemptBody = rewritten
		}
		result := s.proxy.Forward(r.Context(), channel, attemptBody, req.Stream, w)
		if result.Err != nil && !result.Started {
			lastErr = result.Err
			continue
		}
		latency := time.Since(started).Milliseconds()
		entry := &store.Log{
			TokenID: token.ID, TokenName: token.Name,
			ChannelID: channel.ID, ChannelName: channel.Name,
			Model:        req.Model,
			PromptTokens: result.Usage.PromptTokens, CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens: result.Usage.TotalTokens,
			StatusCode:  result.StatusCode, LatencyMS: latency, Stream: req.Stream,
		}
		if result.Err != nil {
			entry.Error = result.Err.Error()
		}
		_ = s.store.InsertLog(entry)
		if result.StatusCode >= 200 && result.StatusCode < 300 {
			_ = s.store.AddTokenUsage(token.ID, result.Usage.TotalTokens)
		}
		return
	}

	latency := time.Since(started).Milliseconds()
	message := "upstream request failed"
	if lastErr != nil {
		message += ": " + lastErr.Error()
	}
	_ = s.store.InsertLog(&store.Log{
		TokenID: token.ID, TokenName: token.Name,
		Model: req.Model, StatusCode: http.StatusBadGateway,
		LatencyMS: latency, Stream: req.Stream, Error: message,
	})
	writeOpenAIError(w, http.StatusBadGateway, "all upstream channels failed", "upstream_error", "upstream_error")
}
