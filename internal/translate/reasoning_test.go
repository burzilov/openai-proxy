package translate_test

import (
	"encoding/json"
	"testing"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

func TestToResponsesRequest_ReasoningReplay(t *testing.T) {
	reasoning, _ := json.Marshal([]map[string]any{{
		"type":              "reasoning",
		"encrypted_content": "enc123",
	}})
	req := openai.ChatCompletionRequest{
		Model: "gpt-5.4",
		Messages: []openai.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{
				Role:                "assistant",
				Content:             json.RawMessage(`"answer"`),
				CodexReasoningItems: reasoning,
			},
		},
	}
	out, err := translate.ToResponsesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Input) < 2 {
		t.Fatalf("input len=%d", len(out.Input))
	}
	if out.Input[1]["type"] != "reasoning" {
		t.Fatalf("reasoning item type=%v", out.Input[1]["type"])
	}
	if out.Include == nil || len(out.Include) == 0 {
		t.Fatal("expected include for reasoning")
	}
}
