// Package gateway is a thin reverse-proxy front for FreeLLMAPI (or any
// OpenAI-compatible upstream with a strict request schema).
//
// Cursor's Override OpenAI Base URL may send:
//   - tools with type "custom" (e.g. ApplyPatch) instead of type "function"
//   - reasoning as a bare effort string ("medium") instead of { "effort": "..." }
//
// FreeLLMAPI rejects both with HTTP 400 before any upstream (including
// openai-proxy as a custom endpoint) is reached. This package rewrites those
// fields so the request passes validation, then the handler forwards the body
// unchanged otherwise.
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeJSON rewrites a Chat Completions / Responses request body for
// FreeLLMAPI-compatible validation. Non-object JSON is returned unchanged.
func NormalizeJSON(raw []byte) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return raw, nil
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse request json: %w", err)
	}

	changed := false

	if reasoning, ok := body["reasoning"]; ok {
		if next, did := normalizeReasoning(reasoning); did {
			body["reasoning"] = next
			changed = true
		}
	}

	if tools, ok := body["tools"]; ok {
		if next, did, err := normalizeTools(tools); err != nil {
			return nil, err
		} else if did {
			body["tools"] = next
			changed = true
		}
	}

	if !changed {
		return raw, nil
	}

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized json: %w", err)
	}
	return out, nil
}

// normalizeReasoning turns a bare effort string into {"effort":"..."}.
// Object / null / other shapes are left alone.
func normalizeReasoning(raw json.RawMessage) (json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return raw, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw, false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return raw, false
	}
	out, err := json.Marshal(map[string]string{"effort": s})
	if err != nil {
		return raw, false
	}
	return out, true
}

type inboundTool struct {
	Type        string          `json:"type"`
	Function    json.RawMessage `json:"function"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Format      json.RawMessage `json:"format"`
	Custom      *struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Format      json.RawMessage `json:"format"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"custom"`
}

type outboundFunctionTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

func normalizeTools(raw json.RawMessage) (json.RawMessage, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return raw, false, nil
	}

	var tools []inboundTool
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, false, fmt.Errorf("parse tools: %w", err)
	}

	out := make([]json.RawMessage, 0, len(tools))
	changed := false

	for _, t := range tools {
		if hasFunctionObject(t.Function) && (t.Type == "" || t.Type == "function") {
			// Already FreeLLMAPI-shaped; force type if omitted.
			item := outboundFunctionTool{Type: "function"}
			if err := json.Unmarshal(t.Function, &item.Function); err != nil {
				return nil, false, fmt.Errorf("parse function tool: %w", err)
			}
			if item.Function.Name == "" {
				continue
			}
			if len(item.Function.Parameters) == 0 {
				item.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			b, err := json.Marshal(item)
			if err != nil {
				return nil, false, err
			}
			if t.Type != "function" {
				changed = true
			}
			out = append(out, b)
			continue
		}

		// Cursor / Responses-style custom tool → function wrapper.
		name := strings.TrimSpace(t.Name)
		desc := t.Description
		params := t.Parameters
		if t.Custom != nil {
			if n := strings.TrimSpace(t.Custom.Name); n != "" {
				name = n
			}
			if t.Custom.Description != "" {
				desc = t.Custom.Description
			}
			if len(t.Custom.Parameters) > 0 {
				params = t.Custom.Parameters
			}
		}
		if name == "" {
			// Unknown non-function tool without a name — drop so validation passes.
			changed = true
			continue
		}

		item := outboundFunctionTool{Type: "function"}
		item.Function.Name = name
		item.Function.Description = desc
		if len(params) > 0 {
			item.Function.Parameters = params
		} else {
			item.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		b, err := json.Marshal(item)
		if err != nil {
			return nil, false, err
		}
		changed = true
		out = append(out, b)
		// Note: custom grammar/format is dropped — FreeLLMAPI only accepts
		// function tools. openai-proxy still maps ApplyPatch by function name.
	}

	if !changed {
		return raw, false, nil
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func hasFunctionObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{' && !bytes.Equal(raw, []byte("null"))
}
