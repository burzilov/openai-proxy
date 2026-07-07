package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"openai-proxy/internal/auth"
	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

type Client struct {
	baseURL string
	auth    *auth.Manager
	http    *http.Client
	models  *ModelClient
}

func NewClient(baseURL string, authMgr *auth.Manager, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    authMgr,
		http:    httpClient,
		models:  NewModelClient(baseURL, authMgr, httpClient),
	}
}

func (c *Client) ListModels(ctx context.Context) (any, error) {
	return c.models.ListModels(ctx)
}

func (c *Client) GetModel(ctx context.Context, modelID string) (openai.Model, error) {
	return c.models.GetModel(ctx, modelID)
}

func (c *Client) CreateResponse(ctx context.Context, req translate.ResponsesRequest) (*translate.ResponsesResponse, error) {
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, JoinURL(c.baseURL, "/responses"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header = UpstreamHeaders(token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readAPIError(resp)
	}

	var out translate.ResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

type StreamEvent struct {
	Event string
	Data  json.RawMessage
	Err   error
}

func (c *Client) CreateResponseStream(ctx context.Context, req translate.ResponsesRequest) (<-chan StreamEvent, error) {
	token, err := c.auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, JoinURL(c.baseURL, "/responses"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header = UpstreamHeaders(token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		err := readAPIError(resp)
		resp.Body.Close()
		return nil, err
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		var pendingEvent string
		var pendingData strings.Builder
		flush := func() {
			if pendingEvent == "" && pendingData.Len() == 0 {
				return
			}
			data := strings.TrimSpace(pendingData.String())
			if data == "[DONE]" {
				pendingEvent = ""
				pendingData.Reset()
				return
			}
			var raw json.RawMessage
			if data != "" {
				raw = json.RawMessage(data)
			}
			select {
			case <-ctx.Done():
				return
			case ch <- StreamEvent{Event: pendingEvent, Data: raw}:
			}
			pendingEvent = ""
			pendingData.Reset()
		}

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, "event:") {
				pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if pendingData.Len() > 0 {
					pendingData.WriteByte('\n')
				}
				pendingData.WriteString(data)
			}
		}
		flush()
		if err := scanner.Err(); err != nil && err != io.EOF {
			ch <- StreamEvent{Err: err}
		}
	}()
	return ch, nil
}

func MapAuthError(err error) *auth.AuthError {
	if ae, ok := err.(*auth.AuthError); ok {
		return ae
	}
	return nil
}
