package ops

// ReplaceText is text.replace (docxdriver's replace_text).
type ReplaceText struct {
	At         ParagraphAddress `json:"at"`
	Select     string           `json:"select"`
	With       string           "json:\"with\" doc:\"Plain text: a tab is \\\\t, a line break \\\\n. An equation or other object in the selection (U+FFFC in select) must stay U+FFFC in `with`; one cannot be made or removed here.\""
	Occurrence *uint32          `json:"occurrence" default:"null"`
	All        bool             "json:\"all,omitzero\" default:\"false\" doc:\"Replace every match of `select` in the paragraph. Not with `occurrence`.\""
}

// opTextReplace is text.replace's entry in the operation table.
var opTextReplace = OpDef{
	Name: "text.replace", Oracle: "replace_text", New: func() Spec { return &ReplaceText{} },
	Moved: "`all` has no docxdriver form: one op per match",
	Rules: replaceTextRules, DirectGuards: textGuard,
}

// Run replaces the selected text of a paragraph's final view (typed.rs
// replace_text_typed; with all, every non-overlapping match, as
// revise_text's all does) through the text engine (span_text.go): word by
// word, a tracked deletion and insertion per changed word, the new text
// wearing the run properties of what it replaces. A selection crossing a
// comment, field, drawing, note or content control is refused; in direct
// mode so is one overlapping a tracked change or a comment range.
func (o *ReplaceText) Run(env *Env) error {
	t, err := env.textTargetOf(o.At)
	if err != nil {
		return err
	}
	if err := checkWith("with", o.With); err != nil {
		return err
	}
	var edits []textEdit
	if o.All {
		if err := plainSelect(t.id, "select", o.Select); err != nil {
			return err
		}
		ms := spanFind(t.p, o.Select)
		if len(ms) == 0 {
			_, _, _, err := selectOne(t.p, t.id, o.Select, nil)
			return err
		}
		for _, m := range ms {
			edits = append(edits, textEdit{m.Start, m.End, o.With, "select", "with"})
		}
	} else {
		cs, ce, _, err := selectOne(t.p, t.id, o.Select, o.Occurrence)
		if err != nil {
			return err
		}
		edits = []textEdit{{cs, ce, o.With, "select", "with"}}
	}
	err = env.editText(t, edits)
	return err
}
