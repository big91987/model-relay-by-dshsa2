package api

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"

	"github.com/big91987/model-relay-by-dshsa2/internal/auth"
	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

type channelView struct {
	store.Channel
	APIKeyMasked string `json:"api_key_masked"`
}

func channelToView(ch store.Channel) channelView {
	return channelView{Channel: ch, APIKeyMasked: maskKey(ch.APIKey)}
}

type channelRequest struct {
	Name         string            `json:"name"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key"`
	Models       []string          `json:"models"`
	ModelMapping map[string]string `json:"model_mapping"`
	Priority     *int              `json:"priority"`
	Weight       *int              `json:"weight"`
	Status       *int              `json:"status"`
}

func normalizeMapping(mapping map[string]string) map[string]string {
	out := map[string]string{}
	for from, to := range mapping {
		from = strings.TrimSpace(from)
		to = strings.TrimSpace(to)
		if from == "" || to == "" {
			continue
		}
		out[from] = to
	}
	return out
}

func normalizeModels(models []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	hash, ok, err := s.store.GetSetting("admin_password_hash")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read admin credentials")
		return
	}
	usernameOK := constantTimeEqual(req.Username, s.cfg.AdminUsername)
	passwordOK := ok && auth.CheckPassword(hash, req.Password)
	if !usernameOK || !passwordOK {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := s.mgr.Issue(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": s.cfg.AdminUsername})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "new password must be at least 6 characters")
		return
	}
	hash, ok, err := s.store.GetSetting("admin_password_hash")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read admin credentials")
		return
	}
	if !ok || !auth.CheckPassword(hash, req.OldPassword) {
		writeError(w, http.StatusBadRequest, "old password is incorrect")
		return
	}
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot hash new password")
		return
	}
	if err := s.store.SetSetting("admin_password_hash", newHash); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update admin password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	channels, total, err := s.store.ListChannels(search, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list channels")
		return
	}
	items := make([]channelView, 0, len(channels))
	for _, ch := range channels {
		items = append(items, channelToView(ch))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize,
	})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Models = normalizeModels(req.Models)
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" || len(req.Models) == 0 {
		writeError(w, http.StatusBadRequest, "name, base_url, api_key and models are required")
		return
	}
	ch := &store.Channel{
		Name: req.Name, Type: "openai", BaseURL: req.BaseURL, APIKey: req.APIKey,
		Models: req.Models, ModelMapping: normalizeMapping(req.ModelMapping), Priority: 0, Weight: 1, Status: 1,
	}
	if req.Priority != nil {
		ch.Priority = *req.Priority
	}
	if req.Weight != nil && *req.Weight > 0 {
		ch.Weight = *req.Weight
	}
	if req.Status != nil {
		ch.Status = *req.Status
	}
	if err := s.store.CreateChannel(ch); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "channel name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot create channel")
		return
	}
	writeJSON(w, http.StatusCreated, channelToView(*ch))
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	ch, err := s.store.GetChannel(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load channel")
		return
	}
	var req channelRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		ch.Name = name
	}
	if baseURL := strings.TrimSpace(req.BaseURL); baseURL != "" {
		ch.BaseURL = baseURL
	}
	if key := strings.TrimSpace(req.APIKey); key != "" {
		ch.APIKey = key
	}
	if req.Models != nil {
		ch.Models = normalizeModels(req.Models)
	}
	if req.ModelMapping != nil {
		ch.ModelMapping = normalizeMapping(req.ModelMapping)
	}
	if req.Priority != nil {
		ch.Priority = *req.Priority
	}
	if req.Weight != nil && *req.Weight > 0 {
		ch.Weight = *req.Weight
	}
	if req.Status != nil {
		ch.Status = *req.Status
	}
	if ch.Name == "" || ch.BaseURL == "" || ch.APIKey == "" || len(ch.Models) == 0 {
		writeError(w, http.StatusBadRequest, "name, base_url, api_key and models are required")
		return
	}
	if err := s.store.UpdateChannel(ch); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "channel name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot update channel")
		return
	}
	writeJSON(w, http.StatusOK, channelToView(*ch))
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	if err := s.store.DeleteChannel(id); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot delete channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	channel, err := s.store.GetChannel(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load channel")
		return
	}
	writeJSON(w, http.StatusOK, s.proxy.TestChannel(r.Context(), *channel))
}

type tokenView struct {
	store.Token
	Key string `json:"key,omitempty"`
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	tokens, total, err := s.store.ListTokens(page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list tokens")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": tokens, "total": total, "page": page, "page_size": pageSize,
	})
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Quota *int64 `json:"quota"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	quota := int64(0)
	if req.Quota != nil {
		quota = *req.Quota
	}
	if quota < 0 {
		writeError(w, http.StatusBadRequest, "quota cannot be negative")
		return
	}
	key, prefix := auth.GenerateAPIKey()
	token, err := s.store.CreateToken(req.Name, auth.HashAPIKey(key), prefix, quota)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot create token")
		return
	}
	writeJSON(w, http.StatusCreated, tokenView{Token: *token, Key: key})
}

func (s *Server) handleUpdateToken(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Quota  *int64  `json:"quota"`
		Status *int    `json:"status"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Quota != nil && *req.Quota < 0 {
		writeError(w, http.StatusBadRequest, "quota cannot be negative")
		return
	}
	token, err := s.store.UpdateToken(id, req.Name, req.Quota, req.Status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update token")
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if err := s.store.DeleteToken(id); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot delete token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination(r)
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	logs, total, err := s.store.ListLogs(model, status, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": logs, "total": total, "page": page, "page_size": pageSize,
	})
}

type pairView struct {
	Total   int64 `json:"total"`
	Enabled int64 `json:"enabled"`
}

type requestsView struct {
	Total int64 `json:"total"`
	Today int64 `json:"today"`
}

type usageView struct {
	TotalTokens int64 `json:"total_tokens"`
	TodayTokens int64 `json:"today_tokens"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Dashboard()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load dashboard")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channels":    pairView{Total: stats.ChannelsTotal, Enabled: stats.ChannelsEnabled},
		"tokens":      pairView{Total: stats.TokensTotal, Enabled: stats.TokensEnabled},
		"requests":    requestsView{Total: stats.RequestsTotal, Today: stats.RequestsToday},
		"usage":       usageView{TotalTokens: stats.TokensSum, TodayTokens: stats.TokensToday},
		"daily":       stats.Daily,
		"recent_logs": stats.Recent,
	})
}

func (s *Server) aggregateModels() ([]string, error) {
	channels, err := s.store.EnabledChannels()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	models := []string{}
	for _, ch := range channels {
		for _, model := range ch.Models {
			if model == "*" || seen[model] {
				continue
			}
			seen[model] = true
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models, nil
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.aggregateModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list models")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}
