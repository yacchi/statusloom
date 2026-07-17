package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/store"
)

// draftTool is the tool whose draft the `statusloom draft` subcommands act on.
// Like `statusloom monitor`, the draft channel is claude-code only for now.
const draftTool = "claude-code"

// defaultDraftFile is where `statusloom draft pull` writes and `statusloom
// draft push` reads when no path argument is given. It holds DSL source text.
const defaultDraftFile = "./statusloom-draft.xml"

// runDraft implements `statusloom draft <pull|push> [file]`, the local
// (network-free, token-free) bridge to the tool's draft working node (the
// internal store's refs.<tool>.draft; plans/config-store-and-format.md §3.5)
// that the web configurator edits live.
//
//	pull  reads the current draft source (falling back to the saved
//	      document, then the built-in default, when no draft exists) and
//	      writes it to [file] for editing.
//	push  reads [file] and writes it to the store's draft working node, where
//	      the web UI and monitor sessions pick it up. Parse/validation
//	      diagnostics are printed but do not block the write: the draft is a
//	      text-sharing channel that tolerates in-progress, invalid input
//	      (markup.md "draft共有").
func runDraft(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "statusloom draft: expected a subcommand (pull|push)")
		return 2
	}

	sub := args[0]
	file := defaultDraftFile
	if len(args) > 1 {
		file = args[1]
	}
	if len(args) > 2 {
		fmt.Fprintln(stderr, "statusloom draft: too many arguments")
		return 2
	}

	switch sub {
	case "pull":
		return runDraftPull(file, stdout, stderr)
	case "push":
		return runDraftPush(file, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "statusloom draft: unknown subcommand %q (want pull|push)\n", sub)
		return 2
	}
}

// runDraftPull reads the draft working node's source and writes it to file
// for editing, falling back to the saved document and then the built-in
// default when no draft exists yet.
func runDraftPull(file string, stdout, stderr io.Writer) int {
	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	src, exists, err := st.ReadDraft(draftTool)
	if err != nil {
		return fail(stderr, err)
	}
	if !exists {
		if rev, _, ok, err := st.Current(draftTool); err != nil {
			return fail(stderr, err)
		} else if ok {
			src = rev.Source
		} else {
			src = config.DefaultDocument(draftTool)
		}
	}
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Wrote draft to %s\n", file)
	return 0
}

// runDraftPush reads file and writes it to the store's draft working node.
// Diagnostics are reported to stderr but never block the write (draft
// tolerates in-progress input; monitor --draft falls back to the saved
// document when the draft is invalid).
func runDraftPush(file string, stdout, stderr io.Writer) int {
	data, err := os.ReadFile(file)
	if err != nil {
		return fail(stderr, err)
	}
	src := string(data)

	_, diags := dsl.ParseAndValidate(src)
	for _, d := range diags {
		fmt.Fprintf(stderr, "statusloom: %s: %s\n", d.Severity, d.Message)
	}

	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	if err := st.WriteDraft(draftTool, src); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Pushed draft from %s\n", file)
	return 0
}
