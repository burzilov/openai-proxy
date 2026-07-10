package translate_test

import (
	"encoding/json"
	"strings"
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

func TestToResponsesRequest_FunctionEchoedCustomTool(t *testing.T) {
	// After we emit custom tools as type=function for LiteLLM, Cursor may echo
	// them back as function tool_calls. Map by tool name to custom_tool_call.
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"patch"`)},
			{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:   "call_patch",
					Type: "function",
					Function: &openai.ToolCallFunction{
						Name:      "ApplyPatch",
						Arguments: "*** Begin Patch\n*** End Patch\n",
					},
				}},
			},
			{Role: "tool", ToolCallID: "call_patch", Content: []byte(`"ok"`)},
		},
		Tools: []openai.Tool{{
			Type: "custom",
			Name: "ApplyPatch",
			Format: json.RawMessage(`{"type":"grammar","syntax":"lark","definition":"start: \"ok\""}`),
		}},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Input[1]["type"] != "custom_tool_call" {
		t.Fatalf("expected custom_tool_call, got %v", out.Input[1]["type"])
	}
	if out.Input[2]["type"] != "custom_tool_call_output" {
		t.Fatalf("expected custom_tool_call_output, got %v", out.Input[2]["type"])
	}
}

func TestToResponsesRequest_ApplyPatchWithoutTools(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: []byte(`"patch"`)},
			{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:   "call_patch",
					Type: "function",
					Function: &openai.ToolCallFunction{
						Name:      "ApplyPatch",
						Arguments: "*** Begin Patch\n*** End Patch\n",
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
	if out.Input[1]["type"] != "custom_tool_call" {
		t.Fatalf("expected custom_tool_call without tools[], got %v", out.Input[1]["type"])
	}
	if out.Input[2]["type"] != "custom_tool_call_output" {
		t.Fatalf("expected custom_tool_call_output, got %v", out.Input[2]["type"])
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
	if start.Type != "function" || start.Function == nil || start.Function.Name != "ApplyPatch" {
		t.Fatalf("start tool call=%+v", start)
	}
	if start.Custom == nil || start.Custom.Name != "ApplyPatch" {
		t.Fatalf("expected dual custom fields, got %+v", start.Custom)
	}

	delta := json.RawMessage(`{"output_index":0,"delta":"*** Begin Patch\\n"}`)
	chunks = translate.ApplyResponseEvent(state, "response.custom_tool_call_input.delta", delta)
	if len(chunks) == 0 {
		t.Fatal("expected input delta chunk")
	}
	d := chunks[0].Choices[0].Delta.ToolCalls[0]
	if d.Function == nil || d.Function.Arguments == "" {
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
	tc := toolCalls[0]
	if tc.Type != "function" || tc.Function == nil || tc.Function.Name != "ApplyPatch" {
		t.Fatalf("expected function wire format, got %+v", tc)
	}
	if tc.Custom == nil || tc.Custom.Name != "ApplyPatch" || !strings.Contains(tc.Custom.Input, "Begin Patch") {
		t.Fatalf("expected dual custom fields, got %+v", tc.Custom)
	}
}

func TestStreamToolCallDenseIndexAfterReasoning(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")

	added := json.RawMessage(`{
		"output_index": 2,
		"item": {"type":"custom_tool_call","call_id":"call_patch","name":"ApplyPatch","input":""}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.output_item.added", added)
	if len(chunks) < 2 {
		t.Fatalf("expected role+tool start, got %d", len(chunks))
	}
	start := chunks[len(chunks)-1].Choices[0].Delta.ToolCalls[0]
	if start.Index != 0 {
		t.Fatalf("expected dense index 0, got %d", start.Index)
	}

	delta := json.RawMessage(`{"output_index":2,"delta":"*** Begin Patch\\n"}`)
	chunks = translate.ApplyResponseEvent(state, "response.custom_tool_call_input.delta", delta)
	if len(chunks) == 0 {
		t.Fatal("expected args delta")
	}
	if chunks[0].Choices[0].Delta.ToolCalls[0].Index != 0 {
		t.Fatalf("args delta index=%d, want 0", chunks[0].Choices[0].Delta.ToolCalls[0].Index)
	}
}

func TestExtractOutput_FunctionCallDenseIndex(t *testing.T) {
	resp := &translate.ResponsesResponse{
		Status: "completed",
		Output: []map[string]any{
			{"type": "reasoning", "summary": []any{}},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "ping",
				"arguments": `{}`,
			},
		},
	}
	_, toolCalls, finish := translate.SummarizeOutput(resp)
	if finish != "tool_calls" || len(toolCalls) != 1 {
		t.Fatalf("finish=%s n=%d", finish, len(toolCalls))
	}
	if toolCalls[0].Index != 0 {
		t.Fatalf("index=%d, want 0", toolCalls[0].Index)
	}
}

func TestStreamCustomToolCallDoneWithoutDeltas(t *testing.T) {
	// Reproduces Cursor/Codex path: custom_tool_call arrives only in
	// output_item.done / response.completed, with finish_reason tool_calls
	// but previously no tool_calls payload in the Chat Completions stream.
	state := translate.NewStreamState("gpt-5.4")

	done := json.RawMessage(`{
		"output_index": 0,
		"item": {
			"type": "custom_tool_call",
			"call_id": "call_patch",
			"name": "ApplyPatch",
			"input": "*** Begin Patch\n*** Add File: /tmp/x.txt\n+hello\n*** End Patch\n"
		}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.output_item.done", done)
	if len(chunks) < 2 {
		t.Fatalf("expected role+tool chunks from done, got %d", len(chunks))
	}
	var sawApplyPatch bool
	for _, ch := range chunks {
		for _, c := range ch.Choices {
			if c.Delta == nil {
				continue
			}
			for _, tc := range c.Delta.ToolCalls {
				if tc.Function != nil && tc.Function.Name == "ApplyPatch" {
					sawApplyPatch = true
				}
				if tc.Function != nil && strings.Contains(tc.Function.Arguments, "Begin Patch") {
					sawApplyPatch = true
				}
				if tc.Custom != nil && strings.Contains(tc.Custom.Input, "Begin Patch") {
					sawApplyPatch = true
				}
			}
		}
	}
	if !sawApplyPatch {
		t.Fatal("expected ApplyPatch tool call chunks in function wire format")
	}

	completed := json.RawMessage(`{
		"response": {
			"status": "completed",
			"output": [{
				"type": "custom_tool_call",
				"call_id": "call_patch",
				"name": "ApplyPatch",
				"input": "*** Begin Patch\n*** Add File: /tmp/x.txt\n+hello\n*** End Patch\n"
			}]
		}
	}`)
	// Second pass must not duplicate payload.
	more := translate.ApplyResponseEvent(state, "response.completed", completed)
	for _, ch := range more {
		for _, c := range ch.Choices {
			if c.Delta == nil {
				continue
			}
			for _, tc := range c.Delta.ToolCalls {
				if tc.Custom != nil && tc.Custom.Input != "" {
					t.Fatalf("unexpected duplicate input on completed: %q", tc.Custom.Input)
				}
			}
		}
	}
	if state.FinishReason != "tool_calls" {
		t.Fatalf("finish=%s", state.FinishReason)
	}
}

func TestStreamCustomToolCallCompletedOnly(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")
	completed := json.RawMessage(`{
		"response": {
			"status": "completed",
			"output": [{
				"type": "custom_tool_call",
				"call_id": "call_patch",
				"name": "ApplyPatch",
				"input": "*** Begin Patch\n*** End Patch\n"
			}]
		}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.completed", completed)
	if len(chunks) < 2 {
		t.Fatalf("expected tool call chunks, got %d", len(chunks))
	}
	var name, input string
	for _, ch := range chunks {
		for _, c := range ch.Choices {
			if c.Delta == nil {
				continue
			}
			for _, tc := range c.Delta.ToolCalls {
				if tc.Function != nil && tc.Function.Name != "" {
					name = tc.Function.Name
				}
				if tc.Function != nil && tc.Function.Arguments != "" {
					input += tc.Function.Arguments
				}
			}
		}
	}
	if name != "ApplyPatch" || !strings.Contains(input, "Begin Patch") {
		t.Fatalf("name=%q input=%q", name, input)
	}
	if state.FinishReason != "tool_calls" {
		t.Fatalf("finish=%s", state.FinishReason)
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
