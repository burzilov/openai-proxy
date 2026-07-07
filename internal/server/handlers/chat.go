package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"openai-proxy/internal/codex"
	"openai-proxy/internal/openai"
	"openai-proxy/internal/server/api"
	"openai-proxy/internal/session"
	"openai-proxy/internal/translate"
)

type ChatCompletions struct {
	Client         *codex.Client
	Sessions       *session.Store
	AgenticOptions codex.AgenticOptions
}

func (h *ChatCompletions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body", "bad_request")
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body", "bad_request")
		return
	}

	sessionID := h.Sessions.ResolveSessionID(r.Header.Get(session.HeaderSessionID), req.User, req.Model, req.Messages)
	req.Messages = h.Sessions.EnrichMessages(sessionID, req.Messages)
	turnIndex := h.Sessions.AssistantTurnIndex(req.Messages)

	upstream, err := translate.ToResponsesRequest(req)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), "bad_request")
		return
	}

	if req.Stream {
		h.stream(w, r, req.Model, upstream, sessionID, turnIndex)
		return
	}
	h.complete(w, r, req.Model, upstream, sessionID, turnIndex)
}

func (h *ChatCompletions) complete(w http.ResponseWriter, r *http.Request, model string, upstream translate.ResponsesRequest, sessionID string, turnIndex int) {
	result, err := h.Client.CompleteAgentic(r.Context(), upstream, h.AgenticOptions)
	if err != nil {
		api.MapUpstreamError(w, err)
		return
	}
	out, err := translate.FromResponsesWithArtifacts(result.Response, model, result.Artifacts)
	if err != nil {
		api.WriteError(w, http.StatusBadGateway, "server_error", err.Error(), "upstream_error")
		return
	}
	if sessionID != "" {
		h.Sessions.RecordTurn(sessionID, turnIndex, result.Artifacts)
	}
	if result.Continuations > 0 || result.UsedFallback {
		slog.Debug("agentic complete",
			"session", sessionID,
			"continuations", result.Continuations,
			"fallback", result.UsedFallback,
		)
	}
	api.WriteJSON(w, http.StatusOK, out)
}

func (h *ChatCompletions) stream(w http.ResponseWriter, r *http.Request, model string, upstream translate.ResponsesRequest, sessionID string, turnIndex int) {
	events, err := h.Client.CreateResponseStream(r.Context(), upstream)
	if err != nil {
		api.MapUpstreamError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, "server_error", "streaming not supported", "internal_error")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	state := translate.NewStreamState(model)
	for ev := range events {
		if ev.Err != nil {
			_ = writeSSE(w, openai.ErrorResponse{
				Error: openai.ErrorDetail{
					Message: ev.Err.Error(),
					Type:    "server_error",
					Code:    "stream_error",
				},
			})
			flusher.Flush()
			return
		}
		chunks := translate.ApplyResponseEvent(state, ev.Event, ev.Data)
		for _, chunk := range chunks {
			if err := writeSSE(w, chunk); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	if state.FinishReason == "stop" && state.HasToolCalls {
		state.FinishReason = "tool_calls"
	}
	final := state.FinalChunk()
	_ = writeSSE(w, final)
	flusher.Flush()
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	if sessionID != "" && state.LastResponse != nil {
		artifacts := translate.ExtractArtifacts(state.LastResponse)
		h.Sessions.RecordTurn(sessionID, turnIndex, artifacts)
	}
}

func writeSSE(w http.ResponseWriter, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
