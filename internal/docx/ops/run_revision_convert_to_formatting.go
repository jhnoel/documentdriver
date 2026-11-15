package ops

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ConvertRevisionsToFormatting is revision.convert_to_formatting (docxdriver's convert_revisions_to_formatting).
type ConvertRevisionsToFormatting struct {
	At     *RevisionScope  "json:\"at\" default:\"null\" doc:\"One revision (`revision:7`), or the paragraphs whose revisions are converted. Not with `all`.\""
	All    bool            `json:"all,omitzero" default:"false" doc:"Every revision of the body and of the headers and footers."`
	Author *RevisionAuthor `json:"author" default:"null" doc:"Only this author's revisions (with all or paragraphs)."`
}

// opRevisionConvertToFormatting is revision.convert_to_formatting's entry in the operation table.
var opRevisionConvertToFormatting = OpDef{
	Name: "revision.convert_to_formatting", Oracle: "convert_revisions_to_formatting", New: func() Spec { return &ConvertRevisionsToFormatting{} },
	Moved: "`at` / `all` / `author` ← `target`, `at`",
	Rules: convertRules, Track: Untracked, Direct: Same,
}

// Run settles the selected revisions into amendment formatting
// (revisions.rs convert_to_formatting): insertions underlined, deletions
// struck through and live again, paragraph marks and rows kept unmarked,
// property changes accepted (revise.Convert). Without paragraphs, the
// headers and footers the sections use are converted too. It writes no
// revision, so both change modes run it alike. A revision address that
// names no revision is not_found.
func (o *ConvertRevisionsToFormatting) Run(env *Env) error {
	main, x, err := env.Main()
	if err != nil {
		return err
	}
	var opt revise.ConvertOptions
	switch {
	case o.At != nil && o.At.ID != "":
		id := o.At.ID
		opt.Select = func(n *tree.Node) bool { return model.AttrOr(n, "id") == id }
	case o.Author != nil:
		author := string(*o.Author)
		opt.Select = func(n *tree.Node) bool { return model.AttrOr(n, "author") == author }
	}
	if o.At != nil && o.At.Paragraphs != nil {
		opt.Paragraphs = map[*tree.Node]bool{}
		for _, a := range o.At.Paragraphs {
			p, _, err := env.paragraph(a)
			if err != nil {
				return err
			}
			opt.Paragraphs[p] = true
		}
	}
	done, err := revise.Convert(env.Story(main, revise.Scope(x)), revise.Scope(x), opt)
	if err != nil {
		return err
	}
	if opt.Paragraphs == nil {
		snap := env.ReadModel()
		for _, part := range view.ChromePartNames(snap, x) {
			px, ok := env.part(part)
			if !ok {
				continue
			}
			// The part is opened as a region only when it has something
			// to convert, so that an untouched part stays undeclared.
			if !hasRevision(revise.Scope(px), opt.Select) {
				continue
			}
			d, err := revise.Convert(env.Story(part, revise.Scope(px)), revise.Scope(px), opt)
			if err != nil {
				return err
			}
			done.IDs = append(done.IDs, d.IDs...)
			done.Inserted += d.Inserted
			done.Deleted += d.Deleted
			done.Marks += d.Marks
			done.Rows += d.Rows
			done.Formatting += d.Formatting
		}
	}
	if o.At != nil && o.At.ID != "" && done.Count() == 0 {
		// A revision address names a revision of the file: one that is
		// not there is not_found, as revision.accept refuses it
		// (docxdriver converts nothing and writes the file).
		return reject("not_found", "at", "revision not found: %s", o.At.ID)
	}
	return nil
}

// hasRevision reports whether scope holds a revision element sel picks.
func hasRevision(scope *tree.Node, sel func(*tree.Node) bool) bool {
	return model.Descendants(scope, func(n *tree.Node) bool {
		return model.IsElement(n) && model.IsW(n, n.Local()) && revisionLocal[n.Local()] && (sel == nil || sel(n))
	}) != nil
}

var revisionLocal = map[string]bool{
	"ins": true, "del": true, "moveFrom": true, "moveTo": true, "rPrChange": true, "pPrChange": true,
	"sectPrChange": true, "tblPrChange": true, "trPrChange": true, "tcPrChange": true, "tblGridChange": true,
	"numberingChange": true, "cellIns": true, "cellDel": true, "cellMerge": true,
}
