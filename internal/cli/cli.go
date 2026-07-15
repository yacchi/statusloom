// Package cli implements the statusloom command-line interface: argument
// parsing and subcommand dispatch. The render pipeline itself lives in
// render.go; this file only wires subcommands to it.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/yacchi/statusloom/internal/webconfig"
)

// Run executes the CLI. getenv is injected for testability (pass os.Getenv
// from main; tests pass a map-backed stub so COLUMNS can be controlled
// without mutating the real process environment).
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string, getenv func(string) string) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "claude":
		// Explicit tool: skip stdin sniffing entirely.
		return runRenderPipeline(stdin, stdout, stderr, getenv, "claude-code")

	case "claude-subagent":
		// Claude Code's subagentStatusLine: a distinct stdin shape (a
		// tasks[] array, no session-level fields) rendered per task.
		return runSubagentRender(args[1:], stdin, stdout, stderr)

	case "monitor":
		// Like `claude`, but also forwards the raw payload to a running
		// config server for live preview. The only network-touching path.
		return runMonitor(args[1:], stdin, stdout, stderr, getenv)

	case "refresh":
		return runRefresh(args[1:], stdout, stderr)

	case "render":
		fs := flag.NewFlagSet("render", flag.ContinueOnError)
		fs.SetOutput(stderr)
		tool := fs.String("tool", "", "explicit tool id (e.g. claude-code); detected from stdin when omitted")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		return runRenderPipeline(stdin, stdout, stderr, getenv, *tool)

	case "config":
		fs := flag.NewFlagSet("config", flag.ContinueOnError)
		fs.SetOutput(stderr)
		port := fs.Int("port", 0, "port to bind on 127.0.0.1 (default: random free port)")
		noBrowser := fs.Bool("no-browser", false, "do not open the configurator in a browser")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		return runConfig(stdout, stderr, *port, !*noBrowser)

	case "draft":
		return runDraft(args[1:], stdout, stderr)

	case "fmt":
		return runFmt(args[1:], stdin, stdout, stderr)

	case "setup":
		return runSetup(args[1:], stdin, stdout, stderr)

	case "doctor":
		return runDoctor(args[1:], stdout, stderr, version)

	case "version":
		fmt.Fprintf(stdout, "statusloom %s\n", version)
		return 0

	default:
		printUsage(stderr)
		return 2
	}
}

// runConfig starts the local configurator web server and blocks until it
// shuts down, either via POST /api/shutdown, its idle timeout, or the
// process receiving SIGINT/SIGTERM.
func runConfig(stdout, stderr io.Writer, port int, openBrowser bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := webconfig.Serve(ctx, webconfig.Options{
		Port:        port,
		OpenBrowser: openBrowser,
		Stdout:      stdout,
	}); err != nil {
		fmt.Fprintf(stderr, "statusloom: %s\n", err)
		return 1
	}
	return 0
}

// printUsage writes the top-level usage summary to w.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `statusloom - status line toolkit for coding agents

Usage:
  statusloom claude               render the status line for Claude Code
  statusloom claude-subagent [--draft] [--preview]   render Claude Code's subagentStatusLine (one JSON line per task); --preview uses a built-in payload instead of stdin
  statusloom monitor --emit-url URL --token TOK   render like claude, forwarding the payload to a config server for live preview
  statusloom refresh --once --session-id ID --transcript PATH   refresh transcript-derived cache data
  statusloom render [--tool ID]   render the status line, detecting the tool from stdin if --tool is omitted
  statusloom draft pull|push [file]   pull the shared web-configurator draft to a file, or push an edited file back
  statusloom fmt [file] [--check]   rewrite a DSL document (or <tool>.xml) in canonical form; --check reports without writing; - uses stdin/stdout
  statusloom config [--port N] [--no-browser]   launch the local web configurator
  statusloom setup claude-code [--settings PATH] [--yes] [--dry-run] [--refresh-interval N]   configure Claude Code
  statusloom doctor [--settings PATH]   diagnose the local environment
  statusloom version              print the statusloom version
`)
}
