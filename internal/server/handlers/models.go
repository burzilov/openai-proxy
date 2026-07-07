package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"openai-proxy/internal/codex"
	"openai-proxy/internal/server/api"
)

type Models struct {
	Client *codex.Client
}

func (h *Models) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Client.ListModels(r.Context())
	if err != nil {
		api.MapError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, list)
}

func (h *Models) Get(w http.ResponseWriter, r *http.Request) {
	modelID := chi.URLParam(r, "modelID")
	model, err := h.Client.GetModel(r.Context(), modelID)
	if err != nil {
		mapModelError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, model)
}

func mapModelError(w http.ResponseWriter, err error) {
	if ae := codex.MapAuthError(err); ae != nil {
		api.MapError(w, ae)
		return
	}
	if ue, ok := err.(*codex.APIError); ok {
		errType := "invalid_request_error"
		if ue.StatusCode == http.StatusNotFound {
			errType = "invalid_request_error"
		}
		api.WriteError(w, ue.StatusCode, errType, ue.Message, ue.Code)
		return
	}
	api.MapError(w, err)
}
