package codex

import (
	"testing"

	"openai-proxy/internal/openai"
)

func TestInputItemToMessageCustomDualWire(t *testing.T) {
	msg := inputItemToMessage(map[string]any{
		"type":    "custom_tool_call",
		"call_id": "call_1",
		"name":    "ApplyPatch",
		"input":   "*** Begin Patch\n",
	})
	if msg == nil || len(msg.ToolCalls) != 1 {
		t.Fatalf("msg=%+v", msg)
	}
	tc := msg.ToolCalls[0]
	if tc.Type != "function" || tc.Function == nil || tc.Function.Name != "ApplyPatch" {
		t.Fatalf("function wire=%+v", tc)
	}
	if tc.Custom == nil || tc.Custom.Input == "" {
		t.Fatalf("custom wire=%+v", tc.Custom)
	}
	if tc.CallPayload() != "*** Begin Patch\n" {
		t.Fatalf("payload=%q", tc.CallPayload())
	}
}

func TestChatCompletionToResponsesCustom(t *testing.T) {
	chat := &openai.ChatCompletionResponse{
		ID: "chatcmpl-x",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatMessage{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: &openai.ToolCallFunction{
						Name:      "ApplyPatch",
						Arguments: "patch-body",
					},
					Custom: &openai.ToolCallCustom{
						Name:  "ApplyPatch",
						Input: "patch-body",
					},
				}},
			},
		}},
	}
	resp, err := chatCompletionToResponses(chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 || resp.Output[0]["type"] != "custom_tool_call" {
		t.Fatalf("output=%v", resp.Output)
	}
}
