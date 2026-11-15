package webconfig

import (
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type refreshItemsResp struct {
	RefreshItems *struct {
		Git        bool     `json:"git"`
		Transcript bool     `json:"transcript"`
		Sources    []string `json:"sources"`
	} `json:"refreshItems"`
}

// TestRefreshItems_ParseAndSerialize: parse and serialize report the work
// items the document uses (dsl.UsesGit / UsesTranscript / ReferencedSources),
// including references only reachable through when / optional= / fallback.
func TestRefreshItems_ParseAndSerialize(t *testing.T) {
	isolateClaudeConfig(t)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	const used = `<statusloom version="1" tool="claude-code">
  <layout name="Default"><line>
    <span when="git-dirty eq true"><text>*</text></span>
    <field name="source" source="zeta" select=".v" fallback="session-total-tokens"/>
    <span optional="source:alpha"><field name="model"/></span>
  </line></layout>
</statusloom>
`
	const unused = `<statusloom version="1" tool="claude-code"><layout name="Default"><line><field name="model"/></line></layout></statusloom>`

	parse := func(src string) refreshItemsResp {
		t.Helper()
		resp := putPOST(t, ts, "/api/dsl/parse", map[string]any{"source": src})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("parse status = %d", resp.StatusCode)
		}
		var out refreshItemsResp
		if err := decodeJSON(resp.Body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := parse(used)
	if got.RefreshItems == nil || !got.RefreshItems.Git || !got.RefreshItems.Transcript ||
		!reflect.DeepEqual(got.RefreshItems.Sources, []string{"alpha", "zeta"}) {
		t.Fatalf("parse refreshItems = %+v", got.RefreshItems)
	}
	none := parse(unused)
	if none.RefreshItems == nil || none.RefreshItems.Git || none.RefreshItems.Transcript ||
		none.RefreshItems.Sources == nil || len(none.RefreshItems.Sources) != 0 {
		t.Fatalf("parse refreshItems (unused) = %+v", none.RefreshItems)
	}
	if bad := parse("<statusloom"); bad.RefreshItems != nil {
		t.Fatalf("unparsable source reported refreshItems: %+v", bad.RefreshItems)
	}

	ast, _ := buildAST(parseSourceFields(t, used))
	resp := putPOST(t, ts, "/api/dsl/serialize", map[string]any{"ast": ast})
	defer resp.Body.Close()
	var ser refreshItemsResp
	if err := decodeJSON(resp.Body, &ser); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ser.RefreshItems, got.RefreshItems) {
		t.Fatalf("serialize refreshItems = %+v, want %+v", ser.RefreshItems, got.RefreshItems)
	}
}
