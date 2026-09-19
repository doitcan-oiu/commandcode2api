package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func strconvI(v int) string { return strconv.Itoa(v) }
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/session", m.handleSession)
	mux.HandleFunc("POST /api/admin/setup", m.handleSetup)
	mux.HandleFunc("POST /api/admin/login", m.handleLogin)
	mux.HandleFunc("POST /api/admin/logout", m.handleLogout)
	mux.HandleFunc("POST /api/admin/password", m.handlePassword)
	mux.HandleFunc("GET /api/admin/overview", m.handleOverview)
	mux.HandleFunc("GET /api/admin/accounts", m.handleAccounts)
	mux.HandleFunc("POST /api/admin/accounts", m.handleCreateAccount)
	mux.HandleFunc("POST /api/admin/accounts/import", m.handleImport)
	mux.HandleFunc("POST /api/admin/accounts/refresh", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": m.RefreshAll(r.Context())})
	})
	mux.HandleFunc("POST /api/admin/accounts/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		a, err := m.Refresh(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"account": a})
	})
	mux.HandleFunc("PATCH /api/admin/accounts/{id}", m.handleUpdateAccount)
	mux.HandleFunc("DELETE /api/admin/accounts/{id}", m.handleDeleteAccount)
	mux.HandleFunc("GET /api/admin/clients", m.handleClients)
	mux.HandleFunc("POST /api/admin/clients", m.handleCreateClient)
	mux.HandleFunc("PATCH /api/admin/clients/{id}", m.handleUpdateClient)
	mux.HandleFunc("DELETE /api/admin/clients/{id}", m.handleDeleteClient)
	mux.HandleFunc("GET /api/admin/logs", m.handleLogs)
	mux.HandleFunc("GET /api/admin/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, m.Settings()) })
	mux.HandleFunc("PATCH /api/admin/settings", m.handleSettings)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && !sameOrigin(r) {
			writeError(w, apiError(403, "Cross-origin administrative changes are forbidden"))
			return
		}
		p := r.URL.Path
		if p != "/api/admin/session" && p != "/api/admin/setup" && p != "/api/admin/login" {
			m.mu.Lock()
			ok := m.authenticatedLocked(r)
			m.mu.Unlock()
			if !ok {
				writeError(w, apiError(401, "Administrator login required"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

type accountInput struct {
	Key           string    `json:"key"`
	Label         *string   `json:"label"`
	Enabled       *bool     `json:"enabled"`
	Weight        *int      `json:"weight"`
	Priority      *int      `json:"priority"`
	MaxConcurrent *int      `json:"maxConcurrent"`
	Models        *[]string `json:"models"`
}

func applyAccount(a *storedAccount, in accountInput) error {
	if in.Label != nil {
		a.Label = strings.TrimSpace(*in.Label)
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.Weight != nil {
		a.Weight = *in.Weight
	}
	if in.Priority != nil {
		a.Priority = *in.Priority
	}
	if in.MaxConcurrent != nil {
		a.MaxConcurrent = *in.MaxConcurrent
	}
	if in.Models != nil {
		a.Models = *in.Models
	}
	if a.Models == nil {
		a.Models = []string{}
	}
	if len(a.Label) < 1 || len(a.Label) > 120 {
		return apiError(400, "Account label must contain 1–120 characters")
	}
	if a.Weight < 1 || a.Weight > 1000 || a.Priority < -100000 || a.Priority > 100000 || a.MaxConcurrent < 0 || a.MaxConcurrent > 10000 {
		return apiError(400, "Invalid weight (1–1000), priority (-100000–100000), or concurrency (0–10000)")
	}
	return validateModels(a.Models)
}
func (m *Manager) createAccountLocked(in accountInput) (*storedAccount, error) {
	key := strings.TrimSpace(in.Key)
	if !strings.HasPrefix(key, "user_") || len(key) < 12 || len(key) > 1024 || strings.ContainsAny(key, " \t\r\n\\") {
		return nil, apiError(400, "A valid user_ API key is required")
	}
	hash := digest(key)
	for _, a := range m.accounts {
		if a.KeyHash == hash {
			return nil, apiError(409, "This upstream key has already been added")
		}
	}
	if len(m.accounts) >= 1000 {
		return nil, apiError(400, "Maximum of 1000 accounts reached")
	}
	now := time.Now().UnixMilli()
	a := &storedAccount{Account: Account{ID: id("acc_"), Label: "Account " + strconv.Itoa(len(m.accounts)+1), Enabled: true, Weight: 1, MaxConcurrent: 5, Models: []string{}, Status: "active", KeyPreview: "user_…" + key[len(key)-4:], CreatedAt: now, UpdatedAt: now}, EncryptedKey: m.encrypt(key), KeyHash: hash}
	if err := applyAccount(a, in); err != nil {
		return nil, err
	}
	if err := m.saveAccount(a); err != nil {
		return nil, err
	}
	m.accounts[a.ID] = a
	return a, nil
}
func (m *Manager) handleAccounts(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	items := m.accountListLocked()
	m.mu.Unlock()
	writeJSON(w, 200, map[string]any{"items": items})
}
func (m *Manager) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	a, err := m.createAccountLocked(in)
	var public Account
	if a != nil {
		public = publicAccount(a)
	}
	m.mu.Unlock()
	if err != nil {
		writeError(w, err)
		return
	}
	if refreshed, e := m.Refresh(r.Context(), public.ID); e == nil {
		public = refreshed
	}
	writeJSON(w, 201, map[string]any{"account": public})
}
func (m *Manager) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.Key != "" {
		writeError(w, apiError(400, "Replace credentials by creating a new account"))
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.accounts[r.PathValue("id")]
	if a == nil {
		writeError(w, apiError(404, "Account not found"))
		return
	}
	next := *a
	if err := applyAccount(&next, in); err != nil {
		writeError(w, err)
		return
	}
	next.UpdatedAt = time.Now().UnixMilli()
	if err := m.saveAccount(&next); err != nil {
		writeError(w, err)
		return
	}
	*a = next
	writeJSON(w, 200, map[string]any{"account": publicAccount(a)})
}
func (m *Manager) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := r.PathValue("id")
	a := m.accounts[key]
	if a == nil {
		writeError(w, apiError(404, "Account not found"))
		return
	}
	if a.Inflight > 0 {
		writeError(w, apiError(409, "Disable this account and wait for active requests before deleting"))
		return
	}
	if _, err := m.db.Exec("DELETE FROM accounts WHERE id=?", key); err != nil {
		writeError(w, err)
		return
	}
	delete(m.accounts, key)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (m *Manager) handleImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Keys        string `json:"keys"`
		LabelPrefix string `json:"labelPrefix"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	keys := strings.FieldsFunc(in.Keys, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ' ' || r == '\t' })
	if len(keys) == 0 || len(keys) > 100 {
		writeError(w, apiError(400, "Import between 1 and 100 keys"))
		return
	}
	prefix := strings.TrimSpace(in.LabelPrefix)
	if prefix == "" {
		prefix = "Imported"
	}
	if len(prefix) > 100 {
		writeError(w, apiError(400, "Label prefix is too long"))
		return
	}
	created, skipped := 0, 0
	failures := []string{}
	m.mu.Lock()
	for i, key := range keys {
		label := fmt.Sprintf("%s %d", prefix, i+1)
		_, err := m.createAccountLocked(accountInput{Key: key, Label: &label})
		if err == nil {
			created++
		} else {
			skipped++
			var message = "Storage error"
			if e, ok := err.(*Error); ok {
				message = e.Message
			}
			failures = append(failures, fmt.Sprintf("Line %d: %s", i+1, message))
		}
	}
	m.mu.Unlock()
	writeJSON(w, 200, map[string]any{"created": created, "skipped": skipped, "errors": failures})
}

type clientInput struct {
	Name          *string   `json:"name"`
	Enabled       *bool     `json:"enabled"`
	RPM           *int      `json:"rpm"`
	MaxConcurrent *int      `json:"maxConcurrent"`
	MaxRequests   *int64    `json:"maxRequests"`
	MaxTokens     *int64    `json:"maxTokens"`
	Models        *[]string `json:"models"`
	ExpiresAt     *int64    `json:"expiresAt"`
}

func applyClient(c *storedClient, in clientInput) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if in.RPM != nil {
		c.RPM = *in.RPM
	}
	if in.MaxConcurrent != nil {
		c.MaxConcurrent = *in.MaxConcurrent
	}
	if in.MaxRequests != nil {
		c.MaxRequests = *in.MaxRequests
	}
	if in.MaxTokens != nil {
		c.MaxTokens = *in.MaxTokens
	}
	if in.Models != nil {
		c.Models = *in.Models
	}
	if c.Models == nil {
		c.Models = []string{}
	}
	if in.ExpiresAt != nil {
		c.ExpiresAt = *in.ExpiresAt
	}
	if len(c.Name) < 1 || len(c.Name) > 120 {
		return apiError(400, "Access token name must contain 1–120 characters")
	}
	if c.RPM < 0 || c.RPM > 1000000 || c.MaxConcurrent < 0 || c.MaxConcurrent > 10000 || c.MaxRequests < 0 || c.MaxTokens < 0 || c.ExpiresAt < 0 {
		return apiError(400, "Limits and expiration must be nonnegative; RPM <= 1000000 and concurrency <= 10000")
	}
	return validateModels(c.Models)
}
func (m *Manager) handleClients(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	items := make([]Client, 0, len(m.clients))
	for _, c := range m.clients {
		items = append(items, c.Client)
	}
	m.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	writeJSON(w, 200, map[string]any{"items": items})
}
func (m *Manager) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var in clientInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	token := "ccg_" + randomToken(32)
	c := &storedClient{Client: Client{ID: id("cli_"), Name: "Access token", Enabled: true, RPM: 60, MaxConcurrent: 5, Models: []string{}, TokenPreview: token[:8] + "…" + token[len(token)-4:], CreatedAt: time.Now().UnixMilli()}, TokenHash: digest(token)}
	if err := applyClient(c, in); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.clients) >= 10000 {
		writeError(w, apiError(400, "Maximum of 10000 access tokens reached"))
		return
	}
	if err := m.saveClient(c); err != nil {
		writeError(w, err)
		return
	}
	m.clients[c.ID] = c
	writeJSON(w, 201, map[string]any{"client": c.Client, "token": token})
}
func (m *Manager) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	var in clientInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.clients[r.PathValue("id")]
	if c == nil {
		writeError(w, apiError(404, "Access token not found"))
		return
	}
	next := *c
	if err := applyClient(&next, in); err != nil {
		writeError(w, err)
		return
	}
	if err := m.saveClient(&next); err != nil {
		writeError(w, err)
		return
	}
	*c = next
	writeJSON(w, 200, map[string]any{"client": c.Client})
}
func (m *Manager) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := r.PathValue("id")
	c := m.clients[key]
	if c == nil {
		writeError(w, apiError(404, "Access token not found"))
		return
	}
	if c.Inflight > 0 {
		writeError(w, apiError(409, "Disable this token and wait for active requests before deleting"))
		return
	}
	if _, err := m.db.Exec("DELETE FROM clients WHERE id=?", key); err != nil {
		writeError(w, err)
		return
	}
	delete(m.clients, key)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (m *Manager) handleSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Strategy               *string `json:"strategy"`
		MaxRetries             *int    `json:"maxRetries"`
		RefreshIntervalSeconds *int    `json:"refreshIntervalSeconds"`
		CooldownSeconds        *int    `json:"cooldownSeconds"`
		LogRetentionDays       *int    `json:"logRetentionDays"`
		SessionAffinity        *bool   `json:"sessionAffinity"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	if in.Strategy != nil {
		s.Strategy = *in.Strategy
	}
	if in.MaxRetries != nil {
		s.MaxRetries = *in.MaxRetries
	}
	if in.RefreshIntervalSeconds != nil {
		s.RefreshIntervalSeconds = *in.RefreshIntervalSeconds
	}
	if in.CooldownSeconds != nil {
		s.CooldownSeconds = *in.CooldownSeconds
	}
	if in.LogRetentionDays != nil {
		s.LogRetentionDays = *in.LogRetentionDays
	}
	if in.SessionAffinity != nil {
		s.SessionAffinity = *in.SessionAffinity
	}
	if s.Strategy != "weighted_round_robin" && s.Strategy != "least_inflight" && s.Strategy != "quota_aware" {
		writeError(w, apiError(400, "Unknown routing strategy"))
		return
	}
	if s.MaxRetries < 0 || s.MaxRetries > 5 || s.RefreshIntervalSeconds < 30 || s.RefreshIntervalSeconds > 86400 || s.CooldownSeconds < 1 || s.CooldownSeconds > 86400 || s.LogRetentionDays < 1 || s.LogRetentionDays > 365 {
		writeError(w, apiError(400, "Retries 0–5, refresh 30–86400s, cooldown 1–86400s, retention 1–365 days are required"))
		return
	}
	if err := m.saveMeta("settings", s); err != nil {
		writeError(w, err)
		return
	}
	m.settings = s
	writeJSON(w, 200, s)
}

func (m *Manager) handleLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page = max(1, min(10000, page))
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = min(100, pageSize)
	where := "1=1"
	args := []any{}
	for _, f := range []struct{ Param, Column string }{{"model", "model"}, {"clientId", "client_id"}, {"accountId", "account_id"}} {
		if v := r.URL.Query().Get(f.Param); v != "" {
			where += " AND " + f.Column + "=?"
			args = append(args, v)
		}
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "success":
		where += " AND status>=200 AND status<300"
	case "error":
		where += " AND (status<200 OR status>=300)"
	case "":
	default:
		n, err := strconv.Atoi(status)
		if err != nil || n < 100 || n > 599 {
			writeError(w, apiError(400, "Invalid status filter"))
			return
		}
		where += " AND status=?"
		args = append(args, n)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int
	if err := m.db.QueryRow("SELECT count(*) FROM logs WHERE "+where, args...).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	items, err := m.queryLogs("SELECT value FROM logs WHERE "+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?", append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": pageSize})
}
func (m *Manager) queryLogs(query string, args ...any) ([]Record, error) {
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var rec Record
		if err = json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
func (m *Manager) handleOverview(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	o := Overview{Stats: Stats{TotalAccounts: len(m.accounts), TotalClients: len(m.clients)}, Series: []SeriesPoint{}, RecentLogs: []Record{}}
	for _, a := range m.accounts {
		if m.eligible(a, "", nil, now) {
			o.Stats.ActiveAccounts++
		}
	}
	for _, c := range m.clients {
		o.Stats.Inflight += c.Inflight
	}
	var successes int64
	err := m.db.QueryRow(`SELECT count(*),coalesce(sum(CASE WHEN status>=200 AND status<300 THEN 1 ELSE 0 END),0),coalesce(sum(json_extract(value,'$.inputTokens')),0),coalesce(sum(json_extract(value,'$.outputTokens')),0),coalesce(avg(json_extract(value,'$.latencyMs')),0) FROM logs WHERE created_at>=?`, now.Add(-24*time.Hour).UnixMilli()).Scan(&o.Stats.Requests24h, &successes, &o.Stats.InputTokens24h, &o.Stats.OutputTokens24h, &o.Stats.AverageLatencyMs)
	if err != nil {
		writeError(w, err)
		return
	}
	if o.Stats.Requests24h > 0 {
		o.Stats.SuccessRate = float64(successes) * 100 / float64(o.Stats.Requests24h)
	}
	cutoff := now.Truncate(time.Hour).Add(-23 * time.Hour).UnixMilli()
	rows, err := m.db.Query(`SELECT created_at/3600000*3600000,count(*),sum(CASE WHEN status>=200 AND status<300 THEN 0 ELSE 1 END),coalesce(sum(json_extract(value,'$.inputTokens')+json_extract(value,'$.outputTokens')),0) FROM logs WHERE created_at>=? GROUP BY 1 ORDER BY 1`, cutoff)
	if err != nil {
		writeError(w, err)
		return
	}
	points := map[int64]SeriesPoint{}
	for rows.Next() {
		var p SeriesPoint
		if err = rows.Scan(&p.Timestamp, &p.Requests, &p.Errors, &p.Tokens); err != nil {
			break
		}
		points[p.Timestamp] = p
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, err)
		return
	}
	for i := 0; i < 24; i++ {
		stamp := cutoff + int64(i)*3600000
		p := points[stamp]
		p.Timestamp = stamp
		o.Series = append(o.Series, p)
	}
	o.RecentLogs, err = m.queryLogs("SELECT value FROM logs ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, o)
}
