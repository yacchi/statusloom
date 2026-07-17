package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/store"
)

// fmtTool is the tool whose current committed document `statusloom fmt`
// formats when no file argument is given. Like the other single-tool
// subcommands, it is claude-code.
const fmtTool = "claude-code"

// runFmt implements `statusloom fmt [file] [--check] [--write]`: it reads a
// DSL document, parses+validates it, and rewrites it in the canonical form
// (whole-document canonicalization plus word-form normalization of every
// `when` expression). Error-severity diagnostics abort the command (reported
// to stderr, non-zero exit, nothing written).
//
//	(no file)  formats the store's current claude-code document. By default
//	           the result is printed to stdout without persisting it;
//	           --write instead saves it as a new revision (origin "cli"; a
//	           no-op if the formatted result is identical to the current tip).
//	file       formats the given path in place (atomic write).
//	-          reads stdin, writes the formatted document to stdout.
//	--check    reports whether formatting would change the document (exit
//	           non-zero on a difference) without writing anything.
func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	check := false
	write := false
	file := ""
	for _, a := range args {
		switch a {
		case "--check", "-check":
			check = true
		case "--write", "-write":
			write = true
		default:
			if len(a) > 0 && a[0] == '-' && a != "-" {
				fmt.Fprintf(stderr, "statusloom fmt: unknown flag %q\n", a)
				return 2
			}
			if file != "" {
				fmt.Fprintln(stderr, "statusloom fmt: too many arguments")
				return 2
			}
			file = a
		}
	}

	useStdin := file == "-"
	usingStore := file == "" && !useStdin

	var src string
	switch {
	case useStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fail(stderr, err)
		}
		src = string(data)
	case usingStore:
		s, err := storeCurrentSourceOrDefault(fmtTool)
		if err != nil {
			return fail(stderr, err)
		}
		src = s
	default:
		data, err := os.ReadFile(file)
		if err != nil {
			return fail(stderr, err)
		}
		src = string(data)
	}

	doc, diags := dsl.ParseAndValidate(src)
	if dsl.HasErrors(diags) || doc == nil || doc.Root == nil {
		for _, d := range diags {
			if d.Severity == dsl.SeverityError {
				fmt.Fprintf(stderr, "statusloom fmt: error: %s\n", d.Message)
			}
		}
		fmt.Fprintln(stderr, "statusloom fmt: refusing to format a document with errors")
		return 1
	}

	formatted := dsl.Canonicalize(doc)
	changed := formatted != src

	if check {
		if changed {
			target := file
			if target == "" {
				target = fmtTool
			}
			fmt.Fprintf(stderr, "statusloom fmt: %s is not formatted\n", target)
			return 1
		}
		return 0
	}

	if useStdin {
		if _, err := io.WriteString(stdout, formatted); err != nil {
			return fail(stderr, err)
		}
		return 0
	}

	if usingStore {
		if !write {
			if _, err := io.WriteString(stdout, formatted); err != nil {
				return fail(stderr, err)
			}
			return 0
		}
		if !changed {
			fmt.Fprintf(stdout, "%s already formatted\n", fmtTool)
			return 0
		}
		st, err := store.Open()
		if err != nil {
			return fail(stderr, err)
		}
		id, saveDiags, err := st.Save(fmtTool, formatted, "cli", store.Meta{})
		if err != nil {
			return fail(stderr, err)
		}
		if dsl.HasErrors(saveDiags) {
			// Unreachable in practice: formatted already passed
			// ParseAndValidate above, so Save's identical validation boundary
			// cannot newly reject it.
			fmt.Fprintln(stderr, "statusloom fmt: formatted document failed validation on save")
			return 1
		}
		fmt.Fprintf(stdout, "Formatted %s (revision %s)\n", fmtTool, id)
		return 0
	}

	if !changed {
		fmt.Fprintf(stdout, "%s already formatted\n", file)
		return 0
	}
	if err := store.WriteFileAtomic(file, []byte(formatted)); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Formatted %s\n", file)
	return 0
}

// storeCurrentSourceOrDefault returns tool's current committed source from
// the internal store, or the built-in default when the store/tool ref is
// absent.
func storeCurrentSourceOrDefault(tool string) (string, error) {
	st, err := store.Open()
	if err != nil {
		return "", err
	}
	rev, _, ok, err := st.Current(tool)
	if err != nil {
		return "", err
	}
	if !ok {
		return config.DefaultDocument(tool), nil
	}
	return rev.Source, nil
}
