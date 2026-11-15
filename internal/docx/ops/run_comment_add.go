package ops

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// CommentAdd is comment.add (docxdriver's comment_add).
type CommentAdd struct {
	At         ParagraphAddress `json:"at"`
	Select     *string          `json:"select" default:"null"`
	Occurrence *uint32          `json:"occurrence" default:"null"`
	Text       string           `json:"text"`
}

// opCommentAdd is comment.add's entry in the operation table.
var opCommentAdd = OpDef{
	Name: "comment.add", Oracle: "comment_add", New: func() Spec { return &CommentAdd{} },
	Rules: commentAddRules, Track: Untracked, Direct: Same,
}

// initials are a comment author's initials (comments.rs
// initials_from_author): the first ASCII letter of each of the first four
// words that have one, upper-cased.
func initials(author string) string {
	var b strings.Builder
	n := 0
	for _, w := range strings.FieldsFunc(author, unicode.IsSpace) {
		if n == 4 {
			break
		}
		for _, c := range w {
			if c < 0x80 && unicode.IsLetter(c) {
				b.WriteRune(unicode.ToUpper(c))
				n++
				break
			}
		}
	}
	return b.String()
}

// commentParts returns the comment parts after checking, before any
// change, that the operation can write them: every existing one parses,
// and a missing one can be registered (unreadable_target otherwise).
func (env *Env) commentParts() error {
	cp, err := env.WS.CommentParts()
	if err != nil {
		return err
	}
	missing := false
	for _, name := range []string{cp.Comments, cp.Extended, cp.IDs, cp.Extensible} {
		if name == "" {
			missing = true
			continue
		}
		if doc, err := env.WS.Tree(name); err != nil || doc.Root() == nil {
			return &Rejection{Code: "unreadable_target", Msg: "the comment part " + strings.TrimPrefix(name, "/") + " cannot be read, so no comment can be added: " + errText(err)}
		}
	}
	if missing {
		if err := env.WS.Package().RegistersTypes(); err != nil {
			return &Rejection{Code: "unreadable_target", Msg: "a comment part must be added, but " + err.Error()}
		}
	}
	return nil
}

// errText returns err's message, or the missing-root text when nil.
func errText(err error) string {
	if err == nil {
		return "it has no root element"
	}
	return err.Error()
}

// anchor returns the span the comment covers (typed.rs comment_add_typed):
// one match of select in the paragraph's final view (with occurrence),
// or the whole paragraph, deleted text included, when there is none.
func (o *CommentAdd) anchor(p *tree.Node, id string) (span.Span, error) {
	para := span.Read(p)
	if o.Select == nil {
		return span.Span{Para: p, Start: 0, End: para.Len()}, nil
	}
	sel := *o.Select
	if strings.Contains(sel, "<") {
		return span.Span{}, reject("operation_failed", "select", "blocked in paragraph %s: select must be plain text without HTML markup — select addresses the paragraph's visible text, never markup", id)
	}
	matches := para.Find(model.Current, sel)
	current := para.Text(model.Current)
	switch n := len(matches); {
	case n == 0:
		clause := hiddenClause(para, sel, true)
		return span.Span{}, reject("not_found", "select", "blocked in paragraph %s: select matched nothing (current visible text: %q)%s", id, current, clause)
	case o.Occurrence != nil && int(*o.Occurrence) > n:
		return span.Span{}, reject("not_found", "occurrence", "blocked in paragraph %s: occurrence %d is out of range: select matched %d times in this paragraph", id, *o.Occurrence, n)
	case o.Occurrence != nil:
		return matches[*o.Occurrence-1].Span, nil
	case n > 1:
		return span.Span{}, &Rejection{Code: "ambiguous", Field: "select",
			Msg: "blocked in paragraph " + id + ": select matched " + strconv.Itoa(n) + " times in this paragraph — pass occurrence (1-based) or extend select with more context (current visible text: " + strconv.Quote(current) + ")"}
	}
	return matches[0].Span, nil
}

// Run anchors a new comment (typed.rs comment_add_typed, comments.rs
// anchor_comment_at): w:commentRangeStart before the span's first
// element, w:commentRangeEnd after its last, and the reference run
// after the range end, outside any wrapper the end sits in; the
// comment's records go to all four comment parts (docx edit's
// AddCommentRecords), with ids from the workspace's allocator. The span
// comes from the span engine (span.Isolate); a selection that crosses a
// comment, field, drawing, note or content control is refused. Comments
// are not tracked, so both change modes write the same.
func (o *CommentAdd) Run(env *Env) error {
	main, _, err := env.Main()
	if err != nil {
		return err
	}
	p, id, err := env.paragraph(o.At)
	if err != nil {
		return err
	}
	s, err := o.anchor(p, id)
	if err != nil {
		return err
	}
	if err := env.commentParts(); err != nil {
		return err
	}
	h, err := env.WS.Core().Range(main, "paragraph "+id, p, p)
	if err != nil {
		return err
	}
	// Isolation refusals come before any change. A blocker is found on
	// the isolated span; the refusal then fails the plan, so the run
	// splits it made are never saved.
	iso, err := span.Isolate(h, s)
	if err != nil {
		if errors.Is(err, span.ErrRange) {
			return err
		}
		return reject("operation_failed", "", "blocked in paragraph %s: the anchor cannot be isolated: %s", id, err.Error())
	}
	if o.Select != nil {
		if label, _, bad := iso.Blocker(); bad {
			return reject("operation_failed", "select", "blocked in paragraph %s: the selection crosses a %s that cannot be safely preserved — update, resolve, or remove it before commenting on this content", id, label)
		}
	}
	cid, err := env.WS.CommentID()
	if err != nil {
		return err
	}
	if err := placeAnchor(h, p, iso.Units, cid); err != nil {
		return err
	}
	paraID, err := env.WS.CommentParaID()
	if err != nil {
		return err
	}
	durable, err := env.WS.DurableID(paraID, env.Author, o.Text)
	if err != nil {
		return err
	}
	err = env.WS.AddCommentRecords(docxedit.NewComment{
		ID: cid, ParaID: paraID, Author: env.Author, Initials: initials(env.Author),
		Date: env.Now.UTC().Format(revise.DateLayout), Text: o.Text, DurableID: durable,
	})
	if errors.Is(err, opc.ErrContentTypes) {
		return &Rejection{Code: "unreadable_target", Msg: err.Error()}
	}
	if err != nil {
		return err
	}
	return nil
}

// placeAnchor puts comment cid's range markers at the edges of units
// (an isolated span of paragraph p; none for an empty paragraph, whose
// markers go at its end), beside the first and last unit wherever they
// stand, inside hyperlinks and revision wrappers too, and its reference
// run after the range end at the paragraph's level: a reference inside a
// hyperlink or a revision wrapper would be linked or revised with it.
func placeAnchor(h *core.Handle, p *tree.Node, units []*tree.Node, cid string) error {
	frag := func(at *tree.Node, s string) (*tree.Node, error) {
		nodes, err := h.Workspace().Parse(at, []byte(s), map[string]string{"w": model.NSW})
		if err != nil {
			return nil, err
		}
		if len(nodes) != 1 {
			return nil, errors.New("comment.add: a marker did not parse as one element")
		}
		return nodes[0], nil
	}
	// place puts the marker s beside n (before it, or after it), or at
	// the end of p when n is nil.
	place := func(s string, n *tree.Node, after bool) (*tree.Node, error) {
		parent, at := p, (*tree.Node)(nil)
		if n != nil {
			parent, at = n.Parent(), n
			if after {
				at = model.NextElement(n)
			}
		}
		m, err := frag(parent, s)
		if err != nil {
			return nil, err
		}
		return m, h.PlaceAt(parent, m, at)
	}
	id := `w:id="` + cid + `"`
	var first, last *tree.Node
	if len(units) > 0 {
		first, last = units[0], units[len(units)-1]
	}
	if _, err := place(`<w:commentRangeStart `+id+`/>`, first, false); err != nil {
		return err
	}
	end, err := place(`<w:commentRangeEnd `+id+`/>`, last, true)
	if err != nil {
		return err
	}
	// The reference run goes after the range end's paragraph child.
	top := end
	for top.Parent() != p {
		top = top.Parent()
	}
	ref, err := frag(p, `<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference `+id+`/></w:r>`)
	if err != nil {
		return err
	}
	return h.PlaceAt(p, ref, model.NextElement(top))
}
