package proxy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// APIError is a protocol-independent error, including the downstream HTTP status.
type APIError struct {
	Status     int
	Type       string
	Message    string
	RetryAfter int
	Code       string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) detail() M {
	v := M{"type": e.Type, "message": e.Message}
	if e.Code != "" {
		v["code"] = e.Code
	}
	return v
}

func ccStatusError(status int, message, code string) *APIError {
	e := &APIError{Status: 502, Type: "upstream_error", Message: message, Code: code}
	switch status {
	case 400, 422:
		e.Status, e.Type = 400, "invalid_request_error"
	case 401, 403:
		e.Status, e.Type = 401, "authentication_error"
	case 402, 429:
		e.Status, e.Type, e.RetryAfter = 429, "rate_limit_error", 30
	case 404:
		e.Status, e.Type = 404, "not_found"
	case 503:
		e.Status, e.Type = 503, "temporarily_unavailable"
	}
	return e
}

func mapCCError(status int, body string) *APIError {
	message, code := fmt.Sprintf("CC API error (%d)", status), ""
	if body != "" {
		var parsed M
		if json.Unmarshal([]byte(body), &parsed) == nil {
			detail := obj(parsed["error"])
			if v := str(detail["message"]); v != "" {
				message = v
			} else if v := str(parsed["message"]); v != "" {
				message = v
			}
			code = str(detail["code"])
			if code == "" {
				code = str(parsed["code"])
			}
		} else {
			runes := []rune(body)
			if len(runes) > 200 {
				runes = runes[:200]
			}
			message = string(runes)
		}
	}
	return ccStatusError(status, message, code)
}

var ccStatusPrefix = regexp.MustCompile(`^<(\d{3})>`)
var ccNetworkFinish = regexp.MustCompile(`^(network|connection|upstream)[-_\s]?error$`)

func mapCCEventError(event M) *APIError {
	detail := obj(event["error"])
	message, code := str(detail["message"]), str(detail["code"])
	if message == "" {
		message = str(event["message"])
	}
	if message == "" {
		message = "Unknown CC error"
	}
	if code == "" {
		code = str(event["code"])
	}
	status := int(num(detail["statusCode"]))
	if matched := ccStatusPrefix.FindStringSubmatch(message); matched != nil {
		status, _ = strconv.Atoi(matched[1])
	}
	return ccStatusError(status, message, code)
}

func mapFinishReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch r {
	case "":
		return "stop"
	case "tool-calls", "tool_calls", "tool_use":
		return "tool_calls"
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		return "length"
	}
	if ccNetworkFinish.MatchString(r) {
		return "upstream_error"
	}
	return r
}

func openAIFinishReason(reason string) string {
	if reason == "pause_turn" {
		return "length"
	}
	return reason
}

func anthropicStopReason(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "pause_turn", "refusal", "stop_sequence":
		return reason
	default:
		return "end_turn"
	}
}

// These signatures preserve the legacy proxy's display compatibility. They are
// synthetic and must not be mistaken for cryptographically valid Anthropic ones.
func fakeThinkingSignature(thinking string) string {
	if thinking == "" {
		thinking = "dsh-proxy-thinking"
	}
	hash := sha256.Sum256([]byte(thinking))
	return base64.StdEncoding.EncodeToString(append([]byte{0x12, 32}, hash[:]...))
}

type responseItem struct {
	kind  string
	index int
	item  M
	text  strings.Builder
}

// Translator consumes CC NDJSON objects. Finish must be called at EOF; it is the
// only place success is emitted, so a truncated stream cannot report completion.
type Translator struct {
	Error      *APIError
	HasContent bool
	Started    bool
	LastEvent  string

	protocol, model, id string
	created             int64
	opts                M
	text, thinking      strings.Builder
	tools               []any
	usage               M
	sawFinish, ended    bool
	finishReason        string
	sequence, nextIndex int
	current             *responseItem
	doneItems           []any
}

func NewTranslator(protocol, model, id string, created int64, opts M) *Translator {
	return &Translator{protocol: protocol, model: model, id: id, created: created,
		opts: opts, finishReason: "stop", tools: []any{}, doneItems: []any{}}
}

func encodeSSE(event string, value M) string {
	b, _ := json.Marshal(value)
	if event != "" {
		return "event: " + event + "\ndata: " + string(b) + "\n\n"
	}
	return "data: " + string(b) + "\n\n"
}

func (t *Translator) named(event string, value M) string {
	value["type"] = event
	if t.protocol == "responses" {
		value["sequence_number"] = t.sequence
		t.sequence++
	}
	return encodeSSE(event, value)
}

func (t *Translator) chatChunk(delta M, reason any, usage M) string {
	value := M{"id": t.id, "object": "chat.completion.chunk", "created": t.created, "model": t.model,
		"choices": []any{M{"index": 0, "delta": delta, "finish_reason": reason}}}
	if usage != nil {
		value["usage"] = usage
	}
	return encodeSSE("", value)
}

func (t *Translator) start() []string {
	if t.Started {
		return nil
	}
	t.Started = true
	switch t.protocol {
	case "messages":
		return []string{t.named("message_start", M{"message": M{"id": t.id, "type": "message", "role": "assistant",
			"content": []any{}, "model": t.model, "stop_reason": nil, "stop_sequence": nil,
			"usage": M{"input_tokens": 0, "output_tokens": 0}}})}
	case "responses":
		return []string{t.named("response.created", M{"response": t.responsesBase("in_progress", []any{})}),
			t.named("response.in_progress", M{"response": t.responsesBase("in_progress", []any{})})}
	}
	return nil
}

func toolArguments(input any) string {
	if s, ok := input.(string); ok {
		return s
	}
	if input == nil {
		input = M{}
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func (t *Translator) Consume(event M) []string {
	if t.ended || t.Error != nil {
		return nil
	}
	t.LastEvent = str(event["type"])
	text := str(event["text"])
	var call M
	switch t.LastEvent {
	case "error":
		t.Error = mapCCEventError(event)
		return nil
	case "finish", "finish-step":
		t.sawFinish = true
		if reason := str(event["finishReason"]); reason != "" {
			// Do not let a generic final stop hide a preceding connection failure.
			if t.finishReason != "upstream_error" {
				t.finishReason = mapFinishReason(reason)
			}
		}
		if u := obj(event["totalUsage"]); u != nil {
			t.usage = u
		} else if u := obj(event["usage"]); u != nil {
			t.usage = u
		}
		return nil
	case "text-delta":
		if text == "" {
			text = str(event["delta"])
		}
		if text == "" {
			return nil
		}
		t.text.WriteString(text)
	case "reasoning-delta":
		if text == "" {
			return nil
		}
		t.thinking.WriteString(text)
	case "tool-call":
		id := str(event["toolCallId"])
		if id == "" {
			id = "call_" + newID()
		}
		call = M{"id": id, "type": "function", "function": M{"name": str(event["toolName"]), "arguments": toolArguments(event["input"])}}
		t.tools = append(t.tools, call)
	default:
		return nil
	}
	t.HasContent = true
	first := !t.Started
	out := t.start()
	switch t.protocol {
	case "messages":
		return append(out, t.messageDelta(text, call)...)
	case "responses":
		return append(out, t.responseDelta(text, call)...)
	default:
		delta := M{}
		if first {
			delta["role"] = "assistant"
		}
		switch t.LastEvent {
		case "text-delta":
			delta["content"] = text
		case "reasoning-delta":
			delta["reasoning_content"] = text
		case "tool-call":
			entry := M{"index": len(t.tools) - 1, "id": call["id"], "type": "function", "function": call["function"]}
			delta["tool_calls"] = []any{entry}
			if first {
				delta["content"] = nil
			}
		}
		return append(out, t.chatChunk(delta, nil, nil))
	}
}

func (t *Translator) messageDelta(text string, call M) []string {
	out := []string{}
	if call != nil {
		out = append(out, t.closeCurrent()...)
		index := t.nextIndex
		t.nextIndex++
		fn := obj(call["function"])
		return append(out,
			t.named("content_block_start", M{"index": index, "content_block": M{"type": "tool_use", "id": call["id"], "name": fn["name"], "input": M{}}}),
			t.named("content_block_delta", M{"index": index, "delta": M{"type": "input_json_delta", "partial_json": fn["arguments"]}}),
			t.named("content_block_stop", M{"index": index}))
	}
	kind, deltaKind := "text", "text_delta"
	if t.LastEvent == "reasoning-delta" {
		kind, deltaKind = "thinking", "thinking_delta"
	}
	if t.current == nil || t.current.kind != kind {
		out = append(out, t.closeCurrent()...)
		t.current = &responseItem{kind: kind, index: t.nextIndex}
		t.nextIndex++
		out = append(out, t.named("content_block_start", M{"index": t.current.index, "content_block": M{"type": kind, kind: ""}}))
	}
	t.current.text.WriteString(text)
	return append(out, t.named("content_block_delta", M{"index": t.current.index, "delta": M{"type": deltaKind, kind: text}}))
}

func (t *Translator) responseDelta(text string, call M) []string {
	kind := "message"
	if t.LastEvent == "reasoning-delta" {
		kind = "reasoning"
	}
	if call != nil {
		kind = "function_call"
	}
	out := []string{}
	if t.current == nil || t.current.kind != kind || call != nil {
		out = append(out, t.closeCurrent()...)
		item := M{"type": kind, "status": "in_progress"}
		switch kind {
		case "message":
			item["id"], item["role"], item["content"] = "msg_"+newID(), "assistant", []any{}
		case "reasoning":
			item["id"], item["summary"] = "rs_"+newID(), []any{}
		case "function_call":
			item["id"], item["call_id"], item["name"], item["arguments"] = "fc_"+newID(), call["id"], obj(call["function"])["name"], ""
		}
		t.current = &responseItem{kind: kind, index: t.nextIndex, item: item}
		t.nextIndex++
		out = append(out, t.named("response.output_item.added", M{"output_index": t.current.index, "item": item}))
		data := M{"item_id": item["id"], "output_index": t.current.index}
		if kind == "message" {
			data["content_index"], data["part"] = 0, M{"type": "output_text", "text": "", "annotations": []any{}}
			out = append(out, t.named("response.content_part.added", data))
		} else if kind == "reasoning" {
			data["summary_index"], data["part"] = 0, M{"type": "summary_text", "text": ""}
			out = append(out, t.named("response.reasoning_summary_part.added", data))
		}
	}
	data := M{"item_id": t.current.item["id"], "output_index": t.current.index, "delta": text}
	switch kind {
	case "function_call":
		args := obj(call["function"])["arguments"]
		t.current.item["arguments"], data["delta"] = args, args
		out = append(out, t.named("response.function_call_arguments.delta", data))
	case "reasoning":
		t.current.text.WriteString(text)
		data["summary_index"] = 0
		out = append(out, t.named("response.reasoning_summary_text.delta", data))
	case "message":
		t.current.text.WriteString(text)
		data["content_index"], data["logprobs"] = 0, []any{}
		out = append(out, t.named("response.output_text.delta", data))
	}
	return out
}

func (t *Translator) closeCurrent() []string {
	c := t.current
	if c == nil {
		return nil
	}
	t.current = nil
	out := []string{}
	if t.protocol == "messages" {
		if c.kind == "thinking" {
			out = append(out, t.named("content_block_delta", M{"index": c.index, "delta": M{"type": "signature_delta", "signature": fakeThinkingSignature(c.text.String())}}))
		}
		return append(out, t.named("content_block_stop", M{"index": c.index}))
	}
	item := c.item
	data := M{"item_id": item["id"], "output_index": c.index}
	switch c.kind {
	case "message":
		part := M{"type": "output_text", "text": c.text.String(), "annotations": []any{}}
		out = append(out, t.named("response.output_text.done", M{"item_id": item["id"], "output_index": c.index, "content_index": 0, "text": c.text.String(), "logprobs": []any{}}))
		data["content_index"], data["part"] = 0, part
		out = append(out, t.named("response.content_part.done", data))
		item["content"] = []any{part}
	case "reasoning":
		part := M{"type": "summary_text", "text": c.text.String()}
		out = append(out, t.named("response.reasoning_summary_text.done", M{"item_id": item["id"], "output_index": c.index, "summary_index": 0, "text": c.text.String()}))
		data["summary_index"], data["part"] = 0, part
		out = append(out, t.named("response.reasoning_summary_part.done", data))
		item["summary"] = []any{part}
	case "function_call":
		data["arguments"] = item["arguments"]
		out = append(out, t.named("response.function_call_arguments.done", data))
	}
	item["status"] = "completed"
	out = append(out, t.named("response.output_item.done", M{"output_index": c.index, "item": item}))
	t.doneItems = append(t.doneItems, item)
	return out
}

func (t *Translator) validate() {
	if t.Error != nil {
		return
	}
	detail := ""
	if !t.sawFinish {
		detail = "no finish event"
	} else if t.finishReason == "upstream_error" {
		detail = "provider reported an upstream connection failure"
	}
	if detail != "" {
		t.Error = &APIError{Status: 502, Type: "upstream_error", RetryAfter: 10,
			Message: "Upstream stream ended without a completion finish (" + detail + ") — response was truncated"}
	} else if !t.HasContent {
		t.Error = &APIError{Status: 429, Type: "rate_limit_error", RetryAfter: 10, Message: "Empty response from upstream (zero output tokens)"}
	}
}

func (t *Translator) Finish() []string {
	if t.ended {
		return nil
	}
	t.validate()
	if t.Error != nil {
		return t.Fail(t.Error.Message)
	}
	t.ended = true
	out := t.closeCurrent()
	switch t.protocol {
	case "messages":
		out = append(out, t.named("message_delta", M{"delta": M{"stop_reason": anthropicStopReason(t.finishReason), "stop_sequence": nil}, "usage": t.anthropicUsage()}),
			t.named("message_stop", M{}))
	case "responses":
		status, event := "completed", "response.completed"
		if t.finishReason == "length" || t.finishReason == "pause_turn" {
			status, event = "incomplete", "response.incomplete"
		}
		response := t.responsesBase(status, t.doneItems)
		response["output_text"], response["usage"], response["completed_at"] = t.text.String(), t.responsesUsage(), time.Now().Unix()
		response["incomplete_details"] = t.incompleteDetails()
		out = append(out, t.named(event, M{"response": response}))
	default:
		out = append(out, t.chatChunk(M{}, openAIFinishReason(t.finishReason), t.chatUsage()), "data: [DONE]\n\n")
	}
	return out
}

func (t *Translator) Fail(message string) []string {
	if t.ended {
		return nil
	}
	t.ended = true
	if t.Error == nil {
		t.Error = &APIError{Status: 502, Type: "upstream_error", Message: message, RetryAfter: 10}
	}
	if !t.Started {
		return nil
	}
	if t.protocol == "responses" {
		response := t.responsesBase("failed", t.doneItems)
		code := t.Error.Code
		if code == "" {
			code = t.Error.Type
		}
		response["error"] = M{"code": code, "message": t.Error.Message}
		return []string{t.named("response.failed", M{"response": response})}
	}
	body := M{"error": t.Error.detail()}
	if t.Error.RetryAfter > 0 {
		body["retry_after"] = t.Error.RetryAfter
	}
	if t.protocol == "messages" {
		return []string{t.named("error", body)}
	}
	return []string{encodeSSE("", body)}
}

// CC counts cached tokens inside inputTokens. Anthropic counts each category
// separately, whereas both OpenAI APIs include cache tokens in their input total.
func (t *Translator) tokenCounts() (input, output, cached, written, uncached float64) {
	u, details := t.usage, obj(t.usage["inputTokenDetails"])
	input, output = num(u["inputTokens"]), num(u["outputTokens"])
	cached, written = num(u["cachedInputTokens"]), num(details["cacheWriteTokens"])
	if _, ok := u["cachedInputTokens"]; !ok {
		cached = num(details["cacheReadTokens"])
	}
	// A provider reporting zero output must not charge input or cache usage.
	if output <= 0 {
		input, output, cached, written = 0, 0, 0, 0
		return
	}
	uncached = math.Max(0, input-cached-written)
	if v, ok := details["noCacheTokens"]; ok && num(v) >= 0 {
		uncached = num(v)
	}
	return
}

func (t *Translator) chatUsage() M {
	in, out, cached, _, _ := t.tokenCounts()
	return M{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out,
		"prompt_tokens_details": M{"cached_tokens": cached}}
}

func (t *Translator) anthropicUsage() M {
	_, out, cached, written, uncached := t.tokenCounts()
	if out <= 0 && t.HasContent {
		out = math.Max(1, math.Ceil(float64(utf8.RuneCountInString(t.text.String())+utf8.RuneCountInString(t.thinking.String()))/4)+float64(len(t.tools)*20))
	}
	return M{"input_tokens": uncached, "output_tokens": out, "cache_read_input_tokens": cached, "cache_creation_input_tokens": written}
}

func (t *Translator) responsesUsage() M {
	in, out, cached, written, _ := t.tokenCounts()
	return M{"input_tokens": in, "output_tokens": out, "total_tokens": in + out,
		"input_tokens_details":  M{"cached_tokens": cached, "cache_write_tokens": written},
		"output_tokens_details": M{"reasoning_tokens": num(obj(t.usage["outputTokenDetails"])["reasoningTokens"])}}
}

func (t *Translator) incompleteDetails() any {
	if t.finishReason == "length" {
		return M{"reason": "max_output_tokens"}
	}
	if t.finishReason == "pause_turn" {
		return M{"reason": "pause_turn"}
	}
	return nil
}

func (t *Translator) responsesBase(status string, output []any) M {
	value := M{"id": t.id, "object": "response", "created_at": t.created, "status": status,
		"output": output, "output_text": "", "model": t.model, "error": nil, "incomplete_details": nil,
		"parallel_tool_calls": true, "previous_response_id": nil, "store": false, "metadata": M{},
		"input": []any{}, "instructions": nil, "max_output_tokens": nil, "reasoning": nil,
		"temperature": 1, "top_p": 1, "tool_choice": "auto", "tools": []any{}, "user": nil,
		"text": M{"format": M{"type": "text"}}, "truncation": "disabled"}
	for _, key := range []string{"instructions", "max_output_tokens", "reasoning", "temperature", "top_p", "tools", "tool_choice"} {
		if v, ok := t.opts[key]; ok {
			value[key] = v
		}
	}
	return value
}

func (t *Translator) Result() (M, *APIError) {
	t.validate()
	if t.Error != nil {
		return nil, t.Error
	}
	switch t.protocol {
	case "messages":
		content := []any{}
		if t.thinking.Len() > 0 {
			content = append(content, M{"type": "thinking", "thinking": t.thinking.String(), "signature": fakeThinkingSignature(t.thinking.String())})
		}
		if t.text.Len() > 0 {
			content = append(content, M{"type": "text", "text": t.text.String()})
		}
		for _, raw := range t.tools {
			call := obj(raw)
			fn := obj(call["function"])
			var input any
			if json.Unmarshal([]byte(str(fn["arguments"])), &input) != nil || input == nil {
				input = M{}
			}
			content = append(content, M{"type": "tool_use", "id": call["id"], "name": fn["name"], "input": input})
		}
		return M{"id": t.id, "type": "message", "role": "assistant", "model": t.model, "content": content,
			"stop_reason": anthropicStopReason(t.finishReason), "stop_sequence": nil, "usage": t.anthropicUsage()}, nil
	case "responses":
		t.closeCurrent()
		status := "completed"
		if t.incompleteDetails() != nil {
			status = "incomplete"
		}
		value := t.responsesBase(status, t.doneItems)
		value["completed_at"], value["output_text"], value["usage"] = time.Now().Unix(), t.text.String(), t.responsesUsage()
		value["incomplete_details"] = t.incompleteDetails()
		return value, nil
	default:
		message := M{"role": "assistant", "content": nil}
		if t.text.Len() > 0 {
			message["content"] = t.text.String()
		}
		if t.thinking.Len() > 0 {
			message["reasoning_content"] = t.thinking.String()
		}
		if len(t.tools) > 0 {
			message["tool_calls"] = t.tools
		}
		return M{"id": t.id, "object": "chat.completion", "created": t.created, "model": t.model,
			"choices": []any{M{"index": 0, "message": message, "finish_reason": openAIFinishReason(t.finishReason)}},
			"usage":   t.chatUsage()}, nil
	}
}
