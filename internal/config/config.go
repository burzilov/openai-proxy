package config

import (
	"fmt"
	"os"
	"time"
)

// Fixed Codex upstream settings — change only when the upstream protocol changes.
const (
	CodexBaseURL    = "https://chatgpt.com/backend-api/codex"
	OAuthClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	OAuthIssuer     = "https://auth.openai.com"
	OAuthTokenURL   = "https://auth.openai.com/oauth/token"
	defaultListen    = ":8080"
	authStorePath    = "/data/auth.json"
	refreshSkew     = 120 * time.Second
	upstreamTimeout = 120 * time.Second
	streamIdle      = 300 * time.Second
	devicePollMax   = 15 * time.Minute
	maxContinuations = 3
	sessionTTL      = 2 * time.Hour
)

type Config struct {
	ListenAddr                    string
	ProxyAPIKey                   string
	AuthStorePath                 string
	CodexBaseURL                  string
	OAuthClientID                 string
	OAuthIssuer                   string
	OAuthTokenURL                 string
	RefreshSkew                   time.Duration
	UpstreamTimeout               time.Duration
	StreamIdleTimeout             time.Duration
	DevicePollMax                 time.Duration
	MaxContinuationAttempts       int
	SessionTTL                    time.Duration
	EnableChatCompletionsFallback bool
}

func Load() (*Config, error) {
	return &Config{
		ListenAddr:                    defaultListen,
		ProxyAPIKey:                   os.Getenv("PROXY_API_KEY"),
		AuthStorePath:                 authStorePath,
		CodexBaseURL:                  CodexBaseURL,
		OAuthClientID:                 OAuthClientID,
		OAuthIssuer:                   OAuthIssuer,
		OAuthTokenURL:                 OAuthTokenURL,
		RefreshSkew:                   refreshSkew,
		UpstreamTimeout:               upstreamTimeout,
		StreamIdleTimeout:             streamIdle,
		DevicePollMax:                 devicePollMax,
		MaxContinuationAttempts:       maxContinuations,
		SessionTTL:                    sessionTTL,
		EnableChatCompletionsFallback: false,
	}, nil
}

func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is empty")
	}
	if c.AuthStorePath == "" {
		return fmt.Errorf("auth store path is empty")
	}
	return nil
}
