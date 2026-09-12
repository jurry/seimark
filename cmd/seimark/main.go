// Command seimark inspects and stamps H.264 streams with seimark markers.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: seimark <command> [flags] FILE

commands:
  dump    print the markers found in an Annex B stream or an MP4 file
  inject  write a marker into every access unit of an Annex B stream

Exit codes: 0 success, 1 the file could not be read or parsed, 2 usage error.`

// Exit codes returned by run and its subcommands.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches to a subcommand and returns the exit code. It exists so tests
// can drive the command without a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)

		return exitUsage
	}

	switch args[0] {
	case "dump":
		return runDump(args[1:], stdout, stderr)
	case "inject":
		return runInject(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)

		return exitOK
	}

	fmt.Fprintf(stderr, "seimark: unknown command %q\n%s\n", args[0], usage)

	return exitUsage
}
