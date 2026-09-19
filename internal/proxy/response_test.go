package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func translatedEvents(t *testing.T, raw []string) []M {
	t.Helper()
	var events []M
	for _, encoded := range raw {
		for _, line := range strings.Split(encoded, "\n") {
			if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
				continue
			}
			var event M
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
	}
	return events
}

func testTranslator(protocol string) *Translator {
	return NewTranslator(protocol, "test-model", "id-test", 1000, nil)
}

func TestResponseErrorMappings(t *testing.T) {
	for _, tc := range []struct {
		upstream, downstream int
		kind                 string
	}{{400, 400, "invalid_request_error"}, {401, 401, "authentication_error"},
		{402, 429, "rate_limit_error"}, {403, 401, "authentication_error"},
		{404, 404, "not_found"}, {422, 400, "invalid_request_error"},
		{429, 429, "rate_limit_error"}, {500, 502, "upstream_error"},
		{503, 503, "temporarily_unavailable"}, {599, 502, "upstream_error"}} {
		err := mapCCError(tc.upstream, `{"error":{"message":"quota","code":"USAGE_EXCEEDED"}}`)
		if err.Status != tc.downstream || err.Type != tc.kind || err.Code != "USAGE_EXCEEDED" || err.Message != "quota" {
			t.Fatalf("status %d mapped to %#v", tc.upstream, err)
		}
		if err.Status == 429 && err.RetryAfter != 30 {
			t.Fatalf("rate limit retry missing: %#v", err)
		}
	}
	for _, event := range []M{
		{"type": "error", "error": M{"message": "limited", "statusCode": 429}},
		{"type": "error", "error": M{"message": "<429> limited", "statusCode": 500}},
	} {
		if err := mapCCEventError(event); err.Status != 429 || err.RetryAfter != 30 {
			t.Fatalf("event status lost: %#v", err)
		}
	}
	if err := mapCCError(502, strings.Repeat("中", 300)); len([]rune(err.Message)) != 200 {
		t.Fatal("plain error truncation broke UTF-8")
	}
}

func TestTranslationCompletionAndIncompleteStreams(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			for _, signal := range []string{"finish", "finish-step"} {
				tr := testTranslator(protocol)
				if got := tr.Consume(M{"type": "start"}); len(got) != 0 || tr.Started {
					t.Fatal("metadata must not commit HTTP 200")
				}
				tr.Consume(M{"type": "text-delta", "delta": "hello"})
				if got := tr.Consume(M{"type": signal, "finishReason": "stop"}); len(got) != 0 {
					t.Fatal("success emitted before EOF")
				}
				if result, err := tr.Result(); err != nil || result == nil {
					t.Fatalf("content without usage rejected: %#v", err)
				}
				if got := tr.Finish(); len(got) == 0 || tr.Error != nil {
					t.Fatal("missing successful completion")
				}
				if got := tr.Finish(); len(got) != 0 {
					t.Fatal("duplicate terminal event")
				}
			}
			for _, reason := range []string{"", "network-error", "connection_error", "upstream error"} {
				tr := testTranslator(protocol)
				tr.Consume(M{"type": "text-delta", "text": "partial"})
				if reason != "" {
					tr.Consume(M{"type": "finish", "finishReason": reason})
				}
				got := strings.Join(tr.Finish(), "")
				if tr.Error == nil || tr.Error.Status != 502 || !strings.Contains(got, "truncated") {
					t.Fatalf("incomplete stream accepted: %s %#v", got, tr.Error)
				}
				if strings.Contains(got, "response.completed") || strings.Contains(got, "message_stop") || strings.Contains(got, "finish_reason") || strings.Contains(got, "[DONE]") {
					t.Fatalf("incomplete stream emits success: %s", got)
				}
			}
			tr := testTranslator(protocol)
			tr.Consume(M{"type": "finish", "finishReason": "stop", "totalUsage": M{"outputTokens": 50}})
			if got := tr.Finish(); len(got) != 0 || tr.Error == nil || tr.Error.Status != 429 || tr.Started {
				t.Fatal("empty content must return HTTP error even with reported output usage")
			}
		})
	}
}

func TestTranslationLateErrorAndTimeout(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		early := testTranslator(protocol)
		if got := early.Consume(M{"type": "error", "error": M{"statusCode": 429, "message": "limited"}}); len(got) != 0 || early.Started {
			t.Fatalf("%s committed stream for early error", protocol)
		}
		if got := early.Finish(); len(got) != 0 || early.Error == nil || early.Error.Status != 429 {
			t.Fatalf("%s lost HTTP error before stream started", protocol)
		}
		tr := testTranslator(protocol)
		tr.Consume(M{"type": "text-delta", "text": "partial"})
		tr.Consume(M{"type": "finish", "finishReason": "stop"})
		tr.Consume(M{"type": "error", "error": M{"statusCode": 503, "message": "capacity", "code": "BUSY"}})
		if result, err := tr.Result(); result != nil || err == nil || err.Status != 503 || err.Code != "BUSY" {
			t.Fatalf("%s swallowed late error: %#v", protocol, err)
		}
		if got := strings.Join(tr.Finish(), ""); !strings.Contains(got, "capacity") || strings.Contains(got, "response.completed") || strings.Contains(got, "message_stop") {
			t.Fatalf("%s error terminal wrong: %s", protocol, got)
		}
		tr = testTranslator(protocol)
		tr.Consume(M{"type": "text-delta", "text": "partial"})
		tr.Error = &APIError{Status: 429, Type: "rate_limit_error", Message: "timeout", RetryAfter: 5}
		if got := strings.Join(tr.Fail("timeout"), ""); !strings.Contains(got, "rate_limit_error") {
			t.Fatalf("%s timeout type lost: %s", protocol, got)
		}
	}
}

func TestFinishReasonSemantics(t *testing.T) {
	for _, raw := range []string{"length", "max_tokens", "max_output_tokens", "model_context_window_exceeded", "pause_turn"} {
		for _, protocol := range []string{"chat", "messages", "responses"} {
			tr := testTranslator(protocol)
			tr.Consume(M{"type": "text-delta", "text": "partial"})
			tr.Consume(M{"type": "finish", "finishReason": raw})
			result, err := tr.Result()
			if err != nil {
				t.Fatal(err)
			}
			switch protocol {
			case "chat":
				if obj(arr(result["choices"])[0])["finish_reason"] != "length" {
					t.Fatal("chat truncation lost")
				}
			case "messages":
				want := "max_tokens"
				if raw == "pause_turn" {
					want = "pause_turn"
				}
				if result["stop_reason"] != want {
					t.Fatal("Anthropic truncation lost")
				}
			case "responses":
				if result["status"] != "incomplete" || result["incomplete_details"] == nil {
					t.Fatal("Responses truncation lost")
				}
				if got := strings.Join(tr.Finish(), ""); !strings.Contains(got, "response.incomplete") {
					t.Fatal("Responses stream truncation lost")
				}
			}
		}
	}
}

func TestTranslationCacheAccounting(t *testing.T) {
	for _, protocol := range []string{"chat", "messages", "responses"} {
		tr := testTranslator(protocol)
		tr.Consume(M{"type": "text-delta", "text": "ok"})
		tr.Consume(M{"type": "finish", "usage": M{"inputTokens": 100, "outputTokens": 10, "cachedInputTokens": 70,
			"inputTokenDetails": M{"noCacheTokens": 20, "cacheWriteTokens": 10}}})
		result, err := tr.Result()
		if err != nil {
			t.Fatal(err)
		}
		u := obj(result["usage"])
		switch protocol {
		case "chat":
			if num(u["prompt_tokens"]) != 100 || num(u["total_tokens"]) != 110 || num(obj(u["prompt_tokens_details"])["cached_tokens"]) != 70 {
				t.Fatalf("bad chat usage: %#v", u)
			}
		case "messages":
			if num(u["input_tokens"]) != 20 || num(u["cache_read_input_tokens"]) != 70 || num(u["cache_creation_input_tokens"]) != 10 {
				t.Fatalf("cache double counted: %#v", u)
			}
		case "responses":
			if num(u["input_tokens"]) != 100 || num(u["total_tokens"]) != 110 || num(obj(u["input_tokens_details"])["cache_write_tokens"]) != 10 {
				t.Fatalf("bad Responses usage: %#v", u)
			}
		}
	}
	tr := testTranslator("messages")
	tr.Consume(M{"type": "text-delta", "text": "ok"})
	tr.Consume(M{"type": "finish", "usage": M{"inputTokens": 100, "outputTokens": 0, "cachedInputTokens": 70,
		"inputTokenDetails": M{"noCacheTokens": 20, "cacheWriteTokens": 10}}})
	result, _ := tr.Result()
	u := obj(result["usage"])
	if num(u["input_tokens"])+num(u["cache_read_input_tokens"])+num(u["cache_creation_input_tokens"]) != 0 {
		t.Fatalf("charged input for zero reported output: %#v", u)
	}
}

func TestResponsesItemSequenceAndTools(t *testing.T) {
	tr := testTranslator("responses")
	var raw []string
	for _, event := range []M{
		{"type": "reasoning-delta", "text": "think"},
		{"type": "text-delta", "text": "hello"},
		{"type": "tool-call", "toolCallId": "call-one", "toolName": "lookup", "input": M{"query": "天气"}},
		{"type": "tool-call", "toolCallId": "call-two", "toolName": "lookup", "input": `{"query":"time"}`},
		{"type": "finish", "finishReason": "tool-calls", "usage": M{"outputTokens": 10}},
	} {
		raw = append(raw, tr.Consume(event)...)
	}
	raw = append(raw, tr.Finish()...)
	events := translatedEvents(t, raw)
	for i, event := range events {
		if num(event["sequence_number"]) != float64(i) {
			t.Fatalf("non-monotonic sequence: %#v", event)
		}
	}
	final := obj(events[len(events)-1]["response"])
	output := arr(final["output"])
	if len(output) != 4 || final["output_text"] != "hello" || final["status"] != "completed" {
		t.Fatalf("incomplete response output: %#v", final)
	}
	for i, kind := range []string{"reasoning", "message", "function_call", "function_call"} {
		if item := obj(output[i]); item["type"] != kind || item["status"] != "completed" {
			t.Fatalf("wrong output item: %#v", item)
		}
	}
	if obj(output[2])["call_id"] != "call-one" || obj(output[3])["arguments"] != `{"query":"time"}` {
		t.Fatal("tool call identity or arguments lost")
	}
}

func TestAnthropicThinkingBlockTransitions(t *testing.T) {
	// Fixed fixture generated with the original Node SHA-256/base64 algorithm.
	if got := fakeThinkingSignature("first thought"); got != "EiDBSeUU9ZhlUgRp9T9KecBe/nx8imKfQePdA1/Xng2fEw==" {
		t.Fatalf("thinking signature compatibility changed: %s", got)
	}
	tr := testTranslator("messages")
	var raw []string
	for _, event := range []M{
		{"type": "reasoning-delta", "text": "first thought"},
		{"type": "text-delta", "text": "answer"},
		{"type": "reasoning-delta", "text": "second thought"},
		{"type": "tool-call", "toolCallId": "call", "toolName": "search", "input": M{}},
		{"type": "finish", "finishReason": "tool_use"},
	} {
		raw = append(raw, tr.Consume(event)...)
	}
	raw = append(raw, tr.Finish()...)
	events := translatedEvents(t, raw)
	if events[0]["type"] != "message_start" || events[len(events)-1]["type"] != "message_stop" {
		t.Fatal("invalid Anthropic message boundaries")
	}
	started, stopped, signatures := 0, 0, []string{}
	for _, event := range events {
		switch event["type"] {
		case "content_block_start":
			if num(event["index"]) != float64(started) {
				t.Fatal("block index was reused")
			}
			started++
		case "content_block_stop":
			stopped++
		case "content_block_delta":
			delta := obj(event["delta"])
			if delta["type"] == "signature_delta" {
				signatures = append(signatures, str(delta["signature"]))
			}
		case "message_delta":
			if obj(event["delta"])["stop_reason"] != "tool_use" {
				t.Fatal("tool-call stop reason lost")
			}
		}
	}
	if started != 4 || stopped != 4 || len(signatures) != 2 || signatures[0] == signatures[1] {
		t.Fatalf("bad block lifecycle: started=%d stopped=%d signatures=%v", started, stopped, signatures)
	}
}
