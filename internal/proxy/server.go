package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errIdle = errors.New("upstream read idle timeout")

// Time only a blocking upstream read. Waiting for the client to drain must not
// consume the upstream idle allowance. Closing via context unblocks net/http.
type idleReader struct {
	body    io.Reader
	timeout time.Duration
	cancel  context.CancelFunc
}

func (r idleReader) Read(b []byte) (int, error) {
	fired := make(chan struct{})
	timer := time.AfterFunc(r.timeout, func() { r.cancel(); close(fired) })
	n, err := r.body.Read(b)
	if !timer.Stop() {
		<-fired
		return n, errIdle
	}
	return n, err
}

func sendJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func sendAPIError(w http.ResponseWriter, protocol string, e *APIError) {
	detail := M{"type": e.Type, "message": e.Message}
	if e.Code != "" {
		detail["code"] = e.Code
	}
	body := M{"error": detail}
	if protocol == "messages" {
		body["type"] = "error"
	}
	if e.RetryAfter > 0 {
		body["retry_after"] = e.RetryAfter
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	sendJSON(w, e.Status, body)
}
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/health" || r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "OK")
		return
	}
	if p.cfg.MaxInflight > 0 {
		if p.inflight.Add(1) > int64(p.cfg.MaxInflight) {
			p.inflight.Add(-1)
			sendAPIError(w, "chat", &APIError{Status: 503, Type: "server_busy", Message: fmt.Sprintf("Too many concurrent requests (limit %d), retry shortly", p.cfg.MaxInflight), RetryAfter: 5})
			return
		}
		defer p.inflight.Add(-1)
	}
	if r.URL.Path == "/v1/models" && r.Method == http.MethodGet {
		ids := p.fetchModels(r.Context(), getAPIKey(r.Header))
		data := make([]any, 0, len(ids))
		for _, id := range ids {
			data = append(data, M{"id": id, "object": "model", "created": time.Now().Unix(), "owned_by": "command-code"})
		}
		sendJSON(w, 200, M{"object": "list", "data": data})
		return
	}
	protocol := map[string]string{"/v1/chat/completions": "chat", "/v1/messages": "messages", "/v1/responses": "responses"}[r.URL.Path]
	if protocol == "" || r.Method != http.MethodPost {
		sendAPIError(w, "chat", &APIError{Status: 404, Type: "not_found", Message: "Not found"})
		return
	}
	p.handleGeneration(w, r, protocol)
}

func (p *Proxy) handleGeneration(w http.ResponseWriter, r *http.Request, protocol string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, p.cfg.MaxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var input M
	err := decoder.Decode(&input)
	if err == nil {
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			if e == nil {
				err = errors.New("multiple JSON values")
			} else {
				err = e
			}
		}
	}
	if err != nil || input == nil {
		status, message := 400, "Invalid JSON body"
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			status = 413
			message = fmt.Sprintf("Request body too large (limit %d MB)", p.cfg.MaxBodyBytes>>20)
		}
		sendAPIError(w, protocol, &APIError{Status: status, Type: "invalid_request_error", Message: message})
		return
	}
	key := getAPIKey(r.Header)
	if key == "" {
		kind := "authentication_error"
		if protocol == "chat" {
			kind = "auth_error"
		}
		sendAPIError(w, protocol, &APIError{Status: 401, Type: kind, Message: "Missing API key. Send in Authorization: Bearer <key> or x-api-key header"})
		return
	}
	chat := input
	switch protocol {
	case "messages":
		chat = convertAnthropicToChat(input)
	case "responses":
		if truth(input["previous_response_id"]) || truth(input["store"]) {
			sendAPIError(w, protocol, &APIError{Status: 400, Type: "invalid_request_error", Message: "previous_response_id and store=true are not supported (this proxy is stateless); send the full input each turn"})
			return
		}
		chat = convertResponsesToChat(input)
	}
	if len(arr(chat["messages"])) == 0 {
		sendAPIError(w, protocol, &APIError{Status: 400, Type: "invalid_request_error", Message: "messages or input is required"})
		return
	}
	stream, _ := chat["stream"].(bool)
	model := str(chat["model"])
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
		if protocol == "messages" {
			model = "claude-sonnet-4-6"
		}
		chat["model"] = model
	}
	id := "chatcmpl-" + newID()[:12]
	if protocol == "messages" {
		id = "msg_" + strings.ReplaceAll(newID(), "-", "")
	}
	if protocol == "responses" {
		id = "resp_" + strings.ReplaceAll(newID(), "-", "")
	}
	translator := NewTranslator(protocol, model, id, time.Now().Unix(), input)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if err = p.ensureInitialized(ctx, key); err != nil {
		return
	}
	idleTimeout := p.cfg.NonstreamIdle
	if stream {
		idleTimeout = p.cfg.StreamIdle
	}
	headerExpired := make(chan struct{})
	timer := time.AfterFunc(idleTimeout, func() { cancel(); close(headerExpired) })
	upstream, err := p.forward(ctx, buildCCRequest(chat, p.cfg), key, r.Header, str(chat["prompt_cache_key"]))
	headerTimeout := !timer.Stop()
	if headerTimeout {
		<-headerExpired
		if upstream != nil {
			upstream.Body.Close()
		}
		err = errIdle
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		e := &APIError{Status: 502, Type: "proxy_error", Message: "Upstream connection failed", RetryAfter: 10}
		if headerTimeout {
			e = p.timeoutError()
		}
		p.log("warn", "Upstream request failed", M{"path": r.URL.Path, "timeout": headerTimeout})
		sendAPIError(w, protocol, e)
		return
	}
	defer upstream.Body.Close()
	reader := idleReader{body: upstream.Body, timeout: idleTimeout, cancel: cancel}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(reader, 1<<20))
		e := mapCCError(upstream.StatusCode, string(data))
		p.log("warn", "CC API error", M{"path": r.URL.Path, "status": upstream.StatusCode, "code": e.Code})
		sendAPIError(w, protocol, e)
		return
	}
	started := false
	writeEvents := func(events []string) error {
		if len(events) == 0 {
			return nil
		}
		controller := http.NewResponseController(w)
		if p.cfg.ClientDrainTimeout > 0 {
			if e := controller.SetWriteDeadline(time.Now().Add(p.cfg.ClientDrainTimeout)); e != nil {
				return e
			}
			defer controller.SetWriteDeadline(time.Time{})
		}
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(200)
			started = true
		}
		for _, event := range events {
			if _, e := io.WriteString(w, event); e != nil {
				return e
			}
		}
		return controller.Flush()
	}
	scanner := bufio.NewScanner(reader)
	// NDJSON deltas can contain a complete tool argument object. Do not use the
	// Scanner default 64 KiB cap; bound a single line independently of body limits.
	scanner.Buffer(make([]byte, 32<<10), 100<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[DONE]" || strings.HasPrefix(line, ":") {
			continue
		}
		var event M
		d := json.NewDecoder(strings.NewReader(line))
		d.UseNumber()
		if d.Decode(&event) != nil || event == nil {
			continue
		}
		events := translator.Consume(event)
		if stream {
			if err = writeEvents(events); err != nil {
				cancel()
				return
			}
		}
		if translator.Error != nil {
			break
		}
	}
	if err = scanner.Err(); err != nil {
		if r.Context().Err() != nil {
			return
		}
		e := &APIError{Status: 502, Type: "proxy_error", Message: "Upstream stream interrupted", RetryAfter: 10}
		if errors.Is(err, errIdle) {
			e = p.timeoutError()
			p.log("warn", "Stream idle timeout", M{"path": r.URL.Path, "timeoutMs": idleTimeout.Milliseconds()})
		}
		translator.Error = e
		if stream && started {
			_ = writeEvents(translator.Fail(e.Message))
		} else {
			sendAPIError(w, protocol, e)
		}
		return
	}
	if stream {
		events := translator.Finish()
		if translator.Error != nil && !started {
			sendAPIError(w, protocol, translator.Error)
			return
		}
		if err = writeEvents(events); err != nil {
			return
		}
	} else {
		result, e := translator.Result()
		if e != nil {
			sendAPIError(w, protocol, e)
			return
		}
		sendJSON(w, 200, result)
	}
	if translator.Error == nil {
		p.timeouts.Store(0)
	}
}

func (p *Proxy) timeoutError() *APIError {
	message := "Response timeout - request timed out"
	if p.timeouts.Add(1) >= 3 {
		message = "Response timeout - try reducing context length (summarize earlier messages)"
	}
	return &APIError{Status: 429, Type: "rate_limit_error", Message: message, RetryAfter: 5}
}
