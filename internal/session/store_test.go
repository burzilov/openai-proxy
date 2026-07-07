package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/session"
	"openai-proxy/internal/translate"
)

func TestStore_EnrichMessagesFromSession(t *testing.T) {
	store := session.NewStore(time.Hour)
	sessionID := "cursor-1"
	reasoning, _ := json.Marshal([]map[string]any{{
		"type":              "reasoning",
		"encrypted_content": "enc",
	}})
	store.RecordTurn(sessionID, 0, translate.AssistantArtifacts{
		ReasoningItems: []map[string]any{{
			"type":              "reasoning",
			"encrypted_content": "enc",
		}},
	})

	messages := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"hi"`)},
		{Role: "assistant", Content: json.RawMessage(`"answer"`)},
	}
	enriched := store.EnrichMessages(sessionID, messages)
	if len(enriched[1].ReasoningItems()) == 0 {
		t.Fatal("expected reasoning items injected")
	}
	_ = reasoning
}

func TestStore_ResolveSessionID_PrefersHeader(t *testing.T) {
	store := session.NewStore(time.Hour)
	id := store.ResolveSessionID("hdr-1", "user-1", "gpt-5.4", nil)
	if id != "hdr-1" {
		t.Fatalf("id=%q", id)
	}
	id = store.ResolveSessionID("", "user-1", "gpt-5.4", nil)
	if id != "user:user-1" {
		t.Fatalf("id=%q", id)
	}
}

func TestStore_ResolveSessionID_StableAcrossGrowingHistory(t *testing.T) {
	store := session.NewStore(time.Hour)
	turn1 := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"fix the failing tests"`)},
	}
	turn2 := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"fix the failing tests"`)},
		{Role: "assistant", Content: json.RawMessage(`"checking"`)},
		{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"ok"`)},
		{Role: "user", Content: json.RawMessage(`"continue"`)},
	}

	id1 := store.ResolveSessionID("", "", "gpt-5.4", turn1)
	id2 := store.ResolveSessionID("", "", "gpt-5.4", turn2)
	if id1 == "" || id2 == "" {
		t.Fatalf("empty session ids: %q %q", id1, id2)
	}
	if id1 != id2 {
		t.Fatalf("expected stable session id, got %q vs %q", id1, id2)
	}
	if id1[:5] != "conv:" {
		t.Fatalf("expected conv: prefix, got %q", id1)
	}
}

func TestStore_ResolveSessionID_DifferentModels(t *testing.T) {
	store := session.NewStore(time.Hour)
	messages := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"hello"`)},
	}
	idA := store.ResolveSessionID("", "", "gpt-5.4", messages)
	idB := store.ResolveSessionID("", "", "gpt-5.4-mini", messages)
	if idA == idB {
		t.Fatalf("expected different session ids per model, got %q", idA)
	}
}

func TestStore_AutoSessionKey_EnrichesAcrossTurns(t *testing.T) {
	store := session.NewStore(time.Hour)
	model := "gpt-5.4"
	turn1Messages := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"agent task"`)},
	}
	sessionID := store.ResolveSessionID("", "", model, turn1Messages)
	store.RecordTurn(sessionID, 0, translate.AssistantArtifacts{
		ReasoningItems: []map[string]any{{
			"type":              "reasoning",
			"encrypted_content": "enc-turn-0",
		}},
	})

	turn2Messages := []openai.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"agent task"`)},
		{Role: "assistant", Content: json.RawMessage(`"done step 1"`)},
		{Role: "user", Content: json.RawMessage(`"next step"`)},
	}
	if got := store.ResolveSessionID("", "", model, turn2Messages); got != sessionID {
		t.Fatalf("session id changed across turns: %q vs %q", got, sessionID)
	}
	enriched := store.EnrichMessages(sessionID, turn2Messages)
	if len(enriched[1].ReasoningItems()) == 0 {
		t.Fatal("expected reasoning replay on turn 2")
	}
}
