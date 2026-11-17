// Package integrity is the structural lint of a drawing and the gate of
// the writes (docs/vsdx-design.md, Verification item 6): shape IDs unique
// per page across nesting, Connects naming shapes that exist, Master,
// MasterShape and style IDs that resolve, page IDs unique, BackPage
// resolving, one Override per part and one relationship per (source,
// target, type), the document content type matching the package kind.
// As docx's and xlsx's gates, a plan is refused only for a finding it
// introduces.
package integrity

import (
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Finding is one structural defect: its kind (duplicate-shape-id,
// dangling-connect, unresolved-master, unresolved-style,
// duplicate-page-id, unresolved-backpage, duplicate-override,
// duplicate-rel, missing-part, kind-mismatch), the part and the
// nearest address, and a sentence.
type Finding struct {
	Kind, Part, At, Detail string
}

// Check returns every finding in the package, parts in name order.
// Chunk G1.
func Check(p *opc.Package) ([]Finding, error) {
	_ = p
	return nil, vsd.ErrNotImplemented
}
