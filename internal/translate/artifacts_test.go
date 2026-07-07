package translate_test

import (
	"fmt"
	"testing"

	"openai-proxy/internal/translate"
)

func TestExtractArtifacts_ReasoningAndMessage(t *testing.T) {
	resp := &translate.ResponsesResponse{
		Status: "completed",
		Output: []map[string]any{
			{
				"type":              "reasoning",
				"encrypted_content": "enc123",
				"summary":           []any{"step1"},
			},
			{
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"phase":  "final",
				"content": []any{
					map[string]any{"type": "output_text", "text": "hello"},
				},
			},
			{
				"type":   "message",
				"role":   "assistant",
				"phase":  "commentary",
				"content": []any{
					map[string]any{"type": "output_text", "text": "hidden"},
				},
			},
		},
	}
	artifacts := translate.ExtractArtifacts(resp)
	if len(artifacts.ReasoningItems) != 1 {
		t.Fatalf("reasoning items=%d", len(artifacts.ReasoningItems))
	}
	if len(artifacts.MessageItems) != 1 {
		t.Fatalf("message items=%d", len(artifacts.MessageItems))
	}
}

func TestShouldContinueTurn_IncompleteWithoutContent(t *testing.T) {
	resp := &translate.ResponsesResponse{
		Status: "incomplete",
		Output: []map[string]any{
			{"type": "reasoning", "encrypted_content": "enc"},
		},
	}
	if !translate.ShouldContinueTurn(resp, "length", 0, 3) {
		t.Fatal("expected continuation")
	}
}

func TestShouldContinueTurn_ToolCallsStop(t *testing.T) {
	resp := &translate.ResponsesResponse{
		Status: "completed",
		Output: []map[string]any{
			{"type": "function_call", "call_id": "c1", "name": "ping", "arguments": "{}"},
		},
	}
	if translate.ShouldContinueTurn(resp, "tool_calls", 0, 3) {
		t.Fatal("should not continue on tool calls")
	}
}

func TestBuildContinuationInput_AppendsAssistantAfterReasoning(t *testing.T) {
	base := []map[string]any{{"role": "user", "content": "hi"}}
	resp := &translate.ResponsesResponse{
		Output: []map[string]any{
			{"type": "reasoning", "encrypted_content": "enc"},
		},
	}
	out := translate.BuildContinuationInput(base, resp)
	if len(out) != 3 {
		t.Fatalf("items=%d", len(out))
	}
	if role, _ := out[2]["role"].(string); role != "assistant" {
		t.Fatalf("last role=%v", out[2])
	}
}

func TestShouldFallbackToChatCompletions_EmptyOutput(t *testing.T) {
	resp := &translate.ResponsesResponse{Status: "completed", Output: []map[string]any{}}
	if !translate.ShouldFallbackToChatCompletions(resp, nil) {
		t.Fatal("expected fallback")
	}
}

func TestShouldFallbackToChatCompletions_NoFallbackOnError(t *testing.T) {
	if translate.ShouldFallbackToChatCompletions(nil, fmt.Errorf("stream failed")) {
		t.Fatal("should not fallback on transport error")
	}
}
