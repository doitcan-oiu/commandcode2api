package proxy

import (
	"encoding/json"
	"reflect"
	"testing"

	"commandcode2api/internal/config"
)

func requestTestDecode(t *testing.T, raw string) M {
	t.Helper()
	var value M
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func requestTestEqual(t *testing.T, actual, expected any) {
	t.Helper()
	a, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got  %s\nwant %s", a, b)
	}
}

func TestBuildCCRequestPreservesWireContract(t *testing.T) {
	request := requestTestDecode(t, `{
		"model":"example/model","max_tokens":300000,"stream":false,"temperature":0,"parallel_tool_calls":false,
		"reasoning_effort":"high","prompt_cache_key":"session", "unknown":"do not forward",
		"messages":[
			{"role":"system","content":"first"},
			{"role":"developer","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]},
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,YQ=="}},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},
			{"role":"assistant","reasoning_content":"think","content":[{"type":"reasoning","text":"duplicate"},{"type":"text","text":"run","cache_control":{"type":"ephemeral"}}],"tool_calls":[{"id":"call_1","function":{"name":"bash_output","arguments":"{\"task\":1}"}}]},
			{"role":"tool","tool_call_id":"call_1","name":"wrong","content":[{"type":"text","text":"one"},{"type":"image","image":"ignored"},{"type":"text","text":"two"}]},
			{"role":"future_role","content":"fallback"}
		],
		"tools":[{"type":"function","function":{"name":"bash_output","description":"read task","parameters":{"type":"object","properties":{"task":{"type":"number"}}}}}],
		"tool_choice":"required"
	}`)
	cfg := config.Config{CLIMode: "plan", DeviceProjectDir: `C:\project`, DevicePlatform: "win32", EmptySystemPlaceholder: true}
	body := buildCCRequest(request, cfg)
	params := obj(body["params"])
	expected := requestTestDecode(t, `{
		"model":"example/model","max_tokens":200000,"stream":true,"temperature":0,"parallel_tool_calls":false,"reasoning_effort":"high",
		"system":[{"type":"text","text":"first\n"},{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","image":"data:image/jpeg;base64,YQ==","mimeType":"image/jpeg"},{"type":"image","image":"https://example.com/a.png"}]},
			{"role":"assistant","content":[{"type":"reasoning","text":"think"},{"type":"text","text":"run","cache_control":{"type":"ephemeral"}},{"type":"tool-call","toolCallId":"call_1","toolName":"bash_output","input":{"task":1}}]},
			{"role":"tool","content":[{"type":"tool-result","toolCallId":"call_1","toolName":"bash_output","output":{"type":"text","value":"one\ntwo"}}]},
			{"role":"user","content":[{"type":"text","text":"fallback"}]}
		],
		"tools":[{"name":"shell_output","description":"read task","input_schema":{"type":"object","properties":{"task":{"type":"number"}}}}],
		"tool_choice":{"type":"any"}
	}`)
	requestTestEqual(t, params, expected)
	if body["mode"] != "plan" || body["permissionMode"] != "standard" || body["skills"] != nil {
		t.Fatalf("incorrect envelope: %#v", body)
	}
	if obj(body["config"])["workingDir"] != `C:\project` || obj(body["config"])["environment"] != "win32" {
		t.Fatalf("incorrect device profile: %#v", body["config"])
	}
}

func TestBuildCCRequestDefaultsAndCaching(t *testing.T) {
	t.Run("empty system placeholder enabled", func(t *testing.T) {
		params := obj(buildCCRequest(M{}, config.Config{EmptySystemPlaceholder: true})["params"])
		requestTestEqual(t, params["system"], []any{M{"type": "text", "text": " "}})
		requestTestEqual(t, params["tools"], []any{})
		if params["model"] != "deepseek/deepseek-v4-flash" || num(params["max_tokens"]) != 64000 {
			t.Fatalf("wrong defaults: %#v", params)
		}
	})
	t.Run("empty system placeholder disabled", func(t *testing.T) {
		params := obj(buildCCRequest(M{}, config.Config{})["params"])
		if _, exists := params["system"]; exists {
			t.Fatal("disabled placeholder must omit system")
		}
	})
	t.Run("cache key marks last system block", func(t *testing.T) {
		req := requestTestDecode(t, `{"messages":[{"role":"system","content":[{"text":"a"},{"text":"b"}]}],"prompt_cache_key":"key"}`)
		params := obj(buildCCRequest(req, config.Config{})["params"])
		blocks := arr(params["system"])
		if _, exists := obj(blocks[0])["cache_control"]; exists {
			t.Fatal("only the last block should receive a cache marker")
		}
		requestTestEqual(t, obj(blocks[1])["cache_control"], M{"type": "ephemeral"})
		// Conversion must not insert newlines or cache markers into client input.
		requestTestEqual(t, obj(arr(req["messages"])[0])["content"], []any{M{"text": "a"}, M{"text": "b"}})
	})
	t.Run("existing message marker prevents another marker", func(t *testing.T) {
		req := requestTestDecode(t, `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"prompt_cache_key":"key"}`)
		params := obj(buildCCRequest(req, config.Config{})["params"])
		if _, exists := obj(arr(params["system"])[0])["cache_control"]; exists {
			t.Fatal("must preserve the client's existing breakpoint")
		}
	})
}

func TestBuildCCRequestToolArgumentsAndAliases(t *testing.T) {
	req := requestTestDecode(t, `{"messages":[
		{"role":"assistant","content":[{"type":"reasoning","text":"preserved"}],"tool_calls":[{"id":"a","function":{"name":"x","arguments":"invalid"}},{"id":"b","function":{"name":"y","arguments":"null"}},{"id":"c","function":{"name":"z","arguments":{"a":1}}}]},
		{"role":"tool","tool_call_id":"missing","name":"fallback","content":null}
	],"tools":[{"name":"task_output"},{"name":"tool_search"},{"name":"read_multiple_files"},{"name":"ordinary"}],"tool_choice":{"type":"function","function":{"name":"tool_search"}}}`)
	params := obj(buildCCRequest(req, config.Config{})["params"])
	parts := arr(obj(arr(params["messages"])[0])["content"])
	requestTestEqual(t, obj(parts[1])["input"], M{})
	if obj(parts[2])["input"] != nil {
		t.Fatal("valid JSON null must not become an empty object")
	}
	requestTestEqual(t, obj(parts[3])["input"], M{"a": 1})
	for i, expected := range []string{"shell_output", "search_tools", "read_file", "ordinary"} {
		if obj(arr(params["tools"])[i])["name"] != expected {
			t.Errorf("tool %d alias mismatch", i)
		}
	}
	requestTestEqual(t, params["tool_choice"], M{"type": "tool", "name": "tool_search"})
	result := obj(arr(obj(arr(params["messages"])[1])["content"])[0])
	if result["toolName"] != "fallback" || obj(result["output"])["value"] != "" {
		t.Fatalf("tool result fallback failed: %#v", result)
	}
}

func TestConvertAnthropicMultimodalToolsAndReasoning(t *testing.T) {
	req := requestTestDecode(t, `{
		"model":"claude","max_tokens":1024,"stream":true,"temperature":0,"top_p":0.8,"stop_sequences":["END"],"metadata":{"user_id":"user"},
		"system":[{"type":"text","text":"","cache_control":{"type":"ephemeral"}}],
		"messages":[
			{"role":"assistant","content":[{"type":"thinking","thinking":"analysis"},{"type":"text","text":"start","cache_control":{"type":"ephemeral"}},{"type":"tool_use","id":"call_1","name":"read","input":{"path":"a"}}]},
			{"role":"user","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"base64","media_type":"image/webp","data":"YQ=="}},{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"done"}]},{"type":"text","text":"after"}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"orphan","content":"recovered"}]},
			{"role":"user","content":"plain"}
		],
		"tools":[{"name":"read","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"read"},"thinking":{"type":"enabled","budget_tokens":10000}
	}`)
	out := convertAnthropicToChat(req)
	messages := arr(out["messages"])
	if len(messages) != 6 {
		t.Fatalf("got %d messages, want 6", len(messages))
	}
	requestTestEqual(t, obj(messages[0])["content"], req["system"])
	assistant := obj(messages[1])
	if assistant["reasoning_content"] != "analysis" {
		t.Fatal("thinking was not preserved")
	}
	requestTestEqual(t, assistant["content"], []any{M{"type": "text", "text": "start", "cache_control": M{"type": "ephemeral"}}})
	if obj(messages[2])["role"] != "tool" || obj(messages[2])["name"] != "read" {
		t.Fatal("tool results must be emitted before user text and images")
	}
	requestTestEqual(t, obj(messages[3])["content"], []any{
		M{"type": "text", "text": "before"}, M{"type": "image_url", "image_url": M{"url": "data:image/webp;base64,YQ=="}}, M{"type": "text", "text": "after"},
	})
	if _, exists := obj(messages[4])["name"]; exists {
		t.Fatal("orphan tool result must not invent a name")
	}
	if obj(messages[5])["content"] != "plain" || out["reasoning_effort"] != "high" {
		t.Fatal("plain content or thinking budget lost")
	}
	requestTestEqual(t, out["tool_choice"], M{"type": "function", "function": M{"name": "read"}})
	params := obj(buildCCRequest(out, config.Config{})["params"])
	if obj(arr(params["system"])[0])["cache_control"] == nil {
		t.Fatal("empty system cache marker must survive both transformations")
	}
	parts := arr(obj(arr(params["messages"])[0])["content"])
	if obj(parts[0])["type"] != "reasoning" || obj(parts[2])["type"] != "tool-call" {
		t.Fatal("wire history must be ordered reasoning, text, tool call")
	}
}

func TestConvertAnthropicThinkingEffort(t *testing.T) {
	tests := []struct {
		name     string
		thinking M
		effort   any
	}{
		{"disabled", M{"type": "disabled", "budget_tokens": 10000}, nil},
		{"none", M{"type": "none", "budget_tokens": 10000}, nil},
		{"adaptive default", M{"type": "adaptive"}, "medium"},
		{"adaptive explicit", M{"type": "adaptive", "effort": "high"}, "high"},
		{"low", M{"type": "enabled", "budget_tokens": 1999}, "low"},
		{"medium", M{"type": "enabled", "budget_tokens": 5000}, "medium"},
		{"high", M{"type": "enabled", "budget_tokens": 10000}, "high"},
		{"unspecified", M{"type": "enabled"}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := convertAnthropicToChat(M{"thinking": test.thinking})
			if out["reasoning_effort"] != test.effort {
				t.Fatalf("got %#v, want %#v", out["reasoning_effort"], test.effort)
			}
		})
	}
}

func TestConvertResponsesEasyMessagesAndToolRoundTrip(t *testing.T) {
	req := requestTestDecode(t, `{
		"model":"model","instructions":[{"text":"sys"}],"stream":true,
		"input":[
			{"role":"developer","content":"more instructions"},
			{"role":"user","content":[{"type":"input_text","text":"hi"}]},
			{"type":"reasoning","summary":[{"text":"step "},{"text":"one"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"calling"}]},
			{"type":"function_call","call_id":"call_1","name":"read","arguments":"{\"path\":\"a\"}"},
			{"type":"function_call_output","call_id":"call_1","output":{"ok":true}},
			{"type":"unknown","role":"user","content":"do not treat as a message"},
			{"role":"user","content":"continue"}
		],
		"tools":[{"type":"web_search"},{"type":"function","name":"read","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"read"},"max_output_tokens":800,"temperature":0,"top_p":0.4,"parallel_tool_calls":false,"reasoning":{"effort":"high"},"unknown":"ignore"
	}`)
	out := convertResponsesToChat(req)
	expected := requestTestDecode(t, `{
		"model":"model","stream":true,
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"system","content":"more instructions"},
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"calling","reasoning_content":"step one","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"{\"ok\":true}"},
			{"role":"user","content":"continue"}
		],
		"tools":[{"type":"function","function":{"name":"read","description":"","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"read"}},"max_tokens":800,"temperature":0,"top_p":0.4,"parallel_tool_calls":false,"reasoning_effort":"high"
	}`)
	requestTestEqual(t, out, expected)
}

func TestConvertResponsesEmptyReasoningAndOutputDefaults(t *testing.T) {
	req := requestTestDecode(t, `{"input":[{"type":"reasoning","content":[{"text":"orphan"}]},{"role":"user","content":"hi"},{"type":"function_call","name":"f"},{"type":"function_call_output","call_id":"unknown"},{"type":"function_call_output","output":null}]}`)
	out := convertResponsesToChat(req)
	messages := arr(out["messages"])
	if len(messages) != 4 || obj(messages[0])["role"] != "user" {
		t.Fatalf("orphan reasoning should not create a history message: %#v", messages)
	}
	call := obj(arr(obj(messages[1])["tool_calls"])[0])
	if len(str(call["id"])) != 13 || str(call["id"])[:5] != "call_" {
		t.Fatalf("missing generated call ID: %#v", call)
	}
	if obj(call["function"])["arguments"] != "{}" || obj(messages[2])["content"] != `""` || obj(messages[3])["content"] != "null" {
		t.Fatal("function argument or output fallback changed")
	}
}
