package cli

// runImportExchange / runExportExchange implement `statusloom import <file>`
// and `statusloom export [--tool <tool>] [-o <file>]`
// (plans/config-store-and-format.md §8.13): thin CLI wiring over
// internal/exchange's self-contained Markdown-frontmatter codec plus the
// store's validation boundary (store.Save). Frontmatter/XML-fence parsing
// itself is entirely internal/exchange's responsibility (§6.7); this file
// only decides which tool a decoded document targets and reports the
// resulting revision.

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/exchange"
	"github.com/yacchi/statusloom/internal/store"
)

// exportDefaultTool is the tool `statusloom export` acts on when --tool is
// omitted, matching every other single-tool subcommand (draft, fmt, history).
const exportDefaultTool = "claude-code"

// scanToolFlag extracts an optional "--tool VALUE" / "--tool=VALUE" pair from
// args (in any position), reporting whether it was present so callers can
// distinguish "not given" from "given, equal to the default" (import needs
// this to know whether to enforce a match; export does not and always
// defaults). The remaining positional arguments are returned in original
// order.
func scanToolFlag(args []string) (tool string, found bool, rest []string, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--tool":
			if i+1 >= len(args) {
				return "", false, nil, fmt.Errorf("--tool requires a value")
			}
			tool = args[i+1]
			found = true
			i++
		case strings.HasPrefix(a, "--tool="):
			tool = strings.TrimPrefix(a, "--tool=")
			found = true
		default:
			rest = append(rest, a)
		}
	}
	return tool, found, rest, nil
}

// runImportExchange implements `statusloom import [--tool <tool>] <file>`: it
// reads file (or stdin, for "-"), decodes it as a *.sloom.md Markdown
// exchange document (internal/exchange.Decode), and saves the extracted DSL
// through the store's validation boundary (store.Save, origin "import"). The
// tool is derived from the decoded document's own `<statusloom tool="...">`
// attribute; --tool, when given, only verifies that attribute rather than
// overriding it — a mismatch is a usage error and nothing is read from the
// file's tool. Error-severity diagnostics (from either exchange.Decode's
// frontmatter/fence parsing or the store's Save validation) reject the
// import: nothing is written, the store is left exactly as it was. A save
// identical to the tool's current tip is reported as a no-op (store.Save's
// dedup, §3.3) rather than a new import.
func runImportExchange(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	tool, hasTool, rest, err := scanToolFlag(args)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom import: %s\n", err)
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "statusloom import: expected <file> (use - for stdin)")
		return 2
	}
	file := rest[0]

	var data []byte
	if file == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return fail(stderr, err)
	}

	meta, xmlSource, err := exchange.Decode(data)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom import: %s\n", err)
		return 1
	}

	doc, diags := dsl.ParseAndValidate(xmlSource)
	if doc == nil || doc.Root == nil || doc.Root.Tool == "" {
		for _, d := range diags {
			if d.Severity == dsl.SeverityError {
				fmt.Fprintf(stderr, "statusloom import: error: %s\n", d.Message)
			}
		}
		fmt.Fprintln(stderr, "statusloom import: document has no tool attribute")
		return 1
	}
	docTool := doc.Root.Tool
	if hasTool && tool != docTool {
		fmt.Fprintf(stderr, "statusloom import: --tool %q does not match the document's tool %q\n", tool, docTool)
		return 2
	}

	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	_, prevCurID, hadCurrent, err := st.Current(docTool)
	if err != nil {
		return fail(stderr, err)
	}

	id, saveDiags, err := st.Save(docTool, xmlSource, "import", store.Meta{
		Name:        meta.Name,
		Description: meta.Description,
		Author:      meta.Author,
		Notes:       meta.Notes,
	})
	if err != nil {
		return fail(stderr, err)
	}
	if dsl.HasErrors(saveDiags) {
		for _, d := range saveDiags {
			if d.Severity == dsl.SeverityError {
				fmt.Fprintf(stderr, "statusloom import: error: %s\n", d.Message)
			}
		}
		fmt.Fprintln(stderr, "statusloom import: document rejected, store unchanged")
		return 1
	}

	if hadCurrent && id == prevCurID {
		fmt.Fprintf(stdout, "No changes: %s already matches revision %s\n", docTool, id)
		return 0
	}
	fmt.Fprintf(stdout, "Imported %s as revision %s\n", docTool, id)
	return 0
}

// runExportExchange implements `statusloom export [--tool <tool>] [-o
// <file>]`: it encodes tool's current committed revision (source + meta) as
// a *.sloom.md Markdown exchange document (internal/exchange.Encode) and
// writes it to stdout, or atomically to file when -o is given (existing
// files are overwritten). A tool with no current revision in the store is an
// error — there is deliberately no built-in-default fallback here, since
// exporting the default would misrepresent it as a saved configuration.
func runExportExchange(args []string, stdout, stderr io.Writer) int {
	tool := exportDefaultTool
	outFile := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--tool":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "statusloom export: --tool requires a value")
				return 2
			}
			tool = args[i+1]
			i++
		case strings.HasPrefix(a, "--tool="):
			tool = strings.TrimPrefix(a, "--tool=")
		case a == "-o":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "statusloom export: -o requires a value")
				return 2
			}
			outFile = args[i+1]
			i++
		case strings.HasPrefix(a, "-o="):
			outFile = strings.TrimPrefix(a, "-o=")
		default:
			fmt.Fprintf(stderr, "statusloom export: unexpected argument %q\n", a)
			return 2
		}
	}

	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		return fail(stderr, err)
	}
	if !ok {
		fmt.Fprintf(stderr, "statusloom export: no saved configuration for %s\n", tool)
		return 1
	}

	md := exchange.Encode(exchange.Meta{
		Name:        rev.Meta.Name,
		Description: rev.Meta.Description,
		Author:      rev.Meta.Author,
		Notes:       rev.Meta.Notes,
	}, rev.Source)

	if outFile == "" {
		if _, err := stdout.Write(md); err != nil {
			return fail(stderr, err)
		}
		return 0
	}
	if err := store.WriteFileAtomic(outFile, md); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Exported %s to %s\n", tool, outFile)
	return 0
}
