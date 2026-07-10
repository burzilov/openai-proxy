package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshPureSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "refresh-xyz" {
			t.Errorf("refresh_token=%q", r.Form.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-new",
			"refresh_token": "refresh-new",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(srv.Close)

	store := NewStore(t.TempDir() + "/auth.json")
	r := NewRefresher(OAuthConfig{
		ClientID:   "test-client",
		TokenURL:   srv.URL + "/oauth/token",
		HTTPClient: srv.Client(),
	}, store)

	creds, err := r.RefreshPure(context.Background(), "refresh-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "access-new" || creds.RefreshToken != "refresh-new" {
		t.Fatalf("creds=%+v", creds)
	}
}

func TestRefreshPureRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limit"}`))
	}))
	t.Cleanup(srv.Close)

	r := NewRefresher(OAuthConfig{
		ClientID:   "test-client",
		TokenURL:   srv.URL,
		HTTPClient: srv.Client(),
	}, NewStore(t.TempDir()+"/auth.json"))

	_, err := r.RefreshPure(context.Background(), "refresh-xyz")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*AuthError)
	if !ok || ae.StatusCode != 429 {
		t.Fatalf("err=%v", err)
	}
	if !IsRateLimited(err) {
		t.Fatal("expected rate limited")
	}
}

func TestRefreshPureMissingToken(t *testing.T) {
	r := NewRefresher(OAuthConfig{ClientID: "c", TokenURL: "http://example"}, NewStore(t.TempDir()+"/a.json"))
	_, err := r.RefreshPure(context.Background(), "  ")
	if err == nil {
		t.Fatal("expected error")
	}
	ae := err.(*AuthError)
	if ae.Code != "codex_auth_missing_refresh_token" {
		t.Fatalf("code=%s", ae.Code)
	}
}
