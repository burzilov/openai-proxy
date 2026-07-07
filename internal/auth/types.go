package auth

import "time"

const StoreVersion = 1

type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	BaseURL      string `json:"base_url,omitempty"`
	Label        string `json:"label,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
}

type StoreFile struct {
	Version   int          `json:"version"`
	UpdatedAt string       `json:"updated_at,omitempty"`
	Codex     *Credentials `json:"codex,omitempty"`
}

type Status struct {
	LoggedIn    bool   `json:"logged_in"`
	Label       string `json:"label,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	PlanType    string `json:"plan_type,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	AuthFile    string `json:"auth_file"`
	LastRefresh string `json:"last_refresh,omitempty"`
	Error       string `json:"error,omitempty"`
}

type AuthError struct {
	Message         string
	Code            string
	ReloginRequired bool
	StatusCode      int
}

func (e *AuthError) Error() string {
	return e.Message
}

func IsRateLimited(err error) bool {
	if ae, ok := err.(*AuthError); ok {
		return ae.Code == "rate_limit_exceeded" || ae.Code == "codex_rate_limited"
	}
	return false
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
