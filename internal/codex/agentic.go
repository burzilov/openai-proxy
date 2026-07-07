package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

type AgenticOptions struct {
	MaxContinuations   int
	EnableChatFallback bool
}

type AgenticResult struct {
	Response      *translate.ResponsesResponse
	Artifacts     translate.AssistantArtifacts
	UsedFallback  bool
	Continuations int
}

func (c *Client) CompleteAgentic(ctx context.Context, upstream translate.ResponsesRequest, opts AgenticOptions) (*AgenticResult, error) {
	if opts.MaxContinuations <= 0 {
		opts.MaxContinuations = 3
	}

	req := upstream
	req.Stream = false

	var lastResp *translate.ResponsesResponse
	var lastErr error
	continuations := 0

	for attempt := 0; attempt <= opts.MaxContinuations; attempt++ {
		resp, err := c.CollectResponseStream(ctx, req)
		lastResp = resp
		lastErr = err
		if err != nil {
			break
		}

		_, _, finish := translate.SummarizeOutput(resp)
		if !translate.ShouldContinueTurn(resp, finish, attempt, opts.MaxContinuations) {
			break
		}
		req.Input = translate.BuildContinuationInput(req.Input, resp)
		continuations++
		slog.Debug("codex continuation", "attempt", attempt+1)
	}

	if opts.EnableChatFallback && translate.ShouldFallbackToChatCompletions(lastResp, lastErr) {
		slog.Info("codex fallback to chat/completions")
		fallback, err := c.createChatCompletionStreaming(ctx, upstream)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		artifacts := translate.ExtractArtifacts(fallback)
		return &AgenticResult{
			Response:      fallback,
			Artifacts:     artifacts,
			UsedFallback:  true,
			Continuations: continuations,
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	if lastResp == nil {
		return nil, fmt.Errorf("empty upstream response")
	}

	artifacts := translate.ExtractArtifacts(lastResp)
	return &AgenticResult{
		Response:      lastResp,
		Artifacts:     artifacts,
		Continuations: continuations,
	}, nil
}

func responsesRequestToChatCompletion(upstream translate.ResponsesRequest) openai.ChatCompletionRequest {
	messages := []openai.ChatMessage{}
	if instructions := upstream.Instructions; instructions != "" {
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: mustRaw(instructions),
		})
	}
	for _, item := range upstream.Input {
		if msg := inputItemToMessage(item); msg != nil {
			messages = append(messages, *msg)
		}
	}
	req := openai.ChatCompletionRequest{
		Model:    upstream.Model,
		Messages: messages,
	}
	if upstream.MaxOutputTokens != nil {
		req.MaxTokens = upstream.MaxOutputTokens
	}
	return req
}

func inputItemToMessage(item map[string]any) *openai.ChatMessage {
	typ, _ := item["type"].(string)
	switch typ {
	case "function_call":
		callID, _ := item["call_id"].(string)
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		return &openai.ChatMessage{
			Role: "assistant",
			ToolCalls: []openai.ToolCall{{
				ID:   callID,
				Type: "function",
				Function: openai.ToolCallFunction{Name: name, Arguments: args},
			}},
		}
	case "function_call_output":
		callID, _ := item["call_id"].(string)
		output, _ := item["output"].(string)
		return &openai.ChatMessage{Role: "tool", ToolCallID: callID, Content: mustRaw(output)}
	}
	if role, _ := item["role"].(string); role != "" {
		content, _ := item["content"].(string)
		return &openai.ChatMessage{Role: role, Content: mustRaw(content)}
	}
	return nil
}

func chatCompletionToResponses(chat *openai.ChatCompletionResponse) (*translate.ResponsesResponse, error) {
	if chat == nil || len(chat.Choices) == 0 {
		return nil, fmt.Errorf("empty chat completion response")
	}
	msg := chat.Choices[0].Message
	var output []map[string]any
	if content := openai.MessageContentString(msg.Content); content != "" {
		output = append(output, map[string]any{
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []map[string]any{{"type": "output_text", "text": content}},
		})
	}
	for _, tc := range msg.ToolCalls {
		output = append(output, map[string]any{
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
		})
	}
	status := "completed"
	return &translate.ResponsesResponse{
		ID:     trimChatPrefix(chat.ID),
		Status: status,
		Output: output,
	}, nil
}

func mustRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func trimChatPrefix(id string) string {
	const prefix = "chatcmpl-"
	if len(id) > len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}
