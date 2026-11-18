package main

import (
	"io"
	"os"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

var applyCommand = &cli.Command{
	Name:    "apply",
	Args:    []string{"drawing.vsdx", "plan.json|-"},
	JSON:    true,
	Summary: "apply a plan of operations, all or nothing",
	Flags: []cli.Flag{
		{Name: "dry-run", Help: "do everything except write"},
		cli.OutFlag(),
		{Name: "changed", Value: "ids|full", Enum: []string{"ids", "full"}, Help: "show changed units as addresses only, or as read shows them (default)"},
		{Name: "seed", Value: "n", Hidden: true},
		{Name: "clock", Value: "rfc3339", Hidden: true},
	},
	Help: `Applies a plan (a JSON file, or - for stdin) to the drawing and prints
the edit result: base and new_base (the drawing's hash before and after),
the addresses given to aliases, every changed object as vsdx read shows
it, deleted addresses, the hazards on the drawing after the plan (new
marks those the plan introduced) and the parts touched. Without -o the
drawing is replaced in place, atomically; --dry-run prints the result
and writes nothing. A plan that fails changes no file.

plan: {"ops":[{"op":"object.move","at":"page0/s5","dx":10,"dy":0}]}
optional plan fields: base (the hash vsdx read shows: the plan is refused
if the drawing has changed), units (pt, the default, or in).

` + "`vsdx help ops`" + ` lists the operations; ` + "`vsdx help <op>`" + ` gives one.
See the contract: docs/contract.md, sections 3 and 4.`,
}

// defaultClock is the fixed clock of deterministic mode when --seed is
// given without --clock, as pptx apply's.
var defaultClock = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// runApply is the apply handler (chunk A2): the plan through
// ops.ParsePlan, apply.Run, the result as pptx apply prints it.
func runApply(p *cli.Parsed, stdout io.Writer) error {
	opt := apply.Options{DryRun: p.Has("dry-run"), Out: p.Get("out"), ChangedIDs: p.Get("changed") == "ids"}
	// Hidden: deterministic mode, as pptx apply's.
	src, err := cli.SeedClock(p)
	if err != nil {
		return err
	}
	opt.IDs = src
	text, err := cli.ReadPlan(p.Args[1], os.Stdin)
	if err != nil {
		return err
	}
	plan, errs := ops.ParsePlan(text)
	if len(errs) > 0 {
		return errs
	}
	res, err := apply.Run(p.Args[0], plan, opt)
	if err != nil {
		return err
	}
	b, err := apply.ResultJSON(res, plan.Units, false)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
