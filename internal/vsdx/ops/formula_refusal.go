package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// formulaCell is one cell a write refuses because vsdx does not
// evaluate its formula: the cell's stored name and formula.
type formulaCell struct {
	name    string
	formula string
}

// formulaRefusal reports the refusal of a write to the address
// whose cells are bound to formulas vsdx does not evaluate:
// operation_failed at field (the field written, at for
// object.move), message "<address> <cell> is bound to a formula
// vsdx does not evaluate", and detail each cell as
// "<cell> F=<formula>", "; "-separated, at most ten.
func formulaRefusal(address, field string, cells []formulaCell) error {
	if len(cells) == 0 {
		return nil
	}
	names := make([]string, len(cells))
	for i, c := range cells {
		names[i] = c.name
	}
	e := cli.Errorf(cli.CodeOperationFailed, field, "%s %s is bound to a formula vsdx does not evaluate", address, strings.Join(names, ", "))
	limit := min(len(cells), 10)
	parts := make([]string, limit)
	for i := range parts {
		parts[i] = cells[i].name + " F=" + cells[i].formula //nolint:gosec // G602: limit is min(len(cells), 10), so i is in range
	}
	e.Detail = strings.Join(parts, "; ")
	return e
}
