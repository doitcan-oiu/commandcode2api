package proxy

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"commandcode2api/internal/gateway"
	"commandcode2api/internal/usage"
	"commandcode2api/internal/webui"
)

// EnableGateway enables durable account pooling and the authenticated admin UI.
// New remains a stateless protocol adapter for callers that only need that mode.
func (p *Proxy) EnableGateway() error {
	manager, err := gateway.Open(gateway.Options{
		DataDir:       p.cfg.DataDir,
		AdminPassword: p.cfg.AdminPassword,
		UsageClient:   usage.NewClient(p.cfg.APIBase, p.client),
	})
	if err != nil {
		return err
	}
	p.manager = manager
	p.adminHandler = manager.Handler()
	p.webHandler = webui.Handler(p.cfg.WebDir)
	if manager.SetupRequired() {
		p.log("info", "首次启动：打开管理页面，设置管理员用户名和密码", nil)
	}
	return nil
}

func credentialFromHeaders(h http.Header) string {
	if fields := strings.Fields(h.Get("Authorization")); len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1]
	}
	return strings.TrimSpace(h.Get("X-Api-Key"))
}

func gatewayError(err error) *APIError {
	e := &APIError{Status: 503, Type: "temporarily_unavailable", Message: "Gateway is temporarily unavailable", RetryAfter: 5}
	var ge *gateway.Error
	if errors.As(err, &ge) {
		e.Status, e.Message, e.RetryAfter = ge.Status, ge.Message, ge.RetryAfter
		switch ge.Status {
		case 401:
			e.Type = "authentication_error"
		case 403:
			e.Type = "permission_error"
		case 429:
			e.Type = "rate_limit_error"
		case 400:
			e.Type = "invalid_request_error"
		}
	}
	return e
}

func modelAllowed(models []string, model string) bool {
	if len(models) == 0 {
		return true
	}
	for _, allowed := range models {
		if allowed == model || allowed == "*" {
			return true
		}
	}
	return false
}

func requestedSession(h http.Header, chat M) string {
	for _, value := range []string{h.Get("X-Session-Id"), h.Get("X-Claude-Code-Session-Id"), h.Get("Session_id"), str(chat["prompt_cache_key"])} {
		if len(value) >= 8 {
			return value
		}
	}
	return ""
}

func affinitySession(h http.Header, chat M, clientID string) string {
	if session := requestedSession(h, chat); session != "" {
		return clientID + "\x00" + session
	}
	return ""
}

// Scope even explicitly supplied sessions to the client and account, so two
// independently issued tokens cannot accidentally share upstream thread state.
func scopedSessionHeaders(h http.Header, chat M, clientID, accountID string) http.Header {
	copy := h.Clone()
	sum := sha256.Sum256([]byte(clientID + "\x00" + accountID + "\x00" + requestedSession(h, chat)))
	// The upstream validates threadId as a UUID, including its version and
	// variant bits. Formatting an unrestricted hash as 8-4-4-4-12 is not enough.
	sum[6] = sum[6]&0x0f | 0x40
	sum[8] = sum[8]&0x3f | 0x80
	copy.Set("X-Session-Id", fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]))
	return copy
}

func retryable(status int) bool {
	return status == 401 || status == 403 || status == 402 || status == 429 || status >= 500
}

func parseRetryAfter(value string) time.Duration {
	var wait time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(value); err == nil {
		wait = time.Until(date)
	}
	if wait < 0 {
		return 0
	}
	if wait > 24*time.Hour {
		return 24 * time.Hour
	}
	return wait
}
