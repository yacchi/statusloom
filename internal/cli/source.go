package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/sources"
)

// probeSource runs the refresh subprocess for command/http sources during
// `source test`. Tests replace it so they never spawn a real probe.
var probeSource sources.ProbeFunc = sources.RunProbe

// runSource implements `statusloom source list|add|rm|test`, the CLI for the
// external JSON sources defined in <configDir>/sources.json.
func runSource(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "statusloom: source: expected a subcommand (list|add|rm|test)")
		return 2
	}
	switch args[0] {
	case "list":
		return runSourceList(args[1:], stdout, stderr)
	case "add":
		return runSourceAdd(args[1:], stdout, stderr)
	case "rm":
		return runSourceRemove(args[1:], stdout, stderr)
	case "test":
		return runSourceTest(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "statusloom: source: unknown subcommand %q (want list, add, rm or test)\n", args[0])
		return 2
	}
}

func runSourceList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("source list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the sources.json document")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	f, err := sources.Load()
	if err != nil {
		return sourceFail(stderr, "list", err)
	}
	if *asJSON {
		f.SchemaVersion = sources.SchemaVersion
		if err := writeSourceJSON(stdout, f); err != nil {
			return sourceFail(stderr, "list", err)
		}
		return 0
	}
	ids := f.IDs()
	if len(ids) == 0 {
		p, err := sources.Path()
		if err != nil {
			return sourceFail(stderr, "list", err)
		}
		fmt.Fprintf(stdout, "no sources defined (%s)\n", p)
		return 0
	}
	for _, id := range ids {
		d := f.Sources[id]
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s", id, d.Kind, sourceTarget(d), sourceTTLColumn(d))
		for _, w := range d.Warnings() {
			fmt.Fprintf(stdout, "\tWARN:%s", w)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func runSourceAdd(args []string, stdout, stderr io.Writer) int {
	id, rest, ok := splitSourceID("add", args, stderr)
	if !ok {
		return 2
	}
	// Everything after the first literal "--" is ARGV for --command and is never
	// parsed as flags, so a flag-like argv element such as --json passes through.
	flagArgs, argv := rest, []string(nil)
	for i, a := range rest {
		if a == "--" {
			flagArgs, argv = rest[:i], rest[i+1:]
			break
		}
	}

	fs := flag.NewFlagSet("source add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "file source: path to a JSON file (absolute or ~/)")
	url := fs.String("url", "", "http source: URL returning JSON")
	command := fs.Bool("command", false, "command source: run ARGV after -- (no shell) and read JSON from stdout")
	allowRemote := fs.Bool("allow-remote", false, "http source: allow non-loopback hosts")
	ttl := fs.Int("ttl", 0, "command/http: fetch interval in seconds (default: max(30, refreshInterval))")
	staleAfter := fs.Int("stale-after", 0, "command/http: seconds before the value is treated as missing (default: 3x ttl)")
	timeout := fs.Int("timeout", 0, "command/http: fetch timeout in seconds (default 2)")
	force := fs.Bool("force", false, "replace an existing source with the same id")
	if err := fs.Parse(flagArgs); err != nil || fs.NArg() != 0 {
		return 2
	}

	kinds := 0
	for _, set := range []bool{*file != "", *url != "", *command} {
		if set {
			kinds++
		}
	}
	switch {
	case kinds != 1:
		return sourceUsage(stderr, "add", "exactly one of --file, --url or --command is required")
	case *command && len(argv) == 0:
		return sourceUsage(stderr, "add", "--command needs ARGV after --")
	case (*file != "" || *url != "") && len(argv) > 0:
		return sourceUsage(stderr, "add", "ARGV after -- applies only to --command")
	}

	f, err := sources.Load()
	if err != nil {
		return sourceFail(stderr, "add", err)
	}
	_, exists := f.Get(id)
	if exists && !*force {
		return sourceFail(stderr, "add", fmt.Errorf("source %q already exists (use --force to replace)", id))
	}

	d := sources.Definition{
		ID:                id,
		Path:              *file,
		URL:               *url,
		Argv:              argv,
		AllowRemote:       *allowRemote,
		TTLSeconds:        *ttl,
		StaleAfterSeconds: *staleAfter,
		TimeoutSeconds:    *timeout,
	}
	switch {
	case *file != "":
		d.Kind = sources.KindFile
	case *url != "":
		d.Kind = sources.KindHTTP
	default:
		d.Kind = sources.KindCommand
	}
	if err := f.Put(d); err != nil {
		return sourceFail(stderr, "add", err)
	}
	if err := sources.Save(f); err != nil {
		return sourceFail(stderr, "add", err)
	}

	verb := "added"
	if exists {
		verb = "replaced"
	}
	fmt.Fprintf(stdout, "%s source %s (%s)\n", verb, id, d.Kind)
	for _, w := range d.Warnings() {
		fmt.Fprintln(stdout, sourceWarningLine(id, w))
	}
	return 0
}

func runSourceRemove(args []string, stdout, stderr io.Writer) int {
	id, rest, ok := splitSourceID("rm", args, stderr)
	if !ok {
		return 2
	}
	if len(rest) != 0 {
		return sourceUsage(stderr, "rm", fmt.Sprintf("unexpected argument %q", rest[0]))
	}
	f, err := sources.Load()
	if err != nil {
		return sourceFail(stderr, "rm", err)
	}
	if !f.Remove(id) {
		return sourceFail(stderr, "rm", fmt.Errorf("unknown source %q", id))
	}
	if err := sources.Save(f); err != nil {
		return sourceFail(stderr, "rm", err)
	}
	fmt.Fprintf(stdout, "removed source %s\n", id)
	return 0
}

func runSourceTest(args []string, stdout, stderr io.Writer) int {
	id, rest, ok := splitSourceID("test", args, stderr)
	if !ok {
		return 2
	}
	fs := flag.NewFlagSet("source test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	selectExpr := fs.String("select", "", "select expression applied to the fetched JSON (default: whole document)")
	payload := fs.String("payload", "", "Claude Code statusline payload file whose values fill ${...} variables")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return 2
	}

	f, err := sources.Load()
	if err != nil {
		return sourceFail(stderr, "test", err)
	}
	def, ok := f.Get(id)
	if !ok {
		return sourceFail(stderr, "test", fmt.Errorf("unknown source %q", id))
	}

	var snap schema.StatusSnapshot
	if *payload != "" {
		raw, err := os.ReadFile(*payload)
		if err != nil {
			return sourceFail(stderr, "test", err)
		}
		snap, err = claude.New().Decode(raw)
		if err != nil {
			return sourceFail(stderr, "test", err)
		}
	}

	// The deadline covers the source's own timeout plus slack for the probe
	// subprocess to start and report.
	ctx, cancel := context.WithTimeout(context.Background(), def.Timeout()+5*time.Second)
	defer cancel()
	doc, err := sources.FetchOnce(ctx, def, probeSource)
	if err != nil {
		return sourceFail(stderr, "test", err)
	}

	res := render.EvalSelect(doc, *selectExpr, snap, os.Getenv)
	if res.Err != nil {
		return sourceFail(stderr, "test", res.Err)
	}
	if !res.Matched {
		return sourceFail(stderr, "test", errors.New("no match for select"))
	}
	if err := writeSourceJSON(stdout, res.Value); err != nil {
		return sourceFail(stderr, "test", err)
	}
	return 0
}

// splitSourceID takes the <id> that must come first for the subcommands that
// take one. Flags after the id are parsed by the caller, so an id is never
// mistaken for a flag value.
func splitSourceID(sub string, args []string, stderr io.Writer) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		sourceUsage(stderr, sub, "expected <id>")
		return "", nil, false
	}
	return args[0], args[1:], true
}

func sourceTarget(d sources.Definition) string {
	switch d.Kind {
	case sources.KindFile:
		return d.Path
	case sources.KindCommand:
		return strings.Join(d.Argv, " ")
	default:
		return d.URL
	}
}

func sourceTTLColumn(d sources.Definition) string {
	if d.Kind == sources.KindFile {
		return "-"
	}
	if d.TTLSeconds == 0 {
		return "ttl=default"
	}
	return fmt.Sprintf("ttl=%ds", d.TTLSeconds)
}

// sourceWarningLine is the stdout text for a Definition.Warnings code.
func sourceWarningLine(id, code string) string {
	switch code {
	case "allow-remote":
		return fmt.Sprintf("warning: %s: allowRemote lets this source reach non-loopback hosts", id)
	case "short-ttl":
		return fmt.Sprintf("warning: %s: ttl below %ds spawns a fetch very often", id, int(sources.ShortTTLWarning.Seconds()))
	default:
		return fmt.Sprintf("warning: %s: %s", id, code)
	}
}

// sourceUsage reports a usage error (exit 2) for the given subcommand.
func sourceUsage(stderr io.Writer, sub, msg string) int {
	fmt.Fprintf(stderr, "statusloom: source %s: %s\n", sub, msg)
	return 2
}

// sourceFail reports a runtime error (exit 1) for the given subcommand.
func sourceFail(stderr io.Writer, sub string, err error) int {
	fmt.Fprintf(stderr, "statusloom: source %s: %s\n", sub, err)
	return 1
}

func writeSourceJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
