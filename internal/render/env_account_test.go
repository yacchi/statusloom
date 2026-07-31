package render

import (
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// renderDoc parses src and renders it, returning the joined plain text.
func renderDoc(t *testing.T, src string, opts Options) string {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil {
		t.Fatalf("parse produced no document: %v", diags)
	}
	if dsl.HasErrors(diags) {
		t.Fatalf("parse errors: %v", diags)
	}
	lines := RenderDocument(schema.StatusSnapshot{}, doc, opts)
	var b strings.Builder
	for _, l := range lines {
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

func wrap(body string) string {
	return `<statusloom version="1" tool="claude-code" color-level="none">` +
		`<layout name="l" active="true"><line>` + body + `</line></layout></statusloom>`
}

func fakeEnv(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestEnvField(t *testing.T) {
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{
		"AWS_PROFILE": "prod",
		"EMPTY":       "",
	})}

	tests := []struct {
		name string
		body string
		want string
	}{
		{"reads the named variable", `<field name="env" var="AWS_PROFILE"/>`, "prod"},
		{"unset variable renders empty", `<field name="env" var="NOT_SET"/>`, ""},
		{"variable set to empty renders empty", `<field name="env" var="EMPTY"/>`, ""},
		{"no var renders empty", `<field name="env"/>`, ""},
		{
			"decorations still render without a var, as for every field",
			`<field name="env" prefix="p:"/>`,
			"p:",
		},
		{
			"optional gates the whole field including its prefix",
			`<field name="env" var="NOT_SET" prefix="p:" optional="env:NOT_SET"/>`,
			"",
		},
		{
			"optional passes when the variable is set",
			`<field name="env" var="AWS_PROFILE" prefix="aws:" optional="env:AWS_PROFILE"/>`,
			"aws:prod",
		},
		{
			"optional on an enclosing span drops the span's prefix too",
			`<span prefix="aws:" optional="env:NOT_SET"><field name="env" var="NOT_SET"/></span>`,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderDoc(t, wrap(tt.body), opts); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestEnvField_SecretMasking pins that a credential-looking variable name
// never renders its value, and that unmask is the only way to override it.
func TestEnvField_SecretMasking(t *testing.T) {
	const secret = "sk-ant-must-not-appear"
	opts := Options{Now: time.Now(), Env: fakeEnv(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": secret,
		"AWS_PROFILE":             "prod",
	})}

	got := renderDoc(t, wrap(`<field name="env" var="CLAUDE_CODE_OAUTH_TOKEN"/>`), opts)
	if strings.Contains(got, secret) {
		t.Fatalf("render = %q, leaks the secret value", got)
	}
	if got != dsl.SecretMask {
		t.Errorf("render = %q, want %q", got, dsl.SecretMask)
	}

	got = renderDoc(t, wrap(`<field name="env" var="CLAUDE_CODE_OAUTH_TOKEN" unmask="true"/>`), opts)
	if got != secret {
		t.Errorf("unmask render = %q, want the raw value", got)
	}

	// unmask on a non-secret name is a no-op, not a double transformation.
	got = renderDoc(t, wrap(`<field name="env" var="AWS_PROFILE" unmask="true"/>`), opts)
	if got != "prod" {
		t.Errorf("render = %q, want prod", got)
	}
}

// TestEnvField_DefaultsToProcessEnv covers Options.Env being nil: the real
// process environment is consulted.
func TestEnvField_DefaultsToProcessEnv(t *testing.T) {
	t.Setenv("STATUSLOOM_ENV_FIELD_TEST", "visible")
	got := renderDoc(t, wrap(`<field name="env" var="STATUSLOOM_ENV_FIELD_TEST"/>`), Options{Now: time.Now()})
	if got != "visible" {
		t.Errorf("render = %q, want visible", got)
	}
}

func TestAccountFields(t *testing.T) {
	profile := &schema.AccountProfile{
		Email:        "dev@example.com",
		DisplayName:  "Dev User",
		Organization: "Example Inc",
		Role:         "primary_owner",
		Type:         "claude_team",
		Plan:         "default_claude_max_5x",
		Seat:         "team_tier_1",
	}
	opts := Options{Now: time.Now(), Profile: func() *schema.AccountProfile { return profile }}

	tests := []struct{ field, want string }{
		{"account-email", "dev@example.com"},
		{"account-name", "Dev User"},
		{"account-org", "Example Inc"},
		{"account-role", "primary_owner"},
		{"account-type", "claude_team"},
		{"account-plan", "default_claude_max_5x"},
		{"account-seat", "team_tier_1"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := renderDoc(t, wrap(`<field name="`+tt.field+`"/>`), opts)
			if got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAccountFields_NoProfile covers a logged-out / missing-.claude.json
// install: every account field renders empty and optional drops it.
func TestAccountFields_NoProfile(t *testing.T) {
	for _, opts := range []Options{
		{Now: time.Now()}, // no provider wired
		{Now: time.Now(), Profile: func() *schema.AccountProfile { return nil }}, // provider found nothing
	} {
		if got := renderDoc(t, wrap(`<field name="account-email"/>`), opts); got != "" {
			t.Errorf("render = %q, want empty", got)
		}
		body := `<span prefix="as:" optional="account-email"><field name="account-email"/></span>`
		if got := renderDoc(t, wrap(body), opts); got != "" {
			t.Errorf("optional span render = %q, want empty", got)
		}
	}
}

// TestAccountFields_PartialProfile covers a personal account with no
// organization: the populated fields render, the absent ones do not.
func TestAccountFields_PartialProfile(t *testing.T) {
	opts := Options{Now: time.Now(), Profile: func() *schema.AccountProfile {
		return &schema.AccountProfile{Email: "solo@example.com"}
	}}
	if got := renderDoc(t, wrap(`<field name="account-email"/>`), opts); got != "solo@example.com" {
		t.Errorf("email = %q, want solo@example.com", got)
	}
	if got := renderDoc(t, wrap(`<field name="account-org"/>`), opts); got != "" {
		t.Errorf("org = %q, want empty", got)
	}
}

// TestAccountFields_ProviderNotCalledWithoutAccountField pins the laziness
// that keeps .claude.json off the render path for documents that do not use
// an account field.
func TestAccountFields_ProviderNotCalledWithoutAccountField(t *testing.T) {
	calls := 0
	opts := Options{Now: time.Now(), Profile: func() *schema.AccountProfile {
		calls++
		return &schema.AccountProfile{Email: "dev@example.com"}
	}}

	renderDoc(t, wrap(`<field name="session-id"/><field name="env" var="X"/>`), opts)
	if calls != 0 {
		t.Errorf("profile provider called %d times for a document with no account field, want 0", calls)
	}

	renderDoc(t, wrap(`<field name="account-email"/>`), opts)
	if calls == 0 {
		t.Error("profile provider never called for a document with an account field")
	}
}
