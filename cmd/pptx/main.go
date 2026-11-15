// Command pptx reads and edits the diagrams in PowerPoint decks as native,
// editable DrawingML objects. Its command line, results and errors follow
// the agent contract (docs/contract.md), through internal/cli.
package main

import (
	"io"
	"os"
	"runtime/debug"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// version is the release this binary is built as. A release build sets it
// with -ldflags "-X main.version=..." (tools/release); a plain build is 0.1.0.
var version = "0.1.0"

// gcPercent is the GC target the CLI runs with unless GOGC is set, as for
// docx: a command's heap is the parsed slide and its package, which live
// to its end, so collecting at half the default growth keeps peak RSS
// lower (read of a 5,000-shape slide: 63 MB -> 53 MB) for wall time
// within noise (0.25 s -> 0.26 s). docs/design.md (Scaling and memory
// budget) has the measurements.
const gcPercent = 50

// main runs the pptx CLI and exits with its code.
func main() {
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(gcPercent)
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// app returns the pptx CLI application.
func app() *cli.App {
	return &cli.App{
		Tool:    "pptx",
		Version: version,
		Summary: "pptx reads and edits the objects on PowerPoint slides: shapes, connectors,\ngroups, pictures and their text. It leaves everything it did not target untouched.",
		Commands: []*cli.Command{
			readCommand, applyCommand, xmlCommand, renderCommand,
		},
		Handlers: map[string]cli.Handler{
			"read": runRead, "apply": runApply, "xml": runXML, "render": runRender,
		},
		Topics: map[string]func() string{"ops": opsHelp, "html": htmlHelp},
		Topic:  opHelp,
	}
}

// run executes the CLI arguments, including hidden debug commands, and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "debug" { // hidden: engine commands for exercising the engine by hand
		return runDebug(args[1:], stdout, stderr)
	}
	return app().Run(args, stdout, stderr)
}
