package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// The table operations' laws: the five write laws (laws.Suite) for one
// property of each format operation, and the change-mode law
// (laws.ModeSuite) of every table operation, on the authored fixtures and,
// with DOCX_CORPUS, on the corpus documents that have tables.

// tableLawLimit bounds the corpus documents the table laws run on (the
// smallest first), so that the suite stays within a quick-check budget.
const tableLawLimit = 120

// tableInputs are the law inputs with a table in their body: the
// authored fixtures and the smallest corpus documents with a table.
func tableInputs(t *testing.T) []string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	var ins []string
	for _, f := range []string{"mixed.docx", "sections.docx"} {
		ins = append(ins, filepath.Join(root, "tools", "oracle", "testdata", "write", f))
	}
	ins = append(ins, filepath.Join(root, "testdata", "docx", "rich.docx"), writeDocx(t, gridBody), writeDocx(t, tableBody))
	dir := os.Getenv("DOCX_CORPUS")
	if dir == "" {
		return ins
	}
	type sized struct {
		path string
		size int64
	}
	var found []sized
	for _, p := range scanCorpus(t, "tables", dir, func(b []byte) bool {
		doc, err := model.OpenDocument(b)
		return err == nil && doc.XML.Body() != nil &&
			len(model.Descendants(doc.XML.Body(), func(n *tree.Node) bool { return model.IsW(n, "tbl") })) > 0
	}) {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, sized{p, info.Size()})
	}
	// Smallest first, then by name: a stable sample.
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && (found[j].size < found[j-1].size || found[j].size == found[j-1].size && found[j].path < found[j-1].path); j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	for i, f := range found {
		if i >= tableLawLimit {
			break
		}
		ins = append(ins, f.path)
	}
	return ins
}

// tableNodes are the body tables of a document, as the addresses count
// them.
func tableNodes(x *model.XML) []*tree.Node {
	if x.Body() == nil {
		return nil
	}
	return model.Descendants(x.Body(), func(n *tree.Node) bool { return model.IsW(n, "tbl") })
}

func elementsW(n *tree.Node, local string) []*tree.Node {
	var out []*tree.Node
	for _, c := range n.Elements() {
		if model.IsW(c, local) {
			out = append(out, c)
		}
	}
	return out
}

// openXML reads the main document of the file at path.
func openXML(path string) (*model.XML, error) {
	x, _, err := openMain(path)
	return x, err
}

// openMain reads the main document of the file at path and its part
// name.
func openMain(path string) (*model.XML, string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return nil, "", err
	}
	d, err := model.OpenDocument(b)
	if err != nil {
		return nil, "", err
	}
	return d.XML, d.Pkg.DocumentPartName(), nil
}

// applyInPlace applies ops (JSON op objects) as one plan to src, writing
// dst (src itself when they are the same).
func applyInPlace(src, dst, mode string, ops []string) error {
	path := src
	if dst != src {
		if err := laws.CopyFile(src, dst); err != nil {
			return err
		}
		path = dst
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	text := []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"` + lawAuthor + `","change_mode":"` + mode + `","ops":[` + strings.Join(ops, ",") + `]}`)
	r, err := runText(path, text, Options{View: viewOpt})
	if err != nil {
		return err
	}
	if r.Outcome == Rejected {
		return refusal{r.Diagnostic}
	}
	c, err := runText(path, text, Options{Commit: r.PreviewKey, Now: "2026-01-01T00:00:00Z", View: viewOpt})
	if err != nil {
		return err
	}
	if c.Outcome == Rejected {
		return refusal{c.Diagnostic}
	}
	return sameHazards(c)
}

func isRefusal(err error) bool {
	var r refusal
	return errors.As(err, &r)
}

// propTarget is one table, row or cell a format law writes, and how to
// read its property.
type propTarget struct {
	at   string
	node *tree.Node
}

// tableUnits lists every table, row and logical cell of the main body by
// address.
func tableUnits(x *model.XML) (tables, rows, cells []propTarget) {
	for ti, tbl := range tableNodes(x) {
		ta := "table:" + strconv.Itoa(ti+1)
		tables = append(tables, propTarget{ta, tbl})
		for ri, tr := range elementsW(tbl, "tr") {
			ra := ta + "/row:" + strconv.Itoa(ri+1)
			rows = append(rows, propTarget{ra, tr})
			for ci, tc := range elementsW(tr, "tc") {
				if pr := model.ChildW(tc, "tcPr"); pr != nil {
					if vm := model.ChildW(pr, "vMerge"); vm != nil || model.ChildW(pr, "hMerge") != nil {
						continue // merged cells: their own law is the grid's
					}
				}
				cells = append(cells, propTarget{ra + "/cell:" + strconv.Itoa(ci+1), tc})
			}
		}
	}
	return tables, rows, cells
}

// unitAddress is the table address of a table, row or cell unit e in its
// document, or "".
func unitAddress(e *tree.Node) string {
	root := e
	for root.Parent() != nil {
		root = root.Parent()
	}
	var body *tree.Node
	root.Walk(func(n *tree.Node) bool {
		if body != nil {
			return false
		}
		if model.IsW(n, "body") {
			body = n
			return false
		}
		return true
	})
	if body == nil {
		return ""
	}
	ti := 0
	out := ""
	body.Walk(func(t *tree.Node) bool {
		if out != "" {
			return false
		}
		if !model.IsW(t, "tbl") {
			return true
		}
		ti++
		ta := "table:" + strconv.Itoa(ti)
		if t == e {
			out = ta
			return false
		}
		for ri, tr := range elementsW(t, "tr") {
			ra := ta + "/row:" + strconv.Itoa(ri+1)
			if tr == e {
				out = ra
				return false
			}
			for ci, tc := range elementsW(tr, "tc") {
				if tc == e {
					out = ra + "/cell:" + strconv.Itoa(ci+1)
					return false
				}
			}
		}
		return true
	})
	return out
}

// formatLaw is one property of a format operation under the five laws.
type formatLaw struct {
	name string
	// pick selects the targets of the op's kind.
	pick func(tables, rows, cells []propTarget) []propTarget
	// read returns the property's value, and false when the op cannot
	// write it back exactly (the target is then left out).
	read func(n *tree.Node) (string, bool)
	// set is the op that gives target at value v.
	set func(at, v string) string
	// vary returns another value.
	vary func(rng *rand.Rand) string
}

func (l formatLaw) suite() *laws.Suite[string] {
	return &laws.Suite[string]{
		Name: l.name,
		Read: func(path string) ([]laws.Value[string], error) {
			x, part, err := openMain(path)
			if err != nil {
				return nil, err
			}
			tables, rows, cells := tableUnits(x)
			var out []laws.Value[string]
			for _, t := range l.pick(tables, rows, cells) {
				if v, ok := l.read(t.node); ok {
					out = append(out, laws.Value[string]{Target: laws.Target{Part: part, Addr: t.at}, V: v})
				}
			}
			return out, nil
		},
		Show: func(v string) string { return v },
		Set:  func(t laws.Target, v string) laws.Op { return l.set(t.Addr, v) },
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			for {
				if v := l.vary(rng); v != cur {
					return v
				}
			}
		},
		Apply: func(src, dst string, ops []laws.Op) error {
			var list []string
			for _, o := range ops {
				list = append(list, o.(string))
			}
			return applyInPlace(src, dst, laws.Direct, list)
		},
		Refused: isRefusal,
		Units:   docxedit.Units(),
		Owns: func(t laws.Target, e *tree.Node) bool {
			a := unitAddress(e)
			return a != "" && a == t.Addr
		},
		Fail: func() laws.Op {
			return `{"op":"cell.format","at":"table:9999/row:1/cell:1","format":{"fill":"#000000"}}`
		},
		Budget: 6,
	}
}

// child returns container's child w:tag of n's property container, or
// nil.
func propChild(n *tree.Node, container, tag string) *tree.Node {
	c := model.ChildW(n, container)
	if c == nil {
		return nil
	}
	return model.ChildW(c, tag)
}

var formatLaws = []formatLaw{
	{
		name: "cell.format fill",
		pick: func(_, _, cells []propTarget) []propTarget { return cells },
		read: func(n *tree.Node) (string, bool) {
			if propChild(n, "tcPr", "tcPrChange") != nil {
				return "", false
			}
			shd := propChild(n, "tcPr", "shd")
			if shd == nil {
				return "", true
			}
			fill, ok := model.Attr(shd, "fill")
			if !ok || len(fill) != 6 || strings.ToUpper(fill) != fill || model.AttrOr(shd, "val") == "" {
				return "", false
			}
			for _, a := range []string{"themeFill", "themeFillTint", "themeFillShade"} {
				if _, ok := model.Attr(shd, a); ok {
					return "", false
				}
			}
			return "#" + fill, true
		},
		set: func(at, v string) string {
			if v == "" {
				return `{"op":"cell.format","at":"` + at + `","format":{"clear":["fill"]}}`
			}
			return `{"op":"cell.format","at":"` + at + `","format":{"fill":"` + v + `"}}`
		},
		vary: func(rng *rand.Rand) string { return fmt.Sprintf("#%06X", rng.IntN(1<<24)) },
	},
	{
		name: "row.format height",
		pick: func(_, rows, _ []propTarget) []propTarget { return rows },
		read: func(n *tree.Node) (string, bool) {
			if model.RowRevision(n) != nil || propChild(n, "trPr", "trPrChange") != nil {
				return "", false
			}
			h := propChild(n, "trPr", "trHeight")
			if h == nil {
				return "", true
			}
			v, ok := model.ParseU32(model.AttrOr(h, "val"))
			if !ok || v%20 != 0 {
				return "", false
			}
			return strconv.Itoa(int(v / 20)), true
		},
		set: func(at, v string) string {
			if v == "" {
				return `{"op":"row.format","at":"` + at + `","format":{"clear":["height"]}}`
			}
			return `{"op":"row.format","at":"` + at + `","format":{"height":` + v + `}}`
		},
		vary: func(rng *rand.Rand) string { return strconv.Itoa(6 + rng.IntN(60)) },
	},
	{
		name: "table.format width",
		pick: func(tables, _, _ []propTarget) []propTarget { return tables },
		read: func(n *tree.Node) (string, bool) {
			if propChild(n, "tblPr", "tblPrChange") != nil {
				return "", false
			}
			w := propChild(n, "tblPr", "tblW")
			if w == nil {
				return "", true
			}
			if model.AttrOr(w, "type") == "auto" && model.AttrOr(w, "w") == "0" {
				return "auto", true
			}
			if model.AttrOr(w, "type") != "dxa" {
				return "", false
			}
			v, ok := model.ParseU32(model.AttrOr(w, "w"))
			if !ok || v%20 != 0 {
				return "", false
			}
			return strconv.Itoa(int(v / 20)), true
		},
		set: func(at, v string) string {
			if v == "" {
				return `{"op":"table.format","at":"` + at + `","format":{"clear":["width"]}}`
			}
			if v == "auto" {
				return `{"op":"table.format","at":"` + at + `","format":{"width":{"unit":"auto"}}}`
			}
			return `{"op":"table.format","at":"` + at + `","format":{"width":{"unit":"points","value":` + v + `}}}`
		},
		vary: func(rng *rand.Rand) string { return strconv.Itoa(100 + rng.IntN(300)) },
	},
}

// storedTableIDs returns a copy of in that a first save has given its stored
// paragraph ids (contract section 6: any write stores the ids the tool
// assigned, the one change outside an op's targets), so that the
// untargeted law sees only what an op changes: a direct cell.format of the
// first cell, or in itself when that is refused.
func storedTableIDs(t *testing.T, in string) string {
	t.Helper()
	x, err := openXML(in)
	if err != nil {
		return in
	}
	tables, rows, cells := tableUnits(x)
	var tries []string
	for _, c := range cells {
		tries = append(tries, `{"op":"cell.format","at":"`+c.at+`","format":{"fill":"#123456"}}`)
	}
	for _, r := range rows {
		tries = append(tries, `{"op":"row.format","at":"`+r.at+`","format":{"height":17}}`)
	}
	for _, tb := range tables {
		tries = append(tries, `{"op":"table.format","at":"`+tb.at+`","format":{"layout":"fixed"}}`)
	}
	out := filepath.Join(t.TempDir(), filepath.Base(in))
	for _, op := range tries {
		if err := applyInPlace(in, out, laws.Direct, []string{op}); err == nil {
			return out
		}
	}
	return in
}

// TestTableFormatLaws runs the five write laws of a property of each
// format operation on the table law inputs.
func TestTableFormatLaws(t *testing.T) {
	t.Parallel() // beside the other law suites (corpus_test.go)
	// Input by input in parallel, each with a report per law of its own,
	// summed law by law in input order.
	reps := make([]laws.Report, len(formatLaws))
	for _, r := range corpustest.Run(t, inputFiles(t, tableInputs(t)), func(t *testing.T, f corpustest.File, own *[]laws.Report) {
		*own = make([]laws.Report, len(formatLaws))
		in := storedTableIDs(t, f.Path)
		for k, l := range formatLaws {
			l.suite().Check(t, in, uint64(f.Index+1), &(*own)[k])
		}
	}) {
		for k := range r.Value {
			reps[k].Add(r.Value[k])
		}
	}
	for k, l := range formatLaws {
		t.Logf("%s: %s", l.name, reps[k])
	}
}

// tableModeSuite is a table operation's change-mode law for one plan.
func tableModeSuite(name string, class laws.ModeClass, ops ...string) *laws.ModeSuite {
	s := settleSuite("all", "accept")
	s.Name = name
	s.Class = class
	s.Project = projectNoSource
	s.Apply = func(src, dst, mode string) error { return applyInPlace(src, dst, mode, ops) }
	return s
}

// firstTargets returns the first table, row and logical cell address of
// the document at path ("" when it has none).
func firstTargets(t *testing.T, path string) (table, row, cell string) {
	t.Helper()
	x, err := openXML(path)
	if err != nil {
		return "", "", ""
	}
	tables, rows, cells := tableUnits(x)
	if len(tables) > 0 {
		table = tables[0].at
	}
	if len(rows) > 0 {
		row = rows[0].at
	}
	if len(cells) > 0 {
		cell = cells[0].at
	}
	return table, row, cell
}

// TestTableFormatModeLaws runs the change-mode law of the three format
// operations on the first table, row and cell of every table law input.
func TestTableFormatModeLaws(t *testing.T) {
	eachInput(t, tableInputs(t), func(t *testing.T, in string, _ int) {
		table, row, cell := firstTargets(t, in)
		if table != "" {
			tableModeSuite("table.format", laws.AcceptAuthored,
				`{"op":"table.format","at":"`+table+`","format":{"width":{"unit":"percent","value":80},"alignment":"center","borders":{"top":{"style":"single","color":"#FF0000","width":1.5}},"padding":{"left":4}}}`).Check(t, in)
		}
		if row != "" {
			tableModeSuite("row.format", laws.AcceptAuthored,
				`{"op":"row.format","at":"`+row+`","format":{"height":24,"height_rule":"at_least","repeating_header":true,"allow_split":false}}`).Check(t, in)
		}
		if cell != "" {
			tableModeSuite("cell.format", laws.AcceptAuthored,
				`{"op":"cell.format","at":"`+cell+`","format":{"fill":"#DDEEFF","vertical_alignment":"center","borders":{"bottom":{"style":"double","width":0.75}},"width":{"unit":"points","value":90}}}`).Check(t, in)
		}
	})
}

// firstMerged returns the address of the first merged logical cell (a
// gridSpan above one or a vertical merge's restart) of the document, or
// "".
func firstMerged(x *model.XML) string {
	for ti, tbl := range tableNodes(x) {
		for ri, tr := range elementsW(tbl, "tr") {
			for ci, tc := range elementsW(tr, "tc") {
				pr := model.ChildW(tc, "tcPr")
				if pr == nil {
					continue
				}
				gs, vm := model.ChildW(pr, "gridSpan"), model.ChildW(pr, "vMerge")
				if gs != nil && model.AttrOr(gs, "val") != "1" || vm != nil && model.AttrOr(vm, "val") == "restart" {
					return "table:" + strconv.Itoa(ti+1) + "/row:" + strconv.Itoa(ri+1) + "/cell:" + strconv.Itoa(ci+1)
				}
			}
		}
	}
	return ""
}

// TestTableStructureModeLaws runs the change-mode law of every structural
// table operation on the first table of every table law input: the row
// edits and table.delete are tracked (direct is accept-all of the
// tracked output), the column, merge and split edits direct only.
func TestTableStructureModeLaws(t *testing.T) {
	eachInput(t, tableInputs(t), func(t *testing.T, in string, _ int) {
		x, err := openXML(in)
		if err != nil {
			return
		}
		tables, _, _ := tableUnits(x)
		if len(tables) == 0 {
			return
		}
		tb := tables[0].at
		suites := []*laws.ModeSuite{
			tableModeSuite("row.insert", laws.AcceptAuthored, `{"op":"row.insert","at":"`+tb+`/row:1","position":"after","count":2,"as":"r"}`),
			tableModeSuite("row.delete", laws.AcceptAuthored, `{"op":"row.delete","at":"`+tb+`/row:1","count":1}`),
			tableModeSuite("row.insert then row.delete", laws.AcceptAuthored,
				`{"op":"row.insert","at":"`+tb+`/row:1","position":"before","count":1}`, `{"op":"row.delete","at":"`+tb+`/row:1","count":1}`),
			tableModeSuite("table.delete", laws.AcceptAuthored, `{"op":"table.delete","at":"`+tb+`"}`),
			tableModeSuite("column.insert", laws.OnlyDirect, `{"op":"column.insert","at":"`+tb+`/column:1","position":"after","count":1,"as":"c"}`),
			tableModeSuite("column.delete", laws.OnlyDirect, `{"op":"column.delete","at":"`+tb+`/column:1","count":1}`),
			tableModeSuite("cell.merge", laws.OnlyDirect, `{"op":"cell.merge","at":"`+tb+`/row:1/cell:1","row_span":2,"column_span":2}`),
		}
		if m := firstMerged(x); m != "" {
			suites = append(suites, tableModeSuite("cell.split", laws.OnlyDirect, `{"op":"cell.split","at":"`+m+`"}`))
		}
		for _, s := range suites {
			s.Check(t, in)
		}
	})
}

// TestTableStructureInverse is the sequencing law of the insertions: rows
// or columns inserted and deleted again in one plan write nothing.
func TestTableStructureInverse(t *testing.T) {
	checked := 0
	defer func() { t.Logf("%d inverse plans checked", checked) }()
	t.Parallel() // beside the other law suites (corpus_test.go)
	// Input by input in parallel, each counting its own plans.
	for _, r := range corpustest.Run(t, inputFiles(t, tableInputs(t)), func(t *testing.T, f corpustest.File, checked *int) {
		in := f.Path
		x, err := openXML(in)
		if err != nil {
			return
		}
		tables, _, _ := tableUnits(x)
		if len(tables) == 0 {
			return
		}
		tb := tables[0].at
		for _, plan := range [][]string{
			{`{"op":"row.insert","at":"` + tb + `/row:1","position":"after","count":2}`, `{"op":"row.delete","at":"` + tb + `/row:2","count":2}`},
			{`{"op":"column.insert","at":"` + tb + `/column:1","position":"before","count":1}`, `{"op":"column.delete","at":"` + tb + `/column:1","count":1}`},
		} {
			for _, mode := range []string{laws.Track, laws.Direct} {
				if mode == laws.Track && strings.Contains(plan[0], "column") {
					continue
				}
				path := laws.Private(t, in)
				before, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
				if err != nil {
					t.Fatal(err)
				}
				err = applyInPlace(path, path, mode, plan)
				if isRefusal(err) {
					continue
				}
				if err != nil {
					t.Errorf("%s %s: %v", filepath.Base(in), mode, err)
					continue
				}
				after, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
				if err != nil {
					t.Fatal(err)
				}
				*checked++
				if string(after) != string(before) {
					t.Errorf("%s %s: %s then its inverse wrote the file", filepath.Base(in), mode, plan[0])
				}
			}
		}
	}) {
		checked += r.Value
	}
}

// TestInsertTableModeLaws runs table.insert's change-mode law (direct is
// accept-all of the tracked output, reject-all leaves the input) after the
// first and before the last body paragraph of every table law input, with
// cell content and a later operation on a cell alias.
func TestInsertTableModeLaws(t *testing.T) {
	eachInput(t, append(lawInputs(t), tableInputs(t)...), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) == 0 {
			return
		}
		tableModeSuite("table.insert", laws.AcceptAuthored,
			`{"op":"table.insert","at":"`+ids[0]+`","position":"after","rows":2,"columns":3,"cells":[["one","<b>two</b>"],["","","<i>six</i>"]],"as":"t"}`,
			`{"op":"paragraph.format","at":"$t_r1c2","alignment":"center"}`).Check(t, in)
		tableModeSuite("table.insert before", laws.AcceptAuthored,
			`{"op":"table.insert","at":"`+ids[len(ids)-1]+`","position":"before","rows":1,"columns":1}`).Check(t, in)
	})
}
