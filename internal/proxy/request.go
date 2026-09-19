package proxy

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"commandcode2api/internal/config"
)

var requestImageMIME = regexp.MustCompile(`^data:([^;,]+)`)

// requestString follows JavaScript String() for JSON values. In particular,
// object-valued tool output must not accidentally become Go's map[...] syntax.
func requestString(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		return "[object Object]"
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			if p != nil {
				parts[i] = requestString(p)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}

func requestOr(v, fallback any) any {
	if truth(v) {
		return v
	}
	return fallback
}

func requestJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func requestParseJSON(v string) any {
	var out any
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return M{}
	}
	return out
}

func toWireToolName(name string) string {
	switch name {
	case "bash_output", "task_output":
		return "shell_output"
	case "tool_search":
		return "search_tools"
	case "read_multiple_files":
		return "read_file"
	default:
		return name
	}
}

// Tool outputs use only text blocks; images and other blocks are not serialized
// into the text value accepted by the upstream wire protocol.
func toWireToolOutputValue(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	if parts, ok := content.([]any); ok {
		texts := []string{}
		for _, p := range parts {
			part := obj(p)
			if str(part["type"]) == "text" {
				text := ""
				if part["text"] != nil {
					text = requestString(part["text"])
				}
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	}
	if content == nil {
		return ""
	}
	return requestString(content)
}

func buildCCRequest(req M, cfg config.Config) M {
	system := []any{}
	chat := []any{}
	for _, raw := range arr(req["messages"]) {
		msg := obj(raw)
		role := str(msg["role"])
		if role != "system" && role != "developer" {
			chat = append(chat, raw)
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			if content != "" {
				system = append(system, M{"type": "text", "text": content})
			}
		case []any:
			for _, c := range content {
				part := obj(c)
				text := part["text"]
				if text == nil {
					text = part["content"]
				}
				if text == nil {
					text = ""
				}
				if text == "" && !truth(part["cache_control"]) {
					continue
				}
				block := M{"type": "text", "text": requestString(text)}
				if truth(part["cache_control"]) {
					block["cache_control"] = part["cache_control"]
				}
				system = append(system, block)
			}
		default:
			if content != nil {
				system = append(system, M{"type": "text", "text": requestString(content)})
			}
		}
	}
	for i := 0; i < len(system)-1; i++ {
		block := obj(system[i])
		block["text"] = str(block["text"]) + "\n"
	}

	toolNames := map[string]string{}
	for _, raw := range chat {
		msg := obj(raw)
		if str(msg["role"]) == "assistant" {
			for _, rawCall := range arr(msg["tool_calls"]) {
				call := obj(rawCall)
				if truth(call["id"]) {
					toolNames[str(call["id"])] = str(obj(call["function"])["name"])
				}
			}
		}
	}

	messages := []any{}
	for _, raw := range chat {
		msg := obj(raw)
		role := str(msg["role"])
		parts := []any{}
		switch role {
		case "user":
			switch content := msg["content"].(type) {
			case string:
				parts = append(parts, M{"type": "text", "text": content})
			case []any:
				for _, p := range content {
					if !truth(p) {
						continue
					}
					part := obj(p)
					if str(part["type"]) == "image_url" {
						url := str(obj(part["image_url"])["url"])
						image := M{"type": "image", "image": url}
						if match := requestImageMIME.FindStringSubmatch(url); len(match) > 1 {
							image["mimeType"] = match[1]
						}
						parts = append(parts, image)
					} else {
						parts = append(parts, p)
					}
				}
			default:
				text := requestString(content)
				if _, exists := msg["content"]; !exists {
					text = "undefined"
				}
				parts = append(parts, M{"type": "text", "text": text})
			}
		case "assistant":
			if truth(msg["reasoning_content"]) {
				parts = append(parts, M{"type": "reasoning", "text": msg["reasoning_content"]})
			}
			if text, ok := msg["content"].(string); ok && text != "" {
				parts = append(parts, M{"type": "text", "text": text})
			} else {
				for _, p := range arr(msg["content"]) {
					part := obj(p)
					if str(part["type"]) == "text" || (str(part["type"]) == "reasoning" && !truth(msg["reasoning_content"])) {
						parts = append(parts, p)
					}
				}
			}
			for _, rawCall := range arr(msg["tool_calls"]) {
				call := obj(rawCall)
				fn := obj(call["function"])
				input := requestOr(fn["arguments"], M{})
				if s, ok := fn["arguments"].(string); ok {
					input = requestParseJSON(s)
				}
				block := M{"type": "tool-call", "toolName": str(fn["name"]), "input": input}
				if id, ok := call["id"]; ok {
					block["toolCallId"] = id
				}
				parts = append(parts, block)
			}
		case "tool":
			name := toolNames[str(msg["tool_call_id"])]
			if name == "" {
				name = str(msg["name"])
			}
			block := M{"type": "tool-result", "toolName": name, "output": M{"type": "text", "value": toWireToolOutputValue(msg["content"])}}
			if id, ok := msg["tool_call_id"]; ok {
				block["toolCallId"] = id
			}
			parts = append(parts, block)
		default:
			role = "user"
			text := ""
			if msg["content"] != nil {
				text = requestString(msg["content"])
			}
			parts = append(parts, M{"type": "text", "text": text})
		}
		messages = append(messages, M{"role": role, "content": parts})
	}

	cacheMarked := false
	for _, b := range system {
		cacheMarked = cacheMarked || truth(obj(b)["cache_control"])
	}
	for _, m := range messages {
		for _, p := range arr(obj(m)["content"]) {
			cacheMarked = cacheMarked || truth(obj(p)["cache_control"])
		}
	}
	if truth(req["prompt_cache_key"]) && !cacheMarked && len(system) > 0 {
		obj(system[len(system)-1])["cache_control"] = M{"type": "ephemeral"}
	}

	params := M{
		"model":    requestOr(req["model"], "deepseek/deepseek-v4-flash"),
		"messages": messages, "max_tokens": math.Min(num(requestOr(req["max_tokens"], 64000)), 200000), "stream": true,
	}
	if len(system) > 0 {
		params["system"] = system
	} else if cfg.EmptySystemPlaceholder {
		params["system"] = []any{M{"type": "text", "text": " "}}
	}
	for _, key := range []string{"temperature", "reasoning_effort", "parallel_tool_calls"} {
		if v, exists := req[key]; exists {
			params[key] = v
		}
	}
	tools := []any{}
	for _, t := range arr(req["tools"]) {
		tool := obj(t)
		fn := obj(tool["function"])
		tools = append(tools, M{
			"name":         toWireToolName(str(requestOr(fn["name"], requestOr(tool["name"], "")))),
			"description":  requestOr(fn["description"], requestOr(tool["description"], "")),
			"input_schema": requestOr(fn["parameters"], requestOr(tool["input_schema"], M{"type": "object", "properties": M{}})),
		})
	}
	params["tools"] = tools
	if tc, exists := req["tool_choice"]; exists {
		if s, ok := tc.(string); ok {
			typeName := "auto"
			switch s {
			case "none":
				typeName = "none"
			case "required":
				typeName = "any"
			}
			params["tool_choice"] = M{"type": typeName}
		} else if str(obj(tc)["type"]) == "function" {
			choice := M{"type": "tool"}
			if name, exists := obj(obj(tc)["function"])["name"]; exists {
				choice["name"] = name
			}
			params["tool_choice"] = choice
		} else {
			params["tool_choice"] = tc
		}
	}
	projectDir := cfg.DeviceProjectDir
	if projectDir == "" {
		projectDir = `C:\Users\dev\projects\app`
	}
	platform := cfg.DevicePlatform
	if platform == "" {
		platform = "win32"
	}
	mode := cfg.CLIMode
	if mode == "" {
		mode = "agent"
	}
	return M{
		"config": M{"workingDir": projectDir, "date": time.Now().UTC().Format("2006-01-02"), "environment": platform,
			"structure": []any{}, "isGitRepo": false, "currentBranch": "", "mainBranch": "", "gitStatus": "", "recentCommits": []any{}},
		"memory": nil, "taste": nil, "skills": nil, "permissionMode": "standard", "mode": mode, "params": params,
	}
}

func convertAnthropicToChat(req M) M {
	messages := []any{}
	if system, ok := req["system"].(string); ok && system != "" {
		messages = append(messages, M{"role": "system", "content": system})
	} else if system, ok := req["system"].([]any); ok {
		blocks := []any{}
		for _, b := range system {
			block := obj(b)
			if str(block["type"]) != "text" {
				continue
			}
			text := block["text"]
			if text == nil {
				text = ""
			}
			part := M{"type": "text", "text": text}
			if truth(block["cache_control"]) {
				part["cache_control"] = block["cache_control"]
			}
			blocks = append(blocks, part)
		}
		// Keep empty text blocks too: a cache marker on an empty system block
		// still has wire meaning and must survive both conversion steps.
		if len(blocks) > 0 {
			messages = append(messages, M{"role": "system", "content": blocks})
		}
	}
	toolNames := map[string]string{}
	for _, raw := range arr(req["messages"]) {
		msg := obj(raw)
		switch str(msg["role"]) {
		case "assistant":
			text, thinking := "", ""
			parts, calls := []any{}, []any{}
			cached := false
			blocks, ok := msg["content"].([]any)
			if !ok {
				blocks = []any{M{"type": "text", "text": requestOr(msg["content"], "")}}
			}
			for _, b := range blocks {
				block := obj(b)
				switch str(block["type"]) {
				case "text":
					value := requestOr(block["text"], "")
					text += requestString(value)
					part := M{"type": "text", "text": value}
					if truth(block["cache_control"]) {
						part["cache_control"] = block["cache_control"]
						cached = true
					}
					parts = append(parts, part)
				case "thinking":
					thinking += requestString(requestOr(block["thinking"], ""))
				case "tool_use":
					toolNames[str(block["id"])] = str(block["name"])
					fn := M{"arguments": requestJSON(requestOr(block["input"], M{}))}
					if name, exists := block["name"]; exists {
						fn["name"] = name
					}
					call := M{"type": "function", "function": fn}
					if id, exists := block["id"]; exists {
						call["id"] = id
					}
					calls = append(calls, call)
				}
			}
			assistant := M{"role": "assistant", "content": nil}
			if len(parts) > 1 || cached {
				assistant["content"] = parts
			} else if text != "" {
				assistant["content"] = text
			}
			if thinking != "" {
				assistant["reasoning_content"] = thinking
			}
			if len(calls) > 0 {
				assistant["tool_calls"] = calls
			}
			messages = append(messages, assistant)
		case "user":
			text, _ := msg["content"].(string)
			parts, results := []any{}, []any{}
			cached := false
			for _, b := range arr(msg["content"]) {
				block := obj(b)
				switch str(block["type"]) {
				case "text":
					value := requestOr(block["text"], "")
					text += requestString(value)
					part := M{"type": "text", "text": value}
					if truth(block["cache_control"]) {
						part["cache_control"] = block["cache_control"]
						cached = true
					}
					parts = append(parts, part)
				case "image":
					source := obj(block["source"])
					url := str(source["url"])
					if str(source["type"]) == "base64" && truth(source["data"]) {
						url = "data:" + str(requestOr(source["media_type"], "image/png")) + ";base64," + str(source["data"])
					}
					if url != "" {
						parts = append(parts, M{"type": "image_url", "image_url": M{"url": url}})
					}
				case "tool_result":
					results = append(results, block)
				}
			}
			// Tool results must directly follow the preceding assistant calls,
			// ahead of text/images included in the same Anthropic user message.
			for _, r := range results {
				result := obj(r)
				content := ""
				switch value := result["content"].(type) {
				case string:
					content = value
				case []any:
					texts := []string{}
					for _, c := range value {
						texts = append(texts, requestString(requestOr(obj(c)["text"], "")))
					}
					content = strings.Join(texts, "\n")
				default:
					content = requestString(requestOr(value, ""))
				}
				tool := M{"role": "tool", "content": content}
				if id, exists := result["tool_use_id"]; exists {
					tool["tool_call_id"] = id
				}
				if name := toolNames[str(result["tool_use_id"])]; name != "" {
					tool["name"] = name
				}
				messages = append(messages, tool)
			}
			if len(parts) > 0 || text != "" {
				var content any = parts
				if len(parts) <= 1 && (len(parts) == 0 || str(obj(parts[0])["type"]) == "text") && !cached {
					content = text
				}
				messages = append(messages, M{"role": "user", "content": content})
			}
		}
	}
	out := M{"model": requestOr(req["model"], "deepseek/deepseek-v4-flash"), "messages": messages, "max_tokens": requestOr(req["max_tokens"], 64000), "stream": req["stream"] == true}
	if len(arr(req["tools"])) > 0 {
		tools := []any{}
		for _, t := range arr(req["tools"]) {
			tool := obj(t)
			fn := M{"description": requestOr(tool["description"], ""), "parameters": requestOr(tool["input_schema"], M{"type": "object", "properties": M{}})}
			if name, exists := tool["name"]; exists {
				fn["name"] = name
			}
			tools = append(tools, M{"type": "function", "function": fn})
		}
		out["tools"] = tools
	}
	if truth(req["tool_choice"]) {
		tc := obj(req["tool_choice"])
		switch str(tc["type"]) {
		case "", "auto":
			out["tool_choice"] = "auto"
		case "any":
			out["tool_choice"] = "required"
		case "tool":
			fn := M{}
			if name, exists := tc["name"]; exists {
				fn["name"] = name
			}
			out["tool_choice"] = M{"type": "function", "function": fn}
		case "none":
			out["tool_choice"] = "none"
		}
	}
	for _, key := range []string{"temperature", "top_p"} {
		if v, exists := req[key]; exists {
			out[key] = v
		}
	}
	if truth(req["stop_sequences"]) {
		out["stop"] = req["stop_sequences"]
	}
	if user := obj(req["metadata"])["user_id"]; truth(user) {
		out["user"] = user
	}
	if thinking := obj(req["thinking"]); thinking != nil {
		switch str(thinking["type"]) {
		case "disabled", "none":
		case "adaptive":
			effort := thinking["effort"]
			if effort == nil {
				effort = "medium"
			}
			out["reasoning_effort"] = effort
		default:
			if budget, exists := thinking["budget_tokens"]; exists {
				effort := "low"
				if num(budget) >= 10000 {
					effort = "high"
				} else if num(budget) >= 5000 {
					effort = "medium"
				}
				out["reasoning_effort"] = effort
			}
		}
	}
	return out
}

func responsesTextOf(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var text strings.Builder
	for _, p := range arr(content) {
		if v := obj(p)["text"]; truth(v) {
			text.WriteString(requestString(v))
		}
	}
	return text.String()
}

func requestResponsesReasoning(item M) string {
	if summary := arr(item["summary"]); len(summary) > 0 {
		return responsesTextOf(summary)
	}
	if content := arr(item["content"]); len(content) > 0 {
		return responsesTextOf(content)
	}
	text, _ := item["text"].(string)
	return text
}

func convertResponsesToChat(req M) M {
	messages := []any{}
	if text := responsesTextOf(req["instructions"]); text != "" {
		messages = append(messages, M{"role": "system", "content": text})
	}
	var pending M
	ensurePending := func() M {
		if pending == nil {
			pending = M{"role": "assistant", "content": nil, "tool_calls": []any{}}
		}
		return pending
	}
	flushPending := func() {
		if pending == nil {
			return
		}
		if len(arr(pending["tool_calls"])) == 0 {
			delete(pending, "tool_calls")
		}
		if !truth(pending["reasoning_content"]) {
			delete(pending, "reasoning_content")
		}
		if pending["content"] != nil || pending["tool_calls"] != nil {
			messages = append(messages, pending)
		}
		pending = nil
	}
	if text, ok := req["input"].(string); ok {
		messages = append(messages, M{"role": "user", "content": text})
	} else {
		for _, raw := range arr(req["input"]) {
			item := obj(raw)
			if item == nil {
				continue
			}
			typ := str(item["type"])
			if item["type"] == nil && truth(item["role"]) {
				typ = "message"
			}
			switch typ {
			case "reasoning":
				if text := requestResponsesReasoning(item); text != "" {
					ensurePending()["reasoning_content"] = text
				}
			case "message":
				text := responsesTextOf(item["content"])
				switch str(item["role"]) {
				case "assistant":
					if text != "" {
						ensurePending()["content"] = text
					}
				case "system", "developer":
					flushPending()
					messages = append(messages, M{"role": "system", "content": text})
				default:
					flushPending()
					messages = append(messages, M{"role": "user", "content": text})
				}
			case "function_call":
				id := requestOr(item["call_id"], item["id"])
				if !truth(id) {
					uuid := newID()
					if len(uuid) > 8 {
						uuid = uuid[:8]
					}
					id = "call_" + uuid
				}
				p := ensurePending()
				p["tool_calls"] = append(arr(p["tool_calls"]), M{"id": id, "type": "function", "function": M{
					"name": requestOr(item["name"], ""), "arguments": requestOr(item["arguments"], "{}"),
				}})
			case "function_call_output":
				flushPending()
				content, ok := item["output"].(string)
				if !ok {
					value, exists := item["output"]
					if !exists {
						value = ""
					}
					content = requestJSON(value)
				}
				messages = append(messages, M{"role": "tool", "tool_call_id": requestOr(item["call_id"], ""), "content": content})
			}
		}
	}
	flushPending()
	out := M{"messages": messages, "stream": req["stream"] == true}
	if model, exists := req["model"]; exists {
		out["model"] = model
	}
	tools := []any{}
	for _, raw := range arr(req["tools"]) {
		tool := obj(raw)
		if str(tool["type"]) != "function" && !truth(tool["name"]) {
			continue
		}
		tools = append(tools, M{"type": "function", "function": M{
			"name": requestOr(tool["name"], ""), "description": requestOr(tool["description"], ""),
			"parameters": requestOr(tool["parameters"], M{"type": "object", "properties": M{}}),
		}})
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if tc, ok := req["tool_choice"].(string); ok && tc != "" {
		out["tool_choice"] = tc
	} else if name := obj(req["tool_choice"])["name"]; truth(name) {
		out["tool_choice"] = M{"type": "function", "function": M{"name": name}}
	}
	if value, exists := req["max_output_tokens"]; exists {
		out["max_tokens"] = value
	}
	for _, key := range []string{"temperature", "top_p", "parallel_tool_calls"} {
		if value, exists := req[key]; exists {
			out[key] = value
		}
	}
	if effort := obj(req["reasoning"])["effort"]; truth(effort) {
		out["reasoning_effort"] = effort
	}
	return out
}
