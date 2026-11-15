package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// settleOps is one revision.accept or revision.reject op as a plan's ops:
// all with author, or the paragraphs ids (with author when not "").
func settleOps(action, author string, ids ...string) string {
	op := map[string]any{"op": "revision." + action}
	if ids == nil {
		op["all"] = true
	} else {
		op["at"] = ids
	}
	if author != "" {
		op["author"] = author
	}
	b, err := json.Marshal([]any{op})
	if err != nil {
		panic(err) // a map of strings, bools and string slices
	}
	return string(b)
}

// settleBySuite is the change-mode law of one settle-by plan: it writes
// no revision, so both modes give one result.
func settleBySuite(name, opsJSON string) *laws.ModeSuite {
	s := settleSuite("all", "accept")
	s.Name = name
	s.Project = projectNoSource
	s.Apply = func(src, dst, mode string) error { return runPlan(src, dst, mode, opsJSON) }
	return s
}

// acceptByAuthor accepts the law author's revisions through the
// pipeline: revision.accept with all and author.
func acceptByAuthor(src, dst string) error {
	return runPlan(src, dst, laws.Direct, settleOps("accept", lawAuthor))
}

// revisionFacts are what the settle-by laws pick their plans from: the
// body's and the used headers' and footers' revision authors (sorted),
// whether every revision has one, and the body paragraphs (stable
// addresses) that own a revision, in body order.
type revisionFacts struct {
	authors    []string
	allAuthors bool
	owning     []string
}

func factsOf(t *testing.T, path string) revisionFacts {
	t.Helper()
	d, err := model.OpenDocument(mustRead(t, path))
	if err != nil || d.XML.Body() == nil {
		return revisionFacts{}
	}
	f := revisionFacts{allAuthors: true}
	seen := map[string]bool{}
	for _, n := range d.XML.RevisionElements() {
		a := model.AttrOr(n, "author")
		if a == "" {
			f.allAuthors = false
		} else if !seen[a] {
			seen[a] = true
			f.authors = append(f.authors, a)
		}
	}
	slices.Sort(f.authors)
	ids := d.ParaIDs()
	owners := map[*tree.Node]bool{}
	for _, n := range revise.Revisions(d.XML.Body()) {
		for _, p := range revise.Owners(n) {
			owners[p] = true
		}
	}
	for _, p := range d.XML.AllParagraphs() {
		if id, ok := ids.IDs[p]; ok && owners[p] && !ids.Unstable[p] {
			f.owning = append(f.owning, id.ID)
		}
	}
	return f
}

// TestSettleByLaws runs the laws of settling by author and by paragraph
// on the law inputs: the change-mode law of each plan; accepting each
// author's revisions in turn equals accepting all; settling paragraphs
// leaves every other paragraph byte for byte (laws.Kept); and, through
// tracked edits, accept-by-author then reject-all equals the direct form
// (the AcceptAuthored mode law with the pipeline's accept by author).
func TestSettleByLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		f := factsOf(t, in)
		for _, action := range []string{"accept", "reject"} {
			for i, a := range f.authors {
				if i < 2 {
					settleBySuite("revision."+action+" author "+a, settleOps(action, a)).Check(t, in)
				}
			}
			if len(f.owning) > 0 {
				settleBySuite("revision."+action+" first paragraph", settleOps(action, "", f.owning[0])).Check(t, in)
				last := f.owning[len(f.owning)-1]
				settleBySuite("revision."+action+" first and last paragraphs", settleOps(action, "", f.owning[0], last)).Check(t, in)
				if len(f.authors) > 0 {
					settleBySuite("revision."+action+" paragraphs by author", settleOps(action, f.authors[0], f.owning...)).Check(t, in)
				}
			}
		}
		if f.allAuthors && len(f.authors) > 1 {
			partitionLaw(t, in, f.authors)
		}
		keptLaw(t, in, f.owning)
		for name, p := range map[string]string{
			"delete first":  atJSON(firstOf(bodyParagraphIDs(t, in))),
			"format first":  `[{"op":"paragraph.format","at":"` + firstOf(bodyParagraphIDs(t, in)) + `","alignment":"right"}]`,
			"delete owning": atJSON(firstOf(f.owning)),
		} {
			if strings.Contains(p, `""`) {
				continue
			}
			s := deleteSuite(name, p)
			s.Name = "accept-by-author " + name
			s.Project = projectNoSource
			s.AcceptAuthored = acceptByAuthor
			s.Check(t, in)
		}
	})
}

func firstOf(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// partitionLaw: accepting (or rejecting) every author's revisions, one
// author per plan, equals accepting (or rejecting) all of them.
func partitionLaw(t *testing.T, in string, authors []string) {
	t.Helper()
	for _, action := range []string{"accept", "reject"} {
		dir := t.TempDir()
		all := filepath.Join(dir, "all.docx")
		err := runPlan(laws.Private(t, in), all, laws.Direct, settleOps(action, ""))
		var r refusal
		if errors.As(err, &r) {
			continue
		}
		if err != nil {
			t.Errorf("%s: %s all: %v", filepath.Base(in), action, err)
			continue
		}
		cur := laws.Private(t, in)
		for i, a := range authors {
			next := filepath.Join(dir, "by"+string(rune('a'+i%26))+".docx")
			if err := runPlan(cur, next, laws.Direct, settleOps(action, a)); err != nil {
				t.Errorf("%s: %s by %s: %v", filepath.Base(in), action, a, err)
				return
			}
			cur = next
		}
		pa, err := projectNoSource(all)
		if err != nil {
			t.Fatal(err)
		}
		pb, err := projectNoSource(cur)
		if err != nil {
			t.Fatal(err)
		}
		if pa != pb {
			t.Errorf("%s: %s author by author differs from %s all: %s", filepath.Base(in), action, action, diffAt(pa, pb))
		}
	}
}

// diffAt describes where two projections first differ.
func diffAt(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-60)
	return a[lo:min(len(a), i+60)] + " | " + b[lo:min(len(b), i+60)]
}

// keptLaw settles the first and the last paragraph owning a revision (on a
// copy with the paragraph ids stored, so the save writes nothing else)
// and checks that every other paragraph, and every part but the document
// and numbering, keeps its bytes. The paragraphs a settled mark joins
// into, and those holding range markers of a settled move, are the op's
// too.
func keptLaw(t *testing.T, in string, owning []string) {
	t.Helper()
	if len(owning) == 0 {
		return
	}
	src := storedSectionIDs(t, in)
	targets := []string{owning[0]}
	if l := owning[len(owning)-1]; l != owning[0] {
		targets = append(targets, l)
	}
	for _, action := range []string{"accept", "reject"} {
		dst := filepath.Join(t.TempDir(), "settled.docx")
		err := runPlan(src, dst, laws.Direct, settleOps(action, "", targets...))
		var r refusal
		if errors.As(err, &r) {
			continue
		}
		if err != nil {
			t.Errorf("%s: %s %v: %v", filepath.Base(in), action, targets, err)
			continue
		}
		d, err := model.OpenDocument(mustRead(t, src))
		if err != nil {
			t.Fatal(err)
		}
		owned := ownedBy(d, targets)
		part := strings.TrimPrefix(d.Pkg.DocumentPartName(), "/")
		if err := laws.Kept(src, dst, part, docxedit.Units(), func(e *tree.Node) bool {
			// A unit inside an owned paragraph (its section properties,
			// a text box's paragraphs) is the paragraph's.
			for a := e; a != nil; a = a.Parent() {
				if v, ok := a.AttrNS(docxedit.W14.URIs[0], "paraId"); ok && model.IsW(a, "p") && owned[strings.ToUpper(v)] {
					return true
				}
			}
			return false
		}); err != nil {
			t.Errorf("%s: %s %v: %v", filepath.Base(in), action, targets, err)
		}
		if err := otherPartsSame(src, dst, part, strings.TrimPrefix(d.Pkg.NumberingPartName(), "/")); err != nil {
			t.Errorf("%s: %s %v: %v", filepath.Base(in), action, targets, err)
		}
	}
}

// ownedBy is what settling the paragraphs ids may change, by stored
// w14:paraId (laws.Kept parses the files again): the paragraphs, the
// paragraphs their marks join into, and the paragraphs holding range
// markers of their moves, and those sharing a complex field with them;
// the units inside them (text boxes, section properties) go with them.
func ownedBy(d *model.Document, ids []string) map[string]bool {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	owned := map[*tree.Node]bool{}
	moves := map[string]bool{}
	pids := d.ParaIDs()
	for _, p := range d.XML.AllParagraphs() {
		if id, ok := pids.IDs[p]; !ok || !want[id.ID] {
			continue
		}
		owned[p] = true
		for q := p; q != nil; q = model.ParagraphJoinTarget(q) {
			owned[q] = true
			if model.ParagraphMarkDel(q) == nil && model.ParagraphMarkIns(q) == nil {
				break
			}
		}
		for _, n := range model.Descendants(p, func(n *tree.Node) bool { return model.IsW(n, "moveFrom") || model.IsW(n, "moveTo") }) {
			moves[model.AttrOr(n, "id")] = true
		}
		if m := model.ParagraphMarkDel(p); m != nil {
			moves[model.AttrOr(m, "id")] = true
		}
		if m := model.ParagraphMarkIns(p); m != nil {
			moves[model.AttrOr(m, "id")] = true
		}
	}
	for _, p := range d.XML.AllParagraphs() {
		if model.Descendants(p, func(n *tree.Node) bool {
			return (model.IsW(n, "moveFromRangeStart") || model.IsW(n, "moveFromRangeEnd") ||
				model.IsW(n, "moveToRangeStart") || model.IsW(n, "moveToRangeEnd")) && moves[model.AttrOr(n, "id")]
		}) != nil {
			owned[p] = true
		}
	}
	// A complex field that spans paragraphs is one thing: settling the
	// revision that holds one of its parts drops the others (a field
	// without its end), so the paragraphs it spans go with an owned one.
	for _, span := range fieldSpans(d.XML.Body()) {
		if slices.ContainsFunc(span, func(p *tree.Node) bool { return owned[p] }) {
			for _, p := range span {
				owned[p] = true
			}
		}
	}
	out := map[string]bool{}
	for p := range owned {
		if v, ok := p.AttrNS(docxedit.W14.URIs[0], "paraId"); ok {
			out[strings.ToUpper(v)] = true
		}
	}
	return out
}

// otherPartsSame fails unless src and dst have the same parts, each but
// except byte for byte.
func otherPartsSame(src, dst string, except ...string) error {
	a, err := laws.Parts(src)
	if err != nil {
		return err
	}
	b, err := laws.Parts(dst)
	if err != nil {
		return err
	}
	var errs []error
	for n, x := range a {
		if slices.ContainsFunc(except, func(e string) bool { return e != "" && strings.EqualFold(strings.TrimPrefix(n, "/"), e) }) {
			continue
		}
		if y, ok := b[n]; !ok || string(x) != string(y) {
			errs = append(errs, errors.New("part "+n+" changed"))
		}
	}
	if len(a) != len(b) {
		errs = append(errs, errors.New("the part count changed"))
	}
	return errors.Join(errs...)
}

// TestSettleBy checks settling by author and by paragraph on a small
// document: what is settled, what is left and the changed
// paragraphs, and the refusals.
func TestSettleBy(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:ins w:id="1" w:author="Ann"><w:r><w:t>a</w:t></w:r></w:ins>` +
		`<w:del w:id="2" w:author="Bob"><w:r><w:delText>b</w:delText></w:r></w:del></w:p>` +
		`<w:p w14:paraId="0000000B"><w:ins w:id="3" w:author="Bob"><w:r><w:t>c</w:t></w:r></w:ins></w:p>` +
		`<w:p w14:paraId="0000000C"><w:pPr><w:rPr><w:del w:id="4" w:author="Ann"/></w:rPr></w:pPr><w:r><w:t>d</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000D"><w:r><w:t>e</w:t></w:r></w:p>`
	type want struct {
		text string // the body's text, paragraphs joined by |, all view
		left int
	}
	for _, c := range []struct {
		name, ops string
		want      want
	}{
		{"accept Ann", settleOps("accept", "Ann"), want{"ab|c|de", 2}},
		{"reject Bob", settleOps("reject", "Bob"), want{"ab||d|e", 2}},
		{"accept one paragraph", settleOps("accept", "", "0000000A"), want{"a|c|d|e", 2}},
		{"reject a paragraph by author", settleOps("reject", "Bob", "0000000A", "0000000B"), want{"ab||d|e", 2}},
		{"accept a joining mark", settleOps("accept", "", "0000000C"), want{"ab|c|de", 3}},
		{"nothing by that author", settleOps("accept", "Cy"), want{"ab|c|d|e", 4}},
	} {
		path := writeDocx(t, body)
		before := mustRead(t, path)
		err := runPlan(path, path+".out", laws.Direct, c.ops)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		out := mustRead(t, path+".out")
		d, err := model.OpenDocument(out)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, p := range model.ParagraphsIn(d.XML.Body()) {
			texts = append(texts, model.ParagraphText(p.Node, model.All))
		}
		if got := strings.Join(texts, "|"); got != c.want.text {
			t.Errorf("%s: text %q, want %q", c.name, got, c.want.text)
		}
		if got := len(d.XML.RevisionElements()); got != c.want.left {
			t.Errorf("%s: %d revisions left, want %d", c.name, got, c.want.left)
		}
		if c.want.left == 4 && string(out) != string(before) {
			t.Errorf("%s: settling nothing wrote the file", c.name)
		}
	}
	for _, c := range []struct{ name, ops, code, path string }{
		{"author with one revision", `[{"op":"revision.accept","at":"revision:1","author":"Ann"}]`, "invalid_operation", "ops[0].author"},
		{"no paragraphs", `[{"op":"revision.reject","at":[]}]`, "invalid_operation", "ops[0].at"},
		{"author alone", `[{"op":"revision.accept","author":"Ann"}]`, "invalid_operation", "ops[0].at"},
		{"unknown paragraph", settleOps("accept", "", "0000000F"), "not_found", "ops[0].at"},
		{"empty author", `[{"op":"revision.accept","all":true,"author":""}]`, "invalid_plan", "ops[0].author"},
	} {
		path := writeDocx(t, body)
		r, err := runText(path, planJSON(t, path, c.ops), Options{View: viewOpt})
		if err != nil {
			t.Fatal(err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != c.code || r.Diagnostic.Path != c.path {
			t.Errorf("%s: %s %+v", c.name, r.Outcome, r.Diagnostic)
		}
	}
}

// planJSON is a direct-mode plan of ops on the file at path.
func planJSON(t *testing.T, path, ops string) []byte {
	t.Helper()
	sum := sha256.Sum256(mustRead(t, path))
	return []byte(`{"base":"sha256:` + hex.EncodeToString(sum[:]) + `","author":"` + lawAuthor + `","change_mode":"direct","ops":` + ops + `}`)
}

// TestSettleByAtomicity: a plan that settles by author or by paragraph
// and then fails, or that fails first, writes nothing; a fault at each
// save stage leaves the file as it was.
func TestSettleByAtomicity(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "tools", "oracle", "testdata", "write", "mixed.docx")
	f := factsOf(t, src)
	if len(f.authors) == 0 || len(f.owning) == 0 {
		t.Fatal("mixed.docx has no revisions to settle")
	}
	fail := `{"op":"paragraph.format","at":"0FFFFFFF","alignment":"right"}`
	for _, settle := range []string{settleOps("accept", f.authors[0]), settleOps("reject", "", f.owning...)} {
		inner := strings.TrimSuffix(strings.TrimPrefix(settle, "["), "]")
		for _, ops := range []string{"[" + inner + "," + fail + "]", "[" + fail + "," + inner + "]"} {
			path := laws.Private(t, src)
			before := mustRead(t, path)
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := runText(path, planJSON(t, path, ops), Options{View: viewOpt})
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != Rejected || r.Diagnostic.Code != "not_found" {
				t.Errorf("%s: %s %+v", ops, r.Outcome, r.Diagnostic)
			}
			unchangedFile(t, path, before, st.ModTime())
		}
		for _, stage := range []string{opc.StageCreate, opc.StageWrite, opc.StageSync, opc.StageClose, opc.StageRename} {
			path := laws.Private(t, src)
			before := mustRead(t, path)
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			restore := opc.SetFaultHookFor(path, func(s, _ string) error {
				if s == stage {
					return errors.New("injected at " + s)
				}
				return nil
			})
			text := planJSON(t, path, settle)
			r, err := runText(path, text, Options{View: viewOpt})
			if err == nil && r.Outcome != Rejected {
				var cerr error
				r, cerr = runText(path, text, Options{Commit: r.PreviewKey, Now: "2026-01-01T00:00:00Z", View: viewOpt})
				err = cerr
			}
			restore()
			if err == nil && r.Outcome == Committed {
				t.Errorf("%s: committed with a fault at %s", settle, stage)
			}
			unchangedFile(t, path, before, st.ModTime())
		}
	}
}

// fieldSpans lists, per outermost complex field under scope that spans
// more than one paragraph, the paragraphs it touches, from its begin to
// its end fldChar in document order.
func fieldSpans(scope *tree.Node) [][]*tree.Node {
	var out [][]*tree.Node
	var cur []*tree.Node
	depth := 0
	for _, c := range model.Descendants(scope, func(n *tree.Node) bool { return model.IsW(n, "fldChar") }) {
		var p *tree.Node
		for a := c.Parent(); a != nil; a = a.Parent() {
			if model.IsW(a, "p") {
				p = a
				break
			}
		}
		if depth > 0 && p != nil && (len(cur) == 0 || cur[len(cur)-1] != p) {
			cur = append(cur, p)
		}
		switch model.AttrOr(c, "fldCharType") {
		case "begin":
			if depth == 0 && p != nil {
				cur = []*tree.Node{p}
			}
			depth++
		case "end":
			if depth > 0 {
				depth--
				if depth == 0 && len(cur) > 1 {
					out = append(out, cur)
				}
			}
		}
	}
	return out
}
