package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

// stickyHints carries untrusted client conversation metadata. Selection only applies it
// when the resource group explicitly enables session stickiness.
type stickyHints struct {
	// Session replaces a missing session key; Replace also lets it replace a raw Claude metadata key.
	Session string
	Replace bool
	// Derived sessions are routing preferences only: they never pin a request to an unavailable resource.
	Derived bool
	// Parent identifies the conversation whose resource a subagent should prefer.
	Parent string
}

type stickyKey struct{}

func withStickyHints(ctx context.Context, hints stickyHints) context.Context {
	return context.WithValue(ctx, stickyKey{}, hints)
}

func stickyFrom(ctx context.Context) (stickyHints, bool) {
	hints, ok := ctx.Value(stickyKey{}).(stickyHints)
	return hints, ok
}

const (
	stickyValueLimit = 1024
	// Derived bindings expire quickly; they exist to keep prompt caches warm, not to preserve provider state.
	derivedAffinitySeconds = 3600
)

var legacyClaudeSession = regexp.MustCompile(`_session_([0-9a-fA-F-]{8,64})$`)

func stickyValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > stickyValueLimit || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ""
	}
	return value
}

func headerValue(headers http.Header, keys ...string) string {
	for _, key := range keys {
		// Repeated headers are ambiguous and must not influence routing.
		if values := headers.Values(key); len(values) == 1 {
			if value := stickyValue(values[0]); value != "" {
				return value
			}
		}
	}
	return ""
}

// claudeIdentity reads the session and parent session that Claude Code places in metadata.user_id,
// either as JSON or as the legacy "..._session_<uuid>" suffix.
func claudeIdentity(input map[string]json.RawMessage) (session, parent string) {
	var metadata struct {
		UserID        string `json:"user_id"`
		ParentSession string `json:"parent_session_id"`
	}
	if raw := input["metadata"]; len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &metadata) != nil {
		return "", ""
	}
	parent = stickyValue(metadata.ParentSession)
	userID := strings.TrimSpace(metadata.UserID)
	if strings.HasPrefix(userID, "{") {
		var parsed struct {
			Session string `json:"session_id"`
			Parent  string `json:"parent_session_id"`
		}
		if json.Unmarshal([]byte(userID), &parsed) == nil {
			if value := stickyValue(parsed.Parent); value != "" {
				parent = value
			}
			return stickyValue(parsed.Session), parent
		}
		return "", parent
	}
	if match := legacyClaudeSession.FindStringSubmatch(userID); len(match) == 2 {
		return match[1], parent
	}
	return "", parent
}

type codexTurn struct {
	Session    string `json:"session_id"`
	Thread     string `json:"thread_id"`
	Parent     string `json:"parent_thread_id"`
	ForkedFrom string `json:"forked_from_thread_id"`
}

func codexTurns(headers http.Header, input map[string]json.RawMessage) []codexTurn {
	raws := []string{}
	if values := headers.Values("X-Codex-Turn-Metadata"); len(values) == 1 {
		raws = append(raws, values[0])
	}
	var client struct {
		Turn string `json:"x-codex-turn-metadata"`
	}
	if raw := input["client_metadata"]; len(raw) > 0 && len(raw) <= 8192 && json.Unmarshal(raw, &client) == nil {
		raws = append(raws, client.Turn)
	}
	turns := []codexTurn{}
	for _, raw := range raws {
		var turn codexTurn
		if raw = strings.TrimSpace(raw); raw != "" && len(raw) <= 4096 && json.Unmarshal([]byte(raw), &turn) == nil {
			turns = append(turns, turn)
		}
	}
	return turns
}

// messageSession hashes the system prompt and first user message, following CLIProxyAPI's fallback.
// Later turns keep the same opening, so a conversation retains one key without an explicit session.
func messageSession(input map[string]json.RawMessage, kind Kind) string {
	system, user := "", ""
	text := func(raw json.RawMessage, field string) string {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return value
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		var builder strings.Builder
		for _, part := range parts {
			var piece string
			if json.Unmarshal(part[field], &piece) == nil && piece != "" {
				builder.WriteString(piece)
			}
		}
		return builder.String()
	}
	type item struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Parts   json.RawMessage `json:"parts"`
	}
	var items []item
	switch {
	case kind.IsGemini():
		var instruction struct {
			Parts json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(input["systemInstruction"], &instruction) == nil {
			system = text(instruction.Parts, "text")
		}
		_ = json.Unmarshal(input["contents"], &items)
		for _, value := range items {
			if value.Role == "user" {
				user = text(value.Parts, "text")
				break
			}
		}
	case kind == Responses || kind == Compact:
		_ = json.Unmarshal(input["instructions"], &system)
		_ = json.Unmarshal(input["input"], &items)
		for _, value := range items {
			if value.Type != "" && value.Type != "message" {
				continue
			}
			content := text(value.Content, "text")
			if (value.Role == "system" || value.Role == "developer") && system == "" {
				system = content
			} else if value.Role == "user" && content != "" {
				user = content
				break
			}
		}
		var prompt string
		if user == "" && json.Unmarshal(input["input"], &prompt) == nil {
			user = prompt
		}
	default:
		system = text(input["system"], "text")
		_ = json.Unmarshal(input["messages"], &items)
		for _, value := range items {
			content := text(value.Content, "text")
			if value.Role == "system" && system == "" {
				system = content
			} else if value.Role == "user" && content != "" {
				user = content
				break
			}
		}
	}
	if user == "" {
		return ""
	}
	truncate := func(value string) string {
		if len(value) > 100 {
			return value[:100]
		}
		return value
	}
	hash := fnv.New64a()
	if system != "" {
		_, _ = hash.Write([]byte("sys:" + truncate(system) + "\n"))
	}
	_, _ = hash.Write([]byte("usr:" + truncate(user) + "\n"))
	return fmt.Sprintf("msg:%016x", hash.Sum64())
}

// requestStickyHints derives session stickiness metadata without trusting it for authorization.
// explicit is the session already chosen by the gateway; fromMetadata reports that it came from Claude metadata.user_id.
func requestStickyHints(headers http.Header, input map[string]json.RawMessage, kind Kind, explicit string, fromMetadata bool) stickyHints {
	hints := stickyHints{}
	claudeSession, claudeParent := "", ""
	if kind == Messages {
		claudeSession, claudeParent = claudeIdentity(input)
		if header := headerValue(headers, "X-Claude-Code-Session-Id"); header != "" {
			claudeSession = header
		}
	}
	if claudeSession != "" && (explicit == "" || fromMetadata) {
		// Claude subagents vary metadata.user_id; the shared session keeps the whole agent tree together.
		hints.Session, hints.Replace = "claude:"+claudeSession, fromMetadata
	}
	if claudeParent != "" && claudeParent != claudeSession {
		hints.Parent = "claude:" + claudeParent
	}
	turns := codexTurns(headers, input)
	if hints.Parent == "" {
		parents := []string{headerValue(headers, "X-Codex-Parent-Thread-Id", "X-Parent-Session-Id", "X-Parent-Session-Affinity", "X-Parent-Id")}
		for _, turn := range turns {
			parents = append(parents, turn.Parent, turn.ForkedFrom)
		}
		for _, value := range parents {
			if value = stickyValue(value); value != "" && value != explicit {
				hints.Parent = value
				break
			}
		}
	}
	if explicit != "" || hints.Session != "" {
		return hints
	}
	candidates := []string{headerValue(headers, "X-Session-Id", "X-Session-Affinity", "Thread-Id", "Thread_id")}
	for _, turn := range turns {
		candidates = append(candidates, turn.Session, turn.Thread)
	}
	if kind == Responses || kind == Compact {
		var conversation struct {
			ID string `json:"id"`
		}
		var id string
		if raw := input["conversation"]; json.Unmarshal(raw, &id) == nil {
			candidates = append(candidates, id)
		} else if json.Unmarshal(raw, &conversation) == nil {
			candidates = append(candidates, conversation.ID)
		}
	}
	for _, value := range candidates {
		if value = stickyValue(value); value != "" {
			// Only identifiers the gateway already honoured keep strict bindings; these are preferences.
			// Raw values match the parent identifiers subagents send for these conversations.
			hints.Session, hints.Derived = value, true
			return hints
		}
	}
	if session := messageSession(input, kind); session != "" {
		hints.Session, hints.Derived = session, true
	}
	return hints
}
