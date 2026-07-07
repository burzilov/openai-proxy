package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/codex"
	"openai-proxy/internal/config"
	"openai-proxy/internal/server/handlers"
	"openai-proxy/internal/session"
)

type Dependencies struct {
	Config   *config.Config
	Auth     *auth.Manager
	Codex    *codex.Client
	Sessions *session.Store
}

func New(dep Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger)
	r.Use(corsMiddleware)

	health := &handlers.Health{Auth: dep.Auth}
	r.Get("/healthz", health.Liveness)
	r.Get("/readyz", health.Readiness)

	r.Route("/", func(r chi.Router) {
		r.Use(apiKeyAuth(dep.Config.ProxyAPIKey))
		models := &handlers.Models{Client: dep.Codex}
		chat := &handlers.ChatCompletions{
			Client:   dep.Codex,
			Sessions: dep.Sessions,
			AgenticOptions: codex.AgenticOptions{
				MaxContinuations:   dep.Config.MaxContinuationAttempts,
				EnableChatFallback: dep.Config.EnableChatCompletionsFallback,
			},
		}
		r.Get("/v1/models", models.List)
		r.Get("/v1/models/{modelID}", models.Get)
		r.Post("/v1/chat/completions", chat.ServeHTTP)
	})

	return r
}

func apiKeyAuth(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expected == "" {
				next.ServeHTTP(w, r)
				return
			}
			authz := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(authz, prefix) || strings.TrimSpace(strings.TrimPrefix(authz, prefix)) != expected {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid proxy API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Session-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}
