package openai

import "testing"

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
