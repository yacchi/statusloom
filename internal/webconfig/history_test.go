package webconfig

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/store"
)

// historySource returns a valid claude-code document whose layout name embeds
// name, so successive Saves produce distinct revisions (identical source
// would dedup to a no-op).
func historySource(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model"/></line></layout></statusloom>`
}

func TestHistory_Get_ListsRevisionsAndRefs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)
	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))

	id1, _, err := st.Save("claude-code", historySource("a"), "test", store.Meta{Name: "first"})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := st.Save("claude-code", historySource("b"), "test", store.Meta{Name: "second"})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := st.WriteDraft("claude-code", "wip"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	resp := authedRequest(t, ts, "GET", "/api/history?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Revisions []struct {
			ID     string  `json:"id"`
			Parent *string `json:"parent"`
			Origin string  `json:"origin"`
			Meta   struct {
				Name string `json:"name"`
			} `json:"meta"`
		} `json:"revisions"`
		Refs struct {
			Current string `json:"current"`
			Draft   bool   `json:"draft"`
		} `json:"refs"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Revisions) != 2 {
		t.Fatalf("revisions len = %d, want 2", len(body.Revisions))
	}
	if body.Revisions[0].ID != id1 || body.Revisions[1].ID != id2 {
		t.Errorf("revision order = [%s, %s], want [%s, %s]", body.Revisions[0].ID, body.Revisions[1].ID, id1, id2)
	}
	if body.Revisions[0].Parent != nil {
		t.Errorf("first revision parent = %v, want nil", body.Revisions[0].Parent)
	}
	if body.Revisions[1].Parent == nil || *body.Revisions[1].Parent != id1 {
		t.Errorf("second revision parent = %v, want %s", body.Revisions[1].Parent, id1)
	}
	if body.Revisions[0].Meta.Name != "first" || body.Revisions[1].Meta.Name != "second" {
		t.Errorf("meta.name not carried: %+v", body.Revisions)
	}
	if body.Refs.Current != id2 {
		t.Errorf("refs.current = %q, want %q", body.Refs.Current, id2)
	}
	if !body.Refs.Draft {
		t.Error("refs.draft = false, want true (a draft was written)")
	}
}

func TestHistory_Get_UnknownToolRejected(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/history?tool=nope", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHistory_GetByID_ReturnsSourceAndMeta(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)
	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))

	src := historySource("a")
	id, _, err := st.Save("claude-code", src, "test", store.Meta{Name: "n", Description: "d", Author: "a"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := authedRequest(t, ts, "GET", "/api/history/"+id, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		ID     string `json:"id"`
		Tool   string `json:"tool"`
		Source string `json:"source"`
		Meta   struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Author      string `json:"author"`
		} `json:"meta"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != id || body.Tool != "claude-code" || body.Source != src {
		t.Errorf("body mismatch: %+v", body)
	}
	if body.Meta.Name != "n" || body.Meta.Description != "d" || body.Meta.Author != "a" {
		t.Errorf("meta mismatch: %+v", body.Meta)
	}
}

func TestHistory_GetByID_UnknownIs404(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/history/does-not-exist", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHistory_Restore_RepointsCurrentAndDiscardsDraft(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)
	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))

	id1, _, err := st.Save("claude-code", historySource("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := st.Save("claude-code", historySource("b"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := st.WriteDraft("claude-code", "wip"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	resp := putPOST(t, ts, "/api/history/"+id1+"/restore", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Tool    string `json:"tool"`
		Current string `json:"current"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK || body.Tool != "claude-code" || body.Current != id1 {
		t.Errorf("restore response mismatch: %+v", body)
	}

	_, curID, ok, err := st.Current("claude-code")
	if err != nil || !ok || curID != id1 {
		t.Fatalf("current after restore = %q, want %q", curID, id1)
	}
	if _, ok, _ := st.ReadDraft("claude-code"); ok {
		t.Error("restore should have discarded the draft working node")
	}
}

func TestHistory_Restore_UnknownIs404(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := putPOST(t, ts, "/api/history/does-not-exist/restore", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHistory_RequiresAuth(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	for _, req := range []struct {
		method, path string
	}{
		{"GET", "/api/history"},
		{"GET", "/api/history/some-id"},
		{"POST", "/api/history/some-id/restore"},
	} {
		httpReq, err := http.NewRequest(req.method, ts.baseURL+req.path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("request %s %s: %v", req.method, req.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated status = %d, want 401", req.method, req.path, resp.StatusCode)
		}
	}
}
