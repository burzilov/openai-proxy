package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/openai"
)

var defaultModels = []string{
	"gpt-5.5",
	"gpt-5.4-mini",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
}

type ModelClient struct {
	baseURL string
	auth    *auth.Manager
	http    *http.Client
}

func NewModelClient(baseURL string, authMgr *auth.Manager, httpClient *http.Client) *ModelClient {
	return &ModelClient{baseURL: strings.TrimRight(baseURL, "/"), auth: authMgr, http: httpClient}
}

func (c *ModelClient) ListModels(ctx context.Context) (openai.ModelList, error) {
	models, err := c.fetchModels(ctx)
	if err != nil {
		return fallbackModelList(), nil
	}
	if len(models) == 0 {
		return fallbackModelList(), nil
	}
	return openai.ModelList{Object: "list", Data: models}, nil
}

func (c *ModelClient) GetModel(ctx context.Context, modelID string) (openai.Model, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return openai.Model{}, &APIError{StatusCode: http.StatusBadRequest, Message: "model id is required", Code: "invalid_request"}
	}

	models, err := c.fetchModels(ctx)
	if err != nil {
		for _, id := range defaultModels {
			if id == modelID {
				return openai.Model{ID: id, Object: "model", OwnedBy: "openai-codex"}, nil
			}
		}
		return openai.Model{}, &APIError{StatusCode: http.StatusNotFound, Message: "model not found", Code: "model_not_found"}
	}
	for _, m := range models {
		if m.ID == modelID {
			return m, nil
		}
	}
	return openai.Model{}, &APIError{StatusCode: http.StatusNotFound, Message: "model not found", Code: "model_not_found"}
}

func (c *ModelClient) fetchModels(ctx context.Context) ([]openai.Model, error) {
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := JoinURL(c.baseURL, "/models?client_version=1.0.0")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = UpstreamHeaders(token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp)
	}

	var payload struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	type ranked struct {
		priority int
		slug     string
	}
	rankedModels := make([]ranked, 0, len(payload.Models))
	for _, m := range payload.Models {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" {
			continue
		}
		vis := strings.ToLower(strings.TrimSpace(m.Visibility))
		if vis == "hide" || vis == "hidden" {
			continue
		}
		priority := m.Priority
		if priority == 0 {
			priority = 10_000
		}
		rankedModels = append(rankedModels, ranked{priority: priority, slug: slug})
	}
	sort.Slice(rankedModels, func(i, j int) bool {
		if rankedModels[i].priority == rankedModels[j].priority {
			return rankedModels[i].slug < rankedModels[j].slug
		}
		return rankedModels[i].priority < rankedModels[j].priority
	})

	data := make([]openai.Model, 0, len(rankedModels))
	for _, m := range rankedModels {
		data = append(data, openai.Model{ID: m.slug, Object: "model", OwnedBy: "openai-codex"})
	}
	return data, nil
}

func fallbackModelList() openai.ModelList {
	data := make([]openai.Model, 0, len(defaultModels))
	for _, id := range defaultModels {
		data = append(data, openai.Model{ID: id, Object: "model", OwnedBy: "openai-codex"})
	}
	return openai.ModelList{Object: "list", Data: data}
}

type APIError struct {
	StatusCode int
	Message    string
	Code       string
	Body       string
	RetryAfter int
}

func (e *APIError) Error() string {
	return e.Message
}

func readAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	code := "upstream_error"

	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Error.Message != "" {
			msg = envelope.Error.Message
		}
		if envelope.Error.Code != "" {
			code = envelope.Error.Code
		}
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		code = "authentication_error"
	case http.StatusTooManyRequests:
		code = "rate_limit_exceeded"
	case http.StatusBadRequest:
		code = "invalid_request_error"
	}
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden && (strings.Contains(msg, "<html") || strings.Contains(msg, "cf-chl")) {
		msg = "upstream blocked by Cloudflare (datacenter IP); use residential egress or disable chat/completions fallback"
		code = "cloudflare_blocked"
	}

	retryAfter := 0
	if raw := resp.Header.Get("Retry-After"); raw != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && secs > 0 {
			retryAfter = secs
		}
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    msg,
		Code:       code,
		Body:       string(body),
		RetryAfter: retryAfter,
	}
}
