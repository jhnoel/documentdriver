package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/xlsx/apply"
	"github.com/jhnoel/documentdriver/internal/xlsx/create"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// createHelp is `xlsx help create`'s body (chunk G19b).
const createHelp = "Writes a new workbook with one empty sheet, Sheet1, and applies the plan to it when one is given."

// runCreate is the create handler (chunk G19b): -o is required (usage,
// path -o, without it); the optional plan through ops.ParsePlan; a plan
// with base is invalid_plan (path base); create.Run; the result
// {"dry_run","saved","new_base"} plus the plan's aliases, changed,
// deleted and hazards when there is a plan.
func runCreate(p *cli.Parsed, stdout io.Writer) error {
	out := p.Get("out")
	if out == "" {
		return cli.Errorf(cli.CodeUsage, "-o", "create needs -o, the workbook to write")
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
	var recalc *apply.RecalcOptions
	if p.Has("recalc") {
		recalc = &apply.RecalcOptions{Soffice: os.Getenv("DD_SOFFICE")}
	}
	res, err := create.Run(planArg, create.Options{Out: out, DryRun: p.Has("dry-run"), Recalc: recalc})
	if err != nil {
		return err
	}
	if !hasPlan {
		b, err := cli.Marshal(struct {
			DryRun  bool   `json:"dry_run"`
			Saved   string `json:"saved,omitempty"`
			NewBase string `json:"new_base,omitempty"`
		}{DryRun: res.DryRun, Saved: res.Saved, NewBase: res.NewBase})
		if err != nil {
			return err
		}
		return cli.WriteJSON(stdout, b, p.Has("pretty"))
	}
	aliases := cli.NonNilMap(res.Aliases)
	changed := cli.NonNil(res.Changed)
	deleted := cli.NonNil(res.Deleted)
	hazards := cli.NonNil(res.Hazards)
	b, err := cli.Marshal(struct {
		DryRun  bool              `json:"dry_run"`
		Saved   string            `json:"saved,omitempty"`
		NewBase string            `json:"new_base,omitempty"`
		Aliases map[string]string `json:"aliases"`
		Changed []json.RawMessage `json:"changed"`
		Deleted []string          `json:"deleted"`
		Hazards []plan.Hazard     `json:"hazards"`
	}{DryRun: res.DryRun, Saved: res.Saved, NewBase: res.NewBase, Aliases: aliases, Changed: changed, Deleted: deleted, Hazards: hazards})
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
