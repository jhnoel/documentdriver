package main

import (
	"io"

	"github.com/jhnoel/documentdriver/internal/cli"
)

var createCommand = &cli.Command{
	Name:     "create",
	Optional: []string{"plan.json|-"},
	JSON:     true,
	Summary:  "write a new drawing, optionally with a plan applied",
	Flags: []cli.Flag{
		{Name: "dry-run", Help: "check and write nothing"},
		cli.OutFlag(),
		{Name: "seed", Value: "n", Hidden: true},
		{Name: "clock", Value: "rfc3339", Hidden: true},
	},
	Help: `Writes a new drawing with one empty page, Page-1 (8.5 x 11 in, portrait),
no masters, and applies the plan to it when one is given. Prints
dry_run, saved and new_base, and with a plan its aliases, changed,
deleted and hazards. The plan must not have base.`,
}

// runCreate is the create handler (chunk CR1): -o required (usage, path
// -o), the optional plan through ops.ParsePlan, create.Run, the result
// as xlsx create prints it; --seed and --clock as pptx apply's hidden
// flags (ids.Fixed) for deterministic output.
func runCreate(p *cli.Parsed, stdout io.Writer) error {
	_, _ = p, stdout
	return cli.Errorf(cli.CodeOperationFailed, "", "vsdx create is not implemented yet")
}
