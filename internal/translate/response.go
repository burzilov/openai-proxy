package translate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"openai-proxy/internal/openai"
)

func FromResponsesResponse(resp *ResponsesResponse, model string) (openai.ChatCompletionResponse, error) {
	artifacts := ExtractArtifacts(resp)
	return FromResponsesWithArtifacts(resp, model, artifacts)
}

func FromResponsesWithArtifacts(resp *ResponsesResponse, model string, artifacts AssistantArtifacts) (openai.ChatCompletionResponse, error) {
	if resp == nil {
		return openai.ChatCompletionResponse{}, fmt.Errorf("empty upstream response")
	}
	if resp.Status == "failed" || resp.Status == "cancelled" {
		msg := formatResponseError(resp)
		return openai.ChatCompletionResponse{}, fmt.Errorf("%s", msg)
	}

	content, toolCalls, finishReason := extractOutput(resp)
	content = FlattenContentBlocks(content)
	id := resp.ID
	if id == "" {
		id = "resp_" + randomID()
	}

	msg := openai.ChatMessage{
		Role:                "assistant",
		Content:             mustRawJSON(content),
		CodexReasoningItems: rawItemsJSON(artifacts.ReasoningItems),
		CodexMessageItems:   rawItemsJSON(artifacts.MessageItems),
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	fr := finishReason
	return openai.ChatCompletionResponse{
		ID:      "chatcmpl-" + id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.ChatCompletionChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: &fr,
		}},
		Usage: usageFromResponse(resp),
	}, nil
}

func extractOutput(resp *ResponsesResponse) (string, []openai.ToolCall, string) {
	var parts []string
	var toolCalls []openai.ToolCall

	output := resp.Output
	if len(output) == 0 && strings.TrimSpace(resp.OutputText) != "" {
		return resp.OutputText, nil, "stop"
	}

	for _, item := range output {
		typ, _ := item["type"].(string)
		switch typ {
		case "message":
			phase, _ := item["phase"].(string)
			phase = strings.ToLower(strings.TrimSpace(phase))
			if phase == "commentary" || phase == "analysis" {
				continue
			}
			if text := messageText(item); text != "" {
				parts = append(parts, text)
			}
		case "function_call":
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			callID, _ := item["call_id"].(string)
			if callID == "" {
				callID = deterministicCallID(name, args, len(toolCalls))
			}
			toolCalls = append(toolCalls, openai.ToolCall{
				Index: len(toolCalls),
				ID:    callID,
				Type:  "function",
				Function: &openai.ToolCallFunction{
					Name:      name,
					Arguments: normalizeArguments(args),
				},
			})
		case "custom_tool_call":
			name, _ := item["name"].(string)
			input, _ := item["input"].(string)
			callID, _ := item["call_id"].(string)
			if callID == "" {
				callID = deterministicCallID(name, input, len(toolCalls))
			}
			// Emit function wire format so LiteLLM/OpenAI streaming clients
			// preserve the call; keep custom for capable clients (Cursor).
			toolCalls = append(toolCalls, wireCustomToolCall(callID, name, input, len(toolCalls)))
		}
	}

	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	} else if len(parts) == 0 && (resp.Status == "incomplete" || hasReasoningOnly(resp)) {
		finish = "length"
	}
	return FlattenContentBlocks(strings.Join(parts, "\n")), toolCalls, finish
}

// wireCustomToolCall builds a Chat Completions tool_call that survives LiteLLM
// (type=function) while still carrying custom.{name,input} for Cursor.
func wireCustomToolCall(callID, name, input string, index int) openai.ToolCall {
	return openai.ToolCall{
		Index: index,
		ID:    callID,
		Type:  "function",
		Function: &openai.ToolCallFunction{
			Name:      name,
			Arguments: input,
		},
		Custom: &openai.ToolCallCustom{
			Name:  name,
			Input: input,
		},
	}
}

// SummarizeOutput exposes extractOutput for agentic continuation logic.
func SummarizeOutput(resp *ResponsesResponse) (string, []openai.ToolCall, string) {
	return extractOutput(resp)
}

func hasReasoningOnly(resp *ResponsesResponse) bool {
	if len(resp.Output) == 0 {
		return false
	}
	hasReasoning := false
	hasContent := false
	for _, item := range resp.Output {
		typ, _ := item["type"].(string)
		switch typ {
		case "reasoning":
			hasReasoning = true
		case "message", "function_call", "custom_tool_call":
			hasContent = true
		}
	}
	return hasReasoning && !hasContent
}

func messageText(item map[string]any) string {
	switch content := item["content"].(type) {
	case string:
		return FlattenContentBlocks(content)
	case []any:
		return textFromContentParts(content)
	default:
		// In-memory items may use []map[string]any before a JSON round-trip.
		if raw, err := json.Marshal(item["content"]); err == nil {
			var parts []any
			if err := json.Unmarshal(raw, &parts); err == nil {
				return textFromContentParts(parts)
			}
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				return FlattenContentBlocks(s)
			}
		}
		return ""
	}
}

func textFromContentParts(parts []any) string {
	var chunks []string
	for _, part := range parts {
		m, ok := part.(map[string]any)
		if !ok {
			continue
		}
		ptype, _ := m["type"].(string)
		if ptype != "output_text" && ptype != "text" {
			continue
		}
		if text, _ := m["text"].(string); text != "" {
			chunks = append(chunks, text)
		}
	}
	return strings.Join(chunks, "")
}

// FlattenContentBlocks unwraps a stringified Responses/Agents content array
// ([{"type":"text","text":"..."}]) into plain text for Chat Completions clients.
// Non-matching strings are returned unchanged.
func FlattenContentBlocks(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || trimmed[0] != '[' {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(trimmed), &blocks); err != nil || len(blocks) == 0 {
		return s
	}
	var out strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "text", "output_text":
			out.WriteString(b.Text)
		default:
			return s
		}
	}
	return out.String()
}

func formatResponseError(resp *ResponsesResponse) string {
	if resp.Error != nil {
		code, _ := resp.Error["code"].(string)
		msg, _ := resp.Error["message"].(string)
		if code != "" && msg != "" {
			return code + ": " + msg
		}
		if msg != "" {
			return msg
		}
	}
	return "upstream response failed"
}

func usageFromResponse(resp *ResponsesResponse) *openai.Usage {
	if resp.Usage == nil {
		return nil
	}
	u := &openai.Usage{}
	if v, ok := asInt(resp.Usage["input_tokens"]); ok {
		u.PromptTokens = v
	}
	if v, ok := asInt(resp.Usage["output_tokens"]); ok {
		u.CompletionTokens = v
	}
	if v, ok := asInt(resp.Usage["total_tokens"]); ok {
		u.TotalTokens = v
	} else {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return nil
	}
	return u
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

func mustRawJSON(s string) []byte {
	b, err := json.Marshal(s)
	if err != nil {
		return []byte(`""`)
	}
	return b
}

func randomID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func randomIDUnix() int64 {
	return time.Now().Unix()
}
