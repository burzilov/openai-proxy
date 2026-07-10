package translate

import (
	"encoding/json"
	"strings"

	"openai-proxy/internal/openai"
)

type toolCallState struct {
	Index     int
	ID        string
	Name      string
	Kind      string // "function" or "custom"
	Arguments strings.Builder
	Started   bool
}

type StreamState struct {
	ID                 string
	Model              string
	Created            int64
	Finished           bool
	FinishReason       string
	TextBuffer         string
	RoleEmitted        bool
	HasToolCalls       bool
	ActiveMessagePhase string
	ToolCalls          map[int]*toolCallState
	LastResponse       *ResponsesResponse
	CollectedOutput    []map[string]any
}

func NewStreamState(model string) *StreamState {
	return &StreamState{
		ID:           "chatcmpl-" + randomID(),
		Model:        model,
		Created:      nowUnix(),
		ToolCalls:    map[int]*toolCallState{},
		FinishReason: "stop",
	}
}

func (s *StreamState) ensureRoleChunk() []openai.ChatCompletionChunk {
	if s.RoleEmitted {
		return nil
	}
	s.RoleEmitted = true
	return []openai.ChatCompletionChunk{s.chunk(openai.ChatCompletionChoice{
		Index: 0,
		Delta: &openai.ChatMessage{Role: "assistant"},
	})}
}

func (s *StreamState) ChunksFromTextDelta(delta string) []openai.ChatCompletionChunk {
	if delta == "" || s.HasToolCalls {
		return nil
	}
	if s.ActiveMessagePhase == "commentary" || s.ActiveMessagePhase == "analysis" {
		return nil
	}
	chunks := s.ensureRoleChunk()
	s.TextBuffer += delta
	chunks = append(chunks, s.chunk(openai.ChatCompletionChoice{
		Index: 0,
		Delta: &openai.ChatMessage{Content: mustRawJSON(delta)},
	}))
	return chunks
}

func (s *StreamState) FinalChunk() openai.ChatCompletionChunk {
	fr := s.FinishReason
	return s.chunk(openai.ChatCompletionChoice{
		Index:        0,
		Delta:        &openai.ChatMessage{},
		FinishReason: &fr,
	})
}

func (s *StreamState) chunk(choice openai.ChatCompletionChoice) openai.ChatCompletionChunk {
	return openai.ChatCompletionChunk{
		ID:      s.ID,
		Object:  "chat.completion.chunk",
		Created: s.Created,
		Model:   s.Model,
		Choices: []openai.ChatCompletionChoice{choice},
	}
}

// ApplyResponseEvent maps a Responses SSE JSON payload to OpenAI chunks.
func ApplyResponseEvent(state *StreamState, eventType string, data json.RawMessage) []openai.ChatCompletionChunk {
	if len(data) == 0 {
		return nil
	}
	eventType = resolveResponseEventType(eventType, data)

	switch {
	case eventType == "response.output_text.delta" || strings.HasSuffix(eventType, "output_text.delta"):
		var payload struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}
		return state.ChunksFromTextDelta(payload.Delta)

	case eventType == "response.output_item.added":
		return state.handleOutputItemAdded(data)

	case eventType == "response.function_call_arguments.delta" || strings.Contains(eventType, "function_call_arguments.delta"):
		return state.handleFunctionCallArgumentsDelta(data)

	case eventType == "response.custom_tool_call_input.delta" || strings.Contains(eventType, "custom_tool_call_input.delta"):
		return state.handleCustomToolCallInputDelta(data)

	case eventType == "response.output_item.done":
		return state.handleOutputItemDone(data)

	case eventType == "response.completed" || eventType == "response.incomplete":
		return state.handleTerminal(data)

	case eventType == "response.failed":
		state.FinishReason = "stop"
		state.Finished = true
		return nil
	}

	return nil
}

func (s *StreamState) handleOutputItemAdded(data json.RawMessage) []openai.ChatCompletionChunk {
	var payload struct {
		OutputIndex int            `json:"output_index"`
		Item        map[string]any `json:"item"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	item := payload.Item
	if item == nil {
		return nil
	}
	itemType, _ := item["type"].(string)
	if itemType == "message" {
		if phase, _ := item["phase"].(string); phase != "" {
			s.ActiveMessagePhase = strings.ToLower(strings.TrimSpace(phase))
		}
		return nil
	}

	var kind string
	switch itemType {
	case "function_call":
		kind = "function"
	case "custom_tool_call":
		kind = "custom"
	default:
		return nil
	}

	s.HasToolCalls = true
	idx := payload.OutputIndex
	callID, _ := item["call_id"].(string)
	name, _ := item["name"].(string)
	if callID == "" {
		callID = deterministicCallID(name, "", idx)
	}

	tc := &toolCallState{Index: idx, ID: callID, Name: name, Kind: kind}
	s.ToolCalls[idx] = tc

	chunks := s.ensureRoleChunk()
	if tc.Started {
		return chunks
	}
	tc.Started = true
	chunks = append(chunks, s.toolCallStartChunk(tc))
	return chunks
}

func (s *StreamState) handleFunctionCallArgumentsDelta(data json.RawMessage) []openai.ChatCompletionChunk {
	var payload struct {
		OutputIndex int    `json:"output_index"`
		Delta       string `json:"delta"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	s.HasToolCalls = true
	tc := s.ToolCalls[payload.OutputIndex]
	if tc == nil {
		tc = &toolCallState{Index: payload.OutputIndex, ID: deterministicCallID("", "", payload.OutputIndex), Kind: "function"}
		s.ToolCalls[payload.OutputIndex] = tc
	}
	if tc.Kind == "" {
		tc.Kind = "function"
	}
	if payload.Delta == "" {
		return nil
	}
	tc.Arguments.WriteString(payload.Delta)

	chunks := s.ensureRoleChunk()
	if !tc.Started {
		tc.Started = true
		chunks = append(chunks, s.toolCallStartChunk(tc))
	}
	chunks = append(chunks, s.toolCallArgsChunk(tc, payload.Delta))
	return chunks
}

func (s *StreamState) handleCustomToolCallInputDelta(data json.RawMessage) []openai.ChatCompletionChunk {
	var payload struct {
		OutputIndex int    `json:"output_index"`
		Delta       string `json:"delta"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	s.HasToolCalls = true
	tc := s.ToolCalls[payload.OutputIndex]
	if tc == nil {
		tc = &toolCallState{Index: payload.OutputIndex, ID: deterministicCallID("", "", payload.OutputIndex), Kind: "custom"}
		s.ToolCalls[payload.OutputIndex] = tc
	}
	tc.Kind = "custom"
	if payload.Delta == "" {
		return nil
	}
	tc.Arguments.WriteString(payload.Delta)

	chunks := s.ensureRoleChunk()
	if !tc.Started {
		tc.Started = true
		chunks = append(chunks, s.toolCallStartChunk(tc))
	}
	chunks = append(chunks, s.toolCallArgsChunk(tc, payload.Delta))
	return chunks
}

func (s *StreamState) handleOutputItemDone(data json.RawMessage) []openai.ChatCompletionChunk {
	var payload struct {
		Item map[string]any `json:"item"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Item == nil {
		return nil
	}
	s.collectOutputItem(payload.Item)

	itemType, _ := payload.Item["type"].(string)
	if itemType != "function_call" && itemType != "custom_tool_call" {
		return nil
	}
	s.HasToolCalls = true
	if s.FinishReason == "stop" {
		s.FinishReason = "tool_calls"
	}
	return nil
}

func (s *StreamState) collectOutputItem(item map[string]any) {
	if item == nil {
		return
	}
	cloned := cloneOutputItem(item)
	if len(cloned) == 0 {
		return
	}
	s.CollectedOutput = append(s.CollectedOutput, cloned)
}

func cloneOutputItem(item map[string]any) map[string]any {
	b, err := json.Marshal(item)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if err := json.Unmarshal(b, &cloned); err != nil {
		return nil
	}
	delete(cloned, "id")
	return cloned
}

func resolveResponseEventType(eventType string, data json.RawMessage) string {
	if strings.TrimSpace(eventType) != "" {
		return eventType
	}
	var probe struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return eventType
	}
	if probe.Type != "" {
		return probe.Type
	}
	return probe.Event
}

func (s *StreamState) handleTerminal(data json.RawMessage) []openai.ChatCompletionChunk {
	var payload struct {
		Response ResponsesResponse `json:"response"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		content, toolCalls, finish := extractOutput(&payload.Response)
		s.LastResponse = &payload.Response
		s.FinishReason = finish

		var chunks []openai.ChatCompletionChunk
		if s.TextBuffer == "" && !s.HasToolCalls && strings.TrimSpace(content) != "" {
			chunks = append(chunks, s.ensureRoleChunk()...)
			chunks = append(chunks, s.chunk(openai.ChatCompletionChoice{
				Index: 0,
				Delta: &openai.ChatMessage{Content: mustRawJSON(content)},
			}))
			s.TextBuffer = content
		}
		if !s.HasToolCalls && len(toolCalls) > 0 {
			chunks = append(chunks, s.emitToolCallsFromComplete(toolCalls)...)
			s.HasToolCalls = true
			s.FinishReason = "tool_calls"
		}
		s.Finished = true
		return chunks
	}
	s.Finished = true
	return nil
}

func (s *StreamState) emitToolCallsFromComplete(toolCalls []openai.ToolCall) []openai.ChatCompletionChunk {
	chunks := s.ensureRoleChunk()
	for i, tc := range toolCalls {
		kind := "function"
		if tc.IsCustom() {
			kind = "custom"
		}
		state := &toolCallState{
			Index:   i,
			ID:      tc.ID,
			Name:    tc.CallName(),
			Kind:    kind,
			Started: true,
		}
		payload := tc.CallPayload()
		state.Arguments.WriteString(payload)
		s.ToolCalls[i] = state
		chunks = append(chunks, s.toolCallStartChunk(state))
		if payload != "" {
			chunks = append(chunks, s.toolCallArgsChunk(state, payload))
		}
	}
	return chunks
}

func (s *StreamState) toolCallStartChunk(tc *toolCallState) openai.ChatCompletionChunk {
	call := openai.ToolCall{
		Index: tc.Index,
		ID:    tc.ID,
		Type:  tc.Kind,
	}
	if tc.Kind == "custom" {
		call.Custom = &openai.ToolCallCustom{
			Name:  tc.Name,
			Input: "",
		}
	} else {
		call.Type = "function"
		call.Function = &openai.ToolCallFunction{
			Name:      tc.Name,
			Arguments: "",
		}
	}
	return s.chunk(openai.ChatCompletionChoice{
		Index: 0,
		Delta: &openai.ChatMessage{ToolCalls: []openai.ToolCall{call}},
	})
}

func (s *StreamState) toolCallArgsChunk(tc *toolCallState, delta string) openai.ChatCompletionChunk {
	call := openai.ToolCall{Index: tc.Index}
	if tc.Kind == "custom" {
		call.Type = "custom"
		call.Custom = &openai.ToolCallCustom{Input: delta}
	} else {
		call.Function = &openai.ToolCallFunction{Arguments: delta}
	}
	return s.chunk(openai.ChatCompletionChoice{
		Index: 0,
		Delta: &openai.ChatMessage{ToolCalls: []openai.ToolCall{call}},
	})
}

func nowUnix() int64 {
	return randomIDUnix()
}

// CollectedToolCall is a finalized tool call from a Responses stream.
type CollectedToolCall struct {
	ID        string
	Name      string
	Arguments string
	Kind      string // "function" or "custom"
}

func (s *StreamState) CollectedToolCalls() []CollectedToolCall {
	if len(s.ToolCalls) == 0 {
		return nil
	}
	max := 0
	for idx := range s.ToolCalls {
		if idx > max {
			max = idx
		}
	}
	out := make([]CollectedToolCall, 0, len(s.ToolCalls))
	for i := 0; i <= max; i++ {
		tc, ok := s.ToolCalls[i]
		if !ok || tc == nil {
			continue
		}
		kind := tc.Kind
		if kind == "" {
			kind = "function"
		}
		out = append(out, CollectedToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments.String(),
			Kind:      kind,
		})
	}
	return out
}
