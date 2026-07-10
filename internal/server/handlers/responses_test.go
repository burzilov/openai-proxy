package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeResponsesUpstream struct {
	status int
	body   string
	stream bool
	err    error
}

func (f *fakeResponsesUpstream) DoResponses(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.stream = stream
	return &http.Response{
		StatusCode: f.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}

func TestResponsesPassthroughJSON(t *testing.T) {
	h := &Responses{Client: &fakeResponsesUpstream{
		status: 200,
		body:   `{"id":"resp_1","status":"completed","output":[]}`,
	}}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5.4","input":[]}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "resp_1") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestResponsesPassthroughUpstreamError(t *testing.T) {
	h := &Responses{Client: &fakeResponsesUpstream{
		status: 429,
		body:   `{"error":{"message":"rate"} }`,
	}}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(`{"model":"x"}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 429 {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestResponsesEmptyBody(t *testing.T) {
	h := &Responses{Client: &fakeResponsesUpstream{status: 200, body: "{}"}}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("status=%d", rr.Code)
	}
}
