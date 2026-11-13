package gateway

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSON_ReasoningString(t *testing.T) {
	in := []byte(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"reasoning":"medium"}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	var reasoning map[string]string
	if err := json.Unmarshal(body["reasoning"], &reasoning); err != nil {
		t.Fatalf("reasoning: %v (%s)", err, body["reasoning"])
	}
	if reasoning["effort"] != "medium" {
		t.Fatalf("effort=%q", reasoning["effort"])
	}
}

func TestNormalizeJSON_ReasoningObjectUnchanged(t *testing.T) {
	in := []byte(`{"reasoning":{"effort":"high","summary":"auto"}}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Fatalf("expected unchanged, got %s", out)
	}
}

func TestNormalizeJSON_CustomToolToFunction(t *testing.T) {
	in := []byte(`{
		"tools":[
			{"type":"function","function":{"name":"ok","parameters":{"type":"object"}}},
			{"type":"custom","name":"ApplyPatch","format":{"type":"grammar","syntax":"lark","definition":"start: TEXT"}}
		]
	}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != 2 {
		t.Fatalf("tools len=%d body=%s", len(body.Tools), out)
	}
	for i, tool := range body.Tools {
		if tool.Type != "function" {
			t.Fatalf("tools[%d].type=%q", i, tool.Type)
		}
	}
	if body.Tools[1].Function.Name != "ApplyPatch" {
		t.Fatalf("custom name=%q", body.Tools[1].Function.Name)
	}
}

func TestNormalizeJSON_NestedCustomTool(t *testing.T) {
	in := []byte(`{"tools":[{"type":"custom","custom":{"name":"ApplyPatch","description":"patch"}}]}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != 1 || body.Tools[0].Function.Name != "ApplyPatch" {
		t.Fatalf("got %#v / %s", body.Tools, out)
	}
	if body.Tools[0].Function.Description != "patch" {
		t.Fatalf("desc=%q", body.Tools[0].Function.Description)
	}
}

func TestNormalizeJSON_DropsNamelessCustom(t *testing.T) {
	in := []byte(`{"tools":[{"type":"custom"},{"type":"function","function":{"name":"keep","parameters":{}}}]}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != 1 || body.Tools[0].Function.Name != "keep" {
		t.Fatalf("got %#v / %s", body.Tools, out)
	}
}

func TestNormalizeJSON_CursorStyleCombo(t *testing.T) {
	in := []byte(`{
		"model":"auto",
		"messages":[{"role":"user","content":"ping"}],
		"reasoning":"medium",
		"tools":[
			{"type":"function","function":{"name":"ok","parameters":{"type":"object"}}},
			{"type":"custom","name":"whatever"}
		]
	}`)
	out, err := NormalizeJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	var reasoning map[string]string
	_ = json.Unmarshal(body["reasoning"], &reasoning)
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning=%s", body["reasoning"])
	}
	var tools []map[string]any
	_ = json.Unmarshal(body["tools"], &tools)
	if len(tools) != 2 {
		t.Fatalf("tools=%s", body["tools"])
	}
	for _, tool := range tools {
		if tool["type"] != "function" {
			t.Fatalf("tool=%v", tool)
		}
		fn, _ := tool["function"].(map[string]any)
		if fn == nil || fn["name"] == nil {
			t.Fatalf("missing function: %v", tool)
		}
	}
}
