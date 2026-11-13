package gateway

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config for the FreeLLMAPI-facing normalize gateway.
type Config struct {
	ListenAddr   string // e.g. ":3002"
	UpstreamURL  string // e.g. "http://127.0.0.1:3001"
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// Handler reverse-proxies to UpstreamURL, normalizing JSON bodies on selected
// POST paths so Cursor payloads pass FreeLLMAPI validation.
type Handler struct {
	proxy  *httputil.ReverseProxy
	logger *slog.Logger
}

func NewHandler(upstream *url.URL, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	rp := httputil.NewSingleHostReverseProxy(upstream)
	original := rp.Director
	rp.Director = func(req *http.Request) {
		original(req)
		// Preserve the client Host only if needed for vhosts; FreeLLMAPI is
		// usually reached by IP/port, so use the upstream host.
		req.Host = upstream.Host
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("upstream error", "error", err, "path", r.URL.Path)
		http.Error(w, `{"error":{"message":"gateway upstream unavailable","type":"server_error"}}`, http.StatusBadGateway)
	}
	rp.FlushInterval = 50 * time.Millisecond // SSE / streamed chat completions
	return &Handler{proxy: rp, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if shouldNormalize(r) {
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			h.logger.Warn("read body failed", "error", err)
			http.Error(w, `{"error":{"message":"failed to read request body","type":"invalid_request_error"}}`, http.StatusBadRequest)
			return
		}
		normalized, err := NormalizeJSON(body)
		if err != nil {
			h.logger.Warn("normalize failed", "error", err)
			http.Error(w, `{"error":{"message":"invalid request json","type":"invalid_request_error"}}`, http.StatusBadRequest)
			return
		}
		if !bytes.Equal(body, normalized) {
			h.logger.Info("normalized request", "path", r.URL.Path, "before_bytes", len(body), "after_bytes", len(normalized))
		}
		r.Body = io.NopCloser(bytes.NewReader(normalized))
		r.ContentLength = int64(len(normalized))
		r.Header.Set("Content-Length", strconv.Itoa(len(normalized)))
	}
	h.proxy.ServeHTTP(w, r)
}

func shouldNormalize(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return true
	case strings.HasSuffix(path, "/completions"):
		return true
	case strings.HasSuffix(path, "/responses"):
		return true
	default:
		return false
	}
}
