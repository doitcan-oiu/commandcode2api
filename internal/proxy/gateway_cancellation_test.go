package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"commandcode2api/internal/gateway"
)

func TestGatewayClientCancellationReleasesBothLeases(t *testing.T) {
	for _, protocol := range []struct{ path, body string }{
		{"/v1/chat/completions", testChat},
		{"/v1/messages", testChat},
		{"/v1/responses", `{"model":"m","input":"hi"}`},
	} {
		t.Run(protocol.path, func(t *testing.T) {
			// This also bounds the fixture handler if cancellation propagation breaks,
			// so httptest cleanup cannot hang after a failed assertion.
			testCtx, endTest := context.WithTimeout(context.Background(), 5*time.Second)
			defer endTest()
			upstreamCanceled := make(chan struct{}, 4)
			var primary, backup atomic.Int32
			p, cookie := gatewayFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer "+fixturePrimary {
					primary.Add(1)
				} else {
					backup.Add(1)
				}
				fmt.Fprintln(w, `{"type":"text-delta","text":"first streamed content"}`)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					upstreamCanceled <- struct{}{}
				case <-testCtx.Done():
				}
			})
			token := seedPool(t, p, cookie, M{"name": "Cancellation fixture", "maxConcurrent": 1})
			requestFinished := make(chan struct{}, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p.ServeHTTP(w, r)
				requestFinished <- struct{}{}
			}))
			defer server.Close()
			requestCtx, cancelRequest := context.WithCancel(testCtx)
			defer cancelRequest()
			body := protocol.body[:len(protocol.body)-1] + `,"stream":true}`
			r, err := http.NewRequestWithContext(requestCtx, http.MethodPost, server.URL+protocol.path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("stream did not start: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
			}
			reader := bufio.NewReader(response.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatalf("read first SSE data: %v", err)
				}
				if strings.Contains(line, "first streamed content") {
					break
				}
			}
			client, err := p.manager.Authenticate(token)
			if err != nil || client.Inflight != 1 {
				t.Fatalf("client lease was not held while streaming: %+v %v", client, err)
			}
			cancelRequest()
			_ = response.Body.Close()
			select {
			case <-upstreamCanceled:
			case <-testCtx.Done():
				t.Fatal("client cancellation did not cancel the upstream request")
			}
			select {
			case <-requestFinished:
			case <-testCtx.Done():
				t.Fatal("gateway did not finish the canceled request")
			}
			if primary.Load() != 1 || backup.Load() != 0 {
				t.Fatalf("canceled stream failed over: primary=%d backup=%d", primary.Load(), backup.Load())
			}
			client, err = p.manager.Authenticate(token)
			if err != nil || client.Inflight != 0 || client.UsedRequests != 1 || p.inflight.Load() != 0 {
				t.Fatalf("client/global lease leaked after cancellation: %+v global=%d error=%v", client, p.inflight.Load(), err)
			}
			accounts := adminRequest(t, p, cookie, "GET", "/accounts", nil)
			var pool struct{ Items []gateway.Account }
			if err := json.Unmarshal(accounts.Body.Bytes(), &pool); err != nil || len(pool.Items) != 2 {
				t.Fatalf("read account state: %v %s", err, accounts.Body)
			}
			for _, account := range pool.Items {
				if account.Inflight != 0 || account.Status != "active" || account.CooldownUntil != 0 {
					t.Fatalf("cancellation leaked a lease or altered account health: %+v", account)
				}
			}
			logs := adminRequest(t, p, cookie, "GET", "/logs", nil)
			var page struct {
				Items []gateway.Record
				Total int
			}
			if err := json.Unmarshal(logs.Body.Bytes(), &page); err != nil || page.Total != 1 || len(page.Items) != 1 {
				t.Fatalf("request log: %v %s", err, logs.Body)
			}
			entry := page.Items[0]
			if entry.Status != 499 || entry.Attempts != 1 || !entry.Stream || entry.ID != response.Header.Get("X-Request-Id") {
				t.Fatalf("cancellation not recorded correctly: %+v", entry)
			}
		})
	}
}
