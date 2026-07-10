package openai

import "encoding/json"

type ChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	User        string          `json:"user,omitempty"`
}

type ChatMessage struct {
	Role                string          `json:"role"`
	Content             json.RawMessage `json:"content,omitempty"`
	ToolCalls           []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID          string          `json:"tool_call_id,omitempty"`
	Name                string          `json:"name,omitempty"`
	CodexReasoningItems json.RawMessage `json:"codex_reasoning_items,omitempty"`
	CodexMessageItems   json.RawMessage `json:"codex_message_items,omitempty"`
}

func (m ChatMessage) ReasoningItems() []map[string]any {
	return parseItemArray(m.CodexReasoningItems)
}

func (m ChatMessage) MessageItems() []map[string]any {
	return parseItemArray(m.CodexMessageItems)
}

func parseItemArray(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return items
}

func MessageContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// Tool is a Chat Completions tool definition.
// Supports classic function tools and custom tools (Cursor ApplyPatch, OpenAI CFG tools).
// Custom tools may arrive in Responses-flat shape ({type,name,format}) or nested
// Chat Completions shape ({type, custom:{name,format}}).
type Tool struct {
	Type        string          `json:"type"`
	Function    ToolFunction    `json:"function"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Format      json.RawMessage `json:"format,omitempty"`
	Custom      *CustomToolDef  `json:"custom,omitempty"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type CustomToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Format      json.RawMessage `json:"format,omitempty"`
}

type ToolCall struct {
	Index    int               `json:"index,omitempty"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function *ToolCallFunction `json:"function,omitempty"`
	Custom   *ToolCallCustom   `json:"custom,omitempty"`
}

type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ToolCallCustom struct {
	Name  string `json:"name,omitempty"`
	Input string `json:"input,omitempty"`
}

func (tc ToolCall) IsCustom() bool {
	return tc.Type == "custom" || tc.Custom != nil
}

func (tc ToolCall) CallName() string {
	if tc.Custom != nil && tc.Custom.Name != "" {
		return tc.Custom.Name
	}
	if tc.Function != nil {
		return tc.Function.Name
	}
	return ""
}

func (tc ToolCall) CallPayload() string {
	if tc.Custom != nil && tc.Custom.Input != "" {
		return tc.Custom.Input
	}
	if tc.Function != nil && tc.Function.Arguments != "" {
		return tc.Function.Arguments
	}
	if tc.Custom != nil {
		return tc.Custom.Input
	}
	return ""
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *Usage                 `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int          `json:"index"`
	Message      ChatMessage  `json:"message,omitempty"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type ChatCompletionChunk struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}
