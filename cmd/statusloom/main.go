// Command statusloom is the CLI entry point. All logic lives in
// internal/cli; main only wires up the process-level plumbing (argv,
// stdio, exit code) so it stays trivially testable via internal/cli.Run.
package main

import (
	"os"

	"github.com/yacchi/statusloom/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version, os.Getenv))
}
