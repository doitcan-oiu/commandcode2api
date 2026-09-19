package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"commandcode2api/internal/config"
)

const testChat = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
const testFinish = `{"type":"finish","finishReason":"stop","totalUsage":{"inputTokens":9,"outputTokens":3}}`

func integrationProxy(t *testing.T, handler http.HandlerFunc, configure func(*config.Config)) (*Proxy, *httptest.Server) {
	t.Helper()
	u := httptest.NewServer(handler)
	t.Cleanup(u.Close)
	cfg := config.Default()
	cfg.APIBase = u.URL
	cfg.CheckProtocolDrift = false
	if configure != nil {
		configure(&cfg)
	}
	p, e := New(cfg, log.New(io.Discard, "", 0))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	s := httptest.NewServer(p)
	t.Cleanup(s.Close)
	return p, s
}
func integrationPost(t *testing.T, base, path, body string) *http.Response {
	t.Helper()
	r, e := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer user_test")
	r.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return res
}
func responseText(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestHTTPProtocolsReadFinalLineWithoutNewline(t *testing.T) {
	_, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/generate" {
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.WriteString(w, "{\"type\":\"text-delta\",\"text\":\"你好🌏\"}\n"+testFinish)
	}, nil)
	for _, tc := range []struct{ path, body string }{{"/v1/chat/completions", testChat}, {"/v1/messages", testChat}, {"/v1/responses", `{"model":"m","input":"hi"}`}} {
		for _, stream := range []bool{false, true} {
			body := tc.body
			if stream {
				body = body[:len(body)-1] + `,"stream":true}`
			}
			r := integrationPost(t, s.URL, tc.path, body)
			text := responseText(t, r)
			if r.StatusCode != 200 || !strings.Contains(text, "你好🌏") {
				t.Fatalf("%s stream=%v: %d %s", tc.path, stream, r.StatusCode, text)
			}
		}
	}
}

func TestHTTPValidationAndBodyLimit(t *testing.T) {
	var calls atomic.Int32
	_, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }, func(c *config.Config) { c.MaxBodyBytes = 256 })
	for _, tc := range []struct {
		body   string
		status int
	}{{"null", 400}, {"[]", 400}, {"{", 400}, {testChat + " {}", 400}, {`{"messages":[]}`, 400}, {strings.Repeat(" ", 257), 413}} {
		r := integrationPost(t, s.URL, "/v1/chat/completions", tc.body)
		text := responseText(t, r)
		if r.StatusCode != tc.status {
			t.Fatalf("want %d got %d %s", tc.status, r.StatusCode, text)
		}
	}
	r := integrationPost(t, s.URL, "/v1/responses", `{"input":"hi","store":true}`)
	responseText(t, r)
	if r.StatusCode != 400 {
		t.Fatal(r.StatusCode)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid requests contacted upstream")
	}
}

func TestDisconnectReleasesInflightAndCancelsUpstream(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	p, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/generate" {
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.WriteString(w, "{\"type\":\"text-delta\",\"text\":\"partial\"}\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}, func(c *config.Config) { c.MaxInflight = 1 })
	r := integrationPost(t, s.URL, "/v1/chat/completions", testChat[:len(testChat)-1]+`,"stream":true}`)
	<-entered
	rejected := integrationPost(t, s.URL, "/v1/chat/completions", testChat)
	responseText(t, rejected)
	if rejected.StatusCode != 503 || rejected.Header.Get("Retry-After") != "5" {
		t.Fatal("inflight admission failed")
	}
	health, e := http.Get(s.URL + "/health")
	if e != nil {
		t.Fatal(e)
	}
	responseText(t, health)
	if health.StatusCode != 200 {
		t.Fatal("health was rate limited")
	}
	r.Body.Close()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not canceled")
	}
	deadline := time.Now().Add(time.Second)
	for p.inflight.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if p.inflight.Load() != 0 {
		t.Fatal("inflight slot leaked")
	}
}

func TestIdleTimeoutDeliversErrorAndClosesUpstream(t *testing.T) {
	canceled := make(chan struct{}, 1)
	_, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/generate" {
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.WriteString(w, "{\"type\":\"text-delta\",\"text\":\"partial\"}\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		canceled <- struct{}{}
	}, func(c *config.Config) { c.StreamIdle = 50 * time.Millisecond })
	r := integrationPost(t, s.URL, "/v1/chat/completions", testChat[:len(testChat)-1]+`,"stream":true}`)
	text := responseText(t, r)
	if r.StatusCode != 200 || !strings.Contains(text, "partial") || !strings.Contains(text, "rate_limit_error") {
		t.Fatalf("%d %s", r.StatusCode, text)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("idle upstream was not canceled")
	}
}

func TestIdleTimeoutResetsWithinNDJSONLine(t *testing.T) {
	_, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/generate" {
			_, _ = io.WriteString(w, "{}")
			return
		}
		for _, part := range []string{`{"type":"text-delta",`, `"text":`, `"slow"}`, "\n" + testFinish} {
			_, _ = io.WriteString(w, part)
			w.(http.Flusher).Flush()
			time.Sleep(25 * time.Millisecond)
		}
	}, func(c *config.Config) { c.StreamIdle = 80 * time.Millisecond })
	r := integrationPost(t, s.URL, "/v1/chat/completions", testChat[:len(testChat)-1]+`,"stream":true}`)
	text := responseText(t, r)
	if !strings.Contains(text, "[DONE]") || strings.Contains(text, "rate_limit_error") {
		t.Fatal(text)
	}
}

func TestInitializationSingleFlightAndStableHeaders(t *testing.T) {
	var fingerprintCalls, lifecycleCalls atomic.Int32
	var mu sync.Mutex
	var sessions []string
	_, s := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Command-Code-Version") != config.ProtocolVersion {
			t.Errorf("unexpected protocol version %q", r.Header.Get("X-Command-Code-Version"))
		}
		switch r.URL.Path {
		case "/alpha/fingerprint/record":
			fingerprintCalls.Add(1)
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(w, "{}")
		case "/alpha/lifecycle-events":
			lifecycleCalls.Add(1)
			_, _ = io.WriteString(w, "{}")
		case "/alpha/generate":
			var b M
			if json.NewDecoder(r.Body).Decode(&b) != nil {
				t.Error("bad wire body")
			}
			if b["threadId"] != r.Header.Get("X-Session-Id") {
				t.Error("thread/session mismatch")
			}
			if r.Header.Get("User-Agent") != "cli" || r.Header.Get("X-Project-Slug") != "c-users-dev-projects-app" {
				t.Error("wrong CLI headers")
			}
			mu.Lock()
			sessions = append(sessions, r.Header.Get("X-Session-Id"))
			mu.Unlock()
			_, _ = io.WriteString(w, "{\"type\":\"text-delta\",\"text\":\"ok\"}\n"+testFinish)
		}
	}, nil)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			r := integrationPost(t, s.URL, "/v1/chat/completions", testChat)
			responseText(t, r)
			if r.StatusCode != 200 {
				t.Errorf("status %d", r.StatusCode)
			}
		})
	}
	wg.Wait()
	if fingerprintCalls.Load() != 1 || lifecycleCalls.Load() != 1 {
		t.Fatalf("duplicate initialization: %d %d", fingerprintCalls.Load(), lifecycleCalls.Load())
	}
	for _, id := range sessions {
		if id != sessions[0] {
			t.Fatal("session changed within a key")
		}
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestNPMDriftOnlyFetchesMetadataAndKeepsImplementedVersion(t *testing.T) {
	cfg := config.Default()
	var logs bytes.Buffer
	p, e := New(cfg, log.New(&logs, "", 0))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var calls int
	p.registryClient.Transport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://registry.npmjs.org/command-code/latest" || r.Method != "GET" {
			t.Errorf("unexpected npm request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Session-Id") != "" {
			t.Error("credentials sent to npm")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"999.0.0","dist":{"tarball":"https://untrusted.invalid/execute-me.tgz"}}`)), Header: http.Header{}}, nil
	})
	p.checkProtocolDrift(context.Background())
	if calls != 1 || !strings.Contains(logs.String(), "version drift") || p.authHeaders("user_test").Get("X-Command-Code-Version") != "1.53.1" {
		t.Fatal("registry modified protocol or failed to warn")
	}
	p.cfg.CheckProtocolDrift = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.background(ctx)
	if calls != 1 {
		t.Fatal("disabled check still called registry")
	}
}

func TestModelCacheIsPerKey(t *testing.T) {
	var calls atomic.Int32
	p, _ := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(M{"data": []any{M{"id": r.Header.Get("Authorization")}}})
	}, nil)
	for _, key := range []string{"user_a", "user_b", "user_a"} {
		ids := p.fetchModels(context.Background(), key)
		if len(ids) != 1 || ids[0] != "Bearer "+key {
			t.Fatalf("models crossed key boundary: %v", ids)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("model cache did not cache")
	}
}
