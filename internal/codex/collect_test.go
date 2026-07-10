package codex

import (
	"encoding/json"
	"testing"

	"openai-proxy/internal/translate"
)

func TestSynthesizeResponseFromStreamState_ToolCall(t *testing.T) {
	state := translate.NewStreamState("gpt-5.4")
	done := json.RawMessage(`{
		"output_index": 2,
		"item": {
			"type": "custom_tool_call",
			"call_id": "call_patch",
			"name": "ApplyPatch",
			"input": "*** Begin Patch\n*** End Patch\n"
		}
	}`)
	_ = translate.ApplyResponseEvent(state, "response.output_item.done", done)
	resp := synthesizeResponseFromStreamState(state)
	if resp == nil {
		t.Fatal("expected synthesized response")
	}
	_, tools, finish := translate.SummarizeOutput(resp)
	if finish != "tool_calls" || len(tools) != 1 {
		t.Fatalf("finish=%s tools=%d", finish, len(tools))
	}
	if tools[0].CallName() != "ApplyPatch" {
		t.Fatalf("name=%s", tools[0].CallName())
	}
}

func TestStreamAgenticContinuationFixture(t *testing.T) {
	// Simulates incomplete reasoning-only then tool call on continuation.
	state := translate.NewStreamState("gpt-5.4")
	incomplete := json.RawMessage(`{
		"response": {
			"id": "resp_1",
			"status": "incomplete",
			"output": [{"type":"reasoning","summary":[]}]
		}
	}`)
	_ = translate.ApplyResponseEvent(state, "response.incomplete", incomplete)
	if !translate.ShouldContinueTurn(state.LastResponse, state.FinishReason, 0, 3) {
		t.Fatal("expected continuation")
	}
	base := []map[string]any{{"role": "user", "content": "edit file"}}
	nextInput := translate.BuildContinuationInput(base, state.LastResponse)
	state.PrepareContinuation()

	done := json.RawMessage(`{
		"output_index": 1,
		"item": {
			"type": "custom_tool_call",
			"call_id": "call_patch",
			"name": "ApplyPatch",
			"input": "*** Begin Patch\n"
		}
	}`)
	chunks := translate.ApplyResponseEvent(state, "response.output_item.done", done)
	if len(chunks) == 0 {
		t.Fatal("expected tool call chunks after continuation")
	}
	completed := json.RawMessage(`{
		"response": {
			"id": "resp_2",
			"status": "completed",
			"output": [{
				"type": "custom_tool_call",
				"call_id": "call_patch",
				"name": "ApplyPatch",
				"input": "*** Begin Patch\n"
			}]
		}
	}`)
	_ = translate.ApplyResponseEvent(state, "response.completed", completed)
	if translate.ShouldContinueTurn(state.LastResponse, "tool_calls", 1, 3) {
		t.Fatal("should stop after tool_calls")
	}
	if len(nextInput) < 2 {
		t.Fatalf("continuation input too short: %d", len(nextInput))
	}
}
