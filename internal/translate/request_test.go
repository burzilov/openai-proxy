package translate_test

import (
	"encoding/json"
	"testing"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

func TestToResponsesRequest_SystemAndUser(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "system", Content: []byte(`"You are terse."`)},
			{Role: "user", Content: []byte(`"Hello"`)},
		},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Instructions != "You are terse." {
		t.Fatalf("instructions=%q", out.Instructions)
	}
	if out.Store {
		t.Fatal("store must be false")
	}
	if len(out.Input) != 1 {
		t.Fatalf("input len=%d", len(out.Input))
	}
}

func TestToResponsesRequest_ToolCallRoundtripShape(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"weather?"`)},
			{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: &openai.ToolCallFunction{
						Name:      "get_weather",
						Arguments: `{"city":"Moscow"}`,
					},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: []byte(`"15C"`)},
		},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(out.Input))
	}
	if out.Input[1]["type"] != "function_call" {
		t.Fatalf("second item type=%v", out.Input[1]["type"])
	}
	if out.Input[2]["type"] != "function_call_output" {
		t.Fatalf("third item type=%v", out.Input[2]["type"])
	}
}

func TestToResponsesRequest_CustomToolPassthrough(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"edit file"`)},
		},
		Tools: []openai.Tool{
			{
				Type: "function",
				Function: openai.ToolFunction{
					Name:       "ReadFile",
					Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
				},
			},
			{
				Type:        "custom",
				Name:        "ApplyPatch",
				Description: "edit files",
				Format:      json.RawMessage(`{"type":"grammar","syntax":"lark","definition":"start: \"ok\""}`),
			},
		},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(out.Tools))
	}
	if out.Tools[1]["type"] != "custom" || out.Tools[1]["name"] != "ApplyPatch" {
		t.Fatalf("custom tool=%v", out.Tools[1])
	}
	format, ok := out.Tools[1]["format"].(map[string]any)
	if !ok || format["syntax"] != "lark" {
		t.Fatalf("format=%v", out.Tools[1]["format"])
	}
	if out.ParallelToolCalls == nil || *out.ParallelToolCalls {
		t.Fatal("expected parallel_tool_calls=false when custom tools present")
	}
}

func TestToResponsesRequest_CustomToolCallRoundtrip(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"patch"`)},
			{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:   "call_patch",
					Type: "custom",
					Custom: &openai.ToolCallCustom{
						Name:  "ApplyPatch",
						Input: "*** Begin Patch\n*** End Patch\n",
					},
				}},
			},
			{Role: "tool", ToolCallID: "call_patch", Content: []byte(`"ok"`)},
		},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(out.Input))
	}
	if out.Input[1]["type"] != "custom_tool_call" {
		t.Fatalf("second item type=%v", out.Input[1]["type"])
	}
	if out.Input[2]["type"] != "custom_tool_call_output" {
		t.Fatalf("third item type=%v", out.Input[2]["type"])
	}
}

func TestToResponsesRequest_ToolChoicePassthrough(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"hi"`)},
		},
		Tools: []openai.Tool{{
			Type: "function",
			Function: openai.ToolFunction{
				Name:       "ping",
				Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
			},
		}},
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"ping"}}`),
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.ToolChoice == nil {
		t.Fatal("expected tool_choice")
	}
}

func TestStreamFunctionCallArgumentsDelta(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")

	added := json.RawMessage(`{
		"output_index": 0,
		"item": {"type":"function_call","call_id":"call_abc","name":"get_weather","arguments":""}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.output_item.added", added)
	if len(chunks) < 2 {
		t.Fatalf("expected role+tool start chunks, got %d", len(chunks))
	}

	delta := json.RawMessage(`{"output_index":0,"delta":"{\"city\":\"Moscow\"}"}`)
	chunks = translate.ApplyResponseEvent(state, "response.function_call_arguments.delta", delta)
	if len(chunks) == 0 {
		t.Fatal("expected arguments delta chunk")
	}
	if chunks[0].Choices[0].Delta.ToolCalls[0].Function == nil || chunks[0].Choices[0].Delta.ToolCalls[0].Function.Arguments == "" {
		t.Fatal("expected arguments in delta")
	}
}

func TestStreamCustomToolCallInputDelta(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")

	added := json.RawMessage(`{
		"output_index": 0,
		"item": {"type":"custom_tool_call","call_id":"call_patch","name":"ApplyPatch","input":""}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.output_item.added", added)
	if len(chunks) < 2 {
		t.Fatalf("expected role+tool start chunks, got %d", len(chunks))
	}
	start := chunks[len(chunks)-1].Choices[0].Delta.ToolCalls[0]
	if start.Type != "custom" || start.Custom == nil || start.Custom.Name != "ApplyPatch" {
		t.Fatalf("start tool call=%+v", start)
	}

	delta := json.RawMessage(`{"output_index":0,"delta":"*** Begin Patch\\n"}`)
	chunks = translate.ApplyResponseEvent(state, "response.custom_tool_call_input.delta", delta)
	if len(chunks) == 0 {
		t.Fatal("expected input delta chunk")
	}
	d := chunks[0].Choices[0].Delta.ToolCalls[0]
	if d.Type != "custom" || d.Custom == nil || d.Custom.Input == "" {
		t.Fatalf("delta tool call=%+v", d)
	}
}

func TestExtractOutput_CustomToolCall(t *testing.T) {
	resp := &translate.ResponsesResponse{
		Status: "completed",
		Output: []map[string]any{
			{
				"type":    "custom_tool_call",
				"call_id": "call_1",
				"name":    "ApplyPatch",
				"input":   "*** Begin Patch\n*** End Patch\n",
			},
		},
	}
	content, toolCalls, finish := translate.SummarizeOutput(resp)
	if content != "" {
		t.Fatalf("content=%q", content)
	}
	if finish != "tool_calls" || len(toolCalls) != 1 {
		t.Fatalf("finish=%s toolCalls=%d", finish, len(toolCalls))
	}
	if !toolCalls[0].IsCustom() || toolCalls[0].Custom.Name != "ApplyPatch" {
		t.Fatalf("tool call=%+v", toolCalls[0])
	}
}

func TestStreamCompletedFallbackText(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")
	completed := json.RawMessage(`{
		"response": {
			"status": "completed",
			"output": [{
				"type": "message",
				"role": "assistant",
				"content": [{"type":"output_text","text":"hello"}]
			}]
		}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.completed", completed)
	if len(chunks) < 2 {
		t.Fatalf("expected fallback text chunks, got %d", len(chunks))
	}
	if state.TextBuffer != "hello" {
		t.Fatalf("text buffer=%q", state.TextBuffer)
	}
}
