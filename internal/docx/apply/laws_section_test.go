package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// The section operations' laws (section.set_margins, section.set_layout,
// section.set_first_page): the five laws of internal/laws as a
// laws.Suite over the body's sections, in each change mode, and the
// change-mode law (direct equals track with its revisions accepted).

// sectionLaw is one section property as a law suite: how a section's
// w:sectPr shows it, and the operation that sets it.
type sectionLaw[V any] struct {
	name string
	// read is the property of a section's w:sectPr; false leaves the
	// section out (a value the op cannot write back as read).
	read func(sect *tree.Node) (V, bool)
	// guarded is set for an op that refuses a section with a pending
	// property change in mode (sectionTarget.guardChange): such sections
	// are left out.
	guarded func(mode string) bool
	show    func(V) string
	// op is the operation (JSON) that sets section addr to v.
	op   func(addr string, v V) string
	vary func(rng *rand.Rand, v V) V
}

// sectionOp is one section law operation: its JSON.
type sectionOp string

// sectPrsOf is view.SectPrNodes over a body element.
func sectPrsOf(body *tree.Node) []*tree.Node {
	var out []*tree.Node
	for _, c := range body.Children() {
		switch {
		case model.IsW(c, "p"), model.IsW(c, "tbl"):
			if s := model.FindSectPr(c); s != nil {
				out = append(out, s)
			}
		case model.IsW(c, "sectPr"):
			out = append(out, c)
		}
	}
	return out
}

// sectionOf returns the 1-based number of the section whose w:sectPr is
// e, or 0.
func sectionOf(e *tree.Node) int {
	if !model.IsW(e, "sectPr") {
		return 0
	}
	var body *tree.Node
	for p := e.Parent(); p != nil; p = p.Parent() {
		if model.IsW(p, "body") {
			body = p
		}
	}
	if body == nil {
		return 0
	}
	for i, s := range sectPrsOf(body) {
		if s == e {
			return i + 1
		}
	}
	return 0
}

// suite is the property's laws.Suite in mode.
func (l sectionLaw[V]) suite(mode string) *laws.Suite[V] {
	return &laws.Suite[V]{
		Name: l.name + " " + mode,
		Read: func(path string) ([]laws.Value[V], error) {
			b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
			if err != nil {
				return nil, err
			}
			d, err := model.OpenDocument(b)
			if err != nil {
				return nil, err
			}
			if d.XML.Body() == nil {
				return nil, errors.New("no body")
			}
			part := d.Pkg.DocumentPartName()
			var out []laws.Value[V]
			for i, s := range view.SectPrNodes(d.XML) {
				if c := model.ChildW(s, "sectPrChange"); c != nil && model.AttrOr(c, "author") != lawAuthor && l.guarded != nil && l.guarded(mode) {
					continue
				}
				if v, ok := l.read(s); ok {
					out = append(out, laws.Value[V]{Target: laws.Target{Part: part, Addr: "section:" + strconv.Itoa(i+1)}, V: v})
				}
			}
			return out, nil
		},
		Show: l.show,
		Set:  func(t laws.Target, v V) laws.Op { return sectionOp(l.op(t.Addr, v)) },
		Vary: func(rng *rand.Rand, _ laws.Target, v V) V { return l.vary(rng, v) },
		Apply: func(src, dst string, opList []laws.Op) error {
			parts := make([]string, len(opList))
			for i, o := range opList {
				parts[i] = string(o.(sectionOp))
			}
			opsJSON := "[" + strings.Join(parts, ",") + "]"
			if src != dst {
				return runPlan(src, dst, mode, opsJSON)
			}
			return runPlanIn(src, mode, opsJSON)
		},
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Units: docxedit.Units(),
		Owns: func(t laws.Target, e *tree.Node) bool {
			return "section:"+strconv.Itoa(sectionOf(e)) == t.Addr
		},
		Fail: func() laws.Op { return sectionOp(l.op("section:999", *new(V))) },
	}
}

// runPlanIn reviews and commits ops (a JSON array) in mode on the file at
// path, in place: a plan that changes nothing leaves it as it is.
func runPlanIn(path, mode, opsJSON string) error {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the law's private copy
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	text := []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"` + lawAuthor + `","change_mode":"` + mode + `","ops":` + opsJSON + `}`)
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

// sectionModeSuite is a section operation's change-mode law for one plan:
// direct equals track with its revisions accepted, and a refusal in one
// mode only is a documented guard (another author's pending section
// change in track mode, any pending change in direct mode).
func sectionModeSuite(name, opsJSON string) *laws.ModeSuite {
	s := settleSuite("all", "accept")
	s.Name = name
	s.Class = laws.AcceptAuthored
	// An op on a body's implicit final section adds the body's w:sectPr,
	// which Word cannot track: reject-all keeps it, empty, where the
	// input had none (as a new header part stays, KeepsAdditions). An
	// empty w:sectPr means what none does, so read's "implicit" marker
	// is not compared.
	s.Project = func(path string) (string, error) {
		p, err := projectNoSource(path)
		return strings.ReplaceAll(p, `"implicit":true,`, ""), err
	}
	s.Apply = func(src, dst, mode string) error { return runPlan(src, dst, mode, opsJSON) }
	s.Guarded = func(_ string, err error) bool {
		var r refusal
		return errors.As(err, &r) && strings.Contains(r.d.Message, "pending property change")
	}
	return s
}

// margins are a section's seven page margins in twips: top, right,
// bottom, left, header, footer, gutter.
type margins [7]int64

var marginNames = [7]string{"top", "right", "bottom", "left", "header", "footer", "gutter"}

func pts(twips int64) string { return strconv.FormatFloat(float64(twips)/20, 'f', -1, 64) }

var marginLaw = sectionLaw[margins]{
	name:    "section.set_margins",
	guarded: func(mode string) bool { return mode == laws.Track },
	read: func(sect *tree.Node) (margins, bool) {
		var m margins
		mar := model.ChildW(sect, "pgMar")
		if mar == nil {
			return m, false
		}
		for i, n := range marginNames {
			v, ok := model.Attr(mar, n)
			if !ok {
				return m, false
			}
			x, err := strconv.ParseInt(v, 10, 64)
			if err != nil || x < 0 || x > 31680 {
				return m, false
			}
			m[i] = x
		}
		return m, true
	},
	show: func(m margins) string {
		var b strings.Builder
		for i, n := range marginNames {
			b.WriteString(n + "=" + strconv.FormatInt(m[i], 10) + " ")
		}
		return b.String()
	},
	op: func(addr string, m margins) string {
		var b strings.Builder
		b.WriteString(`{"op":"section.set_margins","at":"` + addr + `"`)
		for i, n := range marginNames {
			b.WriteString(`,"` + n + `":` + pts(m[i]))
		}
		return b.String() + "}"
	},
	vary: func(rng *rand.Rand, m margins) margins {
		k := rng.IntN(7)
		if m[k] >= 31000 {
			m[k] -= 20 * int64(1+rng.IntN(20))
		} else {
			m[k] += 20 * int64(1+rng.IntN(20))
		}
		return m
	},
}

// sectionLawInputs are the law inputs and the write fixtures.
func sectionLawInputs(t *testing.T) []string { return deleteInputs(t) }

// storedSectionIDs returns a private copy of src with its repaired paragraph
// ids stored, as any writing plan stores them (contract section 6): the
// untargeted law then sees only what the operation changed.
func storedSectionIDs(t *testing.T, src string) string {
	t.Helper()
	path := laws.Private(t, src)
	var ws *docxedit.Workspace
	var openErr error
	tx, err := plan.Begin(path, func(pkg *opc.Package) plan.Workspace {
		ws, openErr = docxedit.New(pkg, docxedit.Options{Source: sha256.Sum256(mustRead(t, src))})
		if openErr != nil {
			return failing{openErr}
		}
		return ws
	})
	if err != nil || openErr != nil {
		return path
	}
	doc, err := ws.Tree(ws.DocumentPart())
	if err != nil {
		tx.Rollback()
		return path
	}
	var missing []*tree.Node
	for _, p := range (&model.XML{Doc: doc}).AllParagraphs() {
		if _, ok := p.AttrNS(docxedit.W14.URIs[0], "paraId"); !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		tx.Rollback()
		return path
	}
	if err := ws.Core().EnsureRootNamespace(ws.DocumentPart(), "w14", docxedit.W14.URIs[0], true); err != nil {
		tx.Rollback()
		return path
	}
	for _, p := range missing {
		id, ok := ws.ParaID(p)
		if !ok {
			continue
		}
		if err := ws.Core().Repair(ws.DocumentPart(), p, "w14:paraId", id.ID); err != nil {
			tx.Rollback()
			return path
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("%s: storing the repaired ids: %v", src, err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSectionMarginsLaws runs section.set_margins's five laws in both
// change modes and its change-mode law on every law input.
func TestSectionMarginsLaws(t *testing.T) {
	rep := reportInputs(t, sectionLawInputs(t), func(t *testing.T, in string, _ int, rep *laws.Report) {
		stored := storedSectionIDs(t, in)
		for _, mode := range []string{laws.Track, laws.Direct} {
			marginLaw.suite(mode).Check(t, stored, 1, rep)
		}
		sectionModeSuite("section.set_margins section 1", `[{"op":"section.set_margins","top":30,"left":90.5}]`).Check(t, in)
		sectionModeSuite("section.set_margins final", `[{"op":"section.set_margins","at":"section:final","gutter":12}]`).Check(t, in)
	})
	t.Log(rep)
}

// pageSize is a section's page width and height in twips and its
// orientation.
type pageSize struct {
	w, h      int64
	landscape bool
}

var layoutLaw = sectionLaw[pageSize]{
	name:    "section.set_layout",
	guarded: func(string) bool { return true },
	read: func(sect *tree.Node) (pageSize, bool) {
		var p pageSize
		sz := model.ChildW(sect, "pgSz")
		if sz == nil {
			return p, false
		}
		num := func(n *tree.Node, local string, def int64) (int64, bool) {
			if n == nil {
				return def, true
			}
			v, ok := model.Attr(n, local)
			if !ok {
				return def, true
			}
			x, err := strconv.ParseInt(v, 10, 64)
			return x, err == nil
		}
		w, ok1 := num(sz, "w", -1)
		h, ok2 := num(sz, "h", -1)
		o, _ := model.Attr(sz, "orient")
		p = pageSize{w, h, o == "landscape"}
		if !ok1 || !ok2 || w <= 0 || h <= 0 || w > 31680 || h > 31680 || (o != "" && o != "portrait" && o != "landscape") ||
			p.landscape && w < h || !p.landscape && w > h {
			return p, false
		}
		// The op refuses columns that do not fit: leave such sections
		// out, and those with columns of their own widths.
		mar := model.ChildW(sect, "pgMar")
		left, _ := num(mar, "left", 1440)
		right, _ := num(mar, "right", 1440)
		gutter, _ := num(mar, "gutter", 0)
		cols := model.ChildW(sect, "cols")
		if cols != nil && model.ChildW(cols, "col") != nil {
			return p, false
		}
		n, _ := num(cols, "num", 1)
		space, _ := num(cols, "space", 720)
		area := w - left - right - gutter
		return p, area > 0 && space*max(n-1, 0) < area
	},
	show: func(p pageSize) string {
		return strconv.FormatInt(p.w, 10) + "x" + strconv.FormatInt(p.h, 10) + " " + strconv.FormatBool(p.landscape)
	},
	op: func(addr string, p pageSize) string {
		o := "portrait"
		if p.landscape {
			o = "landscape"
		}
		if p.w == 0 {
			p.w, p.h = 12240, 15840
		}
		return `{"op":"section.set_layout","at":"` + addr + `","width":` + pts(p.w) + `,"height":` + pts(p.h) + `,"orientation":"` + o + `"}`
	},
	vary: func(rng *rand.Rand, p pageSize) pageSize {
		d := 20 * int64(1+rng.IntN(20))
		if p.landscape {
			p.w = min(p.w+d, 31680)
		} else {
			p.h = min(p.h+d, 31680)
		}
		return p
	},
}

// TestSectionLayoutLaws runs section.set_layout's five laws in both
// change modes and its change-mode law on every law input.
func TestSectionLayoutLaws(t *testing.T) {
	rep := reportInputs(t, sectionLawInputs(t), func(t *testing.T, in string, _ int, rep *laws.Report) {
		stored := storedSectionIDs(t, in)
		for _, mode := range []string{laws.Track, laws.Direct} {
			layoutLaw.suite(mode).Check(t, stored, 2, rep)
		}
		sectionModeSuite("section.set_layout section 1", `[{"op":"section.set_layout","orientation":"landscape","columns":2,"break_type":"continuous"}]`).Check(t, in)
		sectionModeSuite("section.set_layout final", `[{"op":"section.set_layout","at":"section:final","page_size":"a4","page_number_format":"lowerRoman","page_number_start":3}]`).Check(t, in)
	})
	t.Log(rep)
}

var firstPageLaw = sectionLaw[bool]{
	name:    "section.set_first_page",
	guarded: func(string) bool { return true },
	read: func(sect *tree.Node) (bool, bool) {
		t := model.ChildW(sect, "titlePg")
		if t == nil {
			return false, true
		}
		v, _ := model.Attr(t, "val")
		return v != "0" && v != "false" && v != "off", true
	},
	show: strconv.FormatBool,
	op: func(addr string, on bool) string {
		return `{"op":"section.set_first_page","at":"` + addr + `","enabled":` + strconv.FormatBool(on) + `}`
	},
	vary: func(_ *rand.Rand, on bool) bool { return !on },
}

// TestSectionFirstPageLaws runs section.set_first_page's five laws in
// both change modes and its change-mode law on every law input.
func TestSectionFirstPageLaws(t *testing.T) {
	rep := reportInputs(t, sectionLawInputs(t), func(t *testing.T, in string, _ int, rep *laws.Report) {
		stored := storedSectionIDs(t, in)
		for _, mode := range []string{laws.Track, laws.Direct} {
			firstPageLaw.suite(mode).Check(t, stored, 3, rep)
		}
		for _, on := range []string{"true", "false"} {
			sectionModeSuite("section.set_first_page "+on, `[{"op":"section.set_first_page","enabled":`+on+`}]`).Check(t, in)
			sectionModeSuite("section.set_first_page final "+on, `[{"op":"section.set_first_page","at":"section:final","enabled":`+on+`}]`).Check(t, in)
		}
	})
	t.Log(rep)
}
