package render

import (
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

func TestHomeRelativePath(t *testing.T) {
	tests := []struct {
		name string
		abs  string
		home string
		want string
	}{
		{"under home", "/Users/fujie/projects/statusloom", "/Users/fujie", "~/projects/statusloom"},
		{"home itself", "/Users/fujie", "/Users/fujie", "~"},
		{"home with trailing slash", "/Users/fujie", "/Users/fujie/", "~"},
		{"sibling sharing the prefix stays absolute", "/Users/fujieX/proj", "/Users/fujie", "/Users/fujieX/proj"},
		{"sibling of the same name stays absolute", "/Users/fujieX", "/Users/fujie", "/Users/fujieX"},
		{"outside home stays absolute", "/opt/work", "/Users/fujie", "/opt/work"},
		{"unknown home stays absolute", "/Users/fujie/x", "", "/Users/fujie/x"},
		{"root home is not rewritten", "/srv/x", "/", "/srv/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := homeRelativePath(tc.abs, tc.home); got != tc.want {
				t.Errorf("homeRelativePath(%q, %q) = %q, want %q", tc.abs, tc.home, got, tc.want)
			}
		})
	}
}

func TestApplyPathStyle_EmptyStaysEmpty(t *testing.T) {
	for _, style := range []string{"base", "home", "full"} {
		if got := applyPathStyle("", style, "/Users/fujie"); got != "" {
			t.Errorf("applyPathStyle(empty, %q) = %q, want empty", style, got)
		}
	}
}

func pathSnap(cwd string) schema.StatusSnapshot {
	snap := schema.StatusSnapshot{}
	snap.System.Cwd = cwd
	snap.System.ProjectDir = cwd
	return snap
}

func TestPathField_Styles(t *testing.T) {
	const cwd = "/Users/fujie/projects/private/oss/statusloom"
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{"HOME": "/Users/fujie"})}
	tests := []struct {
		name string
		body string
		want string
	}{
		{"current-directory default is base", `<field name="current-directory"/>`, "statusloom"},
		{"current-directory explicit base", `<field name="current-directory" path="base"/>`, "statusloom"},
		{"current-directory home", `<field name="current-directory" path="home"/>`, "~/projects/private/oss/statusloom"},
		{"current-directory full", `<field name="current-directory" path="full"/>`, cwd},
		{"project-directory default is full", `<field name="project-directory"/>`, cwd},
		{"project-directory home", `<field name="project-directory" path="home"/>`, "~/projects/private/oss/statusloom"},
		{"project-directory base", `<field name="project-directory" path="base"/>`, "statusloom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderPathDoc(t, wrap(tc.body), pathSnap(cwd), opts); got != tc.want {
				t.Errorf("rendered %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPathField_CwdEqualToHome(t *testing.T) {
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{"HOME": "/Users/fujie"})}
	got := renderPathDoc(t, wrap(`<field name="current-directory" path="home"/>`), pathSnap("/Users/fujie"), opts)
	if got != "~" {
		t.Errorf("cwd == HOME rendered %q, want ~", got)
	}
}

func TestPathField_BoundaryInRender(t *testing.T) {
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{"HOME": "/Users/fujie"})}
	got := renderPathDoc(t, wrap(`<field name="current-directory" path="home"/>`), pathSnap("/Users/fujieX"), opts)
	if got != "/Users/fujieX" {
		t.Errorf("sibling of HOME rendered %q, want /Users/fujieX", got)
	}
}

func TestPathField_GitRootWithoutRepositoryIsEmpty(t *testing.T) {
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{"HOME": "/Users/fujie"})}
	got := renderPathDoc(t, wrap(`<field name="git-root" path="home"/>`), pathSnap("/Users/fujie/x"), opts)
	if got != "" {
		t.Errorf("git-root without repository rendered %q, want empty", got)
	}
}

// renderPathDoc renders src against snap and returns the joined plain text.
// renderDoc has no snapshot parameter, so the path tests use this variant.
func renderPathDoc(t *testing.T, src string, snap schema.StatusSnapshot, opts Options) string {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil || dsl.HasErrors(diags) {
		t.Fatalf("parse failed: %v", diags)
	}
	var b strings.Builder
	for _, l := range RenderDocument(snap, doc, opts) {
		if l.Omitted {
			continue
		}
		for _, seg := range l.Segments {
			if seg.Visible {
				b.WriteString(seg.Text)
			}
		}
	}
	return b.String()
}
