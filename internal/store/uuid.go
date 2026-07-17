package store

// This file implements a UUIDv7 generator using only the standard library
// (48-bit millisecond timestamp + crypto/rand + version/variant bits), plus
// the process-local monotonicity guard required by
// plans/config-store-and-format.md §3.3: because a revision id is a history
// event identifier (not content-derived) and UUIDv7 is time-ordered, the
// generator remembers the last id it issued and, if a freshly generated id
// would sort lexicographically <= the previous one (same-millisecond
// collision or a clock that did not advance), it bumps the previous value by
// one instead. This keeps ids strictly increasing within a process; strict
// cross-process ordering is explicitly not guaranteed (§3.3).

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

var (
	genMu  sync.Mutex
	lastID string
)

// newUUIDv7 returns a fresh UUIDv7 in canonical
// "xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx" form, guaranteed to sort strictly
// after every id previously returned by this process.
func newUUIDv7() (string, error) {
	genMu.Lock()
	defer genMu.Unlock()

	id, err := generateUUIDv7()
	if err != nil {
		return "", err
	}
	if lastID != "" && id <= lastID {
		id = incrementUUID(lastID)
	}
	lastID = id
	return id, nil
}

// generateUUIDv7 builds a raw UUIDv7 from the current wall clock and random
// bytes, without the monotonicity guard.
func generateUUIDv7() (string, error) {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x70 // version 7 in the high nibble of byte 6
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant in the high bits of byte 8
	return formatUUID(b), nil
}

// incrementUUID parses a canonical UUID string, adds one to its 128-bit
// big-endian integer value, and reformats it. Because the canonical form is
// fixed-width lowercase hex with dashes at fixed positions, integer order and
// lexicographic string order coincide, so the result always sorts strictly
// after the input.
func incrementUUID(s string) string {
	b := parseUUID(s)
	for i := 15; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			break
		}
	}
	return formatUUID(b)
}

// parseUUID converts a canonical UUID string back to its 16 raw bytes. A
// malformed input yields a zero value; callers only ever pass values produced
// by formatUUID, so this is not expected in practice.
func parseUUID(s string) [16]byte {
	var b [16]byte
	decoded, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err == nil {
		copy(b[:], decoded)
	}
	return b
}

// formatUUID renders 16 bytes as the canonical dashed lowercase-hex form.
func formatUUID(b [16]byte) string {
	var sb strings.Builder
	sb.Grow(36)
	enc := hex.EncodeToString
	sb.WriteString(enc(b[0:4]))
	sb.WriteByte('-')
	sb.WriteString(enc(b[4:6]))
	sb.WriteByte('-')
	sb.WriteString(enc(b[6:8]))
	sb.WriteByte('-')
	sb.WriteString(enc(b[8:10]))
	sb.WriteByte('-')
	sb.WriteString(enc(b[10:16]))
	return sb.String()
}
