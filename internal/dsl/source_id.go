package dsl

import "strings"

// ValidSourceID reports whether id is a legal external source id: 1-64 bytes,
// first byte [a-z], the rest [a-z0-9-]. It is shared by DSL validation and the
// sources.json store so both accept exactly the same ids. It works on bytes
// rather than runes, so any non-ASCII byte is rejected outright.
func ValidSourceID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	if c := id[0]; c < 'a' || c > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

// ParseMetricRef splits a parameterized metric reference at the first ':'
// ("source-stale:ccprofile" yields ("source-stale", "ccprofile")). The param is
// the rest verbatim, so further colons are kept ("a:b:c" yields ("a", "b:c")).
// A reference without a colon yields (ref, "").
func ParseMetricRef(ref string) (name, param string) {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}
