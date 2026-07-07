package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Refresher struct {
	cfg   OAuthConfig
	store *Store
}

func NewRefresher(cfg OAuthConfig, store *Store) *Refresher {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Refresher{cfg: cfg, store: store}
}

func (r *Refresher) Refresh(ctx context.Context, refreshToken string) (*Credentials, error) {
	updated, err := r.RefreshPure(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if err := r.store.SaveCredentials(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *Refresher) RefreshPure(ctx context.Context, refreshToken string) (*Credentials, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, &AuthError{
			Message:         "Codex auth is missing refresh_token. Run `openai-proxy auth login`.",
			Code:            "codex_auth_missing_refresh_token",
			ReloginRequired: true,
			StatusCode:      401,
		}
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {r.cfg.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if r.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", r.cfg.UserAgent)
	}

	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, &AuthError{Message: "Codex token refresh failed: " + err.Error(), Code: "codex_refresh_failed"}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &AuthError{
			Message:         "Codex provider quota exhausted (429). Credentials are still valid; retry later.",
			Code:            "codex_rate_limited",
			ReloginRequired: false,
			StatusCode:      429,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseRefreshError(resp.StatusCode, body)
	}

	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &AuthError{Message: "Codex token refresh returned invalid JSON", Code: "codex_refresh_invalid_json", ReloginRequired: true}
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, &AuthError{Message: "Codex token refresh response missing access_token", Code: "codex_refresh_missing_access_token", ReloginRequired: true}
	}

	updated := &Credentials{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: refreshToken,
		BaseURL:      r.cfg.BaseURL,
		Label:        ExtractLabel(payload.AccessToken),
		LastRefresh:  nowUTC(),
	}
	if rt := strings.TrimSpace(payload.RefreshToken); rt != "" {
		updated.RefreshToken = rt
	}

	return updated, nil
}

func parseRefreshError(status int, body []byte) error {
	code := "codex_refresh_failed"
	message := fmt.Sprintf("Codex token refresh failed with status %d", status)
	relogin := false

	var envelope struct {
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &envelope)

	if envelope.Error != "" {
		code = envelope.Error
	}
	if envelope.ErrorDesc != "" {
		message = "Codex token refresh failed: " + envelope.ErrorDesc
	}

	switch code {
	case "invalid_grant", "invalid_token", "invalid_request", "refresh_token_reused":
		relogin = true
		if code == "refresh_token_reused" {
			message = "Codex refresh token was already consumed. Run `openai-proxy auth login` again."
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		relogin = true
	}

	return &AuthError{
		Message:         message,
		Code:            code,
		ReloginRequired: relogin,
		StatusCode:      status,
	}
}
