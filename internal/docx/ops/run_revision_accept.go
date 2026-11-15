package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// AcceptRevisions is revision.accept (docxdriver's revision_settle with
// action accept).
type AcceptRevisions struct {
	At     *RevisionScope  "json:\"at\" default:\"null\" doc:\"One revision (`revision:7`), or the body paragraphs whose revisions are settled. Not with `all`.\""
	All    bool            `json:"all,omitzero" default:"false" doc:"Every revision of the document."`
	Author *RevisionAuthor `json:"author" default:"null" doc:"Only this author's revisions (with all or paragraphs)."`
}

// opRevisionAccept is revision.accept's entry in the operation table.
var opRevisionAccept = OpDef{
	Name: "revision.accept", Oracle: "revision_settle", New: func() Spec { return &AcceptRevisions{} },
	Moved: "`at` / `all` / `author` ← `target`",
	Fixed: map[string]string{"action": `"accept"`}, Rules: revisionRules(func(a *AcceptRevisions) (*RevisionScope, bool, *RevisionAuthor) { return a.At, a.All, a.Author }, "revision.accept"),
	Track: Untracked, Direct: Same,
}

// Run accepts the revisions.
func (o *AcceptRevisions) Run(env *Env) error { return env.settle(o.At, o.Author, true) }

// settle settles revisions (revisions.rs accept_reject_all and
// accept_reject_one_typed): the one revision at names (the body's first
// with that id, else the first header or footer part that has it), or a
// selection: every revision of the body and of the header and footer
// parts the sections use (all), or those of the body paragraphs at lists,
// narrowed to one author's when author is given. A rejection then drops
// the list definitions it orphaned. It writes no revision, so both change
// modes run it alike.
func (env *Env) settle(at *RevisionScope, author *RevisionAuthor, accept bool) error {
	main, x, err := env.Main()
	if err != nil {
		return err
	}
	snap := env.ReadModel()
	mainName := strings.TrimPrefix(main, "/")
	numbering := snap.NumberingPartName()
	var before map[string]bool
	if !accept {
		before = revise.ReferencedNumIDs(append([]*tree.Node{x.Root()}, env.wordParts(mainName, numbering)...))
	}
	if at != nil && at.ID != "" {
		if err := env.settleOne(snap, main, x, at.ID, accept); err != nil {
			return err
		}
	} else {
		if _, err := env.settleSelected(snap, main, x, at, author, accept); err != nil {
			return err
		}
	}
	if accept {
		return nil
	}
	after := revise.ReferencedNumIDs(append([]*tree.Node{x.Root()}, env.wordParts(mainName, numbering)...))
	orphaned := revise.Orphaned(before, after)
	if len(orphaned) == 0 {
		return nil
	}
	nx, ok := env.part(numbering)
	if !ok {
		return nil
	}
	h, err := env.WS.Core().Part(numbering)
	if err != nil {
		return err
	}
	_, err = revise.DropOrphanedLists(h, nx.Root(), orphaned)
	return err
}

// settleOne settles the revision with id id: the body's first, else the
// first header or footer part's.
func (env *Env) settleOne(snap *model.Package, main string, x *model.XML, id string, accept bool) error {
	parts := append([]string{main}, view.ChromePartNames(snap, x)...)
	for i, part := range parts {
		px := x
		if i > 0 {
			var ok bool
			if px, ok = env.part(part); !ok {
				continue
			}
		}
		rev := revise.FindRevision(revise.Scope(px), id)
		if rev == nil {
			continue
		}
		o := revise.Options{Accept: accept, Select: func(n *tree.Node) bool { return n == rev }}
		if i == 0 {
			o.Join = env.join
		}
		_, err := revise.Settle(env.Story(part, revise.Scope(px)), revise.Scope(px), o)
		return err
	}
	return reject("not_found", "at", "revision not found: %s", id)
}

// settleSelected settles every revision of the body and of the header
// and footer parts the sections use, or, when at lists paragraphs, those
// of the listed body paragraphs (revise.Owners); author narrows
// either. A header or footer part is opened as a region only when it has
// a revision to settle under a narrowed selection, so that an untouched
// part stays undeclared. It returns how many revisions it settled.
func (env *Env) settleSelected(snap *model.Package, main string, x *model.XML, at *RevisionScope, author *RevisionAuthor, accept bool) (int, error) {
	var paragraphs map[*tree.Node]bool
	if at != nil && at.Paragraphs != nil {
		paragraphs = map[*tree.Node]bool{}
		for _, a := range at.Paragraphs {
			p, _, err := env.paragraph(a)
			if err != nil {
				return 0, err
			}
			if !paragraphs[p] {
				paragraphs[p] = true
			}
		}
	}
	var name *string
	if author != nil {
		s := string(*author)
		name = &s
	}
	sel := revise.Selection(paragraphs, name)
	count, err := revise.Settle(env.Story(main, revise.Scope(x)), revise.Scope(x), revise.Options{Accept: accept, Select: sel, Join: env.join})
	if err != nil || paragraphs != nil {
		return count, err
	}
	for _, part := range view.ChromePartNames(snap, x) {
		px, ok := env.part(part)
		if !ok {
			continue
		}
		if sel != nil && !hasRevision(revise.Scope(px), sel) {
			continue
		}
		n, err := revise.Settle(env.Story(part, revise.Scope(px)), revise.Scope(px), revise.Options{Accept: accept, Select: sel})
		if err != nil {
			return count, err
		}
		count += n
	}
	return count, nil
}

// part returns the working tree of a part that exists and parses.
func (env *Env) part(name string) (*model.XML, bool) {
	if !env.WS.Package().Has(name) {
		return nil, false
	}
	doc, err := env.WS.Tree(name)
	if err != nil || doc.Root() == nil {
		return nil, false
	}
	return &model.XML{Doc: doc}, true
}

// join records a paragraph join of the main document with the workspace
// (identity and the gone id), before settling moves the content.
func (env *Env) join(h *core.Handle, survivor, absorbed *tree.Node) error {
	_, err := env.WS.JoinParagraphs(h, survivor, absorbed)
	return err
}
