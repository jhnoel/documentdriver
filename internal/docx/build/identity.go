package build

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Resolver resolves the existing identities items carry (contract.md
// section 7, placeholders): the content an operation replaces, by the
// addresses read gave it. An item with an identity is that content,
// moved where the HTML puts it, never a reconversion of it.
type Resolver interface {
	// Equation returns the m:oMath or m:oMathPara element ref names in
	// the content being replaced, and its LaTeX as read shows it; false
	// when ref names no equation there.
	Equation(ref string) (eq *tree.Node, latex string, ok bool)
	// Placeholder returns the placeholder ref names in the content being
	// replaced (model.Placeholders); false when ref names none there.
	Placeholder(ref string) (model.Placeholder, bool)
}

// Equations is a Resolver over a fixed set of equations by address.
type Equations map[string]Equation

// Equation is one existing equation: its element and its LaTeX as read.
type Equation struct {
	Node  *tree.Node
	LaTeX string
}

// Equation returns the equation ref names.
func (e Equations) Equation(ref string) (*tree.Node, string, bool) {
	q, ok := e[ref]
	return q.Node, q.LaTeX, ok
}

// Placeholder names nothing: Equations hold equations only.
func (Equations) Placeholder(string) (model.Placeholder, bool) { return model.Placeholder{}, false }

// Existing is a Resolver over the equations and placeholders of the
// content being replaced, by address.
type Existing struct {
	Equations    Equations
	Placeholders map[string]model.Placeholder
}

// Equation returns the equation ref names.
func (e *Existing) Equation(ref string) (*tree.Node, string, bool) { return e.Equations.Equation(ref) }

// Placeholder returns the placeholder ref names.
func (e *Existing) Placeholder(ref string) (model.Placeholder, bool) {
	ph, ok := e.Placeholders[ref]
	return ph, ok
}

// placeholder places placeholder atom a in container before at: its
// original nodes, moved (never copied: a placeholder is that content),
// and an object of a run in a new run of its own that carries a copy of
// the original run's properties. Its text must be the content's as read
// shows it: a placeholder is not edited through its rendering.
func (b *builder) placeholder(container, at *tree.Node, a html.Atom) ([]*tree.Node, error) {
	var ph model.Placeholder
	ok := false
	if b.opt.Identity != nil {
		ph, ok = b.opt.Identity.Placeholder(a.Ref)
	}
	if !ok {
		return nil, &RefError{Ref: a.Ref}
	}
	if b.moved[ph.Key] {
		return nil, contentErr("placeholder %s appears twice: existing content is moved, never copied", a.Ref)
	}
	if want := model.PlaceholderText(ph); a.Text != want && !Textless(ph.Kind) {
		return nil, contentErr("placeholder %s: its text differs from the document's; the content of a %s is not editable through html", a.Ref, ph.Kind)
	}
	if b.moved == nil {
		b.moved, b.kept = map[*tree.Node]bool{}, map[*tree.Node]bool{}
	}
	b.moved[ph.Key] = true
	if !ph.InRun {
		for _, n := range ph.Nodes {
			if err := b.put(container, at, n); err != nil {
				return nil, err
			}
			b.kept[n] = true
		}
		return ph.Nodes, nil
	}
	r, err := b.elem(container, `<w:r/>`)
	if err != nil {
		return nil, err
	}
	if err := b.h.PlaceAt(container, r, at); err != nil {
		return nil, err
	}
	if orig := ph.Nodes[0].Parent(); orig != nil {
		if rpr := model.ChildW(orig, "rPr"); rpr != nil {
			nodes, err := b.h.Workspace().Parse(r, rpr.Bytes(), nil)
			if err != nil || len(nodes) != 1 {
				return nil, errors.Join(errors.New("build: copying a placeholder's run properties"), err)
			}
			b.h.Workspace().RecordCopy(nodes[0], rpr)
			if err := b.h.PlaceNew(r, nodes[0]); err != nil {
				return nil, err
			}
		}
	}
	if err := b.h.Append(r, ph.Nodes[0]); err != nil {
		return nil, err
	}
	b.kept[r] = true
	return []*tree.Node{r}, nil
}

// put moves n, an element of the handle's content, into container before
// at (at its end when at is nil).
func (b *builder) put(container, at, n *tree.Node) error {
	if at != nil {
		return b.h.PutBefore(n, at)
	}
	return b.h.Append(container, n)
}

// Textless reports whether kind is a placeholder kind read renders
// without the content's text (a picture, a note reference's label, an
// empty <opaque>), whose text content is not compared.
func Textless(kind string) bool {
	switch kind {
	case "field", "content-control", "ruby", "alternate-content", "unknown":
		return false
	}
	return true
}

// equation returns the detached element for equation atom a, to place
// under container: a copy of the original's bytes when a carries a
// data-ref (its LaTeX must be the original's, as read shows it: the text
// of a placeholder is not an edit), else the LaTeX converted with the
// formatting template.
func (b *builder) equation(container *tree.Node, a html.Atom) (*tree.Node, error) {
	if a.Ref != "" {
		var orig *tree.Node
		var latex string
		ok := false
		if b.opt.Identity != nil {
			orig, latex, ok = b.opt.Identity.Equation(a.Ref)
		}
		if !ok {
			return nil, &RefError{Ref: a.Ref}
		}
		if !SameLaTeX(latex, a.Text, a.Display) {
			return nil, contentErr("equation %s: its LaTeX differs from the document's; change an equation with equation.replace, or leave out data-ref to write a new one", a.Ref)
		}
		return b.h.Workspace().Copy(orig)
	}
	if b.p.Space() == model.NSStrictW {
		return nil, ErrStrict
	}
	omml, err := math.LaTeXToOMMLWith(a.Text, a.Display, math.Options{RunProperties: b.opt.EquationRunProperties})
	if err != nil {
		return nil, contentErr("invalid LaTeX equation: %s", err.Error())
	}
	return b.elem(container, omml)
}

// SameLaTeX reports whether two LaTeX texts write the same equation:
// equal, or equal in the grammar's canonical form.
func SameLaTeX(a, b string, display bool) bool {
	if a == b {
		return true
	}
	ca, err := math.CanonicalLaTeX(a, display)
	if err != nil {
		return false
	}
	cb, err := math.CanonicalLaTeX(b, display)
	return err == nil && ca == cb
}
