package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
)

func sessionDigest(userID, groupID int64, session string) [32]byte {
	// Preserve existing upstream session/cache IDs in the former default pool.
	if groupID == groups.LegacyID {
		return sha256.Sum256([]byte(fmt.Sprintf("%d:%s", userID, session)))
	}
	return sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", userID, groupID, session)))
}

// derivedDigest hashes stickiness preferences in a namespace explicit session digests cannot produce:
// explicit inputs always begin with a numeric user ID.
func derivedDigest(userID, groupID int64, session string) [32]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("derived:%d:%d:%s", userID, groupID, session)))
}

func sessionBinding(ctx context.Context, q *db.Queries, params db.ListSessionAffinitiesParams, scope string) (*db.ListSessionAffinitiesRow, error) {
	bindings, err := q.ListSessionAffinities(ctx, params)
	if err != nil {
		return nil, err
	}
	for i := range bindings {
		if bindings[i].Provider == scope {
			return &bindings[i], nil
		}
	}
	if scope == "auto" && len(bindings) > 0 {
		// Multiple legacy bindings can represent different upstream identities.
		if len(bindings) != 1 {
			return nil, ErrAffinityUnavailable
		}
		return &bindings[0], nil
	}
	return nil, nil
}

type affinityMode uint8

const (
	pinnedAffinity affinityMode = iota
	portableAffinity
	continuationAffinity
)

// A complete portable transcript can change models; opaque provider state must
// stay on its original account even when readable history accompanies it.
func requestAffinity(input map[string]json.RawMessage, kind Kind) affinityMode {
	for _, key := range []string{"previous_response_id", "conversation", "cachedContent", "container"} {
		if value := input[key]; len(value) > 0 && string(value) != "null" && string(value) != `""` {
			return continuationAffinity
		}
	}
	key, role := "messages", "assistant"
	if kind == Responses || kind == Compact {
		key = "input"
	} else if kind.IsGemini() {
		key, role = "contents", "model"
	}
	var items []struct {
		Type             string            `json:"type"`
		Role             string            `json:"role"`
		EncryptedContent json.RawMessage   `json:"encrypted_content"`
		Content          json.RawMessage   `json:"content"`
		Parts            json.RawMessage   `json:"parts"`
		ToolCalls        []json.RawMessage `json:"tool_calls"`
		CallID           string            `json:"call_id"`
		Name             string            `json:"name"`
		Arguments        *string           `json:"arguments"`
	}
	if json.Unmarshal(input[key], &items) != nil {
		return pinnedAffinity
	}
	history := false
	for _, item := range items {
		if item.Type == "item_reference" || item.Type == "compaction" || item.Type == "reasoning" || len(item.EncryptedContent) > 0 && string(item.EncryptedContent) != "null" {
			return continuationAffinity
		}
		blocks := item.Content
		if kind.IsGemini() {
			blocks = item.Parts
		}
		var content []struct {
			Type             string          `json:"type"`
			Signature        json.RawMessage `json:"signature"`
			ThoughtSignature json.RawMessage `json:"thoughtSignature"`
		}
		hasContent := false
		if json.Unmarshal(blocks, &content) == nil {
			hasContent = len(content) > 0
			for _, block := range content {
				if block.Type == "thinking" || block.Type == "redacted_thinking" || block.Type == "compaction" || len(block.Signature) > 0 || len(block.ThoughtSignature) > 0 {
					return continuationAffinity
				}
			}
		} else {
			var text string
			hasContent = json.Unmarshal(blocks, &text) == nil && text != ""
		}
		history = history || item.Role == role && (hasContent || len(item.ToolCalls) > 0) ||
			item.Type == "function_call" && item.CallID != "" && item.Name != "" && item.Arguments != nil
	}
	// Compaction produces provider state and must not authorize account rebinding.
	if history && kind != Compact {
		return portableAffinity
	}
	return pinnedAffinity
}
