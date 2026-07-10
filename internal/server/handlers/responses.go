package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"openai-proxy/internal/server/api"
)

type responsesUpstream interface {
	DoResponses(ctx context.Context, body []byte, stream bool) (*http.Response, error)
}

// Responses is a thin authenticated passthrough to Codex POST /responses.
type Responses struct {
	Client responsesUpstream
}

func (h *Responses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_request_error", "failed to read body", "bad_request")
		return
	}
	if len(body) == 0 {
		api.WriteError(w, http.StatusBadRequest, "invalid_request_error", "empty body", "bad_request")
		return
	}

	var probe struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &probe)

	resp, err := h.Client.DoResponses(r.Context(), body, probe.Stream)
	if err != nil {
		api.MapUpstreamError(w, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ct := resp.Header.Get("Content-Type")
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	if probe.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, "server_error", "streaming not supported", "internal_error")
			return
		}
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					return
				}
				flusher.Flush()
			}
			if readErr != nil {
				return
			}
		}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}
