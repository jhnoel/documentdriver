package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ReplaceEquation is equation.replace (docxdriver's replace_equation).
type ReplaceEquation struct {
	At      EquationAddress `json:"at"`
	LaTeX   string          `json:"latex" doc:"The new equation, in LaTeX."`
	Display *bool           `json:"display" default:"null"`
}

// opEquationReplace is equation.replace's entry in the operation table.
var opEquationReplace = OpDef{
	Name: "equation.replace", Oracle: "replace_equation", New: func() Spec { return &ReplaceEquation{} },
	Moved: "`at` `P/equation:n` ← `at` + `equation`; `latex` (docxdriver: `mathml`)",
	Rules: replaceEquationRules, Normalize: normalizeEquation,
}

// Run replaces an equation that stands directly in its paragraph
// (equations.rs replace) with the LaTeX converted to Office Math, its
// runs carrying the replaced equation's most common run properties
// (math.RunProperties), so the replacement keeps its font, size and
// colour. display unset keeps the target's form (a display target is
// m:oMathPara); LaTeX with display lines is refused on an inline one.
// Track mode deletes the old equation and inserts the new one after it;
// direct mode accepts both.
func (o *ReplaceEquation) Run(env *Env) error {
	h, p, eq, _, _, err := env.equationTarget(o.At, "replacing")
	if err != nil {
		return err
	}
	if p.Space() == model.NSStrictW {
		return reject("operation_failed", "at", "new equations cannot be written into a Strict document")
	}
	display := model.IsLocal(eq, "oMathPara")
	if o.Display != nil {
		display = *o.Display
	}
	if _, err := math.CanonicalLaTeX(o.LaTeX, display); err != nil {
		return reject("invalid_operation", "latex", "%s (the target is an inline equation; set display to true for a display equation)", err.Error())
	}
	omml, err := math.LaTeXToOMMLWith(o.LaTeX, display, math.Options{RunProperties: math.RunProperties(eq)})
	if err != nil {
		return reject("invalid_operation", "latex", "invalid LaTeX equation: %s", err.Error())
	}
	nodes, err := h.Workspace().Parse(p, []byte(omml), map[string]string{"w": model.NSW})
	if err != nil || len(nodes) != 1 {
		return errors.Join(errors.New("ops: the converted equation did not parse as one element"), err)
	}
	if err := h.PlaceAt(p, nodes[0], model.NextElement(eq)); err != nil {
		return err
	}
	if err := env.Rev.Delete(h, []*tree.Node{eq}); err != nil {
		return err
	}
	if _, err := env.Rev.Insert(h, nodes); err != nil {
		return err
	}
	return nil
}
