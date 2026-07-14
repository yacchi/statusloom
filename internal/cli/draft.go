package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
)

// draftTool is the tool whose draft the `statusloom draft` subcommands act on.
// Like `statusloom monitor`, the draft channel is claude-code only for now.
const draftTool = "claude-code"

// defaultDraftFile is where `statusloom draft pull` writes and `statusloom
// draft push` reads when no path argument is given. It holds DSL source text.
const defaultDraftFile = "./statusloom-draft.xml"

// runDraft implements `statusloom draft <pull|push> [file]`, the local
// (network-free, token-free) bridge to the shared draft document
// (<configDir>/<tool>.draft.xml) that the web configurator edits live.
//
//	pull  reads the current draft source (config.LoadDraftDocumentSource,
//	      falling back to the saved document / default) and writes it to
//	      [file] for editing.
//	push  reads [file] and writes it to the shared draft, where the web UI and
//	      monitor sessions pick it up. Parse/validation diagnostics are printed
//	      but do not block the write: the draft is a text-sharing channel that
//	      tolerates in-progress, invalid input (markup.md "draft共有").
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

// runDraftPull reads the shared draft source and writes it to file for editing.
func runDraftPull(file string, stdout, stderr io.Writer) int {
	src, _, err := config.LoadDraftDocumentSource(draftTool)
	if err != nil {
		return fail(stderr, err)
	}
	if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Wrote draft to %s\n", file)
	return 0
}

// runDraftPush reads file and writes it to the shared draft. Diagnostics are
// reported to stderr but never block the write (draft tolerates in-progress
// input; monitor --draft falls back to the saved document when the draft is
// invalid).
func runDraftPush(file string, stdout, stderr io.Writer) int {
	data, err := os.ReadFile(file)
	if err != nil {
		return fail(stderr, err)
	}
	src := string(data)

	doc, diags := dsl.Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, dsl.Validate(doc)...)
	}
	for _, d := range diags {
		fmt.Fprintf(stderr, "statusloom: %s: %s\n", d.Severity, d.Message)
	}

	if err := config.SaveDraftDocumentSource(draftTool, src); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Pushed draft from %s\n", file)
	return 0
}
