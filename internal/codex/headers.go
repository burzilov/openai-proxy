package codex

import (
	"net/http"
	"strings"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/version"
)

func UpstreamHeaders(accessToken string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+accessToken)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream, application/json")
	h.Set("originator", "codex_cli_rs")
	h.Set("User-Agent", "codex_cli_rs/0.0.0 (openai-proxy/"+version.Version+")")
	if acct := auth.ExtractAccountID(accessToken); acct != "" {
		h.Set("ChatGPT-Account-ID", acct)
	}
	return h
}

func JoinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}
