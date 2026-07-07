package auth

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"openai-proxy/internal/config"
	"openai-proxy/internal/version"
)

type Manager struct {
	store     *Store
	refresher *Refresher
	device    *DeviceLogin
	cfg       OAuthConfig
	skew      time.Duration
	sf        singleflight.Group
}

func NewManager(app *config.Config, store *Store) *Manager {
	oauth := OAuthConfig{
		ClientID:  app.OAuthClientID,
		Issuer:    app.OAuthIssuer,
		TokenURL:  app.OAuthTokenURL,
		BaseURL:   app.CodexBaseURL,
		PollMax:   app.DevicePollMax,
		UserAgent: fmt.Sprintf("openai-proxy/%s", version.Version),
	}
	return &Manager{
		store:     store,
		refresher: NewRefresher(oauth, store),
		device:    NewDeviceLogin(oauth, store),
		cfg:       oauth,
		skew:      app.RefreshSkew,
	}
}

func (m *Manager) Store() *Store {
	return m.store
}

func (m *Manager) Login(ctx context.Context, w io.Writer) error {
	return m.device.Login(ctx, w)
}

func (m *Manager) Logout() error {
	return m.store.Clear()
}

func (m *Manager) Status() Status {
	st := Status{AuthFile: m.store.Path()}
	creds, err := m.store.Credentials()
	if err != nil {
		st.LoggedIn = false
		st.Error = err.Error()
		return st
	}
	st.LoggedIn = true
	st.Label = creds.Label
	st.LastRefresh = creds.LastRefresh
	st.AccountID = ExtractAccountID(creds.AccessToken)
	st.PlanType = ExtractPlanType(creds.AccessToken)
	if exp, ok := AccessTokenExpiresAt(creds.AccessToken); ok {
		st.ExpiresAt = exp.Format(time.RFC3339)
	}
	return st
}

func (m *Manager) HasCredentials() bool {
	_, err := m.store.Credentials()
	return err == nil
}

func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	creds, err := m.store.Credentials()
	if err != nil {
		return "", err
	}
	if !AccessTokenExpiring(creds.AccessToken, m.skew) {
		return creds.AccessToken, nil
	}

	v, err, _ := m.sf.Do("refresh", func() (any, error) {
		return m.refreshLocked(ctx)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (m *Manager) refreshLocked(ctx context.Context) (string, error) {
	var refreshed *Credentials
	err := m.store.WithLock(func(file *StoreFile) error {
		if file.Codex == nil || strings.TrimSpace(file.Codex.AccessToken) == "" {
			return &AuthError{
				Message:         "No Codex credentials stored. Run `openai-proxy auth login`.",
				Code:            "codex_auth_missing",
				ReloginRequired: true,
				StatusCode:      401,
			}
		}
		if !AccessTokenExpiring(file.Codex.AccessToken, m.skew) {
			refreshed = file.Codex
			return nil
		}
		updated, err := m.refresher.RefreshPure(ctx, file.Codex.RefreshToken)
		if err != nil {
			return err
		}
		if updated.Label == "" {
			updated.Label = file.Codex.Label
		}
		if updated.BaseURL == "" {
			updated.BaseURL = file.Codex.BaseURL
		}
		file.Codex = updated
		refreshed = updated
		return nil
	})
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (m *Manager) BaseURL() string {
	creds, err := m.store.Credentials()
	if err == nil && strings.TrimSpace(creds.BaseURL) != "" {
		return strings.TrimRight(creds.BaseURL, "/")
	}
	return strings.TrimRight(m.cfg.BaseURL, "/")
}
