package config

import (
	"fmt"
	"os"
	"time"
)

// Fixed Codex upstream settings — change only when the upstream protocol changes.
const (
	CodexBaseURL     = "https://chatgpt.com/backend-api/codex"
	OAuthClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	OAuthIssuer      = "https://auth.openai.com"
	OAuthTokenURL    = "https://auth.openai.com/oauth/token"
	defaultListen    = ":8080"
	authStorePath    = "/data/auth.json"
	refreshSkew      = 120 * time.Second
	upstreamTimeout  = 120 * time.Second
	streamIdle       = 300 * time.Second
	devicePollMax    = 15 * time.Minute
	maxContinuations = 3
	sessionTTL       = 2 * time.Hour
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

// Gateway defaults — separate from Codex serve (which stays on :8080).
const (
	defaultGatewayListen   = ":3002"
	defaultGatewayUpstream = "http://127.0.0.1:3001"
)

// GatewayConfig is the FreeLLMAPI-facing normalize reverse proxy.
type GatewayConfig struct {
	ListenAddr   string
	UpstreamURL  string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

func LoadGateway() (*GatewayConfig, error) {
	listen := os.Getenv("GATEWAY_LISTEN")
	if listen == "" {
		listen = defaultGatewayListen
	}
	upstream := os.Getenv("GATEWAY_UPSTREAM")
	if upstream == "" {
		upstream = defaultGatewayUpstream
	}
	return &GatewayConfig{
		ListenAddr:   listen,
		UpstreamURL:  upstream,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: streamIdle, // long enough for streamed chat completions
		IdleTimeout:  120 * time.Second,
	}, nil
}

func (c *GatewayConfig) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("GATEWAY_LISTEN is empty")
	}
	if c.UpstreamURL == "" {
		return fmt.Errorf("GATEWAY_UPSTREAM is empty")
	}
	return nil
}
