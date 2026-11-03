package webconfig

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// TestDSLFields_PathStylesMetadata pins the path metadata the properties panel
// keys off: directory fields list their path styles and default, and fields
// without a path attribute carry neither.
func TestDSLFields_PathStylesMetadata(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Fields []struct {
			Name        string   `json:"name"`
			PathStyles  []string `json:"pathStyles"`
			PathDefault string   `json:"pathDefault"`
		} `json:"fields"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]string{
		"current-directory": "base",
		"project-directory": "full",
		"git-root":          "full",
	}
	all := []string{"base", "home", "full"}
	seen := map[string]bool{}
	for _, f := range body.Fields {
		def, isPath := want[f.Name]
		if !isPath {
			if len(f.PathStyles) != 0 || f.PathDefault != "" {
				t.Errorf("field %q carries path metadata %v/%q, want none", f.Name, f.PathStyles, f.PathDefault)
			}
			continue
		}
		seen[f.Name] = true
		if !reflect.DeepEqual(f.PathStyles, all) || f.PathDefault != def {
			t.Errorf("field %q path = %v/%q, want %v/%q", f.Name, f.PathStyles, f.PathDefault, all, def)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("field %q missing from the catalog", name)
		}
	}
}
