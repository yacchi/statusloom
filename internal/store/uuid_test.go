package store

import (
	"regexp"
	"testing"
)

var uuidV7Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDv7Format(t *testing.T) {
	id, err := generateUUIDv7()
	if err != nil {
		t.Fatalf("generateUUIDv7: %v", err)
	}
	if !uuidV7Re.MatchString(id) {
		t.Fatalf("id %q is not a canonical UUIDv7 (version 7 / RFC4122 variant)", id)
	}
}

func TestUUIDv7MonotonicAndUnique(t *testing.T) {
	// Reset the process-global guard so this test is independent of others.
	genMu.Lock()
	lastID = ""
	genMu.Unlock()

	const n = 5000
	seen := make(map[string]struct{}, n)
	var prev string
	for i := 0; i < n; i++ {
		id, err := newUUIDv7()
		if err != nil {
			t.Fatalf("newUUIDv7: %v", err)
		}
		if !uuidV7Re.MatchString(id) {
			t.Fatalf("id %q is not canonical UUIDv7", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q at iteration %d", id, i)
		}
		seen[id] = struct{}{}
		// Even within the same millisecond the guard must keep ids strictly
		// increasing lexicographically.
		if prev != "" && id <= prev {
			t.Fatalf("id not strictly increasing: %q <= %q (iteration %d)", id, prev, i)
		}
		prev = id
	}
}

func TestIncrementUUIDIncreases(t *testing.T) {
	cases := []string{
		"00000000-0000-7000-8000-000000000000",
		"017f22e2-79b0-7cc3-98c4-dc0c0c07398f",
		// Carry across a byte boundary.
		"00000000-0000-0000-0000-0000000000ff",
	}
	for _, c := range cases {
		got := incrementUUID(c)
		if got <= c {
			t.Fatalf("incrementUUID(%q) = %q, expected strictly greater", c, got)
		}
	}
}
