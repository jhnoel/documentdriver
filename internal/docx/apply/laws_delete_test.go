package apply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/laws"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// acceptAuthoredJoined is acceptAuthored for operations whose accepted
// revisions join paragraphs: the joins go through the workspace, as
// direct mode's do, so a joined paragraph keeps the identity direct mode
// gives it, and the ids are persisted as a save does.
func acceptAuthoredJoined(src, dst string) error {
	if err := laws.CopyFile(src, dst); err != nil {
		return err
	}
	var ws *docxedit.Workspace
	tx, err := plan.Begin(dst, func(pkg *opc.Package) plan.Workspace {
		ws, _ = docxedit.New(pkg, docxedit.Options{}) //nolint:errcheck // the law inputs open
		return ws
	})
	if err != nil {
		return err
	}
	main := ws.DocumentPart()
	for _, name := range ws.Package().Names() {
		doc, err := ws.Tree(name)
		if err != nil || doc.Root() == nil || !docxedit.Units().HasUnits(doc) {
			continue
		}
		x := &model.XML{Doc: doc}
		o := revise.Options{Accept: true, Select: func(n *tree.Node) bool {
			return model.AttrOr(n, "author") == lawAuthor
		}}
		if name == main {
			o.Join = func(h *core.Handle, survivor, absorbed *tree.Node) error {
				_, err := ws.JoinParagraphs(h, survivor, absorbed)
				return err
			}
		}
		if _, err := revise.Settle(ws.Core().Story(name, name, revise.Scope(x)), revise.Scope(x), o); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := ws.Finish(); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// sourceHash is an input hash the listings carry below the top level.
var sourceHash = regexp.MustCompile(`"source":"sha256:[0-9a-f]{64}"`)

// projectNoSource is project without any input hash: two files that
// mean the same project the same.
func projectNoSource(path string) (string, error) {
	s, err := project(path)
	return sourceHash.ReplaceAllString(s, `"source":""`), err
}

// deleteSuite is paragraph.delete's change-mode law for one plan.
func deleteSuite(name, ops string) *laws.ModeSuite {
	return &laws.ModeSuite{
		Name:  "paragraph.delete " + name,
		Class: laws.AcceptAuthored,
		Apply: func(src, dst, mode string) error { return runPlan(src, dst, mode, ops) },
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Project:        projectNoSource,
		Settle:         settleFile,
		AcceptAuthored: acceptAuthoredJoined,
	}
}

// bodyParagraphIDs are the addresses of the body's own paragraphs (not
// in tables or other blocks), in order.
func bodyParagraphIDs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return nil
	}
	ids := d.ParaIDs().IDs
	var out []string
	for _, c := range d.XML.Body().Elements() {
		if model.IsW(c, "p") {
			if id, ok := ids[c]; ok {
				out = append(out, id.ID)
			}
		}
	}
	return out
}

// deleteInputs are the law inputs and the authored write fixtures
// (revisions of several authors, sections, tables, comments).
func deleteInputs(t *testing.T) []string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	ins := lawInputs(t)
	for _, f := range []string{"mixed.docx", "sections.docx"} {
		ins = append(ins, filepath.Join(root, "tools", "oracle", "testdata", "write", f))
	}
	return ins
}

func atJSON(ids ...string) string {
	return `[{"op":"paragraph.delete","at":["` + strings.Join(ids, `","`) + `"]}]`
}

// TestDeleteParagraphsLaws runs paragraph.delete's change-mode law on the
// law inputs: the first, a middle and the last body paragraph, two
// consecutive ones, and every body paragraph one at a time on the write
// fixtures (each paragraph kind: own pending insertions, foreign
// revisions, before a table, ending a section, last of the body).
func TestDeleteParagraphsLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) == 0 {
			return
		}
		plans := map[string]string{
			"first": atJSON(ids[0]),
			"last":  atJSON(ids[len(ids)-1]),
		}
		if len(ids) > 2 {
			plans["middle"] = atJSON(ids[len(ids)/2])
			plans["two"] = atJSON(ids[len(ids)/2-1], ids[len(ids)/2])
			plans["all"] = atJSON(ids...)
		}
		if strings.Contains(in, filepath.Join("testdata", "write")) {
			for i, id := range ids {
				plans["paragraph "+string(rune('a'+i%26))+id] = atJSON(id)
			}
		}
		for name, ops := range plans {
			deleteSuite(name, ops).Check(t, in)
		}
	})
}
