package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"openai-proxy/internal/openai"
	"openai-proxy/internal/translate"
)

const HeaderSessionID = "X-Session-Id"

type TurnArtifacts = translate.AssistantArtifacts

type entry struct {
	turns   []TurnArtifacts
	updated time.Time
}

// Store keeps per-session reasoning artifacts for clients that do not echo custom fields (e.g. Cursor).
type Store struct {
	mu      sync.RWMutex
	entries map[string]*entry
	ttl     time.Duration
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	s := &Store{
		entries: map[string]*entry{},
		ttl:     ttl,
	}
	go s.reapLoop()
	return s
}

func (s *Store) ResolveSessionID(headerValue, userField, model string, messages []openai.ChatMessage) string {
	if id := strings.TrimSpace(headerValue); id != "" {
		return id
	}
	if id := strings.TrimSpace(userField); id != "" {
		return "user:" + id
	}
	if id := conversationSessionKey(model, messages); id != "" {
		return id
	}
	return "hash:" + hashMessages(messages)
}

// conversationSessionKey derives a stable session id from the first user message and model.
// Cursor sends a growing messages history but keeps the opening user turn unchanged.
func conversationSessionKey(model string, messages []openai.ChatMessage) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		content := strings.TrimSpace(openai.MessageContentString(msg.Content))
		if content == "" {
			continue
		}
		seed := strings.TrimSpace(model) + "\x00" + content
		sum := sha256.Sum256([]byte(seed))
		return "conv:" + hex.EncodeToString(sum[:16])
	}
	return ""
}

func hashMessages(messages []openai.ChatMessage) string {
	b, _ := json.Marshal(messages)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func (s *Store) EnrichMessages(sessionID string, messages []openai.ChatMessage) []openai.ChatMessage {
	if sessionID == "" {
		return messages
	}
	s.mu.RLock()
	e, ok := s.entries[sessionID]
	s.mu.RUnlock()
	if !ok || len(e.turns) == 0 {
		return messages
	}

	assistantIdx := 0
	out := make([]openai.ChatMessage, len(messages))
	copy(out, messages)
	for i, msg := range out {
		if msg.Role != "assistant" {
			continue
		}
		if assistantIdx >= len(e.turns) {
			break
		}
		stored := e.turns[assistantIdx]
		assistantIdx++
		if len(msg.ReasoningItems()) == 0 && len(stored.ReasoningItems) > 0 {
			out[i].CodexReasoningItems = rawItemsJSON(stored.ReasoningItems)
		}
		if len(msg.MessageItems()) == 0 && len(stored.MessageItems) > 0 {
			out[i].CodexMessageItems = rawItemsJSON(stored.MessageItems)
		}
	}
	return out
}

func (s *Store) RecordTurn(sessionID string, assistantTurnIndex int, artifacts TurnArtifacts) {
	if sessionID == "" {
		return
	}
	if len(artifacts.ReasoningItems) == 0 && len(artifacts.MessageItems) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		e = &entry{turns: []TurnArtifacts{}}
		s.entries[sessionID] = e
	}
	for len(e.turns) <= assistantTurnIndex {
		e.turns = append(e.turns, TurnArtifacts{})
	}
	e.turns[assistantTurnIndex] = artifacts
	e.updated = time.Now()
}

func (s *Store) AssistantTurnIndex(messages []openai.ChatMessage) int {
	n := 0
	for _, msg := range messages {
		if msg.Role == "assistant" {
			n++
		}
	}
	return n
}

func (s *Store) reapLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.reap()
	}
}

func (s *Store) reap() {
	deadline := time.Now().Add(-s.ttl)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		if e.updated.Before(deadline) {
			delete(s.entries, id)
		}
	}
}

func rawItemsJSON(items []map[string]any) json.RawMessage {
	if len(items) == 0 {
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	return b
}
