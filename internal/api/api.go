// Package api wires the HTTP surface: admin API, OpenAI-compatible gateway and static UI.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/big91987/model-relay-by-dshsa2/internal/auth"
	"github.com/big91987/model-relay-by-dshsa2/internal/config"
	"github.com/big91987/model-relay-by-dshsa2/internal/gateway"
	"github.com/big91987/model-relay-by-dshsa2/internal/store"
)

// Server owns the HTTP routes and dependencies.
type Server struct {
	cfg   config.Config
	store *store.Store
	mgr   *auth.Manager
	proxy *gateway.Proxy
	mux   *http.ServeMux
}

// New creates the server, seeding the admin account and JWT secret on first run.
func New(cfg config.Config, st *store.Store) (*Server, error) {
	if err := seedAdmin(st, cfg.AdminPassword); err != nil {
		return nil, err
	}
	secret, err := ensureJWTSecret(st, cfg.JWTSecret)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		mgr:   auth.NewManager(secret),
		proxy: gateway.NewProxy(),
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func seedAdmin(st *store.Store, password string) error {
	if _, ok, err := st.GetSetting("admin_password_hash"); err != nil {
		return err
	} else if ok {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	return st.SetSetting("admin_password_hash", hash)
}

func ensureJWTSecret(st *store.Store, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if secret, ok, err := st.GetSetting("jwt_secret"); err != nil {
		return "", err
	} else if ok {
		return secret, nil
	}
	secret, err := auth.RandomHex(32)
	if err != nil {
		return "", err
	}
	if err := st.SetSetting("jwt_secret", secret); err != nil {
		return "", err
	}
	return secret, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/auth/me", s.admin(s.handleMe))
	s.mux.HandleFunc("PUT /api/auth/password", s.admin(s.handleChangePassword))

	s.mux.HandleFunc("GET /api/dashboard", s.admin(s.handleDashboard))
	s.mux.HandleFunc("GET /api/models", s.admin(s.handleModels))

	s.mux.HandleFunc("GET /api/channels", s.admin(s.handleListChannels))
	s.mux.HandleFunc("POST /api/channels", s.admin(s.handleCreateChannel))
	s.mux.HandleFunc("PUT /api/channels/{id}", s.admin(s.handleUpdateChannel))
	s.mux.HandleFunc("DELETE /api/channels/{id}", s.admin(s.handleDeleteChannel))
	s.mux.HandleFunc("POST /api/channels/{id}/test", s.admin(s.handleTestChannel))

	s.mux.HandleFunc("GET /api/tokens", s.admin(s.handleListTokens))
	s.mux.HandleFunc("POST /api/tokens", s.admin(s.handleCreateToken))
	s.mux.HandleFunc("PUT /api/tokens/{id}", s.admin(s.handleUpdateToken))
	s.mux.HandleFunc("DELETE /api/tokens/{id}", s.admin(s.handleDeleteToken))

	s.mux.HandleFunc("GET /api/logs", s.admin(s.handleListLogs))

	s.mux.HandleFunc("GET /v1/models", s.handleGatewayModels)
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)

	s.mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	s.mux.HandleFunc("GET /v1/", func(w http.ResponseWriter, r *http.Request) {
		writeOpenAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "not_found")
	})
	s.mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeOpenAIError(w, http.StatusNotFound, "not found", "invalid_request_error", "not_found")
	})

	s.mux.HandleFunc("/", s.serveSPA)
}

// admin wraps a handler with Bearer JWT authentication.
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		username, err := s.mgr.Verify(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil || username != s.cfg.AdminUsername {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeOpenAIError(w http.ResponseWriter, status int, message, errType, code string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	})
}

func readJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return errors.New("empty request body")
	}
	return json.Unmarshal(body, target)
}

func pagination(r *http.Request) (page, pageSize int) {
	page, pageSize = 1, 20
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && v > 0 {
		pageSize = v
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func parsePathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:3] + "…" + key[len(key)-4:]
}

// constantTimeEqual avoids timing differences for admin username checks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
