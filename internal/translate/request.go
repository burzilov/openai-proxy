package translate

import (
	"encoding/json"
	"fmt"
	"strings"

	"openai-proxy/internal/openai"
)

const defaultInstructions = ""

type ResponsesRequest struct {
	Model             string           `json:"model"`
	Instructions      string           `json:"instructions"`
	Input             []map[string]any `json:"input"`
	Store             bool             `json:"store"`
	Stream            bool             `json:"stream,omitempty"`
	MaxOutputTokens   *int             `json:"max_output_tokens,omitempty"`
	Tools             []map[string]any `json:"tools,omitempty"`
	ToolChoice        any              `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
	Reasoning         map[string]any   `json:"reasoning,omitempty"`
	Include           []string         `json:"include,omitempty"`
}

type ResponsesResponse struct {
	ID         string           `json:"id"`
	Object     string           `json:"object"`
	Model      string           `json:"model"`
	Status     string           `json:"status"`
	Output     []map[string]any `json:"output"`
	OutputText string           `json:"output_text,omitempty"`
	Usage      map[string]any   `json:"usage,omitempty"`
	Error      map[string]any   `json:"error,omitempty"`
}

func ToResponsesRequest(req openai.ChatCompletionRequest) (ResponsesRequest, error) {
	if strings.TrimSpace(req.Model) == "" {
		return ResponsesRequest{}, fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return ResponsesRequest{}, fmt.Errorf("messages are required")
	}

	instructions := defaultInstructions
	messages := req.Messages
	if messages[0].Role == "system" {
		if text := strings.TrimSpace(openai.MessageContentString(messages[0].Content)); text != "" {
			instructions = text
		}
		messages = messages[1:]
	}

	input, err := messagesToInput(messages)
	if err != nil {
		return ResponsesRequest{}, err
	}

	out := ResponsesRequest{
		Model:        req.Model,
		Instructions: instructions,
		Input:        input,
		Store:        false,
		Stream:       req.Stream,
		Reasoning:    DefaultReasoningConfig(),
		Include:      DefaultResponsesInclude(),
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		out.MaxOutputTokens = req.MaxTokens
	}
	tools, hasCustom := toolsToResponses(req.Tools)
	if len(tools) > 0 {
		out.Tools = tools
		if len(req.ToolChoice) > 0 {
			var toolChoice any
			if err := json.Unmarshal(req.ToolChoice, &toolChoice); err == nil {
				out.ToolChoice = toolChoice
			} else {
				out.ToolChoice = "auto"
			}
		} else {
			out.ToolChoice = "auto"
		}
		// Custom tools do not support parallel tool calling.
		parallel := !hasCustom
		out.ParallelToolCalls = &parallel
	}
	return out, nil
}

func messagesToInput(messages []openai.ChatMessage) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(messages))
	customCallIDs := map[string]bool{}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			content := openai.MessageContentString(msg.Content)
			items = append(items, map[string]any{
				"role":    "user",
				"content": content,
			})
		case "assistant":
			items = appendReasoningReplay(items, msg)
			if len(msg.MessageItems()) > 0 {
				items = appendMessageReplay(items, msg)
			} else if len(msg.ToolCalls) == 0 {
				content := openai.MessageContentString(msg.Content)
				if content != "" || len(msg.ReasoningItems()) > 0 {
					items = append(items, map[string]any{
						"role":    "assistant",
						"content": content,
					})
				}
			}
			for _, tc := range msg.ToolCalls {
				callID := strings.TrimSpace(tc.ID)
				if callID == "" {
					callID = deterministicCallID(tc.CallName(), tc.CallPayload(), len(items))
				}
				if tc.IsCustom() {
					customCallIDs[callID] = true
					name := tc.CallName()
					input := tc.CallPayload()
					items = append(items, map[string]any{
						"type":    "custom_tool_call",
						"call_id": callID,
						"name":    name,
						"input":   input,
					})
					continue
				}
				name := ""
				args := ""
				if tc.Function != nil {
					name = tc.Function.Name
					args = tc.Function.Arguments
				}
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      name,
					"arguments": normalizeArguments(args),
				})
			}
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				return nil, fmt.Errorf("tool message missing tool_call_id")
			}
			output := openai.MessageContentString(msg.Content)
			if customCallIDs[callID] {
				items = append(items, map[string]any{
					"type":    "custom_tool_call_output",
					"call_id": callID,
					"output":  output,
				})
			} else {
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  output,
				})
			}
		default:
			return nil, fmt.Errorf("unsupported message role: %s", msg.Role)
		}
	}
	return items, nil
}

func toolsToResponses(tools []openai.Tool) (out []map[string]any, hasCustom bool) {
	if len(tools) == 0 {
		return nil, false
	}
	out = make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if isCustomTool(tool) {
			item, ok := customToolToResponses(tool)
			if !ok {
				continue
			}
			out = append(out, item)
			hasCustom = true
			continue
		}
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		params := tool.Function.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		var paramsObj any
		_ = json.Unmarshal(params, &paramsObj)
		if paramsObj == nil {
			paramsObj = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        name,
			"description": tool.Function.Description,
			"strict":      false,
			"parameters":  paramsObj,
		})
	}
	return out, hasCustom
}

func isCustomTool(tool openai.Tool) bool {
	return tool.Type == "custom" || tool.Custom != nil
}

func customToolToResponses(tool openai.Tool) (map[string]any, bool) {
	name := strings.TrimSpace(tool.Name)
	desc := tool.Description
	formatRaw := tool.Format
	if tool.Custom != nil {
		if n := strings.TrimSpace(tool.Custom.Name); n != "" {
			name = n
		}
		if tool.Custom.Description != "" {
			desc = tool.Custom.Description
		}
		if len(tool.Custom.Format) > 0 {
			formatRaw = tool.Custom.Format
		}
	}
	if name == "" {
		return nil, false
	}
	item := map[string]any{
		"type": "custom",
		"name": name,
	}
	if desc != "" {
		item["description"] = desc
	}
	if format := normalizeCustomFormat(formatRaw); format != nil {
		item["format"] = format
	}
	return item, true
}

// normalizeCustomFormat accepts both Responses-flat grammar
// ({type,syntax,definition}) and Chat Completions nested grammar
// ({type,grammar:{syntax,definition}}).
func normalizeCustomFormat(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	typ, _ := obj["type"].(string)
	if typ != "grammar" {
		return obj
	}
	// Already Responses-flat: has syntax/definition at top level.
	if _, ok := obj["syntax"]; ok {
		return obj
	}
	// Nested Chat Completions: format.grammar.{syntax,definition}
	if grammar, ok := obj["grammar"].(map[string]any); ok {
		out := map[string]any{"type": "grammar"}
		if syntax, ok := grammar["syntax"]; ok {
			out["syntax"] = syntax
		}
		if definition, ok := grammar["definition"]; ok {
			out["definition"] = definition
		}
		return out
	}
	return obj
}

func normalizeArguments(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "{}"
	}
	return args
}

func deterministicCallID(name, args string, index int) string {
	seed := fmt.Sprintf("%s:%s:%d", name, args, index)
	sum := sha256Hex(seed)
	return "call_" + sum[:12]
}

func sha256Hex(s string) string {
	h := sha256Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}
