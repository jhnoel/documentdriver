package ops

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
)

// InsertText is text.insert (the contract's own; docxdriver has no counterpart).
type InsertText struct {
	At         ParagraphAddress `json:"at"`
	Select     *string          "json:\"select\" default:\"null\" doc:\"Insert just before or after this text; omitted, at the start (`before`) or end (`after`) of the paragraph.\""
	Occurrence *uint32          `json:"occurrence" default:"null"`
	Position   InsertPosition   `json:"position"`
	With       string           `json:"with" doc:"Plain text: a tab is \\t, a line break \\n; no U+FFFC."`
}

// opTextInsert is text.insert's entry in the operation table.
var opTextInsert = OpDef{
	Name: "text.insert", Oracle: "", New: func() Spec { return &InsertText{} },
	Moved: "none (new)",
	Rules: insertTextRules,
}

// Run inserts plain text at an insertion point of the paragraph's final
// view (docs/contract.md, section 6): just before or after the selected
// text, or at the start or end of the paragraph. The new text is one run
// wearing the run properties of the text before the point (else after
// it), recorded as a tracked insertion; inside or beside the author's own
// pending insertion it joins that insertion. It is text.replace of an
// empty range: the text around the point is never rewritten, so the
// direct-mode guard of a replacement does not apply.
func (o *InsertText) Run(env *Env) error {
	t, err := env.textTargetOf(o.At)
	if err != nil {
		return err
	}
	if err := checkWith("with", o.With); err != nil {
		return err
	}
	point := 0
	if o.Select != nil {
		cs, ce, _, err := selectOne(t.p, t.id, *o.Select, o.Occurrence)
		if err != nil {
			return err
		}
		point = cs
		if o.Position == "after" {
			point = ce
		}
	} else if o.Position == "after" {
		point = len([]rune(span.Read(t.p).Text(model.Current)))
	}
	err = env.editText(t, []textEdit{{point, point, o.With, "select", "with"}})
	return err
}
