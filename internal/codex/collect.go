package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

// CollectResponseStream calls Responses API with stream=true and returns a synthetic non-stream payload.
// Codex often returns empty output for stream=false (Hermes #5718).
func (c *Client) CollectResponseStream(ctx context.Context, upstream translate.ResponsesRequest) (*translate.ResponsesResponse, error) {
	events, err := c.CreateResponseStream(ctx, upstream)
	if err != nil {
		return nil, err
	}

	state := translate.NewStreamState(upstream.Model)
	var lastResp *translate.ResponsesResponse

	for ev := range events {
		if ev.Err != nil {
			return nil, ev.Err
		}
		_ = translate.ApplyResponseEvent(state, ev.Event, ev.Data)

		switch ev.Event {
		case "response.completed", "response.incomplete":
			var payload struct {
				Response translate.ResponsesResponse `json:"response"`
			}
			if err := json.Unmarshal(ev.Data, &payload); err == nil {
				lastResp = &payload.Response
			}
		case "response.failed":
			if lastResp != nil {
				return nil, fmt.Errorf("upstream response failed")
			}
			var payload struct {
				Response translate.ResponsesResponse `json:"response"`
			}
			if err := json.Unmarshal(ev.Data, &payload); err == nil {
				lastResp = &payload.Response
			}
			if lastResp != nil {
				content, toolCalls, _ := translate.SummarizeOutput(lastResp)
				if content == "" && len(toolCalls) == 0 {
					return nil, fmt.Errorf("upstream response failed")
				}
			}
		}
	}

	if lastResp != nil {
		content, toolCalls, _ := translate.SummarizeOutput(lastResp)
		if strings.TrimSpace(content) != "" || len(toolCalls) > 0 || strings.TrimSpace(lastResp.OutputText) != "" || len(lastResp.Output) > 0 {
			return lastResp, nil
		}
		if len(state.CollectedOutput) > 0 {
			return backfillResponseFromItems(lastResp, state.CollectedOutput), nil
		}
	}

	if synthesized := synthesizeResponseFromStreamState(state); synthesized != nil {
		return synthesized, nil
	}
	return nil, fmt.Errorf("empty upstream stream response")
}

func backfillResponseFromItems(base *translate.ResponsesResponse, items []map[string]any) *translate.ResponsesResponse {
	resp := &translate.ResponsesResponse{Status: "completed", Output: items}
	if base != nil {
		resp.ID = base.ID
		resp.Status = base.Status
		if resp.Status == "" {
			resp.Status = "completed"
		}
	}
	return resp
}

func synthesizeResponseFromStreamState(state *translate.StreamState) *translate.ResponsesResponse {
	if state == nil {
		return nil
	}
	if state.LastResponse != nil {
		content, toolCalls, _ := translate.SummarizeOutput(state.LastResponse)
		if strings.TrimSpace(content) != "" || len(toolCalls) > 0 {
			return state.LastResponse
		}
	}

	resp := &translate.ResponsesResponse{Status: "completed"}
	if state.TextBuffer != "" {
		resp.Output = append(resp.Output, map[string]any{
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []map[string]any{{"type": "output_text", "text": state.TextBuffer}},
		})
	}
	if state.HasToolCalls {
		for _, tc := range state.CollectedToolCalls() {
			if tc.Kind == "custom" {
				resp.Output = append(resp.Output, map[string]any{
					"type":    "custom_tool_call",
					"call_id": tc.ID,
					"name":    tc.Name,
					"input":   tc.Arguments,
				})
				continue
			}
			resp.Output = append(resp.Output, map[string]any{
				"type":      "function_call",
				"call_id":   tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
			})
		}
	}
	if len(resp.Output) == 0 && len(state.CollectedOutput) > 0 {
		resp.Output = append(resp.Output, state.CollectedOutput...)
	}
	if len(resp.Output) == 0 {
		return nil
	}
	return resp
}

func (c *Client) createChatCompletionStreaming(ctx context.Context, upstream translate.ResponsesRequest) (*translate.ResponsesResponse, error) {
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	chatReq := responsesRequestToChatCompletion(upstream)
	chatReq.Stream = true
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, JoinURL(c.baseURL, "/chat/completions"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header = UpstreamHeaders(token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp)
	}

	chatResp, err := drainOpenAIChatStream(resp.Body)
	if err != nil {
		return nil, err
	}
	return chatCompletionToResponses(chatResp)
}

func drainOpenAIChatStream(body io.Reader) (*openai.ChatCompletionResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var (
		id      string
		model   string
		created int64
		text    strings.Builder
		finish  = "stop"
		tools   = map[int]*openai.ToolCall{}
	)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			id = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Created != 0 {
			created = chunk.Created
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finish = *choice.FinishReason
		}
		if choice.Delta == nil {
			continue
		}
		if content := openai.MessageContentString(choice.Delta.Content); content != "" {
			text.WriteString(content)
		}
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			cur, ok := tools[idx]
			if !ok {
				kind := tc.Type
				if kind == "" {
					if tc.IsCustom() {
						kind = "custom"
					} else {
						kind = "function"
					}
				}
				cur = &openai.ToolCall{Index: idx, Type: kind}
				tools[idx] = cur
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Type != "" {
				cur.Type = tc.Type
			}
			if tc.Custom != nil {
				if cur.Custom == nil {
					cur.Custom = &openai.ToolCallCustom{}
				}
				if tc.Custom.Name != "" {
					cur.Custom.Name = tc.Custom.Name
				}
				if tc.Custom.Input != "" {
					cur.Custom.Input += tc.Custom.Input
				}
				cur.Type = "custom"
			}
			if tc.Function != nil {
				if cur.Function == nil {
					cur.Function = &openai.ToolCallFunction{}
				}
				if tc.Function.Name != "" {
					cur.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					cur.Function.Arguments += tc.Function.Arguments
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	msg := openai.ChatMessage{Role: "assistant"}
	if s := text.String(); s != "" {
		msg.Content = mustRaw(s)
	}
	if len(tools) > 0 {
		ordered := make([]openai.ToolCall, 0, len(tools))
		for i := 0; i < len(tools)+8; i++ {
			if tc, ok := tools[i]; ok {
				ordered = append(ordered, *tc)
			}
		}
		msg.ToolCalls = ordered
		if finish == "stop" {
			finish = "tool_calls"
		}
	}

	if id == "" {
		id = "chatcmpl-stream"
	}
	return &openai.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []openai.ChatCompletionChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: &finish,
		}},
	}, nil
}
