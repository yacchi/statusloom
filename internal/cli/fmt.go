package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
)

// fmtTool is the tool whose <tool>.xml `statusloom fmt` formats when no file
// argument is given. Like the other single-tool subcommands, it is claude-code.
const fmtTool = "claude-code"

// runFmt implements `statusloom fmt [file] [--check]`: it reads a DSL
// document, parses+validates it, and rewrites it in the canonical form
// (whole-document canonicalization plus word-form normalization of every
// `when` expression). Error-severity diagnostics abort the command (reported
// to stderr, non-zero exit, nothing written).
//
//	(no file)  formats <configDir>/<tool>.xml in place (atomic write)
//	file       formats the given path in place (atomic write)
//	-          reads stdin, writes the formatted document to stdout
//	--check    reports whether formatting would change the document (exit
//	           non-zero on a difference) without writing anything
func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	check := false
	file := ""
	for _, a := range args {
		switch a {
		case "--check", "-check":
			check = true
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

	var src string
	if useStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fail(stderr, err)
		}
		src = string(data)
	} else {
		path := file
		if path == "" {
			path = config.DocumentPath(fmtTool)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fail(stderr, err)
		}
		src = string(data)
	}

	doc, diags := dsl.Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, dsl.Validate(doc)...)
	}
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
				target = config.DocumentPath(fmtTool)
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

	path := file
	if path == "" {
		path = config.DocumentPath(fmtTool)
	}
	if !changed {
		fmt.Fprintf(stdout, "%s already formatted\n", path)
		return 0
	}
	if err := config.WriteFileAtomic(path, []byte(formatted)); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Formatted %s\n", path)
	return 0
}
