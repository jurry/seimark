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

Exit codes: 0 success, 1 the file could not be read or parsed, 2 usage error.`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches to a subcommand and returns the exit code. It exists so tests
// can drive the command without a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)

		return 2
	}

	switch args[0] {
	case "dump":
		return runDump(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)

		return 0
	}

	fmt.Fprintf(stderr, "seimark: unknown command %q\n%s\n", args[0], usage)

	return 2
}
