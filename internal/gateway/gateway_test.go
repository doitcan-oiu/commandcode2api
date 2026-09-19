package gateway

import (
	"bytes"
	"commandcode2api/internal/usage"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureResponse(r *http.Request) *http.Response {
	body := `{}`
	switch r.URL.Path {
	case "/alpha/whoami":
		body = `{"user":{"id":"fixture-user","name":"Fixture"}}`
	case "/alpha/billing/credits":
		body = `{"credits":{"monthlyCredits":10,"purchasedCredits":0,"freeCredits":0}}`
	case "/alpha/billing/subscriptions":
		body = `{"planId":"fixture","status":"active"}`
	case "/alpha/usage/summary":
		body = `{"totalCount":1,"totalTokensIn":2,"totalTokensOut":3}`
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := Open(Options{DataDir: t.TempDir(), UsageClient: usage.NewClient("http://fixture.local", &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) { return fixtureResponse(r), nil })})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func request(t *testing.T, m *Manager, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "http://gateway.local"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	return w
}
func setup(t *testing.T, m *Manager) *http.Cookie {
	t.Helper()
	w := request(t, m, "POST", "/api/admin/setup", map[string]any{"username": "admin", "password": "Test1234"}, nil)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session")
	}
	return cookies[0]
}
func createFixtureAccount(t *testing.T, m *Manager, key string, weight, priority, limit int) *storedAccount {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.createAccountLocked(accountInput{Key: key, Weight: &weight, Priority: &priority, MaxConcurrent: &limit})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func createFixtureClient(t *testing.T, m *Manager, c Client) string {
	t.Helper()
	token := "ccg_" + randomToken(32)
	if c.ID == "" {
		c.ID = id("cli_")
	}
	if c.Name == "" {
		c.Name = "Fixture"
	}
	c.Enabled = true
	s := &storedClient{Client: c, TokenHash: digest(token)}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[c.ID] = s
	if err := m.saveClient(s); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAdminSetupAuthenticationAndCSRF(t *testing.T) {
	m := testManager(t)
	if !m.SetupRequired() {
		t.Fatal("new database should allow setup")
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "setup.token")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new database generated a setup token file")
	}
	w := request(t, m, "GET", "/api/admin/session", nil, nil)
	if !strings.Contains(w.Body.String(), `"setupRequired":true`) {
		t.Fatal(w.Body.String())
	}
	if w = request(t, m, "GET", "/api/admin/accounts", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w = request(t, m, "POST", "/api/admin/setup", map[string]string{"username": "admin", "password": "short12"}, nil); w.Code != 400 || !m.SetupRequired() {
		t.Fatalf("short setup password accepted: %d", w.Code)
	}
	crossOrigin := httptest.NewRequest("POST", "http://gateway.local/api/admin/setup", strings.NewReader(`{"username":"admin","password":"Test1234"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://attacker.invalid")
	w = httptest.NewRecorder()
	m.Handler().ServeHTTP(w, crossOrigin)
	if w.Code != 403 || !m.SetupRequired() {
		t.Fatalf("cross-origin setup allowed: %d", w.Code)
	}
	cookie := setup(t, m)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api/admin" {
		t.Fatalf("insecure cookie: %+v", cookie)
	}
	if m.SetupRequired() {
		t.Fatal("setup remains open after initialization")
	}
	if w = request(t, m, "POST", "/api/admin/setup", map[string]string{"username": "replacement", "password": "Other123"}, nil); w.Code != 409 {
		t.Fatalf("existing administrator overwritten: %d", w.Code)
	}
	w = request(t, m, "GET", "/api/admin/session", nil, cookie)
	if !strings.Contains(w.Body.String(), `"setupRequired":false`) || !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatal(w.Body.String())
	}
	r := httptest.NewRequest("PATCH", "http://gateway.local/api/admin/settings", strings.NewReader(`{"maxRetries":1}`))
	r.Header.Set("Origin", "https://attacker.invalid")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross origin write allowed: %d", w.Code)
	}
	w = request(t, m, "POST", "/api/admin/password", map[string]string{"currentPassword": "Test1234", "newPassword": "Next1234"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if request(t, m, "GET", "/api/admin/accounts", nil, cookie).Code != 401 {
		t.Fatal("old sessions survive password change")
	}
	newCookie := w.Result().Cookies()[0]
	if request(t, m, "GET", "/api/admin/accounts", nil, newCookie).Code != 200 {
		t.Fatal("new session invalid")
	}
	request(t, m, "POST", "/api/admin/logout", nil, newCookie)
	if request(t, m, "GET", "/api/admin/accounts", nil, newCookie).Code != 401 {
		t.Fatal("logout did not revoke")
	}
	if request(t, m, "POST", "/api/admin/login", map[string]string{"username": "admin", "password": "Test1234"}, nil).Code != 401 {
		t.Fatal("old password works")
	}
	if request(t, m, "POST", "/api/admin/login", map[string]string{"username": "admin", "password": "Next1234"}, nil).Code != 200 {
		t.Fatal("new password rejected")
	}
}

func TestEncryptedPersistenceAndCredentialRedaction(t *testing.T) {
	m := testManager(t)
	cookie := setup(t, m)
	key := "user_fixture_never_real_secret_123456"
	w := request(t, m, "POST", "/api/admin/accounts", map[string]any{"key": key, "label": "Fixture"}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "encryptedKey") || strings.Contains(w.Body.String(), "keyHash") {
		t.Fatal("upstream credential exposed")
	}
	w = request(t, m, "POST", "/api/admin/clients", map[string]any{"name": "Test client"}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var out struct {
		Client Client `json:"client"`
		Token  string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if !strings.HasPrefix(out.Token, "ccg_") {
		t.Fatal("invalid downstream token")
	}
	w = request(t, m, "GET", "/api/admin/clients", nil, cookie)
	if strings.Contains(w.Body.String(), out.Token) || strings.Contains(w.Body.String(), "tokenHash") {
		t.Fatal("client secret exposed")
	}
	var accountRaw, clientRaw string
	if err := m.db.QueryRow("SELECT value FROM accounts LIMIT 1").Scan(&accountRaw); err != nil {
		t.Fatal(err)
	}
	if err := m.db.QueryRow("SELECT value FROM clients LIMIT 1").Scan(&clientRaw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(accountRaw, key) || strings.Contains(clientRaw, out.Token) {
		t.Fatal("plaintext credential persisted")
	}
	if !strings.Contains(clientRaw, digest(out.Token)) {
		t.Fatal("missing token digest")
	}
	dir := m.dataDir
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	lease, err := reopened.Select("fixture-model", "", nil)
	if err != nil || lease.Key != key {
		t.Fatalf("decrypt after restart: %v", err)
	}
	lease.Finish(200, 0)
	if _, err = reopened.Authenticate(out.Token); err != nil {
		t.Fatal(err)
	}
	if reopened.SetupRequired() {
		t.Fatal("restart reenabled setup")
	}
	if w = request(t, reopened, "POST", "/api/admin/setup", map[string]string{"username": "replacement", "password": "Other123"}, nil); w.Code != 409 {
		t.Fatalf("setup allowed after restart: %d", w.Code)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "master.key")); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(Options{DataDir: dir}); err == nil {
		t.Fatal("missing encryption key silently replaced")
	}
}

func TestWeightedRoutingPriorityFallbackAndRecovery(t *testing.T) {
	m := testManager(t)
	m.settings.Strategy = "weighted_round_robin"
	a := createFixtureAccount(t, m, "user_fixture_account_a", 1, 0, 2)
	b := createFixtureAccount(t, m, "user_fixture_account_b", 3, 0, 2)
	counts := map[string]int{}
	for i := 0; i < 40; i++ {
		lease, err := m.Select("m", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		counts[lease.AccountID]++
		lease.Finish(200, 0)
	}
	if counts[a.ID] != 10 || counts[b.ID] != 30 {
		t.Fatalf("wrong weight distribution: %v", counts)
	}
	b.Priority = 2
	lease, err := m.Select("m", "", nil)
	if err != nil || lease.AccountID != b.ID {
		t.Fatal("priority ignored")
	}
	lease.Finish(429, time.Second)
	lease, err = m.Select("m", "", nil)
	if err != nil || lease.AccountID != a.ID {
		t.Fatal("cooldown did not fail over")
	}
	lease.Finish(401, 0)
	if _, err = m.Select("m", "", nil); err == nil {
		t.Fatal("invalid/cooldown keys remained eligible")
	}
	b.CooldownUntil = time.Now().Add(-time.Second).UnixMilli()
	lease, err = m.Select("m", "", nil)
	if err != nil || lease.AccountID != b.ID {
		t.Fatal("expired cooldown did not recover")
	}
	lease.Finish(400, 0)
	if b.Status == "invalid" {
		t.Fatal("bad user input invalidated account")
	}
	if _, err = m.Select("m", "", map[string]bool{b.ID: true}); err == nil {
		t.Fatal("excluded key retried")
	}
}

func TestConcurrencySessionAffinityAndNeutralCancellation(t *testing.T) {
	m := testManager(t)
	a := createFixtureAccount(t, m, "user_fixture_account_affinity_a", 1, 0, 1)
	createFixtureAccount(t, m, "user_fixture_account_affinity_b", 1, 0, 1)
	first, err := m.Select("m", "session", nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Finish(200, 0)
	second, err := m.Select("m", "session", nil)
	if err != nil || first.AccountID != second.AccountID {
		t.Fatal("session affinity changed")
	}
	other, err := m.Select("m", "session", nil)
	if err != nil || other.AccountID == second.AccountID {
		t.Fatal("concurrency did not fail over")
	}
	if _, err = m.Select("m", "third", nil); err == nil {
		t.Fatal("account concurrency exceeded")
	}
	second.Finish(499, 0)
	second.Finish(401, 0)
	other.Finish(499, 0)
	if a.Inflight != 0 || a.Status != "active" {
		t.Fatalf("cancellation altered health: %+v", a.Account)
	}
}

func TestClientRestrictionsReservationsAndLogs(t *testing.T) {
	m := testManager(t)
	token := createFixtureClient(t, m, Client{MaxConcurrent: 1, MaxRequests: 2, MaxTokens: 10, Models: []string{"allowed"}})
	if _, err := m.Authenticate(token); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authorize(token, "blocked"); err == nil {
		t.Fatal("model whitelist ignored")
	}
	p, err := m.Authorize(token, "allowed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Authorize(token, "allowed"); err == nil {
		t.Fatal("client concurrency ignored")
	}
	p.Finish(Record{ID: "req_fixture", Model: "allowed", Protocol: "openai", Status: 500, InputTokens: 3, OutputTokens: 4, Error: "user_do_not_log_or_prompt", LatencyMS: 10})
	p.Finish(Record{Status: 200})
	c, err := m.Authenticate(token)
	if err != nil || c.UsedRequests != 1 || c.UsedTokens != 7 || c.Inflight != 0 {
		t.Fatalf("incorrect accounting: %+v %v", c, err)
	}
	p, err = m.Authorize(token, "allowed")
	if err != nil {
		t.Fatal(err)
	}
	p.Finish(Record{Status: 200, InputTokens: 3})
	if _, err = m.Authorize(token, "allowed"); err == nil {
		t.Fatal("quota not enforced")
	}
	logs, err := m.queryLogs("SELECT value FROM logs ORDER BY created_at")
	if err != nil || len(logs) != 2 {
		t.Fatalf("logs: %+v %v", logs, err)
	}
	if logs[0].ID != "req_fixture" || strings.Contains(logs[0].Error, "user_") {
		t.Fatalf("request ID or redaction failed: %+v", logs[0])
	}
	cookie := setup(t, m)
	w := request(t, m, "GET", "/api/admin/overview", nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var o Overview
	json.Unmarshal(w.Body.Bytes(), &o)
	if o.Stats.Requests24h != 2 || o.Stats.SuccessRate != 50 || o.Stats.InputTokens24h != 6 || len(o.Series) != 24 {
		t.Fatalf("bad dashboard stats: %+v", o)
	}
	w = request(t, m, "GET", "/api/admin/logs?status=error&pageSize=1", nil, cookie)
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatal(w.Body.String())
	}
}

func TestConcurrentQuotaReservations(t *testing.T) {
	m := testManager(t)
	token := createFixtureClient(t, m, Client{MaxRequests: 5})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p, e := m.Authorize(token, "m"); e == nil {
				accepted.Add(1)
				p.Finish(Record{Status: 200})
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 5 {
		t.Fatalf("request quota race: %d", accepted.Load())
	}
}

func TestQuotaAwareLeastInflightAndModelAvailability(t *testing.T) {
	m := testManager(t)
	a := createFixtureAccount(t, m, "user_fixture_quota_a", 1, 0, 5)
	b := createFixtureAccount(t, m, "user_fixture_quota_b", 1, 0, 5)
	usedA, usedB, cap := 9.0, 1.0, 10.0
	a.Usage = &usage.Report{Credits: &usage.Credits{FiveHour: &usage.Window{Used: &usedA, Cap: &cap}}}
	b.Usage = &usage.Report{Credits: &usage.Credits{FiveHour: &usage.Window{Used: &usedB, Cap: &cap}}}
	counts := map[string]int{}
	for i := 0; i < 20; i++ {
		l, e := m.Select("m", "", nil)
		if e != nil {
			t.Fatal(e)
		}
		counts[l.AccountID]++
		l.Finish(200, 0)
	}
	if counts[a.ID] != 2 || counts[b.ID] != 18 {
		t.Fatalf("quota routing ignored available window: %v", counts)
	}
	m.settings.Strategy = "least_inflight"
	first, e := m.Select("m", "", nil)
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Select("m", "", nil)
	if e != nil || first.AccountID == second.AccountID {
		t.Fatal("least-inflight scheduling ignored load")
	}
	first.Finish(499, 0)
	second.Finish(499, 0)
	a.Models = []string{"model-a"}
	b.Models = []string{"model-b"}
	b.Enabled = false
	models := m.FilterModels([]string{"model-a", "model-b", "model-c", "model-a"})
	if len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("model availability leaked: %v", models)
	}
	token := createFixtureClient(t, m, Client{})
	if _, e = m.Authorize(token, strings.Repeat("a", 201)); e == nil {
		t.Fatal("oversized metadata accepted")
	}
}

func TestOldSuccessAndPartialRefreshPreserveCooldown(t *testing.T) {
	m := testManager(t)
	a := createFixtureAccount(t, m, "user_fixture_concurrent_health", 1, 0, 5)
	first, e := m.Select("m", "", nil)
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Select("m", "", nil)
	if e != nil {
		t.Fatal(e)
	}
	first.Finish(429, time.Hour)
	second.Finish(200, 0)
	if a.Status != "cooldown" {
		t.Fatal("old successful request cleared newer cooldown")
	}
	m.usage = usage.NewClient("http://fixture.local", &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		response := fixtureResponse(r)
		if r.URL.Path == "/alpha/billing/credits" {
			response.StatusCode = 503
		}
		return response, nil
	})})
	if _, e = m.Refresh(context.Background(), a.ID); e != nil {
		t.Fatal(e)
	}
	if a.Status != "cooldown" {
		t.Fatal("missing credits cleared cooldown")
	}
}

func TestRefreshDedupAndCannotUndoNewFailure(t *testing.T) {
	m := testManager(t)
	a := createFixtureAccount(t, m, "user_fixture_refresh_account", 1, 0, 5)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	m.usage = usage.NewClient("http://fixture.local", &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/alpha/whoami" {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-release
		}
		return fixtureResponse(r), nil
	})})
	done := make(chan error, 1)
	go func() { _, e := m.Refresh(context.Background(), a.ID); done <- e }()
	<-entered
	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Refresh(waitCtx, a.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("duplicate refresh wait did not honor cancellation: %v", err)
	}
	lease, err := m.Select("m", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	lease.Finish(429, time.Hour)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh was not deduplicated: %d", calls.Load())
	}
	if a.Status != "cooldown" || a.CooldownUntil < time.Now().Add(59*time.Minute).UnixMilli() {
		t.Fatal("old refresh cleared newer cooldown")
	}
	_, err = m.Refresh(context.Background(), a.ID)
	if err != nil || a.Status != "active" {
		t.Fatalf("fresh refresh cannot recover: %v", err)
	}
}

func TestAPILimitsImportAndValidation(t *testing.T) {
	m := testManager(t)
	cookie := setup(t, m)
	w := request(t, m, "POST", "/api/admin/accounts/import", map[string]any{"keys": "user_fixture_import_a\nuser_fixture_import_b\nuser_fixture_import_a\nbad", "labelPrefix": "Fixture"}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"created":2`) || !strings.Contains(w.Body.String(), `"skipped":2`) {
		t.Fatal(w.Body.String())
	}
	w = request(t, m, "PATCH", "/api/admin/settings", map[string]any{"maxRetries": 100}, cookie)
	if w.Code != 400 {
		t.Fatal("retry validation failed")
	}
	w = request(t, m, "POST", "/api/admin/clients", map[string]any{"maxTokens": -1}, cookie)
	if w.Code != 400 {
		t.Fatal("negative quota accepted")
	}
	w = request(t, m, "POST", "/api/admin/clients", map[string]any{"upstreamUrl": "http://evil.invalid"}, cookie)
	if w.Code != 400 {
		t.Fatal("unknown fields accepted")
	}
	token := createFixtureClient(t, m, Client{RPM: 1})
	p, err := m.Authorize(token, "m")
	if err != nil {
		t.Fatal(err)
	}
	p.Finish(Record{Status: 200})
	if _, err = m.Authorize(token, "m"); err == nil {
		t.Fatal("RPM ignored")
	}
	expired := createFixtureClient(t, m, Client{ExpiresAt: time.Now().Add(-time.Second).UnixMilli()})
	if _, err = m.Authenticate(expired); err == nil {
		t.Fatal("expired token accepted")
	}
	for i := 0; i < 11; i++ {
		w = request(t, m, "POST", "/api/admin/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
	}
	if w.Code != 429 {
		t.Fatalf("missing login throttle: %d", w.Code)
	}
}
