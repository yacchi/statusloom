package modrec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/schema"
)

// ErrUnsupportedVersion is returned by Ingest when the request's protocol
// version v is missing or is not 1. Nothing is loaded or written for such a
// request, so an older or newer mod never corrupts a record.
var ErrUnsupportedVersion = errors.New("mod-ingest: unsupported protocol version")

// request is one mod-ingest request as the mod sends it on stdin.
type request struct {
	V         json.RawMessage `json:"v"`
	SessionID string          `json:"sessionId"`
	Boot      string          `json:"boot"`
	Events    []event         `json:"events"`
	Queries   queries         `json:"queries"`
}

// event is one recorded hook event of a request.
type event struct {
	Seq     int64           `json:"seq"`
	At      int64           `json:"at"` // epoch ms
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// queries holds the optional query results carried by a request.
type queries struct {
	Agents *query `json:"agents"`
	Usage  *query `json:"usage"`
}

// query is one query result together with the time it was taken.
type query struct {
	At    int64           `json:"at"` // epoch ms
	Value json.RawMessage `json:"value"`
}

// payload is the top-level key/value map of one event's payload. Values stay
// raw so each rule decides how to read them.
type payload map[string]json.RawMessage

// Ingest folds one mod-ingest request into the session's mod record and
// stores it. A request with a missing or unsupported v returns
// ErrUnsupportedVersion before anything is read or written. An empty sessionId
// or an invalid request body is an error that also writes nothing. On success
// the record is stored and records older than the retention period are pruned.
func Ingest(raw []byte, now time.Time) error {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("mod-ingest: invalid request: %w", err)
	}
	if string(bytes.TrimSpace(req.V)) != "1" {
		return ErrUnsupportedVersion
	}
	if req.SessionID == "" {
		return errors.New("mod-ingest: empty sessionId")
	}

	rec, err := cache.LoadModRecord(req.SessionID)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &cache.ModRecord{SessionID: req.SessionID}
	}
	if rec.Events == nil {
		rec.Events = make(map[string]cache.ModEventStat)
	}

	fold(rec, &req)
	if q := req.Queries.Agents; q != nil {
		ApplyAgents(&rec.Snapshot, time.UnixMilli(q.At), q.Value)
	}
	if q := req.Queries.Usage; q != nil {
		ApplyUsage(&rec.Snapshot, time.UnixMilli(q.At), q.Value)
	}
	rec.Snapshot.UpdatedAt = now

	if err := cache.StoreModRecord(rec); err != nil {
		return err
	}
	cache.PruneModRecords(now)
	return nil
}

// fold applies the request's events to rec in seq order. Events already
// covered by the cursor (same boot, seq at or below the cursor) are skipped,
// which makes a retried send harmless.
func fold(rec *cache.ModRecord, req *request) {
	events := append([]event(nil), req.Events...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Seq < events[j].Seq
	})

	for _, ev := range events {
		if req.Boot == rec.Cursor.Boot && ev.Seq <= rec.Cursor.Seq {
			continue
		}
		rec.Cursor = cache.ModCursor{Boot: req.Boot, Seq: ev.Seq}

		var p payload
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p == nil {
			p = payload{}
		}
		if s, ok := str(p, "session_id"); ok && s != req.SessionID {
			continue
		}

		st := rec.Events[ev.Name]
		st.Count++
		st.LastAt = ev.At
		rec.Events[ev.Name] = st

		foldEvent(rec, ev.Name, p)
	}
}

// foldEvent applies the per-name rules to the snapshot. Events without a
// matching rule change nothing beyond the Events counters kept by fold.
func foldEvent(rec *cache.ModRecord, name string, p payload) {
	s := &rec.Snapshot

	switch name {
	case "turn.complete":
		if has(p, "agentId") {
			return
		}
		duration, _ := num(p, "durationMs")
		reason, _ := str(p, "reason")
		s.LastTurn = &schema.ModTurn{Duration: ms(duration), Reason: reason}
		foldModels(s, p)

	case "agent.spawn":
		// The mode the spawn inherits is the parent's; only the main loop's counts.
		if has(p, "parentAgentId") {
			return
		}
		if mode, ok := str(p, "permissionMode"); ok {
			s.PermissionMode = mode
		}
	}
}

// foldModels folds the models a main-loop turn.complete carries: sessionModel
// (the session's model when the turn ended) and usageModel (the model that
// answered the turn). A change of the session model between turns is a switch.
// The turn's model is kept only while the turn reported one.
func foldModels(s *schema.ModSnapshot, p payload) {
	s.TurnModel = ""
	if m, ok := str(p, "usageModel"); ok && m != "" {
		s.TurnModel = claude.PrettyModelName(m)
	}
	if m, ok := str(p, "sessionModel"); ok && m != "" {
		session := claude.PrettyModelName(m)
		if s.SessionModel != "" && s.SessionModel != session {
			s.ModelSwitches++
		}
		s.SessionModel = session
	}
}

// ms converts a duration reported in milliseconds to a time.Duration.
func ms(v float64) time.Duration {
	return time.Duration(v * float64(time.Millisecond))
}

// str returns the value of key when it is a JSON string. A missing key, null
// and any other JSON type all report false.
func str(p payload, key string) (string, bool) {
	raw := bytes.TrimSpace(p[key])
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// num returns the value of key when it is a JSON number. A missing key, null
// and any other JSON type all report false.
func num(p payload, key string) (float64, bool) {
	raw := bytes.TrimSpace(p[key])
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

// has reports whether key is present with a value other than JSON null.
func has(p payload, key string) bool {
	raw := bytes.TrimSpace(p[key])
	return len(raw) > 0 && string(raw) != "null"
}
