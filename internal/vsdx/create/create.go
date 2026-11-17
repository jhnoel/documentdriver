// Package create writes a new drawing: the blank template (one page,
// no masters; docs/vsdx-design.md, Create) and, given a plan, that plan
// applied to it through internal/vsdx/apply, as xlsx create does.
package create

import (
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
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
	_, _ = p, opt
	return nil, vsd.ErrNotImplemented
}
