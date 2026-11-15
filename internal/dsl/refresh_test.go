package dsl

import (
	"slices"
	"strings"
	"testing"
)

func refreshDoc(refresh string) string {
	return `<statusloom version="1" tool="claude-code">
  <git cache-ttl-ms="3000"/>
  ` + refresh + `
  <layout name="default" active="true">
    <line><field name="git-branch"/></line>
  </layout>
</statusloom>
`
}

func TestRefreshParse(t *testing.T) {
	doc, diags := ParseAndValidate(refreshDoc(`<refresh wait="git source:ccprofile git" timeout-ms="1500" wait-on="due"/>`))
	if len(diags) != 0 {
		t.Fatalf("diags = %v, want none", diags)
	}
	r := doc.Root.Refresh
	if r == nil || r.Wait != "git source:ccprofile git" || r.TimeoutMS == nil || *r.TimeoutMS != 1500 || r.WaitOn != "due" {
		t.Fatalf("Refresh = %+v", r)
	}
	p := DocumentRefreshPolicy(doc)
	if !p.Waits() || p.TimeoutMS != 1500 || !p.WaitOnDue {
		t.Fatalf("policy = %+v", p)
	}
	if got := p.Tokens(); !slices.Equal(got, []string{"git", "source:ccprofile"}) {
		t.Errorf("Tokens = %q, want duplicates folded", got)
	}
}

func TestRefreshPolicyDefaults(t *testing.T) {
	doc, _ := ParseAndValidate(refreshDoc(`<refresh wait="usage"/>`))
	p := DocumentRefreshPolicy(doc)
	if p.TimeoutMS != DefaultRefreshTimeoutMS || p.WaitOnDue {
		t.Errorf("policy = %+v, want timeout %d, wait-on cold", p, DefaultRefreshTimeoutMS)
	}
	for _, src := range []string{"", `<refresh/>`, `<refresh wait=""/>`, `<refresh wait="   "/>`} {
		doc, diags := ParseAndValidate(refreshDoc(src))
		if len(diags) != 0 {
			t.Errorf("%q: diags = %v, want none", src, diags)
		}
		if DocumentRefreshPolicy(doc).Waits() {
			t.Errorf("%q: policy waits, want nothing", src)
		}
	}
}

func TestRefreshPolicyWants(t *testing.T) {
	cases := []struct {
		wait string
		kind string
		key  string
		want bool
	}{
		{"git", "git", "/repo", true},
		{"git", "usage", "default", false},
		{"transcript", "transcript", "s1", true},
		{"usage", "usage", "org", true},
		{"source:a", "source", "a", true},
		{"source:a", "source", "b", false},
		{"sources", "source", "b", true},
		{"sources", "git", "/repo", false},
		{"all", "git", "/repo", true},
		{"all", "source", "x", true},
		{"all", "usage", "k", true},
	}
	for _, tc := range cases {
		doc, _ := ParseAndValidate(refreshDoc(`<refresh wait="` + tc.wait + `"/>`))
		if got := DocumentRefreshPolicy(doc).Wants(tc.kind, tc.key); got != tc.want {
			t.Errorf("wait=%q Wants(%q, %q) = %v, want %v", tc.wait, tc.kind, tc.key, got, tc.want)
		}
	}
}

func TestRefreshValidation(t *testing.T) {
	cases := []struct {
		name, refresh, want string
	}{
		{"duplicate element", `<refresh wait="git"/><refresh wait="usage"/>`, "at most one <refresh>"},
		{"unknown token", `<refresh wait="git branch"/>`, `unknown <refresh> wait token "branch"`},
		{"bad source id", `<refresh wait="source:Bad_Id"/>`, `invalid source id "Bad_Id"`},
		{"empty source id", `<refresh wait="source:"/>`, `invalid source id ""`},
		{"timeout zero", `<refresh wait="git" timeout-ms="0"/>`, "timeout-ms must be between 1 and 3000, got 0"},
		{"timeout above max", `<refresh wait="git" timeout-ms="3001"/>`, "timeout-ms must be between 1 and 3000, got 3001"},
		{"timeout not integer", `<refresh wait="git" timeout-ms="1.5s"/>`, `"timeout-ms" must be a non-negative integer`},
		{"timeout negative", `<refresh wait="git" timeout-ms="-1"/>`, `"timeout-ms" must be a non-negative integer`},
		{"wait-on", `<refresh wait="git" wait-on="always"/>`, `invalid <refresh> wait-on "always"`},
		{"unknown attribute", `<refresh wait="git" timeout="5"/>`, `unknown attribute "timeout" on <refresh>`},
		{"child element", `<refresh wait="git"><git/></refresh>`, "<refresh> may not contain child elements"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := ParseAndValidate(refreshDoc(tc.refresh))
			if !HasErrors(diags) {
				t.Fatalf("no error, want %q", tc.want)
			}
			var msgs []string
			for _, d := range diags {
				msgs = append(msgs, d.Message)
			}
			if !strings.Contains(strings.Join(msgs, "\n"), tc.want) {
				t.Errorf("diags = %q, want one containing %q", msgs, tc.want)
			}
		})
	}
	// Boundaries are accepted; unused work items are not diagnosed.
	for _, ok := range []string{`<refresh wait="git" timeout-ms="1"/>`, `<refresh wait="transcript usage" timeout-ms="3000" wait-on="cold"/>`} {
		if _, diags := ParseAndValidate(refreshDoc(ok)); len(diags) != 0 {
			t.Errorf("%s: diags = %v, want none", ok, diags)
		}
	}
}

func TestRefreshSerializeRoundTrip(t *testing.T) {
	src := refreshDoc(`<refresh wait-on="due" timeout-ms="800" wait="git sources"/>`)
	doc, diags := ParseAndValidate(src)
	if HasErrors(diags) {
		t.Fatalf("diags = %v", diags)
	}
	canon := Serialize(doc)
	if !strings.Contains(canon, `<refresh wait="git sources" timeout-ms="800" wait-on="due"/>`) {
		t.Errorf("canonical output lacks the normalized <refresh>:\n%s", canon)
	}
	if strings.Index(canon, "<git") > strings.Index(canon, "<refresh") || strings.Index(canon, "<refresh") > strings.Index(canon, "<layout") {
		t.Errorf("canonical output reorders root children:\n%s", canon)
	}
	again, _ := ParseAndValidate(canon)
	if Serialize(again) != canon {
		t.Errorf("canonical serialization is not stable")
	}
	if got := SerializeMinimal(doc); got != src {
		t.Errorf("minimal serialization of a clean document is not byte-identical:\n%s", got)
	}
	doc.Root.Refresh.Meta.Dirty = true
	doc.Root.Refresh.WaitOn = ""
	if got := SerializeMinimal(doc); !strings.Contains(got, `<refresh wait="git sources" timeout-ms="800"/>`) || !strings.Contains(got, `<git cache-ttl-ms="3000"/>`) {
		t.Errorf("minimal serialization of a dirty <refresh>:\n%s", got)
	}
}
