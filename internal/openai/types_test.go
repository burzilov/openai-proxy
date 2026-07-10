package openai

import (
	"encoding/json"
	"testing"
)

func TestCallPayloadDualWirePrefersNonEmpty(t *testing.T) {
	tc := ToolCall{
		Type: "function",
		Function: &ToolCallFunction{
			Name:      "ApplyPatch",
			Arguments: "*** Begin Patch\n",
		},
		Custom: &ToolCallCustom{
			Name:  "ApplyPatch",
			Input: "",
		},
	}
	if got := tc.CallPayload(); got != "*** Begin Patch\n" {
		t.Fatalf("CallPayload() = %q, want function.arguments", got)
	}
}

func TestCallPayloadCustomInputWins(t *testing.T) {
	tc := ToolCall{
		Type: "function",
		Function: &ToolCallFunction{
			Name:      "ApplyPatch",
			Arguments: "from-function",
		},
		Custom: &ToolCallCustom{
			Name:  "ApplyPatch",
			Input: "from-custom",
		},
	}
	if got := tc.CallPayload(); got != "from-custom" {
		t.Fatalf("CallPayload() = %q, want custom.input", got)
	}
}

func TestMessageContentStringFlattensContentBlocks(t *testing.T) {
	asString, err := json.Marshal(`[{"type":"text","text":"Hello **Partner**"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got := MessageContentString(asString); got != "Hello **Partner**" {
		t.Fatalf("string content got %q", got)
	}
	if got := MessageContentString(json.RawMessage(`[{"type":"text","text":"plain"}]`)); got != "plain" {
		t.Fatalf("array content got %q", got)
	}
}
