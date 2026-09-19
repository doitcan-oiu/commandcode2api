package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"commandcode2api/internal/config"
	"commandcode2api/internal/gateway"
)

const fixturePrimary = "user_fixture_primary_only"
const fixtureBackup = "user_fixture_backup_only"

func gatewayFixture(t *testing.T, generate http.HandlerFunc) (*Proxy, *http.Cookie) {
	t.Helper()
	p, _ := integrationProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alpha/generate":
			generate(w, r)
		case "/alpha/whoami":
			fmt.Fprint(w, `{"user":{"id":"fixture","name":"Fixture"},"org":{"id":"fixture"}}`)
		case "/alpha/billing/credits":
			fmt.Fprint(w, `{"credits":{"monthlyCredits":10,"freeCredits":0,"purchasedCredits":0}}`)
		case "/provider/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"m"},{"id":"forbidden"}]}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}, func(c *config.Config) {
		c.DataDir = t.TempDir()
		c.AdminPassword = "fixture-password-123"
		c.CheckProtocolDrift = false
	})
	if err := p.EnableGateway(); err != nil {
		t.Fatal(err)
	}
	w := adminRequest(t, p, nil, "POST", "/login", M{"username": "admin", "password": "fixture-password-123"})
	if w.Code != 200 || len(w.Result().Cookies()) == 0 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	return p, w.Result().Cookies()[0]
}

func adminRequest(t *testing.T, p *Proxy, cookie *http.Cookie, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	r := httptest.NewRequest(method, "/api/admin"+path, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func seedPool(t *testing.T, p *Proxy, cookie *http.Cookie, clientOptions M) string {
	t.Helper()
	for i, key := range []string{fixturePrimary, fixtureBackup} {
		w := adminRequest(t, p, cookie, "POST", "/accounts", M{"key": key, "label": fmt.Sprintf("Account %d", i), "priority": 2 - i})
		if w.Code != 201 {
			t.Fatalf("create account: %d %s", w.Code, w.Body)
		}
	}
	if clientOptions == nil {
		clientOptions = M{"name": "Fixture"}
	}
	w := adminRequest(t, p, cookie, "POST", "/clients", clientOptions)
	var v struct{ Token string }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || w.Code != 201 || v.Token == "" {
		t.Fatalf("create token: %d %s", w.Code, w.Body)
	}
	return v.Token
}

func gatewayPost(p *Proxy, token, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

func TestGatewayFailsOverAndRecordsSingleRequest(t *testing.T) {
	for _, protocol := range []struct{ path, body string }{{"/v1/chat/completions", testChat}, {"/v1/messages", testChat}, {"/v1/responses", `{"model":"m","input":"hi"}`}} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", protocol.path, stream), func(t *testing.T) {
				var primary, backup atomic.Int32
				p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") == "Bearer "+fixturePrimary {
						primary.Add(1)
						w.Header().Set("Retry-After", "120")
						w.WriteHeader(429)
						fmt.Fprint(w, `{"error":{"message":"quota exhausted"}}`)
						return
					}
					if r.Header.Get("Authorization") != "Bearer "+fixtureBackup {
						t.Error("downstream token forwarded upstream")
					}
					backup.Add(1)
					fmt.Fprintln(w, `{"type":"text-delta","text":"pool works"}`)
					fmt.Fprint(w, testFinish)
				})
				token := seedPool(t, p, cookie, nil)
				body := protocol.body
				if stream {
					body = body[:len(body)-1] + `,"stream":true}`
				}
				w := gatewayPost(p, token, protocol.path, body)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "pool works") || primary.Load() != 1 || backup.Load() != 1 {
					t.Fatalf("response %d %s counts %d/%d", w.Code, w.Body, primary.Load(), backup.Load())
				}
				logs := adminRequest(t, p, cookie, "GET", "/logs", nil)
				var page struct {
					Items []gateway.Record
					Total int
				}
				if err := json.Unmarshal(logs.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if page.Total != 1 || len(page.Items) != 1 {
					t.Fatalf("logs %s", logs.Body)
				}
				rec := page.Items[0]
				if rec.Attempts != 2 || rec.Status != 200 || rec.InputTokens != 9 || rec.OutputTokens != 3 || rec.ID != w.Header().Get("X-Request-Id") {
					t.Fatalf("record %+v", rec)
				}
				accounts := adminRequest(t, p, cookie, "GET", "/accounts", nil)
				if strings.Contains(accounts.Body.String(), fixturePrimary) || strings.Contains(logs.Body.String(), token) {
					t.Fatal("credentials leaked")
				}
				w = gatewayPost(p, token, protocol.path, body)
				if w.Code != 200 || primary.Load() != 1 || backup.Load() != 2 {
					t.Fatal("cooled account was reused")
				}
			})
		}
	}
}

func TestGatewayDoesNotReplayCommittedStreamOrBadRequest(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var calls atomic.Int32
			p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !stream {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"message":"invalid prompt"}`)
					return
				}
				fmt.Fprintln(w, `{"type":"text-delta","text":"partial"}`)
				w.(http.Flusher).Flush()
				fmt.Fprintln(w, `{"type":"error","message":"<503> disconnected"}`)
			})
			token := seedPool(t, p, cookie, nil)
			body := testChat
			if stream {
				body = body[:len(body)-1] + `,"stream":true}`
			}
			w := gatewayPost(p, token, "/v1/chat/completions", body)
			if calls.Load() != 1 {
				t.Fatal("unsafe request replayed")
			}
			if stream {
				if w.Code != 200 || !strings.Contains(w.Body.String(), "partial") || strings.Contains(w.Body.String(), "[DONE]") {
					t.Fatalf("bad stream %s", w.Body)
				}
			} else if w.Code != 400 {
				t.Fatalf("want 400 got %d", w.Code)
			}
		})
	}
}

func TestGatewayAuthModelFilterAndRequestQuota(t *testing.T) {
	p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"text-delta","text":"ok"}`)
		fmt.Fprint(w, testFinish)
	})
	token := seedPool(t, p, cookie, M{"name": "Limited", "maxRequests": 1, "models": []string{"m"}})
	for _, invalid := range []string{"", fixturePrimary, "ccg_invalid"} {
		w := gatewayPost(p, invalid, "/v1/chat/completions", testChat)
		if w.Code != 401 {
			t.Fatalf("accepted invalid token: %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "forbidden") {
		t.Fatalf("models %d %s", w.Code, w.Body)
	}
	denied := gatewayPost(p, token, "/v1/chat/completions", strings.Replace(testChat, `"m"`, `"forbidden"`, 1))
	if denied.Code != 403 {
		t.Fatalf("model permission %d", denied.Code)
	}
	first := gatewayPost(p, token, "/v1/chat/completions", testChat)
	second := gatewayPost(p, token, "/v1/chat/completions", testChat)
	if first.Code != 200 || second.Code != 429 {
		t.Fatalf("quota %d/%d", first.Code, second.Code)
	}
	clients := adminRequest(t, p, cookie, "GET", "/clients", nil)
	var list struct{ Items []gateway.Client }
	json.Unmarshal(clients.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].UsedRequests != 1 || list.Items[0].UsedTokens != 12 || list.Items[0].Inflight != 0 {
		t.Fatalf("clients %s", clients.Body)
	}
	unauth := adminRequest(t, p, nil, "GET", "/accounts", nil)
	if unauth.Code != 401 || unauth.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("admin route exposed via public CORS")
	}
}

func TestGatewayPreStreamFailureRedactsCredentials(t *testing.T) {
	var calls atomic.Int32
	p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		fmt.Fprintf(w, "{\"type\":\"error\",\"message\":\"<503> %s\",\"code\":\"%s\"}\n", key, key)
	})
	token := seedPool(t, p, cookie, nil)
	w := gatewayPost(p, token, "/v1/chat/completions", testChat[:len(testChat)-1]+`,"stream":true}`)
	if w.Code != 503 || calls.Load() != 2 || strings.Contains(w.Body.String(), fixturePrimary) || strings.Contains(w.Body.String(), fixtureBackup) || strings.Contains(w.Header().Get("Content-Type"), "event-stream") {
		t.Fatalf("failure %d %s", w.Code, w.Body)
	}
	logs := adminRequest(t, p, cookie, "GET", "/logs", nil)
	if strings.Contains(logs.Body.String(), fixturePrimary) || strings.Contains(logs.Body.String(), fixtureBackup) {
		t.Fatal("log leaked key")
	}
}

func TestGatewayScopesUpstreamSessionByClientAndAccount(t *testing.T) {
	h := http.Header{"X-Session-Id": []string{"12345678-session"}}
	a := scopedSessionHeaders(h, nil, "clientA", "accountA").Get("X-Session-Id")
	b := scopedSessionHeaders(h, nil, "clientB", "accountA").Get("X-Session-Id")
	c := scopedSessionHeaders(h, nil, "clientA", "accountB").Get("X-Session-Id")
	if a == b || a == c || !uuidPattern.MatchString(a) || a != scopedSessionHeaders(h, nil, "clientA", "accountA").Get("X-Session-Id") {
		t.Fatal("session isolation failed")
	}
	strictUUID := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, session := range []string{a, b, c, scopedSessionHeaders(http.Header{"X-Session-Id": {"fixture-session"}}, nil, "fixture-client", "fixture-account").Get("X-Session-Id")} {
		if !strictUUID.MatchString(session) {
			t.Errorf("upstream rejects session with invalid UUID version or variant: %s", session)
		}
	}
	if h.Get("X-Session-Id") != "12345678-session" {
		t.Fatal("scoping changed the client's request headers")
	}
	if affinitySession(http.Header{}, nil, "clientA") != "" {
		t.Fatal("requests without a session must balance")
	}
}

func TestGatewayThreadIDPassesUpstreamUUIDValidation(t *testing.T) {
	strictUUID := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	var calls atomic.Int32
	p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body M
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		threadID := str(body["threadId"])
		if !strictUUID.MatchString(threadID) || threadID != r.Header.Get("X-Session-Id") {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"code":"BAD_REQUEST","message":"Validation error: Invalid UUID at threadId"}}`)
			return
		}
		fmt.Fprintln(w, `{"type":"text-delta","text":"valid session"}`)
		fmt.Fprintln(w, testFinish)
	})
	token := seedPool(t, p, cookie, nil)
	for _, protocol := range []struct{ path, body string }{
		{"/v1/chat/completions", testChat},
		{"/v1/messages", testChat},
		{"/v1/responses", `{"model":"m","input":"hi"}`},
	} {
		for _, stream := range []bool{false, true} {
			body := protocol.body
			if stream {
				body = body[:len(body)-1] + `,"stream":true}`
			}
			w := gatewayPost(p, token, protocol.path, body)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "valid session") {
				t.Errorf("%s stream=%v rejected: %d %s", protocol.path, stream, w.Code, w.Body)
			}
		}
	}
	if calls.Load() != 6 {
		t.Fatalf("unexpected retries: %d requests", calls.Load())
	}
}

func TestGatewayHTTPFailureDoesNotLeakCredentialsToRuntimeLog(t *testing.T) {
	p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(M{"error": M{"message": key, "code": key}})
	})
	var logs bytes.Buffer
	p.logger = log.New(&logs, "", 0)
	token := seedPool(t, p, cookie, nil)
	w := gatewayPost(p, token, "/v1/chat/completions", testChat)
	for _, key := range []string{fixturePrimary, fixtureBackup, token} {
		if strings.Contains(logs.String(), key) || strings.Contains(w.Body.String(), key) {
			t.Fatal("runtime log or response leaked credential")
		}
	}
	if w.Code != 503 {
		t.Fatalf("want upstream unavailable, got %d", w.Code)
	}
}
