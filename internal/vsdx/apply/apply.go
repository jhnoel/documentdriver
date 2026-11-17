// Package apply runs a vsdx plan on a drawing: the transaction through
// the edit layer, each op in order, the integrity gate on what the plan
// changed, the recalculation policy of decision (c), and the edit
// result with changed objects read back through internal/vsdx/read.
package apply

import (
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options control a run, as xlsx apply's.
type Options struct {
	DryRun     bool
	Out        string
	ChangedIDs bool
	IDs        ids.Source
}

// Run runs plan p (from ops.ParsePlan) against the drawing at path and
// returns the edit result. Errors are *cli.Error with the contract's
// codes. Chunk A1.
func Run(path string, p *ops.Plan, opt Options) (*plan.Result, error) {
	_, _, _ = path, p, opt
	return nil, vsd.ErrNotImplemented
}
