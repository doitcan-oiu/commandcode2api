package gateway

import (
	"commandcode2api/internal/usage"
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func allowed(models []string, model string) bool {
	if model == "" || len(models) == 0 {
		return true
	}
	for _, x := range models {
		if x == model || x == "*" {
			return true
		}
	}
	return false
}
func (m *Manager) authenticateLocked(token string) (*storedClient, error) {
	h := digest(token)
	for _, c := range m.clients {
		if c.TokenHash == h {
			if !c.Enabled {
				return nil, apiError(403, "Access token is disabled")
			}
			if c.ExpiresAt > 0 && c.ExpiresAt <= time.Now().UnixMilli() {
				return nil, apiError(401, "Access token has expired")
			}
			return c, nil
		}
	}
	return nil, apiError(401, "Invalid access token")
}
func (m *Manager) Authenticate(token string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.authenticateLocked(token)
	if err != nil {
		return nil, err
	}
	copy := c.Client
	copy.Models = append([]string{}, c.Models...)
	return &copy, nil
}
func (m *Manager) Authorize(token, model string) (*Permit, error) {
	if len(model) == 0 || len(model) > 200 {
		return nil, apiError(400, "Model must contain 1–200 characters")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.authenticateLocked(token)
	if err != nil {
		return nil, err
	}
	if !allowed(c.Models, model) {
		return nil, apiError(403, "Model is not allowed for this access token")
	}
	if c.MaxRequests > 0 && c.UsedRequests >= c.MaxRequests {
		return nil, apiError(429, "Request quota is exhausted")
	}
	if c.MaxTokens > 0 && c.UsedTokens >= c.MaxTokens {
		return nil, apiError(429, "Token quota is exhausted")
	}
	if c.MaxConcurrent > 0 && c.Inflight >= c.MaxConcurrent {
		return nil, &Error{Status: 429, Message: "Concurrent request limit reached", RetryAfter: 1}
	}
	now := time.Now()
	if now.Sub(c.windowStart) >= time.Minute {
		c.windowStart = now
		c.windowRequests = 0
	}
	if c.RPM > 0 && c.windowRequests >= c.RPM {
		return nil, &Error{Status: 429, Message: "Requests per minute limit reached", RetryAfter: max(1, int(time.Until(c.windowStart.Add(time.Minute)).Seconds())+1)}
	}
	before := *c
	c.UsedRequests++
	c.LastUsedAt = now.UnixMilli()
	c.Inflight++
	c.windowRequests++
	if err = m.saveClient(c); err != nil {
		*c = before
		return nil, apiError(503, "Could not persist request reservation")
	}
	return &Permit{ClientID: c.ID, ClientName: c.Name, manager: m}, nil
}
func (m *Manager) finishPermit(p *Permit, r Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[p.ClientID]; c != nil {
		if c.Inflight > 0 {
			c.Inflight--
		}
		c.UsedTokens = addTokens(c.UsedTokens, addTokens(r.InputTokens, r.OutputTokens))
		if err := m.saveClient(c); err != nil {
			log.Printf("gateway usage persistence: %v", err)
		}
	}
	r.ClientID = p.ClientID
	r.ClientName = p.ClientName
	if r.ID == "" {
		r.ID = id("req_")
	}
	if r.CreatedAt <= 0 {
		r.CreatedAt = time.Now().UnixMilli()
	}
	r.InputTokens = max(0, r.InputTokens)
	r.OutputTokens = max(0, r.OutputTokens)
	if r.Status >= 400 {
		r.Error = http.StatusText(r.Status)
		if r.Error == "" {
			r.Error = "Request interrupted"
		}
	} else {
		r.Error = ""
	}
	b, err := json.Marshal(r)
	if err == nil {
		_, err = m.db.Exec("INSERT INTO logs(id,created_at,status,model,client_id,account_id,value) VALUES(?,?,?,?,?,?,?)", r.ID, r.CreatedAt, r.Status, r.Model, r.ClientID, r.AccountID, string(b))
	}
	if err != nil {
		log.Printf("gateway request log persistence: %v", err)
	}
}
func addTokens(a, b int64) int64 {
	a = max(0, a)
	b = max(0, b)
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

// FilterModels returns models which can currently be routed to the pool.
func (m *Manager) FilterModels(ids []string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, model := range ids {
		if seen[model] {
			continue
		}
		for _, a := range m.accounts {
			if m.eligible(a, model, nil, now) {
				out = append(out, model)
				seen[model] = true
				break
			}
		}
	}
	return out
}
func (m *Manager) eligible(a *storedAccount, model string, exclude map[string]bool, now time.Time) bool {
	return a.Enabled && a.Status != "invalid" && !exclude[a.ID] && allowed(a.Models, model) && (a.MaxConcurrent == 0 || a.Inflight < a.MaxConcurrent) && a.CooldownUntil <= now.UnixMilli()
}
func (m *Manager) Select(model, sessionKey string, exclude map[string]bool) (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	candidates := []*storedAccount{}
	priority := -1000001
	for _, a := range m.accounts {
		if m.eligible(a, model, exclude, now) {
			if a.Priority > priority {
				priority = a.Priority
				candidates = candidates[:0]
			}
			if a.Priority == priority {
				candidates = append(candidates, a)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, unavailable("No upstream account is currently available for this model")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	var selected *storedAccount
	affinityKey := digest(sessionKey + "\x00" + model)
	if m.settings.SessionAffinity && sessionKey != "" {
		if bound, ok := m.affinity[affinityKey]; ok && now.Before(bound.Expires) {
			for _, a := range candidates {
				if a.ID == bound.AccountID {
					selected = a
					break
				}
			}
		}
	}
	if selected == nil {
		if m.settings.Strategy == "least_inflight" {
			for _, a := range candidates {
				if selected == nil || float64(a.Inflight+1)/float64(a.Weight) < float64(selected.Inflight+1)/float64(selected.Weight) {
					selected = a
				}
			}
		} else {
			total := 0.0
			for _, a := range candidates {
				weight := float64(a.Weight)
				if m.settings.Strategy == "quota_aware" {
					weight *= quotaFactor(a.Usage)
				}
				a.score += weight
				total += weight
				if selected == nil || a.score > selected.score {
					selected = a
				}
			}
			selected.score -= total
		}
	}
	key, err := m.decrypt(selected.EncryptedKey)
	if err != nil {
		return nil, unavailable("Could not decrypt upstream credentials")
	}
	selected.Inflight++
	if m.settings.SessionAffinity && sessionKey != "" {
		if len(m.affinity) >= 10000 {
			for k, v := range m.affinity {
				if now.After(v.Expires) {
					delete(m.affinity, k)
				}
			}
			if len(m.affinity) >= 10000 {
				for k := range m.affinity {
					delete(m.affinity, k)
					break
				}
			}
		}
		m.affinity[affinityKey] = affinity{AccountID: selected.ID, Expires: now.Add(30 * time.Minute)}
	}
	return &Lease{AccountID: selected.ID, AccountName: selected.Label, Key: key, manager: m}, nil
}
func quotaFactor(r *usage.Report) float64 {
	if r == nil || r.Credits == nil {
		return 1
	}
	factor := 1.0
	for _, w := range []*usage.Window{r.Credits.FiveHour, r.Credits.Weekly} {
		if w != nil && w.Used != nil && w.Cap != nil && *w.Cap > 0 {
			remaining := 1 - *w.Used / *w.Cap
			if remaining < factor {
				factor = remaining
			}
		}
	}
	if factor < 0.05 {
		return 0.05
	}
	return factor
}
func (m *Manager) finishLease(l *Lease, status int, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.accounts[l.AccountID]
	if a == nil {
		return
	}
	if a.Inflight > 0 {
		a.Inflight--
	}
	now := time.Now()
	switch {
	case status == 401 || status == 403:
		a.healthVersion++
		a.Status = "invalid"
		a.LastError = "Upstream authentication rejected"
		a.CooldownUntil = 0
	case status == 402 || status == 429:
		a.healthVersion++
		a.Status = "cooldown"
		if retryAfter <= 0 {
			retryAfter = time.Duration(m.settings.CooldownSeconds) * time.Second
		}
		if retryAfter > 24*time.Hour {
			retryAfter = 24 * time.Hour
		}
		a.CooldownUntil = now.Add(retryAfter).UnixMilli()
		a.LastError = "Upstream quota or rate limit reached"
	case status >= 500:
		a.healthVersion++
		a.Status = "cooldown"
		a.CooldownUntil = now.Add(15 * time.Second).UnixMilli()
		a.LastError = "Upstream temporarily unavailable"
	case status >= 200 && status < 300:
		// An older concurrent success cannot undo a newer quota or auth failure.
		if a.Status != "invalid" && a.CooldownUntil <= now.UnixMilli() {
			a.Status = "active"
			a.CooldownUntil = 0
			a.LastError = ""
		}
	default:
		return
	}
	a.UpdatedAt = now.UnixMilli()
	if err := m.saveAccount(a); err != nil {
		log.Printf("gateway account health persistence: %v", err)
	}
}
func (m *Manager) Refresh(ctx context.Context, accountID string) (Account, error) {
	m.mu.Lock()
	if existing := m.refreshJobs[accountID]; existing != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return Account{}, ctx.Err()
		case <-existing.done:
			return existing.account, existing.err
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	m.refreshJobs[accountID] = call
	m.mu.Unlock()
	account, err := m.refreshOne(ctx, accountID)
	m.mu.Lock()
	call.account = account
	call.err = err
	delete(m.refreshJobs, accountID)
	close(call.done)
	m.mu.Unlock()
	return account, err
}
func (m *Manager) refreshOne(ctx context.Context, accountID string) (Account, error) {
	m.mu.Lock()
	a := m.accounts[accountID]
	if a == nil {
		m.mu.Unlock()
		return Account{}, apiError(404, "Account not found")
	}
	key, err := m.decrypt(a.EncryptedKey)
	healthVersion := a.healthVersion
	m.mu.Unlock()
	if err != nil {
		return Account{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	report, fetchErr := m.usage.Fetch(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	a = m.accounts[accountID]
	if a == nil {
		return Account{}, apiError(404, "Account not found")
	}
	now := time.Now()
	a.LastRefreshAt = now.UnixMilli()
	a.UpdatedAt = now.UnixMilli()
	if report != nil {
		a.Usage = report
	}
	// A response fetched before a newer generation error cannot revive that key.
	if a.healthVersion != healthVersion {
		if err = m.saveAccount(a); err != nil {
			return Account{}, err
		}
		return publicAccount(a), nil
	}
	if fetchErr != nil {
		var ue *usage.Error
		if errors.As(fetchErr, &ue) && (ue.Status == 401 || ue.Status == 403) {
			a.Status = "invalid"
			a.LastError = "Upstream authentication rejected"
		} else {
			a.LastError = "Usage refresh failed; retrying on next refresh"
		}
	} else {
		if blocked, until, reason := report.BlockedUntil(now); blocked {
			a.Status = "cooldown"
			a.CooldownUntil = until.UnixMilli()
			a.LastError = reason
		} else if report != nil && report.Credits != nil && report.Account != nil {
			a.Status = "active"
			a.CooldownUntil = 0
			a.LastError = ""
		}
		if report != nil && len(report.Failures) > 0 {
			a.LastError = "Some usage endpoints are temporarily unavailable"
		}
	}
	if err = m.saveAccount(a); err != nil {
		return Account{}, err
	}
	return publicAccount(a), nil
}
func (m *Manager) RefreshAll(ctx context.Context) []Account {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	ids := []string{}
	for _, a := range m.accounts {
		if a.Enabled {
			ids = append(ids, a.ID)
		}
	}
	m.mu.Unlock()
	jobs := make(chan string)
	var workers sync.WaitGroup
	for i := 0; i < min(4, len(ids)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				if ctx.Err() == nil {
					_, _ = m.Refresh(ctx, id)
				}
			}
		}()
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accountListLocked()
}
func validateModels(models []string) error {
	if len(models) > 100 {
		return apiError(400, "At most 100 allowed models are supported")
	}
	for _, s := range models {
		if len(s) > 200 || strings.TrimSpace(s) == "" {
			return apiError(400, "Invalid allowed model")
		}
	}
	return nil
}
