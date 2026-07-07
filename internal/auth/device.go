package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OAuthConfig struct {
	ClientID    string
	Issuer      string
	TokenURL    string
	BaseURL     string
	PollMax     time.Duration
	UserAgent   string
	HTTPClient  *http.Client
}

type DeviceLogin struct {
	cfg   OAuthConfig
	store *Store
}

func NewDeviceLogin(cfg OAuthConfig, store *Store) *DeviceLogin {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.PollMax == 0 {
		cfg.PollMax = 15 * time.Minute
	}
	return &DeviceLogin{cfg: cfg, store: store}
}

type deviceUserCodeResponse struct {
	UserCode     string `json:"user_code"`
	DeviceAuthID string `json:"device_auth_id"`
	Interval     any    `json:"interval"`
}

type deviceTokenPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (d *DeviceLogin) Login(ctx context.Context, w io.Writer) error {
	device, err := d.requestUserCode(ctx)
	if err != nil {
		return err
	}

	deviceURL := strings.TrimRight(d.cfg.Issuer, "/") + "/codex/device"
	fmt.Fprintln(w)
	fmt.Fprintln(w, "To continue, follow these steps:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  1. Open this URL in your browser:\n     %s\n\n", deviceURL)
	fmt.Fprintf(w, "  2. Enter this code:\n     %s\n\n", device.UserCode)
	fmt.Fprintln(w, "Waiting for sign-in... (press Ctrl+C to cancel)")

	poll, err := d.pollDeviceToken(ctx, device)
	if err != nil {
		return err
	}

	tokens, err := d.exchangeCode(ctx, poll)
	if err != nil {
		return err
	}

	label := ExtractLabel(tokens.AccessToken)
	creds := &Credentials{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		BaseURL:      d.cfg.BaseURL,
		Label:        label,
		LastRefresh:  nowUTC(),
	}
	if err := d.store.SaveCredentials(creds); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nCodex login successful. Account: %s\n", label)
	fmt.Fprintf(w, "Auth saved to %s\n", d.store.Path())
	return nil
}

func (d *DeviceLogin) requestUserCode(ctx context.Context) (*deviceUserCodeResponse, error) {
	endpoint := strings.TrimRight(d.cfg.Issuer, "/") + "/api/accounts/deviceauth/usercode"
	body, _ := json.Marshal(map[string]string{"client_id": d.cfg.ClientID})

	var lastResp *http.Response
	for attempt := 1; attempt <= 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if d.cfg.UserAgent != "" {
			req.Header.Set("User-Agent", d.cfg.UserAgent)
		}

		resp, err := d.cfg.HTTPClient.Do(req)
		if err != nil {
			return nil, &AuthError{Message: "Failed to request device code: " + err.Error(), Code: "device_code_request_failed"}
		}
		lastResp = resp

		if resp.StatusCode != http.StatusTooManyRequests {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, &AuthError{
					Message:    fmt.Sprintf("Device code request returned status %d", resp.StatusCode),
					Code:       "device_code_request_error",
					StatusCode: resp.StatusCode,
				}
			}
			var device deviceUserCodeResponse
			if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
				return nil, err
			}
			if device.UserCode == "" || device.DeviceAuthID == "" {
				return nil, &AuthError{Message: "Device code response missing required fields", Code: "device_code_incomplete"}
			}
			return &device, nil
		}

		resp.Body.Close()
		delay := parseRetryAfter(resp.Header.Get("Retry-After"), attempt)
		fmt.Printf("OpenAI is rate-limiting login requests (429); retrying in %ds...\n", delay)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(delay) * time.Second):
		}
	}

	if lastResp != nil && lastResp.StatusCode == http.StatusTooManyRequests {
		return nil, &AuthError{
			Message:    "OpenAI is rate-limiting Codex login requests (HTTP 429). Wait a minute and try again.",
			Code:       "codex_rate_limited",
			StatusCode: 429,
		}
	}
	return nil, &AuthError{Message: "Device code request failed", Code: "device_code_request_failed"}
}

func (d *DeviceLogin) pollDeviceToken(ctx context.Context, device *deviceUserCodeResponse) (*deviceTokenPollResponse, error) {
	endpoint := strings.TrimRight(d.cfg.Issuer, "/") + "/api/accounts/deviceauth/token"
	interval := pollInterval(device.Interval)
	deadline := time.Now().Add(d.cfg.PollMax)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		payload, _ := json.Marshal(map[string]string{
			"device_auth_id": device.DeviceAuthID,
			"user_code":      device.UserCode,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := d.cfg.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}

		switch resp.StatusCode {
		case http.StatusOK:
			var out deviceTokenPollResponse
			err := json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			if out.AuthorizationCode == "" || out.CodeVerifier == "" {
				return nil, &AuthError{Message: "Device auth response missing authorization_code or code_verifier", Code: "device_code_incomplete_exchange"}
			}
			return &out, nil
		case http.StatusForbidden, http.StatusNotFound:
			resp.Body.Close()
			continue
		default:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, &AuthError{
				Message:    fmt.Sprintf("Device auth polling returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
				Code:       "device_code_poll_error",
				StatusCode: resp.StatusCode,
			}
		}
	}

	return nil, &AuthError{Message: "Login timed out after 15 minutes", Code: "device_code_timeout"}
}

func (d *DeviceLogin) exchangeCode(ctx context.Context, poll *deviceTokenPollResponse) (*tokenResponse, error) {
	redirectURI := strings.TrimRight(d.cfg.Issuer, "/") + "/deviceauth/callback"
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {poll.AuthorizationCode},
		"redirect_uri":  {redirectURI},
		"client_id":     {d.cfg.ClientID},
		"code_verifier": {poll.CodeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if d.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", d.cfg.UserAgent)
	}

	resp, err := d.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, &AuthError{Message: "Token exchange failed: " + err.Error(), Code: "token_exchange_failed"}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &AuthError{
			Message:    "OpenAI is rate-limiting token exchange (HTTP 429). Wait and try again.",
			Code:       "codex_rate_limited",
			StatusCode: 429,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &AuthError{
			Message:    fmt.Sprintf("Token exchange returned status %d", resp.StatusCode),
			Code:       "token_exchange_error",
			StatusCode: resp.StatusCode,
		}
	}

	var tokens tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, &AuthError{Message: "Token exchange did not return access_token", Code: "token_exchange_no_access_token"}
	}
	return &tokens, nil
}

func pollInterval(raw any) time.Duration {
	switch v := raw.(type) {
	case string:
		if n, err := strconv.Atoi(v); err == nil && n >= 3 {
			return time.Duration(n) * time.Second
		}
	case float64:
		if int(v) >= 3 {
			return time.Duration(int(v)) * time.Second
		}
	case int:
		if v >= 3 {
			return time.Duration(v) * time.Second
		}
	}
	return 5 * time.Second
}

func parseRetryAfter(header string, attempt int) int {
	if header != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs > 0 {
			if secs > 60 {
				return 60
			}
			return secs
		}
	}
	delay := 1 << attempt
	if delay > 60 {
		return 60
	}
	if delay < 1 {
		return 1
	}
	return delay
}
