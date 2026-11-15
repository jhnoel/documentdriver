package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// equationTarget resolves an equation address (equations.rs replace and
// delete): the paragraph, its current equation N (or its only one when
// the address names the paragraph alone), editable only where it stands
// directly in the paragraph, and a region over the paragraph. It
// returns the paragraph's id and the equation's 1-based number.
func (env *Env) equationTarget(at EquationAddress, verb string) (h *core.Handle, p, eq *tree.Node, id string, n int, err error) {
	part, _, err := env.Main()
	if err != nil {
		return nil, nil, nil, "", 0, err
	}
	p, id, err = env.paragraph(at.Paragraph)
	if err != nil {
		return nil, nil, nil, "", 0, err
	}
	eqs := model.CurrentEquations(p)
	switch {
	case len(eqs) == 0:
		return nil, nil, nil, "", 0, reject("not_found", "at", "paragraph %s has no current equations", id)
	case at.N == nil && len(eqs) > 1:
		return nil, nil, nil, "", 0, reject("ambiguous", "at", "paragraph %s has %d equations; choose one: %s/equation:1 to %s/equation:%d", id, len(eqs), id, id, len(eqs))
	case at.N == nil:
		n = 1
	case int(*at.N) > len(eqs):
		return nil, nil, nil, "", 0, reject("not_found", "at", "equation %d is out of range; paragraph %s has %d current equations", *at.N, id, len(eqs))
	default:
		n = int(*at.N)
	}
	eq = eqs[n-1]
	if eq.Parent() != p {
		return nil, nil, nil, "", 0, reject("operation_failed", "at", "equation %s/equation:%s is inside a revision or structured wrapper; settle its tracked changes or unwrap it before %s", id, strconv.Itoa(n), verb)
	}
	h, err = env.WS.Core().Range(part, "equation at "+id, p, p)
	if err != nil {
		return nil, nil, nil, "", 0, err
	}
	return h, p, eq, id, n, nil
}
