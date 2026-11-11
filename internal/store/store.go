// Package store implements SQLite persistence for channels, tokens, logs and settings.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Channel is an upstream OpenAI-compatible provider.
type Channel struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"-"`
	Models       []string          `json:"models"`
	ModelMapping map[string]string `json:"model_mapping"`
	Priority     int               `json:"priority"`
	Weight       int               `json:"weight"`
	Status       int               `json:"status"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

// Token is a gateway API key with optional total-token quota.
type Token struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	KeyHash    string `json:"-"`
	KeyPrefix  string `json:"key_prefix"`
	Quota      int64  `json:"quota"`
	UsedQuota  int64  `json:"used_quota"`
	Status     int    `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	LastUsedAt string `json:"last_used_at"`
}

// Log records one gateway request.
type Log struct {
	ID               int64  `json:"id"`
	TokenID          int64  `json:"token_id"`
	TokenName        string `json:"token_name"`
	ChannelID        int64  `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	Model            string `json:"model"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	StatusCode       int    `json:"status_code"`
	LatencyMS        int64  `json:"latency_ms"`
	Stream           bool   `json:"stream"`
	Error            string `json:"error"`
	CreatedAt        string `json:"created_at"`
}

// DailyPoint is one day of request/token traffic on the overview chart.
type DailyPoint struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// Dashboard aggregates counters for the overview page.
type Dashboard struct {
	ChannelsTotal   int64 `json:"channels_total"`
	ChannelsEnabled int64 `json:"channels_enabled"`
	TokensTotal     int64 `json:"tokens_total"`
	TokensEnabled   int64 `json:"tokens_enabled"`
	RequestsTotal   int64 `json:"requests_total"`
	RequestsToday   int64 `json:"requests_today"`
	TokensSum       int64 `json:"tokens_sum"`
	TokensToday     int64 `json:"tokens_today"`
	Recent          []Log
	Daily           []DailyPoint
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'openai',
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			models TEXT NOT NULL DEFAULT '[]',
			model_mapping TEXT NOT NULL DEFAULT '{}',
			priority INTEGER NOT NULL DEFAULT 0,
			weight INTEGER NOT NULL DEFAULT 1,
			status INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			quota INTEGER NOT NULL DEFAULT 0,
			used_quota INTEGER NOT NULL DEFAULT 0,
			status INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER NOT NULL DEFAULT 0,
			token_name TEXT NOT NULL DEFAULT '',
			channel_id INTEGER NOT NULL DEFAULT 0,
			channel_name TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			stream INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE channels ADD COLUMN model_mapping TEXT NOT NULL DEFAULT '{}'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate model_mapping: %w", err)
	}
	return nil
}

func now() string { return time.Now().Format(time.RFC3339) }

// GetSetting returns a setting value and whether it exists.
func (s *Store) GetSetting(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSetting upserts a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func encodeModels(models []string) (string, error) {
	if models == nil {
		models = []string{}
	}
	data, err := json.Marshal(models)
	return string(data), err
}

func decodeModels(raw string) []string {
	models := []string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &models)
	}
	return models
}

func encodeMapping(mapping map[string]string) (string, error) {
	if mapping == nil {
		mapping = map[string]string{}
	}
	data, err := json.Marshal(mapping)
	return string(data), err
}

func decodeMapping(raw string) map[string]string {
	mapping := map[string]string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &mapping)
	}
	if mapping == nil {
		mapping = map[string]string{}
	}
	return mapping
}

func scanChannel(scanner interface{ Scan(...any) error }) (Channel, error) {
	var ch Channel
	var models, mapping string
	err := scanner.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &models, &mapping, &ch.Priority, &ch.Weight, &ch.Status, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		return ch, err
	}
	ch.Models = decodeModels(models)
	ch.ModelMapping = decodeMapping(mapping)
	return ch, nil
}

const channelColumns = `id, name, type, base_url, api_key, models, model_mapping, priority, weight, status, created_at, updated_at`

// CreateChannel inserts a channel and fills its ID and timestamps.
func (s *Store) CreateChannel(ch *Channel) error {
	models, err := encodeModels(ch.Models)
	if err != nil {
		return err
	}
	mapping, err := encodeMapping(ch.ModelMapping)
	if err != nil {
		return err
	}
	if ch.Type == "" {
		ch.Type = "openai"
	}
	stamp := now()
	ch.CreatedAt, ch.UpdatedAt = stamp, stamp
	res, err := s.db.Exec(`INSERT INTO channels(name, type, base_url, api_key, models, model_mapping, priority, weight, status, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.Name, ch.Type, ch.BaseURL, ch.APIKey, models, mapping, ch.Priority, ch.Weight, ch.Status, ch.CreatedAt, ch.UpdatedAt)
	if err != nil {
		return err
	}
	ch.ID, err = res.LastInsertId()
	return err
}

// GetChannel loads one channel by ID.
func (s *Store) GetChannel(id int64) (*Channel, error) {
	row := s.db.QueryRow(`SELECT `+channelColumns+` FROM channels WHERE id = ?`, id)
	ch, err := scanChannel(row)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// UpdateChannel replaces a channel's mutable fields.
func (s *Store) UpdateChannel(ch *Channel) error {
	models, err := encodeModels(ch.Models)
	if err != nil {
		return err
	}
	mapping, err := encodeMapping(ch.ModelMapping)
	if err != nil {
		return err
	}
	ch.UpdatedAt = now()
	_, err = s.db.Exec(`UPDATE channels SET name = ?, base_url = ?, api_key = ?, models = ?, model_mapping = ?, priority = ?, weight = ?, status = ?, updated_at = ? WHERE id = ?`,
		ch.Name, ch.BaseURL, ch.APIKey, models, mapping, ch.Priority, ch.Weight, ch.Status, ch.UpdatedAt, ch.ID)
	return err
}

// DeleteChannel removes a channel.
func (s *Store) DeleteChannel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	return err
}

// ListChannels returns a page of channels filtered by a name/URL search term.
func (s *Store) ListChannels(search string, page, pageSize int) ([]Channel, int64, error) {
	where := ""
	args := []any{}
	if search != "" {
		where = ` WHERE name LIKE ? OR base_url LIKE ?`
		like := "%" + search + "%"
		args = append(args, like, like)
	}
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM channels`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + channelColumns + ` FROM channels` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	channels := []Channel{}
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, 0, err
		}
		channels = append(channels, ch)
	}
	return channels, total, rows.Err()
}

// EnabledChannels returns all enabled channels ordered by priority.
func (s *Store) EnabledChannels() ([]Channel, error) {
	rows, err := s.db.Query(`SELECT ` + channelColumns + ` FROM channels WHERE status = 1 ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := []Channel{}
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func scanToken(scanner interface{ Scan(...any) error }) (Token, error) {
	var t Token
	err := scanner.Scan(&t.ID, &t.Name, &t.KeyHash, &t.KeyPrefix, &t.Quota, &t.UsedQuota, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.LastUsedAt)
	return t, err
}

const tokenColumns = `id, name, key_hash, key_prefix, quota, used_quota, status, created_at, updated_at, last_used_at`

// CreateToken inserts a token with the supplied key hash and prefix.
func (s *Store) CreateToken(name, keyHash, keyPrefix string, quota int64) (*Token, error) {
	stamp := now()
	res, err := s.db.Exec(`INSERT INTO tokens(name, key_hash, key_prefix, quota, used_quota, status, created_at, updated_at, last_used_at)
		VALUES(?, ?, ?, ?, 0, 1, ?, ?, '')`, name, keyHash, keyPrefix, quota, stamp, stamp)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetToken(id)
}

// GetToken loads a token by ID.
func (s *Store) GetToken(id int64) (*Token, error) {
	row := s.db.QueryRow(`SELECT `+tokenColumns+` FROM tokens WHERE id = ?`, id)
	t, err := scanToken(row)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTokenByHash loads a token by its key hash.
func (s *Store) GetTokenByHash(hash string) (*Token, error) {
	row := s.db.QueryRow(`SELECT `+tokenColumns+` FROM tokens WHERE key_hash = ?`, hash)
	t, err := scanToken(row)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateToken applies partial updates and returns the fresh token.
func (s *Store) UpdateToken(id int64, name *string, quota *int64, status *int) (*Token, error) {
	t, err := s.GetToken(id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		t.Name = *name
	}
	if quota != nil {
		t.Quota = *quota
	}
	if status != nil {
		t.Status = *status
	}
	t.UpdatedAt = now()
	_, err = s.db.Exec(`UPDATE tokens SET name = ?, quota = ?, status = ?, updated_at = ? WHERE id = ?`, t.Name, t.Quota, t.Status, t.UpdatedAt, t.ID)
	if err != nil {
		return nil, err
	}
	return s.GetToken(id)
}

// DeleteToken removes a token.
func (s *Store) DeleteToken(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE id = ?`, id)
	return err
}

// ListTokens returns a page of tokens.
func (s *Store) ListTokens(page, pageSize int) ([]Token, int64, error) {
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tokens`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+tokenColumns+` FROM tokens ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tokens := []Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, 0, err
		}
		tokens = append(tokens, t)
	}
	return tokens, total, rows.Err()
}

// AddTokenUsage increments a token's used quota and last-used timestamp.
func (s *Store) AddTokenUsage(id, tokens int64) error {
	_, err := s.db.Exec(`UPDATE tokens SET used_quota = used_quota + ?, last_used_at = ? WHERE id = ?`, tokens, now(), id)
	return err
}

// InsertLog stores one request log.
func (s *Store) InsertLog(l *Log) error {
	l.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO logs(token_id, token_name, channel_id, channel_name, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, stream, error, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.TokenID, l.TokenName, l.ChannelID, l.ChannelName, l.Model, l.PromptTokens, l.CompletionTokens, l.TotalTokens, l.StatusCode, l.LatencyMS, boolInt(l.Stream), l.Error, l.CreatedAt)
	if err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ListLogs returns a page of logs with optional model/status filters.
func (s *Store) ListLogs(model, status string, page, pageSize int) ([]Log, int64, error) {
	clauses := []string{}
	args := []any{}
	if model != "" {
		clauses = append(clauses, `model LIKE ?`)
		args = append(args, "%"+model+"%")
	}
	switch status {
	case "success":
		clauses = append(clauses, `status_code >= 200 AND status_code < 300`)
	case "error":
		clauses = append(clauses, `(status_code < 200 OR status_code >= 300)`)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT id, token_id, token_name, channel_id, channel_name, model, prompt_tokens, completion_tokens, total_tokens, status_code, latency_ms, stream, error, created_at FROM logs` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var l Log
		var stream int
		if err := rows.Scan(&l.ID, &l.TokenID, &l.TokenName, &l.ChannelID, &l.ChannelName, &l.Model, &l.PromptTokens, &l.CompletionTokens, &l.TotalTokens, &l.StatusCode, &l.LatencyMS, &stream, &l.Error, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		l.Stream = stream != 0
		logs = append(logs, l)
	}
	return logs, total, rows.Err()
}

// Dashboard computes overview counters and recent logs.
func (s *Store) Dashboard() (*Dashboard, error) {
	d := &Dashboard{}
	today := time.Now().Format("2006-01-02") + "%"
	queries := []struct {
		query string
		args  []any
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM channels`, nil, &d.ChannelsTotal},
		{`SELECT COUNT(*) FROM channels WHERE status = 1`, nil, &d.ChannelsEnabled},
		{`SELECT COUNT(*) FROM tokens`, nil, &d.TokensTotal},
		{`SELECT COUNT(*) FROM tokens WHERE status = 1`, nil, &d.TokensEnabled},
		{`SELECT COUNT(*) FROM logs`, nil, &d.RequestsTotal},
		{`SELECT COUNT(*) FROM logs WHERE created_at LIKE ?`, []any{today}, &d.RequestsToday},
		{`SELECT COALESCE(SUM(total_tokens), 0) FROM logs`, nil, &d.TokensSum},
		{`SELECT COALESCE(SUM(total_tokens), 0) FROM logs WHERE created_at LIKE ?`, []any{today}, &d.TokensToday},
	}
	for _, q := range queries {
		if err := s.db.QueryRow(q.query, q.args...).Scan(q.dest); err != nil {
			return nil, err
		}
	}
	recent, _, err := s.ListLogs("", "", 1, 10)
	if err != nil {
		return nil, err
	}
	d.Recent = recent
	daily, err := s.dailyStats(7)
	if err != nil {
		return nil, err
	}
	d.Daily = daily
	return d, nil
}

func (s *Store) dailyStats(days int) ([]DailyPoint, error) {
	start := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.db.Query(`SELECT substr(created_at, 1, 10) AS day, COUNT(*), COALESCE(SUM(total_tokens), 0)
		FROM logs WHERE substr(created_at, 1, 10) >= ? GROUP BY day`, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDate := map[string]DailyPoint{}
	for rows.Next() {
		var point DailyPoint
		if err := rows.Scan(&point.Date, &point.Requests, &point.Tokens); err != nil {
			return nil, err
		}
		byDate[point.Date] = point
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	points := make([]DailyPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		point, ok := byDate[date]
		if !ok {
			point = DailyPoint{Date: date}
		}
		points = append(points, point)
	}
	return points, nil
}
