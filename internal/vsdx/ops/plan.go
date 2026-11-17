// Package ops is the vsdx plan grammar: the op table, the specs (whose
// struct tags are the plan fields), decoding through internal/cli and
// the document-independent rules. Running an op is apply's (with the
// edit layer); nothing here touches a package. Specs follow the contract
// draft (docs/vsdx-contract-draft.md, section 3).
package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
)

// Spec is one decoded operation.
type Spec any

// Plan is a decoded vsdx plan: base, units ("pt" or "in"; "" is pt) and
// the operations in order.
type Plan struct {
	Base  string
	Units string
	Ops   []Spec
}

// ParsePlan decodes and checks a plan's text: the envelope
// (cli.ParsePlan with the setting "units"), every op's fields through
// the shared decoder and then every op's rules; all document-independent
// faults come back together in plan order. Chunk O0.
func ParsePlan(text []byte) (*Plan, cli.Errors) {
	_ = text
	return nil, cli.Errors{cli.Errorf(cli.CodeOperationFailed, "", "vsdx plans are not implemented yet")}
}
