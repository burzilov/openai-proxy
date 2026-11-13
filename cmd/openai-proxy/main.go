package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/codex"
	"openai-proxy/internal/config"
	"openai-proxy/internal/gateway"
	"openai-proxy/internal/server"
	"openai-proxy/internal/session"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		if err := runServe(); err != nil {
			slog.Error("serve failed", "error", err)
			os.Exit(1)
		}
	case "gateway":
		if err := runGateway(); err != nil {
			slog.Error("gateway failed", "error", err)
			os.Exit(1)
		}
	case "auth":
		if len(os.Args) < 3 {
			usage()
			os.Exit(2)
		}
		if err := runAuth(os.Args[2]); err != nil {
			slog.Error("auth failed", "error", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n  %s serve\n  %s gateway\n  %s auth login|status|logout\n", os.Args[0], os.Args[0], os.Args[0])
}

func runServe() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	store := auth.NewStore(cfg.AuthStorePath)
	authMgr := auth.NewManager(cfg, store)
	httpClient := &http.Client{Timeout: cfg.UpstreamTimeout}

	streamTransport := http.DefaultTransport.(*http.Transport).Clone()
	streamTransport.ResponseHeaderTimeout = cfg.UpstreamTimeout
	streamClient := &http.Client{
		Timeout:   0, // SSE body may exceed UpstreamTimeout
		Transport: streamTransport,
	}

	codexClient := codex.NewClient(authMgr.BaseURL(), authMgr, httpClient, streamClient)
	sessions := session.NewStore(cfg.SessionTTL)

	handler := server.New(server.Dependencies{
		Config:   cfg,
		Auth:     authMgr,
		Codex:    codexClient,
		Sessions: sessions,
	})

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.StreamIdleTimeout,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func runGateway() error {
	cfg, err := config.LoadGateway()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	upstream, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("GATEWAY_UPSTREAM: %w", err)
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		return fmt.Errorf("GATEWAY_UPSTREAM must be absolute, got %q", cfg.UpstreamURL)
	}

	handler := gateway.NewHandler(upstream, slog.Default())
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		slog.Info("gateway listening", "addr", cfg.ListenAddr, "upstream", cfg.UpstreamURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("gateway server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func runAuth(sub string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store := auth.NewStore(cfg.AuthStorePath)
	authMgr := auth.NewManager(cfg, store)

	switch sub {
	case "login":
		return authMgr.Login(context.Background(), os.Stdout)
	case "status":
		st := authMgr.Status()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	case "logout":
		if err := authMgr.Logout(); err != nil {
			return err
		}
		fmt.Println("Codex credentials cleared.")
		return nil
	default:
		return fmt.Errorf("unknown auth command: %s", sub)
	}
}
