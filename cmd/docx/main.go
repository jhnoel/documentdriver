// Command docx reads and edits Word documents: text, tables, sections,
// comments, tracked changes and equations. Its command line, results and
// errors follow the agent contract (docs/contract.md), through
// internal/cli.
package main

import (
	"io"
	"os"
	"runtime/debug"

	"github.com/jhnoel/documentdriver/docs"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/read"
)

// version is the release this binary is built as. A release build sets it
// with -ldflags "-X main.version=..." (tools/release); a plain build is 0.1.0.
var version = "0.1.0"

// options are the read options the CLI runs with: equations as LaTeX.
func options() read.Options {
	return read.Options{Equation: math.OMMLToLaTeX}
}

// gcPercent is the GC target the CLI runs with unless GOGC is set: a
// command's heap is one or two parsed parts that live to its end, so
// collecting at half the default growth keeps peak RSS about 15% lower
// (apply at 20,000 paragraphs: 346 MB -> 289 MB) for about 4% more time.
// docs/design.md (Scaling and memory budget) has the measurements.
const gcPercent = 50

// main runs the docx CLI and exits with its code.
func main() {
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(gcPercent)
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, options()))
}

// app returns the docx CLI application configured with the read options.
func app(opt read.Options) *cli.App {
	return &cli.App{
		Tool:    "docx",
		Version: version,
		Summary: "docx reads and edits Word documents: text, tables, sections, comments,\ntracked changes and equations. It leaves everything it did not target untouched.",
		Commands: []*cli.Command{
			readCommand, findCommand, applyCommand, createCommand,
		},
		Handlers: map[string]cli.Handler{
			"read":   func(p *cli.Parsed, w io.Writer) error { return runRead(p, w, opt) },
			"find":   func(p *cli.Parsed, w io.Writer) error { return runFind(p, w, opt) },
			"apply":  func(p *cli.Parsed, w io.Writer) error { return runApply(p, w, opt) },
			"create": runCreate,
		},
		Topics: map[string]func() string{"ops": opsHelp, "html": htmlHelp, "schema": schemaHelp},
		Topic:  opHelp,
	}
}

// schemaHelp is `docx help schema`: the plan JSON Schema
// (docs/docx-plan.schema.json, generated from the operation table).
func schemaHelp() string { return docs.DocxPlanSchema }

// run is the CLI: exit 0 when the command completed, 1 when it was
// refused or failed, 2 when the command line is wrong.
func run(args []string, stdout, stderr io.Writer, opt read.Options) int {
	return app(opt).Run(args, stdout, stderr)
}
