package translate

import (
	"encoding/json"
	"strings"

	"openai-proxy/internal/openai"
)

const issuerKindCodex = "codex_backend"

// AssistantArtifacts holds Responses API items needed for multi-turn agent continuity.
type AssistantArtifacts struct {
	ReasoningItems []map[string]any `json:"codex_reasoning_items,omitempty"`
	MessageItems   []map[string]any `json:"codex_message_items,omitempty"`
}

func DefaultResponsesInclude() []string {
	return []string{"reasoning.encrypted_content"}
}

func DefaultReasoningConfig() map[string]any {
	return map[string]any{"effort": "medium"}
}

// ExtractArtifacts parses reasoning/message items from a Responses payload.
func ExtractArtifacts(resp *ResponsesResponse) AssistantArtifacts {
	if resp == nil {
		return AssistantArtifacts{}
	}
	var out AssistantArtifacts
	for _, item := range resp.Output {
		typ, _ := item["type"].(string)
		switch typ {
		case "reasoning":
			if encrypted, _ := item["encrypted_content"].(string); strings.TrimSpace(encrypted) != "" {
				replay := map[string]any{
					"type":               "reasoning",
					"encrypted_content":  encrypted,
					"_issuer_kind":       issuerKindCodex,
				}
				if summary, ok := item["summary"]; ok {
					replay["summary"] = summary
				}
				out.ReasoningItems = append(out.ReasoningItems, replay)
			}
		case "message":
			phase, _ := item["phase"].(string)
			phase = strings.ToLower(strings.TrimSpace(phase))
			if phase == "commentary" || phase == "analysis" {
				continue
			}
			if text := messageText(item); text != "" {
				msgItem := map[string]any{
					"type":    "message",
					"role":    "assistant",
					"status":  normalizeMessageStatus(item["status"]),
					"content": []map[string]any{{"type": "output_text", "text": text}},
				}
				if id, ok := item["id"].(string); ok && id != "" {
					msgItem["id"] = id
				}
				if phase != "" {
					msgItem["phase"] = phase
				}
				out.MessageItems = append(out.MessageItems, msgItem)
			}
		}
	}
	return out
}

func normalizeMessageStatus(v any) string {
	if s, ok := v.(string); ok {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "completed" || s == "incomplete" || s == "in_progress" {
			return s
		}
	}
	return "completed"
}

func appendReasoningReplay(items []map[string]any, msg openai.ChatMessage) []map[string]any {
	for _, ri := range msg.ReasoningItems() {
		replay := map[string]any{
			"type":              ri["type"],
			"encrypted_content": ri["encrypted_content"],
		}
		if summary, ok := ri["summary"]; ok {
			replay["summary"] = summary
		}
		items = append(items, replay)
	}
	return items
}

func appendMessageReplay(items []map[string]any, msg openai.ChatMessage) []map[string]any {
	for _, mi := range msg.MessageItems() {
		items = append(items, mi)
	}
	return items
}

func parseRawItems(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return items
}

func rawItemsJSON(items []map[string]any) json.RawMessage {
	if len(items) == 0 {
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	return b
}

// ShouldContinueTurn reports whether an incomplete Responses payload should be continued.
func ShouldContinueTurn(resp *ResponsesResponse, finishReason string, attempts int, maxAttempts int) bool {
	if resp == nil || attempts >= maxAttempts {
		return false
	}
	if finishReason == "tool_calls" {
		return false
	}
	content, toolCalls, _ := extractOutput(resp)
	if len(toolCalls) > 0 {
		return false
	}
	if strings.TrimSpace(content) != "" && finishReason == "stop" {
		return false
	}
	if resp.Status == "incomplete" || finishReason == "length" || hasReasoningOnly(resp) {
		return true
	}
	return false
}

// BuildContinuationInput appends prior response output items for another Responses call.
func BuildContinuationInput(base []map[string]any, resp *ResponsesResponse) []map[string]any {
	if resp == nil || len(resp.Output) == 0 {
		return base
	}
	out := make([]map[string]any, 0, len(base)+len(resp.Output))
	out = append(out, base...)
	for _, item := range resp.Output {
		cloned := cloneItem(item)
		if typ, _ := cloned["type"].(string); typ == "reasoning" {
			cloned["_issuer_kind"] = issuerKindCodex
		}
		out = append(out, cloned)
	}
	// Responses API requires a following item after reasoning-only output.
	if needsFollowingAssistantItem(out) {
		out = append(out, map[string]any{"role": "assistant", "content": ""})
	}
	return out
}

func needsFollowingAssistantItem(items []map[string]any) bool {
	if len(items) == 0 {
		return false
	}
	last := items[len(items)-1]
	if typ, _ := last["type"].(string); typ == "reasoning" {
		return true
	}
	return false
}

func cloneItem(item map[string]any) map[string]any {
	b, _ := json.Marshal(item)
	var cloned map[string]any
	_ = json.Unmarshal(b, &cloned)
	delete(cloned, "id")
	delete(cloned, "_issuer_kind")
	return cloned
}

// ShouldFallbackToChatCompletions decides whether to retry via /chat/completions.
// Hermes uses chat/completions as a separate api_mode, not as transport-error recovery.
func ShouldFallbackToChatCompletions(resp *ResponsesResponse, err error) bool {
	if err != nil {
		return false
	}
	if resp == nil {
		return true
	}
	content, toolCalls, _ := extractOutput(resp)
	if len(toolCalls) > 0 {
		return false
	}
	if strings.TrimSpace(content) == "" && strings.TrimSpace(resp.OutputText) == "" {
		return true
	}
	if resp.Status == "failed" {
		return true
	}
	return false
}
