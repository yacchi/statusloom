package cache

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// RefreshTranscript incrementally consumes complete JSONL records. It is only
// called by the refresh worker, never by the status-line render path.
func RefreshTranscript(sessionID, path string, now time.Time) error {
	cp, err := transcriptPath(sessionID)
	if err != nil {
		return err
	}
	var env TranscriptEnvelope
	_, _ = ReadJSON(cp, &env)
	if env.SessionID != sessionID || env.Cursor.Path != path {
		env = TranscriptEnvelope{SchemaVersion: 1, Source: "claude-transcript", SessionID: sessionID, Cursor: TranscriptCursor{Path: path, StartedAt: now}}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < env.Cursor.Offset {
		env.Cursor.Offset = 0
		env.Cursor.Seen = nil
		env.Value = schema.SessionAnalytics{}
		env.Cursor.StartedAt = now
	}
	if _, err := f.Seek(env.Cursor.Offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReader(f)
	seen := make(map[string]struct{}, len(env.Cursor.Seen))
	for _, id := range env.Cursor.Seen {
		seen[id] = struct{}{}
	}
	if env.Cursor.Usage == nil {
		env.Cursor.Usage = map[string]TranscriptUsage{}
	}
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		} // keep a partial final record for next pass
		if err != nil {
			return err
		}
		env.Cursor.Offset += int64(len(line))
		consumeTranscriptLine(bytes.TrimSpace(line), &env, seen)
	}
	env.Cursor.Size = st.Size()
	if env.Cursor.StartedAt.IsZero() {
		env.Cursor.StartedAt = now
	}
	elapsed := now.Sub(env.Cursor.StartedAt).Seconds()
	if elapsed > 0 {
		env.Value.InputTokensPerSecond = float64(env.Value.InputTokens) / elapsed
		env.Value.OutputTokensPerSecond = float64(env.Value.OutputTokens) / elapsed
		env.Value.TotalTokensPerSecond = float64(env.Value.TotalTokens) / elapsed
	}
	if len(env.Cursor.Seen) > 512 {
		env.Cursor.Seen = env.Cursor.Seen[len(env.Cursor.Seen)-512:]
	}
	env.ObservedAt, env.ExpiresAt, env.StaleUntil = now, now.Add(transcriptRefreshTTL), now.Add(24*time.Hour)
	return WriteJSONAtomic(cp, env)
}

func consumeTranscriptLine(line []byte, env *TranscriptEnvelope, seen map[string]struct{}) {
	if len(line) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(line, &raw) != nil {
		return
	}
	var sidechain bool
	_ = json.Unmarshal(raw["isSidechain"], &sidechain)
	if sidechain {
		return
	}
	if sid := stringValue(raw["sessionId"]); sid != "" && sid != env.SessionID {
		return
	}
	kind := stringValue(raw["type"])
	msgRaw := raw["message"]
	if kind == "assistant" || len(msgRaw) > 0 {
		var msg map[string]json.RawMessage
		if json.Unmarshal(msgRaw, &msg) == nil {
			id := stringValue(msg["id"])
			if id == "" {
				id = stringValue(raw["requestId"])
			}
			if id == "" {
				id = stringValue(raw["uuid"])
			}
			usageRaw := msg["usage"]
			if len(usageRaw) > 0 {
				if id == "" {
					sum := sha256.Sum256(usageRaw)
					id = hex.EncodeToString(sum[:])
				}
				var u struct {
					Input         int `json:"input_tokens"`
					Output        int `json:"output_tokens"`
					CacheCreation int `json:"cache_creation_input_tokens"`
					CacheRead     int `json:"cache_read_input_tokens"`
				}
				if json.Unmarshal(usageRaw, &u) == nil {
					old := env.Cursor.Usage[id]
					env.Value.InputTokens += u.Input - old.Input
					env.Value.OutputTokens += u.Output - old.Output
					env.Value.CacheCreationTokens += u.CacheCreation - old.CacheCreation
					env.Value.CacheReadTokens += u.CacheRead - old.CacheRead
					env.Value.TotalTokens += u.Input + u.Output + u.CacheCreation + u.CacheRead - old.Input - old.Output - old.CacheCreation - old.CacheRead
					env.Cursor.Usage[id] = TranscriptUsage{Input: u.Input, Output: u.Output, CacheCreation: u.CacheCreation, CacheRead: u.CacheRead}
				}
			}
		}
	}
	// Claude has used several compact record shapes. Deliberately accept only
	// explicit compact markers; ordinary summary messages must not be counted.
	lowerKind := strings.ToLower(kind)
	subtype := strings.ToLower(stringValue(raw["subtype"]))
	if strings.Contains(lowerKind, "compact") || subtype == "compact" || subtype == "compact_boundary" {
		compactID := "compact:" + stringValue(raw["uuid"])
		if compactID != "compact:" {
			if _, ok := seen[compactID]; ok {
				return
			}
			seen[compactID] = struct{}{}
			env.Cursor.Seen = append(env.Cursor.Seen, compactID)
		}
		env.Value.Compactions++
		mode := strings.ToLower(stringValue(raw["compact_type"]))
		if mode == "" {
			mode = strings.ToLower(stringValue(raw["trigger"]))
		}
		var metadata map[string]json.RawMessage
		if json.Unmarshal(raw["compactMetadata"], &metadata) == nil {
			if mode == "" {
				mode = strings.ToLower(stringValue(metadata["trigger"]))
			}
			pre, post := intValue(metadata["preTokens"]), intValue(metadata["postTokens"])
			if pre > post {
				env.Value.TokensReclaimed += pre - post
			}
		}
		switch mode {
		case "auto", "automatic":
			env.Value.CompactionsAuto++
		case "manual", "user":
			env.Value.CompactionsManual++
		default:
			env.Value.CompactionsUnknown++
		}
		reclaimed := intValue(raw["tokens_reclaimed"])
		if reclaimed == 0 {
			reclaimed = intValue(raw["reclaimed_tokens"])
		}
		env.Value.TokensReclaimed += reclaimed
	}
}

func stringValue(b json.RawMessage) string { var s string; _ = json.Unmarshal(b, &s); return s }
func intValue(b json.RawMessage) int       { var n int; _ = json.Unmarshal(b, &n); return n }
