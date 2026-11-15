package cli

import (
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/sources"
)

func refreshLines(out string) []string {
	var got []string
	for _, l := range lines(out) {
		if strings.Contains(l, " refresh - ") {
			got = append(got, l)
		}
	}
	return got
}

func TestDoctorRefreshWait(t *testing.T) {
	t.Run("no refresh element reports nothing", func(t *testing.T) {
		settings := setupDoctorSources(t)
		out, _ := runDoctorOut(t, settings)
		if got := refreshLines(out); len(got) != 0 {
			t.Errorf("refresh lines = %q, want none", got)
		}
	})

	t.Run("all used", func(t *testing.T) {
		settings := setupDoctorSources(t)
		writeDocument(t, "claude-code", waitDoc(`<refresh wait="git" timeout-ms="1500"/>`))
		out, _ := runDoctorOut(t, settings)
		got := refreshLines(out)
		if len(got) != 1 || got[0] != "PASS refresh - waits for git (timeout 1500ms)" {
			t.Errorf("refresh lines = %q", got)
		}
	})

	t.Run("unused, undefined and file sources, due with git", func(t *testing.T) {
		settings := setupDoctorSources(t)
		putSources(t, sources.Definition{ID: "f", Kind: sources.KindFile, Path: "~/x.json"})
		writeDocument(t, "claude-code", `<statusloom version="1" tool="claude-code">
  <refresh wait="git transcript source:f source:gone source:unref sources" wait-on="due"/>
  <layout name="default" active="true"><line><field name="git-branch"/><field name="source" source="f"/><field name="source" source="gone"/></line></layout>
</statusloom>`)
		out, code := runDoctorOut(t, settings)
		if code != 0 {
			t.Errorf("doctor exit = %d, want 0 (warnings only)", code)
		}
		joined := strings.Join(refreshLines(out), "\n")
		for _, want := range []string{
			"WARN refresh - <refresh wait> names work the document does not use (ignored): source:unref, sources, transcript",
			"WARN refresh - source:f: file sources are read directly while rendering; there is nothing to wait for",
			"WARN refresh - source:gone: source is not defined in sources.json",
			`WARN refresh - wait-on="due" with git`,
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("missing %q in:\n%s", want, joined)
			}
		}
		if strings.Contains(joined, "PASS refresh") {
			t.Errorf("PASS reported alongside warnings:\n%s", joined)
		}
	})
}
