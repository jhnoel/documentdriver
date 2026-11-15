package ops

import (
	"errors"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// DeleteParagraphs is paragraph.delete (docxdriver's delete_paragraphs).
type DeleteParagraphs struct {
	At []ParagraphAddress `json:"at"`
}

// opParagraphDelete is paragraph.delete's entry in the operation table.
var opParagraphDelete = OpDef{
	Name: "paragraph.delete", Oracle: "delete_paragraphs", New: func() Spec { return &DeleteParagraphs{} },
	Rules: deleteParagraphsRules,
}

// topLevel reports whether p is a paragraph of the body story itself
// (is_top_level_body_paragraph): not inside a table, a text box, a
// content control or an embedded object.
func topLevel(p, body *tree.Node) bool {
	for c := p.Parent(); c != nil; c = c.Parent() {
		if c == body {
			return true
		}
		for _, l := range []string{"tbl", "tc", "txbxContent", "sdt", "drawing", "object"} {
			if model.IsW(c, l) {
				return false
			}
		}
	}
	return false
}

// Run deletes the paragraphs (typed.rs delete_paragraphs_typed): targets
// resolve against the document as earlier operations left it, then go in
// document order without repeats. The revision core does the deletion
// (revise.Reviser.DeleteParagraphs); direct mode accepts exactly what it
// authored, joining through the workspace.
func (o *DeleteParagraphs) Run(env *Env) error {
	main, x, err := env.Main()
	if err != nil {
		return err
	}
	body := x.Body()
	if body == nil {
		return reject("not_found", "at", "the document has no body")
	}
	type target struct {
		p  *tree.Node
		id string
	}
	var targets []target
	for i, at := range o.At {
		p, id, err := env.paragraph(at)
		if err != nil {
			var r *Rejection
			if errors.As(err, &r) {
				r.Field = "at[" + strconv.Itoa(i) + "]"
			}
			return err
		}
		if !topLevel(p, body) {
			return reject("operation_failed", "", "blocked in paragraph %s: the paragraph is not a top-level body paragraph in word/document.xml — paragraph.delete targets top-level body paragraphs; table cells, text boxes and other embedded scopes are outside it", id)
		}
		targets = append(targets, target{p, id})
	}
	order := map[*tree.Node]int{}
	for i, p := range x.AllParagraphs() {
		order[p] = i
	}
	slices.SortStableFunc(targets, func(a, b target) int { return order[a.p] - order[b.p] })
	targets = slices.CompactFunc(targets, func(a, b target) bool { return a.p == b.p })
	ps := make([]*tree.Node, len(targets))
	for i, t := range targets {
		ps[i] = t.p
		if err := revise.ContentBlocker(t.p); err != nil && !env.Rev.OwnParagraph(t.p) {
			return reject("operation_failed", "", "blocked in paragraph %s: %s", t.id, err.Error())
		}
	}
	env.Rev.SetJoin(env.join)
	_, err = env.Rev.DeleteParagraphs(env.Story(main, body), ps)
	return err
}
