package integrity

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// editRun is a plan's workspace over a test package, edited through the
// edit core as operations do.
type editRun struct {
	t     *testing.T
	input []byte
	pkg   *opc.Package
	ws    *core.Workspace
	part  string
	doc   *tree.Document
	log   *OpLog
	op    int
}

func startEdit(t *testing.T, input []byte) *editRun {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.docx")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pkg.Close(); err != nil {
			t.Error(err)
		}
	})
	w, err := docxedit.New(pkg, docxedit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := w.Tree(w.DocumentPart())
	if err != nil {
		t.Fatal(err)
	}
	return &editRun{t: t, input: input, pkg: pkg, ws: w.Core(), part: w.DocumentPart(), doc: doc, log: NewOpLog()}
}

// done ends one operation.
func (e *editRun) done() {
	e.t.Helper()
	if err := e.log.Record(e.op, e.ws); err != nil {
		e.t.Fatal(err)
	}
	e.op++
}

// body returns the w:body element, or nil.
func (e *editRun) body() *tree.Node {
	for _, c := range e.doc.Root().Elements() {
		if c.Name() == "w:body" {
			return c
		}
	}
	return nil
}

// para returns the body's i-th element.
func (e *editRun) para(i int) *tree.Node { return e.body().Elements()[i] }

// insert parses frag in the body and inserts it before ref, or at the end.
func (e *editRun) insert(frag string, ref *tree.Node) *tree.Node {
	e.t.Helper()
	body := e.body()
	nodes, err := e.ws.Parse(body, []byte(frag), nil)
	if err != nil || len(nodes) != 1 {
		e.t.Fatalf("parse %s: %v", frag, err)
	}
	pos := core.Into(body)
	if ref != nil {
		pos = core.Before(ref)
	}
	if err := e.ws.Insert(e.part, pos, nodes[0], nil); err != nil {
		e.t.Fatal(err)
	}
	return nodes[0]
}

// output returns the working copy as a package, every part's current
// bytes (in tests, without a plan transaction to commit it).
func (e *editRun) output() []byte {
	e.t.Helper()
	var parts []part
	for _, name := range e.pkg.Names() {
		pt, err := e.pkg.Part(name)
		if err != nil {
			e.t.Fatal(err)
		}
		b, err := pt.Bytes()
		if err != nil {
			e.t.Fatal(err)
		}
		parts = append(parts, part{strings.TrimPrefix(name, "/"), string(b)})
	}
	return zipParts(e.t, parts)
}

// gate runs GateEdit on the working copy.
func (e *editRun) gate() *GateFailure { return e.gateOn(e.output()) }

func (e *editRun) gateOn(out []byte) *GateFailure {
	e.t.Helper()
	gerr := GateEdit(e.input, out, e.ws, e.log)
	lazyAgrees(e.t, "gate", e.input, out, e.ws, e.log)
	if gerr == nil {
		return nil
	}
	var g *GateFailure
	if !asGateFailure(gerr, &g) {
		e.t.Fatalf("GateEdit = %v, want a *GateFailure", gerr)
	}
	return g
}

// lazyAgrees checks that the gate, which checks the input only when the
// output has findings, gives the eager gate's answer: the same findings
// (instances compared in sorted order: some checks find them in map
// order) and the same refusal.
func lazyAgrees(t *testing.T, label string, before, after []byte, ws Workspace, log *OpLog) {
	t.Helper()
	lf, lerr := GateEditReport(before, after, ws, log, opc.DefaultLimits())
	ef, eerr := GateEditWith(before, after, ws, log, GateOptions{Eager: true})
	if got, want := sortedInstances(lf), sortedInstances(ef); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: lazy gate findings %v, eager %v", label, got, want)
	}
	if lg, eg := gateFailureOf(lerr), gateFailureOf(eerr); !reflect.DeepEqual(lg, eg) || (lg == nil) != (lerr == nil) || (eg == nil) != (eerr == nil) {
		t.Errorf("%s: lazy gate %v, eager %v", label, lerr, eerr)
	}
}

// gateFailureOf is the gate's refusal in err, or nil.
func gateFailureOf(err error) *GateFailure {
	var g *GateFailure
	if errors.As(err, &g) {
		return g
	}
	return nil
}

func sortedInstances(f *Findings) []Instance {
	if f == nil {
		return nil
	}
	out := f.Instances()
	slices.SortFunc(out, func(a, b Instance) int {
		return cmp.Or(compareFindings(a.Finding, b.Finding), slices.Compare(a.Nodes, b.Nodes))
	})
	return out
}

const (
	badJc    = `<w:p w14:paraId="22222222"><w:pPr><w:jc w:val="nowhere"/></w:pPr><w:r><w:t>Bad.</w:t></w:r></w:p>`
	cleanTwo = `<w:p w14:paraId="33333333"><w:r><w:t>Clean.</w:t></w:r></w:p>`
	jcFault  = "schema: w:jc/@w:val is not a valid ST_Jc"
)

func locatedMessages(g *GateFailure) []string {
	var out []string
	for _, i := range g.Located {
		out = append(out, i.String())
	}
	return out
}

// A bad edit is caught although the part already has its kind, and even
// when the same edit removes the existing instance (the count stays the
// same, so Gate passes it).
func TestGateEditCatchesAKindTheInputHas(t *testing.T) {
	input := testPackage(t, paragraph+badJc+cleanTwo, "")
	e := startEdit(t, input)
	e.insert(`<w:p><w:pPr><w:jc w:val="sideways"/></w:pPr></w:p>`, nil)
	e.done()
	g := e.gate()
	if g == nil {
		t.Fatal("GateEdit passed a new invalid w:jc")
	}
	want := []string{"word/document.xml: new w:jc: " + jcFault}
	if got := locatedMessages(g); !slices.Equal(got, want) {
		t.Fatalf("located %q, want %q", got, want)
	}
	if g.Op() != 0 {
		t.Errorf("Op() = %d, want 0", g.Op())
	}
	if !strings.Contains(g.Summary(), "new w:jc") {
		t.Errorf("summary %q does not name the element", g.Summary())
	}

	e = startEdit(t, input)
	if err := e.ws.Delete(e.part, e.para(1)); err != nil {
		t.Fatal(err)
	}
	e.done()
	e.insert(`<w:p><w:pPr><w:jc w:val="sideways"/></w:pPr></w:p>`, nil)
	e.done()
	if g := e.gate(); g == nil || g.Op() != 1 {
		t.Fatalf("delete one bad paragraph, add another: GateEdit = %v, want a refusal at op 1", g)
	}
}

// A freshly written element identical to an existing faulty one is no
// copy of it: only the edit core's provenance makes a copy.
func TestGateEditRefusesAnIdenticalFreshFault(t *testing.T) {
	input := testPackage(t, paragraph+badJc+cleanTwo, "")
	e := startEdit(t, input)
	e.insert(`<w:p><w:pPr><w:jc w:val="nowhere"/></w:pPr><w:r><w:t>Bad.</w:t></w:r></w:p>`, nil)
	e.done()
	g := e.gate()
	if g == nil {
		t.Fatal("GateEdit passed a fresh w:jc identical to an existing faulty one")
	}
	if want := []string{"word/document.xml: new w:jc: " + jcFault}; !slices.Equal(locatedMessages(g), want) {
		t.Fatalf("located %q, want %q", locatedMessages(g), want)
	}
	if g.Op() != 0 {
		t.Errorf("Op() = %d, want 0", g.Op())
	}
}

// Splitting a faulty run gives a new half that carries the run's own
// fault through its provenance.
func TestGateEditSplitRunCarriesItsFault(t *testing.T) {
	odd := `<w:p w14:paraId="22222222"><w:r w:bogus="1"><w:t>ab</w:t><w:t>cd</w:t></w:r></w:p>`
	input := testPackage(t, paragraph+odd, "")
	if f := Check(input).Kinds(); len(f) == 0 {
		t.Fatal("fixture lacks its fault")
	}
	e := startEdit(t, input)
	p := e.para(1)
	h, err := e.ws.Range(e.part, "paragraph", p, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.SplitAt(p.Elements()[0], 1, nil); err != nil {
		t.Fatal(err)
	}
	e.done()
	k := Finding{"word/document.xml", "schema: w:r has unexpected attribute w:bogus"}
	if n := Check(e.output()).Count(k); n != 2 {
		t.Fatalf("%d instances of %q after the split, want 2 (both halves)", n, k.Message)
	}
	if g := e.gate(); g != nil {
		t.Fatalf("splitting a faulty run refused: %s", g.Summary())
	}
}

// Pre-existing faults never block: not in a changed paragraph, not
// elsewhere in the part, not when the faulty paragraph moves.
func TestGateEditKeepsPreexistingFaults(t *testing.T) {
	input := testPackage(t, paragraph+badJc+cleanTwo, "")

	e := startEdit(t, input)
	h := e.ws.Target(e.part, "paragraph", []*tree.Node{e.para(1)})
	if err := h.SetAttr(nil, "w:rsidR", "00AB12CD"); err != nil {
		t.Fatal(err)
	}
	e.done()
	e.insert(`<w:p w14:paraId="44444444"><w:r><w:t>New.</w:t></w:r></w:p>`, e.para(0))
	e.done()
	if g := e.gate(); g != nil {
		t.Fatalf("edit beside a pre-existing fault refused: %s", g.Summary())
	}

	e = startEdit(t, input)
	if err := e.ws.Move(e.part, e.para(1), core.Into(e.body())); err != nil {
		t.Fatal(err)
	}
	e.done()
	if g := e.gate(); g != nil {
		t.Fatalf("moving a faulty paragraph refused: %s", g.Summary())
	}
}

// Moved content keeps its findings, and a created element that copies
// an existing one (a run split in two copies its start tag and
// properties) carries the copied faults.
func TestGateEditMovedAndCopiedContentIsNotNew(t *testing.T) {
	outOfOrder := `<w:p w14:paraId="22222222"><w:r><w:t>x</w:t><w:rPr><w:b/></w:rPr></w:r></w:p>`
	input := testPackage(t, paragraph+outOfOrder+cleanTwo, "")
	if !Check(input).Has(Finding{"word/document.xml", "schema: w:rPr is out of order in w:r"}) {
		t.Fatalf("fixture lacks its fault: %q", messages(input))
	}

	// The faulty run moves into a new paragraph.
	e := startEdit(t, input)
	run := e.para(1).Elements()[0]
	np := e.insert(`<w:p/>`, nil)
	if err := e.ws.Move(e.part, run, core.Into(np)); err != nil {
		t.Fatal(err)
	}
	e.done()
	if g := e.gate(); g != nil {
		t.Fatalf("moved faulty run refused: %s", g.Summary())
	}

	// A copy of the faulty paragraph is inserted.
	e = startEdit(t, input)
	cp, err := e.ws.Copy(e.para(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ws.Insert(e.part, core.Into(e.body()), cp, []core.NodeAttr{{Node: cp, Name: "w14:paraId", Value: "55555555"}}); err != nil {
		t.Fatal(err)
	}
	e.done()
	if g := e.gate(); g != nil {
		t.Fatalf("copied faulty paragraph refused: %s", g.Summary())
	}

	// But a new run with the same kind of fault, not a copy, is new.
	e = startEdit(t, input)
	e.insert(`<w:p><w:r><w:t>y</w:t><w:rPr><w:i/></w:rPr></w:r></w:p>`, nil)
	e.done()
	if g := e.gate(); g == nil {
		t.Fatal("a new out-of-order w:rPr passed")
	}
}

// A fault in an unchanged element that a removal elsewhere caused is
// refused: the edit broke its pairing.
func TestGateEditRefusesABrokenPairing(t *testing.T) {
	start := `<w:p w14:paraId="22222222"><w:bookmarkStart w:id="1" w:name="b"/><w:r><w:t>a</w:t></w:r></w:p>`
	end := `<w:p w14:paraId="33333333"><w:r><w:t>b</w:t></w:r><w:bookmarkEnd w:id="1"/></w:p>`
	input := testPackage(t, start+end, "")
	e := startEdit(t, input)
	if err := e.ws.Delete(e.part, e.para(1)); err != nil {
		t.Fatal(err)
	}
	e.done()
	g := e.gate()
	if g == nil {
		t.Fatal("removing a bookmarkEnd passed")
	}
	want := []string{"word/document.xml: w:bookmarkStart in paragraph 22222222: integrity: bookmarkStart without a bookmarkEnd"}
	if got := locatedMessages(g); !slices.Equal(got, want) {
		t.Fatalf("located %q, want %q", got, want)
	}
	if g.Op() != 0 {
		// No operation declared the start's paragraph; op 0 declared the
		// part.
		t.Errorf("Op() = %d, want 0", g.Op())
	}
}

// A created element that duplicates an existing id is refused at the
// created one only.
func TestGateEditRefusesADuplicateID(t *testing.T) {
	input := testPackage(t, paragraph+cleanTwo, "")
	e := startEdit(t, input)
	e.insert(`<w:p w14:paraId="11111111"/>`, nil)
	e.done()
	g := e.gate()
	if g == nil {
		t.Fatal("a duplicate w14:paraId passed")
	}
	want := []string{"word/document.xml: new w:p 11111111: integrity: duplicate w14:paraId"}
	if got := locatedMessages(g); !slices.Equal(got, want) {
		t.Fatalf("located %q, want %q", got, want)
	}
}

// GateEdit agrees with Gate when the plan wrote nothing through its trees
// it could locate: the findings are compared by count.
func TestGateEditFallsBackToCounts(t *testing.T) {
	input := testPackage(t, paragraph, "")
	e := startEdit(t, input)
	// The output's document part is not what the working tree holds (as
	// for a part written as bytes): its faults are counted.
	out := testPackage(t, paragraph+`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r></w:p>`, "")
	if g := e.gateOn(out); g == nil || len(g.Located) != 1 || g.Located[0].Node != "" {
		t.Fatalf("a replaced part's new fault: %v, want one unlocated refusal", g)
	}
	if g := e.gateOn(input); g != nil {
		t.Fatalf("no change refused: %s", g.Summary())
	}
}

// Corpus documents with faults still save after an edit that inserts a
// clean paragraph and changes every paragraph's start tag (moves are
// left to the unit tests: moving a paragraph can split a field that spans
// paragraphs, a fault the edit does introduce).
func TestGateEditCorpusPreexisting(t *testing.T) {
	dir := docxCorpus(t, docxdriverRepo(t))
	if dir == "" {
		t.Skip("no corpus")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no corpus")
	}
	// Document by document in parallel; each reports whether it was
	// edited.
	tried := 0
	for _, r := range corpustest.Run(t, corpustest.Files(docxFiles(t, dir), dir), func(t *testing.T, f corpustest.File, edited *bool) {
		path := f.Path
		input, err := os.ReadFile(path) //nolint:gosec // G304: corpus path from the environment
		if err != nil {
			t.Fatal(err)
		}
		if Check(input).Empty() {
			return
		}
		pkg, err := opc.OpenBytes(input, opc.DefaultLimits())
		if err != nil {
			return
		}
		if _, err := docxedit.New(pkg, docxedit.Options{}); err != nil {
			return // the editor refuses it before any gate
		}
		e := startEdit(t, input)
		body := e.body()
		if body == nil || len(body.Elements()) == 0 {
			return
		}
		e.insert(`<w:p><w:r><w:t>Inserted.</w:t></w:r></w:p>`, body.Elements()[0])
		e.done()
		var paras []*tree.Node
		body.Walk(func(n *tree.Node) bool {
			if n.Kind() == tree.ElementNode && n.Name() == "w:p" && !e.ws.Created(n) {
				paras = append(paras, n)
			}
			return true
		})
		for _, p := range paras {
			if err := e.ws.Target(e.part, "paragraph", []*tree.Node{p}).SetAttr(nil, "w:rsidR", "00AB12CD"); err != nil {
				t.Fatal(err)
			}
		}
		e.done()
		*edited = true
		if g := e.gate(); g != nil {
			t.Errorf("%s: refused: %s", filepath.Base(path), g.Summary())
		}
	}) {
		if r.Value {
			tried++
		}
	}
	t.Logf("%d corpus documents with findings edited", tried)
}
