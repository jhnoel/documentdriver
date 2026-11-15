package apply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// viewOpt renders the reports' markup with the CLI's equation converter.
var viewOpt = view.Options{Equation: math.OMMLToLaTeX}

// lawAuthor is the author the law plans write as.
const lawAuthor = "Law Author"

// refusal is a plan the pipeline rejected.
type refusal struct{ d *Diagnostic }

func (r refusal) Error() string { return r.d.Code + ": " + r.d.Message }

// runPlan reviews and commits ops (a JSON array) in mode on a copy of src
// written to dst.
func runPlan(src, dst, mode, opsJSON string) error {
	if err := laws.CopyFile(src, dst); err != nil {
		return err
	}
	b, err := os.ReadFile(dst) //nolint:gosec // G304: the law's private copy
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	text := []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"` + lawAuthor + `","change_mode":"` + mode + `","ops":` + opsJSON + `}`)
	r, err := runText(dst, text, Options{View: viewOpt})
	if err != nil {
		return err
	}
	if r.Outcome == Rejected {
		return refusal{r.Diagnostic}
	}
	c, err := runText(dst, text, Options{Commit: r.PreviewKey, Now: "2026-01-01T00:00:00Z", View: viewOpt})
	if err != nil {
		return err
	}
	if c.Outcome == Rejected {
		return refusal{c.Diagnostic}
	}
	return sameHazards(c)
}

// project is what a document means through the read side: the HTML
// projection in the three views and the revision, comment and bookmark
// listings, without the input hash.
func project(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
	if err != nil {
		return "", err
	}
	opt := read.Options{Equation: math.OMMLToLaTeX}
	var out strings.Builder
	for _, q := range [][2]string{{"document", ""}, {"document", "final"}, {"document", "original"}, {"revisions", ""}, {"comments", ""}, {"bookmarks", ""}} {
		res, err := read.Read(b, q[0], q[1], opt)
		if err != nil {
			return "", err
		}
		o := res.Object()
		delete(o, "base")
		delete(o, "id_repairs_needed")
		s, err := read.Encode(o, false)
		if err != nil {
			return "", err
		}
		out.WriteString(s + "\n")
	}
	return out.String(), nil
}

// settleFile settles every revision of src into dst through the pipeline.
func settleFile(src, dst string, accept bool) error {
	action := "reject"
	if accept {
		action = "accept"
	}
	err := runPlan(src, dst, laws.Direct, `[{"op":"revision.`+action+`","all":true}]`)
	var r refusal
	if errors.As(err, &r) {
		return nil
	}
	return err
}

// acceptAuthored accepts the law author's revisions of src in its story
// parts, writing dst.
func acceptAuthored(src, dst string) error {
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
	for _, name := range ws.Package().Names() {
		doc, err := ws.Tree(name)
		if err != nil || doc.Root() == nil || !docxedit.Units().HasUnits(doc) {
			continue
		}
		x := &model.XML{Doc: doc}
		if _, err := revise.Settle(ws.Core().Story(name, name, revise.Scope(x)), revise.Scope(x), revise.Options{Accept: true, Select: func(n *tree.Node) bool {
			return model.AttrOr(n, "author") == lawAuthor
		}}); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// settleSuite is revision.accept and revision.reject's change-mode law: it writes no revision,
// so both modes give one result.
func settleSuite(target, action string) *laws.ModeSuite {
	ops := `[{"op":"revision.` + action + `","at":"revision:` + target + `"}]`
	if target == "all" {
		ops = `[{"op":"revision.` + action + `","all":true}]`
	}
	return &laws.ModeSuite{
		Name:  "revision." + action + " " + target,
		Class: laws.Same,
		Apply: func(src, dst, mode string) error { return runPlan(src, dst, mode, ops) },
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Project:        project,
		Settle:         settleFile,
		AcceptAuthored: acceptAuthored,
	}
}

// lawInputs are the authored fixtures and, with DOCX_CORPUS, the corpus
// documents with tracked changes in their body.
func lawInputs(t *testing.T) []string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	ins := []string{filepath.Join(root, "testdata", "docx", "rich.docx")}
	dir := os.Getenv("DOCX_CORPUS")
	if dir == "" {
		return ins
	}
	return append(ins, scanCorpus(t, "revisions", dir, func(b []byte) bool {
		doc, err := model.OpenDocument(b)
		return err == nil && len(doc.XML.RevisionElements()) > 0
	})...)
}

// firstID is the id of the first revision the listing shows, or "".
func firstID(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return ""
	}
	d, err := model.OpenDocument(b)
	if err != nil || d.XML.Body() == nil {
		return ""
	}
	for _, n := range revise.Revisions(d.XML.Body()) {
		if id, ok := model.Attr(n, "id"); ok && id != "" && strings.Trim(id, "0123456789") == "" {
			return id
		}
	}
	return ""
}

// TestSettleLaws runs revision.accept and revision.reject's laws on every law input: the
// change-mode law (track equals direct, refusals alike), and settling
// twice writes nothing the second time.
func TestSettleLaws(t *testing.T) {
	eachInput(t, lawInputs(t), func(t *testing.T, in string, _ int) {
		targets := []string{"all"}
		if id := firstID(in); id != "" {
			targets = append(targets, id)
		}
		for _, target := range targets {
			for _, action := range []string{"accept", "reject"} {
				settleSuite(target, action).Check(t, in)
			}
		}
		for _, accept := range []bool{true, false} {
			dir := t.TempDir()
			once, twice := filepath.Join(dir, "once.docx"), filepath.Join(dir, "twice.docx")
			if err := settleFile(laws.Private(t, in), once, accept); err != nil {
				t.Errorf("%s: %v", filepath.Base(in), err)
				continue
			}
			if err := settleFile(once, twice, accept); err != nil {
				t.Errorf("%s: %v", filepath.Base(in), err)
				continue
			}
			a, _ := os.ReadFile(once)  //nolint:errcheck,gosec // compared below
			b, _ := os.ReadFile(twice) //nolint:errcheck,gosec // compared below
			if !bytes.Equal(a, b) {
				t.Errorf("%s: settling all (accept %v) again wrote changes", filepath.Base(in), accept)
			}
		}
	})
}
