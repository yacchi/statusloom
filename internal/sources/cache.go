package sources

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// Envelope is the last fetch result of one command/http source. Scheduling (next due
// time, backoff) lives in the refresh schedule (internal/cache), not here.
type Envelope struct {
	SchemaVersion int             `json:"schemaVersion"` // 1
	ID            string          `json:"id"`
	DefHash       string          `json:"defHash"`             // Definition.Hash() of the fetched definition
	FetchedAt     time.Time       `json:"fetchedAt,omitempty"` // last success (200 or 304)
	AttemptedAt   time.Time       `json:"attemptedAt"`
	OK            bool            `json:"ok"`
	Error         string          `json:"error,omitempty"`
	ETag          string          `json:"etag,omitempty"`
	Body          json.RawMessage `json:"body,omitempty"` // present only when OK
}

// envelopeSchemaVersion is the on-disk version written by StoreEnvelope.
const envelopeSchemaVersion = 1

// envelopePath lives under sources/external/ so that source ids cannot collide with
// the transcript cache files sources/transcript-*.json.
func envelopePath(id string) (string, error) {
	dir, err := cache.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sources", "external", id+".json"), nil
}

// LoadEnvelope returns the cached envelope for id. The bool is false when the file is
// missing, unreadable or corrupt; the render path treats all of these as "no value".
func LoadEnvelope(id string) (Envelope, bool) {
	path, err := envelopePath(id)
	if err != nil {
		return Envelope{}, false
	}
	var env Envelope
	ok, err := cache.ReadJSON(path, &env)
	if err != nil || !ok {
		return Envelope{}, false
	}
	return env, true
}

// StoreEnvelope atomically writes the envelope for e.ID.
func StoreEnvelope(e Envelope) error {
	path, err := envelopePath(e.ID)
	if err != nil {
		return err
	}
	if e.SchemaVersion == 0 {
		e.SchemaVersion = envelopeSchemaVersion
	}
	return cache.WriteJSONAtomic(path, e)
}

// Available reports whether env may be served for def: it must be a successful fetch of
// the current definition (same Hash), carry a body, and be no older than the definition's
// StaleAfter. The boundary is inclusive: an age exactly equal to StaleAfter is still available.
func Available(def Definition, env Envelope, now time.Time, refreshInterval time.Duration) bool {
	return env.OK &&
		env.DefHash == def.Hash() &&
		len(env.Body) > 0 &&
		now.Sub(env.FetchedAt) <= def.StaleAfter(refreshInterval)
}
