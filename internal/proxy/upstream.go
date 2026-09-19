package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"commandcode2api/internal/config"
	"commandcode2api/internal/gateway"
)

type keyState struct {
	sessionID           string
	expiresAt, nextInit time.Time
	initializing        chan struct{}
	fingerprint         M
}
type modelEntry struct {
	ids       []string
	fetchedAt time.Time
}
type Proxy struct {
	cfg            config.Config
	client         *http.Client
	registryClient *http.Client
	logger         *log.Logger
	mu             sync.Mutex
	keys           map[string]*keyState
	models         map[string]modelEntry
	inflight       atomic.Int64
	timeouts       atomic.Int64
	manager        *gateway.Manager
	adminHandler   http.Handler
	webHandler     http.Handler
}

// New creates a proxy with isolated upstream clients and per-key state.
func New(cfg config.Config, logger *log.Logger) (*Proxy, error) {
	u, err := url.Parse(cfg.APIBase)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("apiBase must be an http(s) URL without credentials")
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	// Only the explicit CC proxy setting affects upstream traffic. Never inherit
	// ambient HTTP_PROXY settings or send CC authorization to the npm registry.
	t.Proxy = nil
	t.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	// Generation owns its per-mode header deadline; initialization/models/registry
	// have their own contexts. A shared timeout would cap long thinking streams.
	t.ResponseHeaderTimeout = 0
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 32
	t.IdleConnTimeout = 90 * time.Second
	if cfg.UpstreamProxy != "" {
		pu, e := url.Parse(cfg.UpstreamProxy)
		if e != nil || pu.Hostname() == "" || pu.Scheme != "http" {
			return nil, fmt.Errorf("upstreamProxy must be a valid http://host:port URL")
		}
		t.Proxy = http.ProxyURL(pu)
	}
	rt := t.Clone()
	rt.Proxy = nil
	// Reject redirects so authentication headers cannot leave the configured upstream.
	noRedirect := func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Proxy{cfg: cfg, client: &http.Client{Transport: t, CheckRedirect: noRedirect}, registryClient: &http.Client{Transport: rt, Timeout: 10 * time.Second, CheckRedirect: noRedirect}, logger: logger, keys: map[string]*keyState{}, models: map[string]modelEntry{}}, nil
}

func (p *Proxy) log(level, message string, data M) {
	if p.logger == nil {
		return
	}
	if p.cfg.LogLevel == "error" && level != "error" {
		return
	}
	if p.cfg.LogLevel == "warn" && level == "info" {
		return
	}
	b, _ := json.Marshal(data)
	p.logger.Printf("[%s] %s %s", level, message, b)
}

// Close releases idle outbound connections after serving has stopped.
func (p *Proxy) Close() {
	p.client.CloseIdleConnections()
	p.registryClient.CloseIdleConnections()
	if p.manager != nil {
		_ = p.manager.Close()
	}
}

var keyPattern = regexp.MustCompile(`user_[a-zA-Z0-9_-]+`)
var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func getAPIKey(h http.Header) string {
	if a := h.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		if k := keyPattern.FindString(a[7:]); k != "" {
			return k
		}
	}
	return keyPattern.FindString(h.Get("X-Api-Key"))
}
func slugifyProjectPath(path string) string {
	s := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(path), "-"), "-")
	if s == "" {
		return "root"
	}
	return s
}
func (p *Proxy) stateLocked(key string) *keyState {
	now := time.Now()
	s := p.keys[key]
	if s == nil || now.After(s.expiresAt) && s.initializing == nil {
		s = &keyState{sessionID: newID(), expiresAt: now.Add(12*time.Hour + time.Duration(rand.Int64N(int64(time.Hour)))), fingerprint: generateFingerprint(key, p.cfg)}
		p.keys[key] = s
	}
	return s
}
func (p *Proxy) sessionID(key string, h http.Header, cacheKey string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stateLocked(key)
	for _, id := range []string{h.Get("X-Session-Id"), h.Get("X-Claude-Code-Session-Id"), h.Get("Session_id"), cacheKey} {
		if len(id) >= 8 {
			return id
		}
	}
	return s.sessionID
}
func (p *Proxy) authHeaders(key string) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer "+key)
	h.Set("X-Cli-Environment", "production")
	h.Set("X-Command-Code-Version", config.ProtocolVersion)
	if p.cfg.ZDR {
		h.Set("X-Cmd-Zdr", "1")
	}
	return h
}
func (p *Proxy) request(ctx context.Context, method, path string, body M, headers http.Header) (*http.Response, error) {
	var data io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		data = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.cfg.APIBase, "/")+path, data)
	if e != nil {
		return nil, e
	}
	r.Header = headers
	return p.client.Do(r)
}
func (p *Proxy) ensureInitialized(ctx context.Context, key string) error {
	p.mu.Lock()
	s := p.stateLocked(key)
	if time.Now().Before(s.nextInit) {
		p.mu.Unlock()
		return nil
	}
	if done := s.initializing; done != nil {
		p.mu.Unlock()
		select {
		case <-done:
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	s.initializing = done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if ctx.Err() == nil {
			s.nextInit = time.Now().Add(8*time.Hour + time.Duration(rand.Int64N(int64(2*time.Hour))))
		}
		s.initializing = nil
		close(done)
		p.mu.Unlock()
	}()
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for path, body := range map[string]M{
		"/alpha/fingerprint/record": s.fingerprint,
		"/alpha/lifecycle-events":   {"eventType": "cli_session_exists", "metadata": M{"sessionId": "sess_" + randomHex(8), "cliVersion": config.ProtocolVersion, "mode": p.cfg.CLISessionMode, "os": "win32-x64"}},
	} {
		wg.Go(func() {
			r, e := p.request(initCtx, http.MethodPost, path, body, p.authHeaders(key))
			if e != nil {
				if ctx.Err() == nil {
					p.log("warn", "Initialization request failed", M{"path": path})
				}
				return
			}
			defer r.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
			if r.StatusCode >= 300 {
				p.log("warn", "Initialization request failed", M{"path": path, "status": r.StatusCode})
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}
func (p *Proxy) forward(ctx context.Context, body M, key string, h http.Header, cacheKey string) (*http.Response, error) {
	session := p.sessionID(key, h, cacheKey)
	if uuidPattern.MatchString(session) {
		body["threadId"] = session
	}
	headers := p.authHeaders(key)
	headers.Set("User-Agent", "cli")
	headers.Set("X-Project-Slug", slugifyProjectPath(p.cfg.DeviceProjectDir))
	headers.Set("X-Taste-Learning", "false")
	headers.Set("X-Session-Id", session)
	headers.Set("Traceparent", "00-"+randomHex(16)+"-"+randomHex(8)+"-01")
	if h.Get("X-Cmd-Zdr") == "1" {
		headers.Set("X-Cmd-Zdr", "1")
	}
	return p.request(ctx, http.MethodPost, "/alpha/generate", body, headers)
}

var fallbackModels = []string{"claude-sonnet-4-6", "claude-opus-4-8", "claude-opus-4-7", "claude-haiku-4-5-20251001", "gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash", "moonshotai/Kimi-K2.6", "moonshotai/Kimi-K2.5", "zai-org/GLM-5.1", "zai-org/GLM-5", "MiniMaxAI/MiniMax-M3", "MiniMaxAI/MiniMax-M2.7", "MiniMaxAI/MiniMax-M2.5", "Qwen/Qwen3.6-Max-Preview", "Qwen/Qwen3.6-Plus", "Qwen/Qwen3.7-Max", "stepfun/Step-3.7-Flash", "stepfun/Step-3.5-Flash", "xiaomi/mimo-v2.5-pro", "xiaomi/mimo-v2.5", "google/gemini-3.5-flash", "google/gemini-3.1-flash-lite"}

func (p *Proxy) fetchModels(ctx context.Context, key string) []string {
	if key == "" || !p.cfg.UseProviderModels {
		return fallbackModels
	}
	p.mu.Lock()
	entry, ok := p.models[key]
	p.mu.Unlock()
	if ok && time.Since(entry.fetchedAt) < time.Duration(p.cfg.ModelRefreshIntervalMS)*time.Millisecond {
		return entry.ids
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, e := p.request(ctx, http.MethodGet, "/provider/v1/models", nil, p.authHeaders(key))
	if e != nil {
		return fallbackModels
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fallbackModels
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&payload) != nil || payload.Data == nil {
		return fallbackModels
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	p.mu.Lock()
	p.models[key] = modelEntry{ids: ids, fetchedAt: time.Now()}
	p.mu.Unlock()
	return ids
}

// npm metadata is advisory only: never download/execute the package and never
// change the implemented wire version based on a registry response.
func (p *Proxy) checkProtocolDrift(ctx context.Context) {
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/command-code/latest", nil)
	if e != nil {
		return
	}
	resp, e := p.registryClient.Do(r)
	if e != nil {
		if ctx.Err() == nil {
			p.log("warn", "CC version check failed", nil)
		}
		return
	}
	defer resp.Body.Close()
	var pkg struct {
		Version string `json:"version"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pkg) != nil || pkg.Version == "" {
		p.log("warn", "CC version check failed", M{"status": resp.StatusCode})
		return
	}
	if pkg.Version != config.ProtocolVersion {
		p.log("warn", "CC CLI version drift: protocol may have changed, re-align from the npm package", M{"implemented": config.ProtocolVersion, "latest": pkg.Version})
	} else {
		p.log("info", "CC CLI version in sync", M{"version": pkg.Version})
	}
}
func (p *Proxy) background(ctx context.Context) {
	if p.cfg.CheckProtocolDrift {
		p.checkProtocolDrift(ctx)
	}
	drift := time.NewTicker(24 * time.Hour)
	cleanup := time.NewTicker(time.Hour)
	defer drift.Stop()
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-drift.C:
			if p.cfg.CheckProtocolDrift {
				p.checkProtocolDrift(ctx)
			}
		case now := <-cleanup.C:
			p.mu.Lock()
			for k, s := range p.keys {
				if now.After(s.expiresAt) && s.initializing == nil {
					delete(p.keys, k)
				}
			}
			for k, v := range p.models {
				if now.Sub(v.fetchedAt) > time.Duration(p.cfg.ModelRefreshIntervalMS)*time.Millisecond {
					delete(p.models, k)
				}
			}
			p.mu.Unlock()
		}
	}
}
