// Command vsdx reads (and, from phase P2, edits) Microsoft Visio
// drawings. It is in phase P1 (docs/vsdx-design.md): read, find and xml;
// apply and create come later. Its command line, results and errors
// follow the agent contract (docs/contract.md, with the vsdx amendments
// drafted in docs/vsdx-contract-draft.md), through internal/cli.
package main

import (
	"io"
	"os"
	"runtime/debug"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// version is the release this binary is built as. A release build sets it
// with -ldflags "-X main.version=..." (tools/release); a plain build is 0.0.0.
var version = "0.0.0"

// gcPercent is the GC target the CLI runs with unless GOGC is set, as for
// the other tools; vsdx has no measurements of its own yet (P1 budgets).
const gcPercent = 50

// main runs the CLI with the GC target set and exits with run's exit code.
func main() {
	setGC()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// setGC sets the GC percent to gcPercent unless the environment sets GOGC.
func setGC() {
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(gcPercent)
	}
}

// app returns the vsdx CLI application with its commands and handlers.
func app() *cli.App {
	return &cli.App{
		Tool:     "vsdx",
		Version:  version,
		Summary:  "vsdx reads, edits and creates Visio drawings: pages, shapes, connectors, groups, shape data and their text; its command line, results and errors follow docs/contract.md.",
		Commands: []*cli.Command{readCommand, findCommand, xmlCommand, applyCommand, createCommand, renderCommand},
		Topics:   map[string]func() string{"ops": opsHelp, "html": htmlHelp},
		Topic:    opHelp,
		Handlers: map[string]cli.Handler{
			"read": runRead, "find": runFind, "xml": runXML, "apply": runApply, "create": runCreate, "render": runRender,
		},
	}
}

// run is the CLI: exit 0 when the command completed, 1 when it was
// refused or failed, 2 when the command line is wrong.
func run(args []string, stdout, stderr io.Writer) int {
	return app().Run(args, stdout, stderr)
}
