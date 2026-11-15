package ops

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// DeleteEquation is equation.delete (docxdriver's delete_equation).
type DeleteEquation struct {
	At EquationAddress `json:"at"`
}

// opEquationDelete is equation.delete's entry in the operation table.
var opEquationDelete = OpDef{
	Name: "equation.delete", Oracle: "delete_equation", New: func() Spec { return &DeleteEquation{} },
	Moved: "`at` `P/equation:n` ← `at` + `equation`",
	Rules: deleteEquationRules,
}

// Run deletes an equation that stands directly in its paragraph
// (equations.rs delete): the author's w:del around it in track mode,
// removed in direct mode (the Reviser accepts the deletion).
func (o *DeleteEquation) Run(env *Env) error {
	h, _, eq, id, n, err := env.equationTarget(o.At, "deleting")
	if err != nil {
		return err
	}
	if err := env.Rev.Delete(h, []*tree.Node{eq}); err != nil {
		return err
	}
	ref := id + "/equation:" + strconv.Itoa(n)
	env.Deleted = append(env.Deleted, ref)
	return nil
}
