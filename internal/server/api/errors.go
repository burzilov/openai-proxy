package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/codex"
	"openai-proxy/internal/openai"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, errType, message, code string) {
	WriteJSON(w, status, openai.ErrorResponse{
		Error: openai.ErrorDetail{
			Message: message,
			Type:    errType,
			Code:    code,
		},
	})
}

func WriteErrorWithRetry(w http.ResponseWriter, status int, errType, message, code string, retryAfter int) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	WriteError(w, status, errType, message, code)
}

func MapError(w http.ResponseWriter, err error) {
	if ae, ok := err.(*auth.AuthError); ok {
		status := ae.StatusCode
		if status == 0 {
			status = http.StatusUnauthorized
		}
		errType := "authentication_error"
		if auth.IsRateLimited(err) {
			errType = "rate_limit_error"
			status = http.StatusTooManyRequests
		}
		WriteError(w, status, errType, ae.Message, ae.Code)
		return
	}
	if ue, ok := err.(*codex.APIError); ok {
		mapUpstreamAPIError(w, ue)
		return
	}
	WriteError(w, http.StatusInternalServerError, "server_error", err.Error(), "internal_error")
}

func mapUpstreamAPIError(w http.ResponseWriter, ue *codex.APIError) {
	errType := "server_error"
	status := ue.StatusCode
	if status == 0 {
		status = http.StatusBadGateway
	}
	switch status {
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
	case http.StatusUnauthorized, http.StatusForbidden:
		errType = "authentication_error"
	case http.StatusBadRequest:
		errType = "invalid_request_error"
	}
	if ue.RetryAfter > 0 {
		WriteErrorWithRetry(w, status, errType, ue.Message, ue.Code, ue.RetryAfter)
		return
	}
	WriteError(w, status, errType, ue.Message, ue.Code)
}

func MapUpstreamError(w http.ResponseWriter, err error) {
	if ae := codex.MapAuthError(err); ae != nil {
		MapError(w, ae)
		return
	}
	if ue, ok := err.(*codex.APIError); ok {
		mapUpstreamAPIError(w, ue)
		return
	}
	WriteError(w, http.StatusBadGateway, "server_error", fmt.Sprintf("%v", err), "upstream_error")
}
