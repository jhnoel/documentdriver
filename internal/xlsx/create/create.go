// Package create is `xlsx create` (docs/xlsx-contract-draft.md, §4;
// decision 14): a blank workbook authored in code (template.go), with an
// optional plan applied to it.
package create

import (
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/xlsx/apply"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// Options control a create.
type Options struct {
	// Out is the file to write (required); DryRun writes nothing.
	Out    string
	DryRun bool
	// Recalc, when set, is apply's --recalc step (create --recalc).
	Recalc *apply.RecalcOptions
}

// Run writes the blank workbook, with plan p applied when p is not nil
// (chunk G16). The template is written to a temporary file and opened as
// a plan transaction (plan.Begin); without a plan, or with a plan that
// changes nothing, the transaction commits the template unchanged to
// Out (plan.Tx.CommitTo, a byte copy); with one, apply.Run on the
// temporary file with Out. A plan with base is refused (invalid_plan,
// path base: the new workbook has no base yet). The result is apply's
// (Base empty; Saved Out; NewBase the hash of Out); with DryRun nothing
// is written and NewBase and Saved are empty.
func Run(p *ops.Plan, opt Options) (*apply.Result, error) {
	if p != nil && p.Base != "" {
		return nil, cli.Errorf(cli.CodeInvalidPlan, "base", "a new workbook has no base")
	}
	if opt.Out == "" {
		return nil, cli.Errorf(cli.CodeUsage, "out", "an output path is required")
	}
	template, err := Template()
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "xlsx-create-*.xlsx")
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "create temporary workbook: %v", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck // best-effort cleanup of a temporary file
	if _, err := f.Write(template); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, cli.Errorf(cli.CodeIO, "", "write temporary workbook: %v; close: %v", err, closeErr)
		}
		return nil, cli.Errorf(cli.CodeIO, "", "write temporary workbook: %v", err)
	}
	if err := f.Close(); err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "close temporary workbook: %v", err)
	}
	if p == nil {
		p = &ops.Plan{}
	}
	res, err := apply.Run(f.Name(), p, apply.Options{Out: opt.Out, DryRun: opt.DryRun, Recalc: opt.Recalc})
	if err != nil {
		return nil, err
	}
	res.Base = ""
	if opt.DryRun || res.Saved != "" {
		return res, nil
	}
	// apply deliberately does not write a no-op. A create must still
	// write the template, using the transaction's atomic copy path.
	var openErr error
	tx, err := plan.Begin(f.Name(), func(pkg *opc.Package) plan.Workspace {
		var ws *edit.Workspace
		ws, openErr = edit.New(pkg)
		return ws
	})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if openErr != nil {
		return nil, openErr
	}
	if err := tx.CommitTo(opt.Out); err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "write the workbook: %v", err)
	}
	res.Saved = opt.Out
	res.NewBase = cli.Hash(template)
	return res, nil
}
