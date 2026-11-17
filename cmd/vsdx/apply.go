package main

import (
	"io"
	"os"
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
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
	Help: "Applies a plan to the drawing and prints the edit result (docs/vsdx-contract-draft.md, section 4).",
}

// defaultClock is the fixed clock of deterministic mode when --seed is
// given without --clock, as pptx apply's.
var defaultClock = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// runApply is the apply handler (chunk A2): the plan through
// ops.ParsePlan, apply.Run, the result as pptx apply prints it.
func runApply(p *cli.Parsed, stdout io.Writer) error {
	opt := apply.Options{DryRun: p.Has("dry-run"), Out: p.Get("out"), ChangedIDs: p.Get("changed") == "ids"}
	// Hidden: deterministic mode, as pptx apply's.
	if p.Has("seed") || p.Has("clock") {
		seed, clock := int64(0), defaultClock
		if p.Has("seed") {
			n, err := strconv.ParseInt(p.Get("seed"), 10, 64)
			if err != nil {
				return cli.Errorf(cli.CodeUsage, "--seed", "--seed is a whole number")
			}
			seed = n
		}
		if p.Has("clock") {
			c, err := time.Parse(time.RFC3339, p.Get("clock"))
			if err != nil {
				return cli.Errorf(cli.CodeUsage, "--clock", "--clock is an RFC 3339 time")
			}
			clock = c
		}
		opt.IDs = ids.Fixed(seed, clock)
	}
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
	b, err := res.JSON()
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
