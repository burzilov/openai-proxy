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
	if tools := toolsToResponses(req.Tools); len(tools) > 0 {
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
		parallel := true
		out.ParallelToolCalls = &parallel
	}
	return out, nil
}

func messagesToInput(messages []openai.ChatMessage) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(messages))
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
					callID = deterministicCallID(tc.Function.Name, tc.Function.Arguments, len(items))
				}
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      tc.Function.Name,
					"arguments": normalizeArguments(tc.Function.Arguments),
				})
			}
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				return nil, fmt.Errorf("tool message missing tool_call_id")
			}
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  openai.MessageContentString(msg.Content),
			})
		default:
			return nil, fmt.Errorf("unsupported message role: %s", msg.Role)
		}
	}
	return items, nil
}

func toolsToResponses(tools []openai.Tool) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
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
	return out
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
