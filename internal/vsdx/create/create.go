// Package create writes a new drawing: the blank template (one page,
// no masters; docs/vsdx-design.md, Create) and, given a plan, that plan
// applied to it through internal/vsdx/apply, as xlsx create does.
package create

import (
	"os"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// Options control a create.
type Options struct {
	Out    string
	DryRun bool
	IDs    ids.Source // the clock of docProps/core.xml; nil: ids.Default()
}

// Run writes the blank drawing with plan p applied when p is not nil.
// A plan with base is refused (invalid_plan, path base). Without a plan
// the template is written unchanged. Chunk CR1.
func Run(p *ops.Plan, opt Options) (*plan.Result, error) {
	if p != nil && p.Base != "" {
		return nil, cli.Errorf(cli.CodeInvalidPlan, "base", "a new drawing has no base")
	}
	if opt.Out == "" {
		return nil, cli.Errorf(cli.CodeUsage, "out", "an output path is required")
	}
	src := opt.IDs
	if src == nil {
		src = ids.Default()
	}
	template, err := Template(src.Now())
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "vsdx-create-*.vsdx")
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "create temporary drawing: %v", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck // best-effort cleanup of a temporary file
	if _, err := f.Write(template); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, cli.Errorf(cli.CodeIO, "", "write temporary drawing: %v; close: %v", err, closeErr)
		}
		return nil, cli.Errorf(cli.CodeIO, "", "write temporary drawing: %v", err)
	}
	if err := f.Close(); err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "close temporary drawing: %v", err)
	}
	if p == nil {
		return blank(f.Name(), template, opt)
	}
	res, err := apply.Run(f.Name(), p, apply.Options{Out: opt.Out, DryRun: opt.DryRun, IDs: src})
	if err != nil {
		return nil, err
	}
	res.Base = ""
	if opt.DryRun || res.Saved != "" {
		return res, nil
	}
	if err := commit(f.Name(), opt.Out, src); err != nil {
		return nil, err
	}
	res.Saved = opt.Out
	res.NewBase = cli.Hash(template)
	return res, nil
}

// blank returns the result of a create with no plan: a dry run writes
// nothing, otherwise the template is committed unchanged to opt.Out.
// The result's Base is empty and NewBase the hash of the template.
func blank(temp string, template []byte, opt Options) (*plan.Result, error) {
	res := plan.NewResult()
	res.DryRun = opt.DryRun
	if opt.DryRun {
		return &res, nil
	}
	src := opt.IDs
	if src == nil {
		src = ids.Default()
	}
	if err := commit(temp, opt.Out, src); err != nil {
		return nil, err
	}
	res.Saved = opt.Out
	res.NewBase = cli.Hash(template)
	return &res, nil
}

// commit writes the drawing at temp to out unchanged, through the
// plan transaction's atomic copy path, with src as the id source.
func commit(temp, out string, src ids.Source) error {
	ws, err := edit.Begin(temp, plan.WithIDs(src))
	if err != nil {
		return err
	}
	tx := ws.Tx()
	defer tx.Rollback()
	if err := tx.CommitTo(out); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the drawing: %v", err)
	}
	return nil
}
