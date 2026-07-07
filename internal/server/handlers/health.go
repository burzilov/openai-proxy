package handlers

import (
	"encoding/json"
	"net/http"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/server/api"
)

type Health struct {
	Auth *auth.Manager
}

func (h *Health) Liveness(w http.ResponseWriter, _ *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Health) Readiness(w http.ResponseWriter, _ *http.Request) {
	st := h.Auth.Status()
	if !st.LoggedIn {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "not_ready",
			"codex_auth": false,
			"error":      st.Error,
		})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"status":     "ready",
		"codex_auth": true,
	})
}
