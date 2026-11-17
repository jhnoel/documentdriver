package main

import (
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/create"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
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
	out := p.Get("out")
	if out == "" {
		return cli.Errorf(cli.CodeUsage, "-o", "create needs -o, the drawing to write")
	}
	src, err := cli.SeedClock(p)
	if err != nil {
		return err
	}
	var planArg *ops.Plan
	hasPlan := len(p.Args) > 0
	if hasPlan {
		text, err := cli.ReadPlan(p.Args[0], os.Stdin)
		if err != nil {
			return err
		}
		parsed, errs := ops.ParsePlan(text)
		if len(errs) > 0 {
			return errs
		}
		planArg = parsed
	}
	res, err := create.Run(planArg, create.Options{Out: out, DryRun: p.Has("dry-run"), IDs: src})
	if err != nil {
		return err
	}
	if !hasPlan {
		b, err := cli.Marshal(struct {
			NewBase string `json:"new_base,omitempty"`
			DryRun  bool   `json:"dry_run"`
			Saved   string `json:"saved,omitempty"`
		}{NewBase: res.NewBase, DryRun: res.DryRun, Saved: res.Saved})
		if err != nil {
			return err
		}
		return cli.WriteJSON(stdout, b, p.Has("pretty"))
	}
	b, err := res.JSON()
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
