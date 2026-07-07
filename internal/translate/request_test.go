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
					Function: openai.ToolCallFunction{
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
	if chunks[0].Choices[0].Delta.ToolCalls[0].Function.Arguments == "" {
		t.Fatal("expected arguments in delta")
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
