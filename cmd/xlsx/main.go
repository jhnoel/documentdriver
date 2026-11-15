// Command xlsx reads and edits Excel workbooks. It is in phase P1
// (docs/design.md, xlsx): read and find are being built (track A1);
// apply and create refuse until P2. Its command line, results and errors follow the
// agent contract (docs/contract.md, with the xlsx amendments drafted in
// docs/xlsx-contract-draft.md), through internal/cli.
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
// docx and pptx: memory outranks a few percent of time for tools that run
// beside many parallel agents. xlsx has no measurements of its own yet;
// they come with the first command (docs/design.md, xlsx budgets).
const gcPercent = 50

// main sets the GC percent unless GOGC is set and exits with run's exit code.
func main() {
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(gcPercent)
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// The commands of the draft contract. read and find are defined in
// read.go and find.go; apply in apply.go, create in create.go, the
// operation reference in help.go (chunks G19a, G19b, G19c).
var (
	applyCommand = &cli.Command{
		Name:    "apply",
		Args:    []string{"file.xlsx", "plan.json|-"},
		JSON:    true,
		Summary: "apply a plan of operations, all or nothing",
		Flags: []cli.Flag{
			{Name: "dry-run", Help: "do everything except write"},
			cli.OutFlag(),
			{Name: "changed", Value: "ids|full", Enum: []string{"ids", "full"}, Help: "show changed units as addresses only, or as read shows them (default)"},
			{Name: "recalc", Help: "compute formula values with LibreOffice (soffice from DD_SOFFICE or PATH) and store them"},
		},
		Help: applyHelp,
	}
	createCommand = &cli.Command{
		Name:     "create",
		Optional: []string{"plan.json|-"},
		JSON:     true,
		Summary:  "write a new workbook, optionally with a plan applied",
		Flags: []cli.Flag{
			{Name: "dry-run", Help: "check and write nothing"},
			cli.OutFlag(),
			{Name: "recalc", Help: "compute formula values with LibreOffice, as apply --recalc"},
		},
		Help: createHelp,
	}
)

// app returns the xlsx CLI application with its commands and handlers.
func app() *cli.App {
	return &cli.App{
		Tool:     "xlsx",
		Version:  version,
		Summary:  "xlsx reads and edits Excel workbooks: cells, formulas, sheets, names and formats.",
		Commands: []*cli.Command{readCommand, findCommand, applyCommand, createCommand},
		Topics:   map[string]func() string{"values": valuesHelp, "ops": opsHelp},
		Topic:    opHelp,
		Handlers: map[string]cli.Handler{
			"read": runRead, "find": runFind, "apply": runApply, "create": runCreate,
		},
	}
}

// run is the CLI: exit 0 when the command completed, 1 when it was
// refused or failed, 2 when the command line is wrong.
func run(args []string, stdout, stderr io.Writer) int {
	return app().Run(args, stdout, stderr)
}
