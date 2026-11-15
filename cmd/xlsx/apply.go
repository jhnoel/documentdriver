package main

import (
	"io"
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/soffice"
	"github.com/jhnoel/documentdriver/internal/xlsx/apply"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// applyHelp is `xlsx help apply`'s body (chunk G19a writes it: the plan
// envelope, an example, --dry-run, -o, --changed, --recalc, hazards).
const applyHelp = `Applies a plan (a JSON file, or - for stdin) to the workbook and prints the edit result: base and new_base (the file's hash before and after), dry_run, saved, aliases, changed, deleted, hazards and parts.

plan: {"ops": [{"op": "cell.set", "at": "Budget!B1", "value": "=A1*2"}]}
The plan envelope has ops, and optionally base (the hash read shows) and contract. A plan that fails changes no file.

Without -o the workbook is replaced in place, atomically; --dry-run does everything except write (with --recalc LibreOffice still runs). --changed ids prints each changed unit's id only; full (the default) prints each unit as read shows it. --recalc computes formula values with LibreOffice (soffice from DD_SOFFICE or PATH) and stores them; without it --recalc is operation_failed at --recalc and nothing is written, and fullCalcOnLoad and the stale-values hazard still apply.

Hazards are stale-values (the save asks Excel to recalculate), ref-error (a delete turned a reference into #REF!), and the findings of the integrity check on the parts the plan touched. Parts are package part names, as docx and pptx list them (/xl/worksheets/sheet1.xml).

` + "`xlsx help ops`" + ` lists the operations; ` + "`xlsx help values`" + ` lists the cell value forms.`

// applyResult is the edit result as the contract shapes it (draft §4).
type applyResult struct {
	Base    string            `json:"base"`
	NewBase string            `json:"new_base,omitempty"`
	DryRun  bool              `json:"dry_run"`
	Saved   string            `json:"saved,omitempty"`
	Aliases map[string]string `json:"aliases"`
	Changed []any             `json:"changed"`
	Deleted []string          `json:"deleted"`
	Hazards []plan.Hazard     `json:"hazards"`
	Parts   []string          `json:"parts"`
}

// runApply is the apply handler (chunk G19a): cli.ReadPlan, ops.ParsePlan
// (its cli.Errors as they are), apply.Run with --dry-run, -o and
// --recalc (apply.RecalcOptions{} with the binary from DD_SOFFICE, read
// here and passed in), then the result as docs/xlsx-contract-draft.md §4
// shapes it: base, new_base, dry_run, saved, aliases, changed (each unit's
// JSON, or with --changed ids its id string only), deleted, hazards
// (plan.Hazard), parts (the package part names, with their leading
// slash, as docx and pptx print them). Output through
// cli.Marshal and cli.WriteJSON with --pretty.
func runApply(p *cli.Parsed, stdout io.Writer) error {
	text, err := cli.ReadPlan(p.Args[1], os.Stdin)
	if err != nil {
		return err
	}
	decoded, errs := ops.ParsePlan(text)
	if len(errs) > 0 {
		return errs
	}
	opt := apply.Options{DryRun: p.Has("dry-run"), Out: p.Get("out")}
	if p.Has("recalc") {
		bin := os.Getenv("DD_SOFFICE")
		found := ""
		if bin != "" {
			found = soffice.Find("", bin)
		} else {
			found = soffice.Find("", "soffice", soffice.DefaultPaths...)
		}
		if found == "" {
			return cli.Errorf(cli.CodeOperationFailed, "--recalc", "LibreOffice (soffice) is not available").
				WithHint("install LibreOffice or set DD_SOFFICE")
		}
		opt.Recalc = &apply.RecalcOptions{Soffice: bin}
	}
	res, err := apply.Run(p.Args[0], decoded, opt)
	if err != nil {
		return err
	}
	var changed []any
	if p.Get("changed") == "ids" {
		for _, id := range res.ChangedIDs {
			changed = append(changed, id)
		}
	} else {
		for _, raw := range res.Changed {
			changed = append(changed, raw)
		}
	}
	changed = cli.NonNil(changed)
	aliases := cli.NonNilMap(res.Aliases)
	deleted := cli.NonNil(res.Deleted)
	hazards := cli.NonNil(res.Hazards)
	parts := cli.NonNil(res.Parts)
	out := applyResult{
		Base:    res.Base,
		NewBase: res.NewBase,
		DryRun:  res.DryRun,
		Saved:   res.Saved,
		Aliases: aliases,
		Changed: changed,
		Deleted: deleted,
		Hazards: hazards,
		Parts:   parts,
	}
	b, err := cli.Marshal(out)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
