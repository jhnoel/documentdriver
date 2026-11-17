package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// wireOp is a test-only operation that edits through the edit layer
// directly: Value is a PrintPageOrientation value written to the page
// sheet, ShapeValue a PinX value written to shape page0/s7, Stale the
// stale detail to report ("" for fresh), Fail to refuse on purpose, and
// BadConnect to insert a Connect whose FromSheet names no shape.
type wireOp struct {
	Value      string
	ShapeValue string
	Stale      string
	Fail       bool
	BadConnect bool
}

// Run applies the wiring op to ws for the wiring tests.
func (o *wireOp) Run(ws *edit.Workspace, _ map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	if o.Fail {
		return "", nil, nil, "", &cli.Error{Code: cli.CodeOperationFailed, Path: "at", Message: "refused on purpose"}
	}
	if o.BadConnect {
		frag := []byte(`<Connect FromSheet='99' FromCell='BeginX' FromPart='9' ToSheet='1' ToCell='PinX' ToPart='3'/>`)
		if err := ws.InsertConnects("page0", [][]byte{frag}); err != nil {
			return "", nil, nil, "", err
		}
		return "", []string{"page0"}, nil, "", nil
	}
	var changed []string
	if o.Value != "" {
		if err := ws.SetCells("page0", []edit.Cell{{N: "PrintPageOrientation", V: o.Value}}); err != nil {
			return "", nil, nil, "", err
		}
		changed = append(changed, "page0")
	}
	if o.ShapeValue != "" {
		if err := ws.SetCells("page0/s7", []edit.Cell{{N: "PinX", V: o.ShapeValue}}); err != nil {
			return "", nil, nil, "", err
		}
		changed = append(changed, "page0/s7")
	}
	return "", changed, nil, o.Stale, nil
}

// wirePlan builds a plan holding specs, with no base.
func wirePlan(specs ...ops.Spec) *ops.Plan {
	return &ops.Plan{Ops: specs}
}

// readFile reads the file at path, failing the test on any error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestUnimplementedOps checks the A1 row for whichever ops of the table
// have no Run yet (ops.OpDef.Implemented): a plan of one refuses with
// operation_failed at ops[0], naming the op, and leaves the file as it
// was. The plans are built from the table's zero specs, so this test
// needs no edit as ops land; when every op is implemented it checks
// nothing and says so.
func TestUnimplementedOps(t *testing.T) {
	n := 0
	for i := range ops.Ops {
		def := &ops.Ops[i]
		if def.Implemented() {
			continue
		}
		n++
		path := vsdxtest.Fixture(t, "v1-minimal")
		before := readFile(t, path)
		_, err := Run(path, &ops.Plan{Ops: []ops.Spec{def.New()}}, Options{})
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0]" || !strings.Contains(ce.Message, def.Name) {
			t.Errorf("%s: got %v, want operation_failed at ops[0] naming it", def.Name, err)
		}
		if got := readFile(t, path); !bytes.Equal(got, before) {
			t.Errorf("%s: a refused plan changed the file", def.Name)
		}
	}
	if n == 0 {
		t.Log("every op of the table is implemented")
	}
}

// TestImplementedDerived checks that Implemented follows the spec's Run
// method and nothing else: a spec type with Run is implemented, the
// table's entries agree with a type assertion on their specs.
func TestImplementedDerived(t *testing.T) {
	for i := range ops.Ops {
		def := &ops.Ops[i]
		_, runs := def.New().(ops.Runner)
		if def.Implemented() != runs {
			t.Errorf("%s: Implemented %v, spec Runner %v", def.Name, def.Implemented(), runs)
		}
	}
}

// TestStaleBase checks the A1 row: a stale base refuses with stale_base
// at base, and the hash of the file passes the guard.
func TestStaleBase(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	before := readFile(t, path)
	stale := &ops.Plan{Base: "sha256:" + strings.Repeat("0", 64), Ops: []ops.Spec{&wireOp{Value: "2"}}}
	_, err := Run(path, stale, Options{})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("stale base: error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeStaleBase || ce.Path != "base" {
		t.Errorf("stale base: got %s at %q, want stale_base at base", ce.Code, ce.Path)
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Error("a stale-base plan changed the file")
	}
	fresh := &ops.Plan{Base: cli.Hash(before), Ops: []ops.Spec{&wireOp{Value: "2"}}}
	out := filepath.Join(t.TempDir(), "fresh.vsdx")
	if _, err := Run(path, fresh, Options{Out: out}); err != nil {
		t.Errorf("fresh base: %v", err)
	}
}

// TestWiringDryRunAndNoOp checks the A1 rows: a dry run writes nothing
// and equals the run's result, and a no-op plan writes nothing.
func TestWiringDryRunAndNoOp(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	before := readFile(t, path)
	change := wirePlan(&wireOp{Value: "2"})
	dryOut := filepath.Join(t.TempDir(), "dry.vsdx")
	dry, err := Run(path, change, Options{DryRun: true, Out: dryOut})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.Saved != "" || dry.NewBase != "" || !dry.DryRun {
		t.Errorf("dry run result %+v, want saved and new_base empty", *dry)
	}
	if _, err := os.Stat(dryOut); !errors.Is(err, os.ErrNotExist) {
		t.Error("a dry run wrote its out file")
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Error("a dry run changed the input")
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	ran, err := Run(path, wirePlan(&wireOp{Value: "2"}), Options{Out: out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if ran.Base != dry.Base || ran.DryRun {
		t.Errorf("run result %+v", *ran)
	}
	if !equalStrings(ran.Parts, dry.Parts) || !equalHazardKinds(ran.Hazards, dry.Hazards) || len(ran.Changed) != len(dry.Changed) {
		t.Errorf("dry %+v != run %+v", *dry, *ran)
	}
	for i := range ran.Changed {
		if !bytes.Equal(ran.Changed[i], dry.Changed[i]) {
			t.Errorf("changed[%d] dry %s != run %s", i, dry.Changed[i], ran.Changed[i])
		}
	}
	if string(ran.Changed[0]) != `{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"landscape","objects":1}` {
		t.Errorf("changed page entry %s", ran.Changed[0])
	}
	written := readFile(t, out)
	if ran.NewBase != cli.Hash(written) || ran.Saved != out {
		t.Errorf("run result %+v, want saved %s and base of its file", *ran, out)
	}
	noopOut := filepath.Join(t.TempDir(), "noop.vsdx")
	noop, err := Run(path, wirePlan(&wireOp{}), Options{Out: noopOut})
	if err != nil {
		t.Fatalf("no-op: %v", err)
	}
	if noop.Saved != "" || noop.NewBase != noop.Base || len(noop.Parts) != 0 || len(noop.Changed) != 0 || len(noop.Hazards) != 0 {
		t.Errorf("no-op result %+v", *noop)
	}
	if _, err := os.Stat(noopOut); !errors.Is(err, os.ErrNotExist) {
		t.Error("a no-op plan wrote a file")
	}
}

// TestOpFailureAndSaveFault check the A1 rows: an op failure leaves the
// file unchanged, and a fault hook failing at each save stage leaves
// the file unchanged.
func TestOpFailureAndSaveFault(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	before := readFile(t, path)
	out := filepath.Join(t.TempDir(), "fail.vsdx")
	_, err := Run(path, wirePlan(&wireOp{Value: "2"}, &wireOp{Fail: true}), Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeOperationFailed || ce.Path != "ops[1].at" {
		t.Errorf("failing op: got %v, want operation_failed at ops[1].at", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a plan with a failing op wrote its out file")
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Error("a plan with a failing op changed the input")
	}
	for _, stage := range []string{opc.StageCreate, opc.StageWrite, opc.StageChmod, opc.StageSync, opc.StageClose, opc.StageRename} {
		out := filepath.Join(t.TempDir(), "fault.vsdx")
		restore := opc.SetFaultHookFor(out, func(got, _ string) error {
			if got == stage {
				return errors.New("fault at " + got)
			}
			return nil
		})
		_, err := Run(path, wirePlan(&wireOp{Value: "2"}), Options{Out: out})
		restore()
		if err == nil {
			t.Errorf("stage %s: no error, want the fault", stage)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stage %s: the out file exists after the fault", stage)
		}
		if got := readFile(t, path); !bytes.Equal(got, before) {
			t.Errorf("stage %s: the input changed after the fault", stage)
		}
	}
}

// TestRecalcDocument checks the recalculation policy: a stale write
// creates docProps/custom.xml asking Visio to recalculate on open, and
// reports stale-values, new when the input did not already ask.
func TestRecalcDocument(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "stale.vsdx")
	res, err := Run(path, wirePlan(&wireOp{Value: "2", Stale: "PageSheet.PageWidth left stale"}), Options{Out: out})
	if err != nil {
		t.Fatalf("stale run: %v", err)
	}
	if len(res.Hazards) == 0 || res.Hazards[0].Kind != "stale-values" || res.Hazards[0].ID != "document" || !res.Hazards[0].New {
		t.Errorf("hazards %+v, want new stale-values on document", res.Hazards)
	}
	if res.Hazards[0].Detail != "PageSheet.PageWidth left stale" {
		t.Errorf("stale detail %q", res.Hazards[0].Detail)
	}
	pkg, err := opc.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	d, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open output: %v", err)
	}
	if !d.Recalc {
		t.Error("the output does not ask Visio to recalculate on open")
	}
	if !pkg.Has("/docProps/custom.xml") {
		t.Error("the output has no docProps/custom.xml")
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close output: %v", err)
	}
	again := filepath.Join(t.TempDir(), "again.vsdx")
	res, err = Run(out, wirePlan(&wireOp{Value: "1", Stale: "PageSheet.PageWidth left stale"}), Options{Out: again})
	if err != nil {
		t.Fatalf("second stale run: %v", err)
	}
	if len(res.Hazards) == 0 || res.Hazards[0].Kind != "stale-values" || res.Hazards[0].New {
		t.Errorf("second hazards %+v, want stale-values, not new", res.Hazards)
	}
	dry := filepath.Join(t.TempDir(), "dry-stale.vsdx")
	dryRes, err := Run(path, wirePlan(&wireOp{Value: "2", Stale: "PageSheet.PageWidth left stale"}), Options{DryRun: true, Out: dry})
	if err != nil {
		t.Fatalf("dry stale run: %v", err)
	}
	if _, err := os.Stat(dry); !errors.Is(err, os.ErrNotExist) {
		t.Error("a dry stale run wrote its out file")
	}
	if len(dryRes.Hazards) == 0 || !dryRes.Hazards[0].New {
		t.Errorf("dry stale hazards %+v", dryRes.Hazards)
	}
}

// TestGateRefusesOnlyIntroduced checks the scoped gate: a finding the
// plan introduces refuses with invalid_output, and one the input
// already has does not refuse.
func TestGateRefusesOnlyIntroduced(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	before := readFile(t, path)
	out := filepath.Join(t.TempDir(), "bad.vsdx")
	_, err := Run(path, wirePlan(&wireOp{BadConnect: true}), Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidOutput || ce.Path != "ops[0]" {
		t.Errorf("bad connect: got %v, want invalid_output at ops[0]", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused plan wrote its out file")
	}
	if got := readFile(t, path); !bytes.Equal(got, before) {
		t.Error("a refused plan changed the input")
	}
	defects := vsdxtest.Fixture(t, "v10-defects")
	ok := filepath.Join(t.TempDir(), "ok.vsdx")
	res, err := Run(defects, wirePlan(&wireOp{ShapeValue: "2.5"}), Options{Out: ok})
	if err != nil {
		t.Fatalf("pre-existing findings refused: %v", err)
	}
	if len(res.Changed) != 1 || string(res.Changed[0]) == "" {
		t.Errorf("pre-existing changed %+v", *res)
	}
	if _, err := os.Stat(ok); err != nil {
		t.Errorf("the output is missing: %v", err)
	}
}

// staleHazards returns the stale-values hazards of a result in order.
func staleHazards(res *plan.Result) []plan.Hazard {
	var out []plan.Hazard
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" {
			out = append(out, h)
		}
	}
	return out
}

// withMalformedCustom copies V1 into a new file with a malformed
// docProps/custom.xml part and its relationship and Override.
func withMalformedCustom(t *testing.T, body string) string {
	t.Helper()
	src, err := laws.Parts(vsdxtest.Fixture(t, "v1-minimal"))
	if err != nil {
		t.Fatal(err)
	}
	ct := string(src["/[Content_Types].xml"])
	ct = strings.Replace(ct, "</Types>",
		`<Override PartName="/docProps/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.custom-properties+xml"/></Types>`, 1)
	rels := string(src["/_rels/.rels"])
	rels = strings.Replace(rels, "</Relationships>",
		`<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/custom-properties" Target="docProps/custom.xml"/></Relationships>`, 1)
	src["/[Content_Types].xml"] = []byte(ct)
	src["/_rels/.rels"] = []byte(rels)
	src["/docProps/custom.xml"] = []byte(body)
	names := make([]string, 0, len(src))
	for n := range src {
		if n != "/[Content_Types].xml" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	members := make([]opc.ZipMember, 0, len(src))
	members = append(members, opc.ZipMember{Name: "[Content_Types].xml", Body: string(src["/[Content_Types].xml"])})
	for _, n := range names {
		members = append(members, opc.ZipMember{Name: strings.TrimPrefix(n, "/"), Body: string(src[n])})
	}
	data, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "malformed-custom.vsdx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// partBytes returns the bytes of part name in the drawing at path.
func partBytes(t *testing.T, path, name string) []byte {
	t.Helper()
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	pt, err := pkg.Part(name)
	if err != nil {
		t.Fatalf("part %s of %s: %v", name, path, err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatalf("bytes of %s in %s: %v", name, path, err)
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	return b
}

// TestStaleOncePerSave checks the A1 row: two ops marking values
// stale set RecalcDocument once per save and report one stale-values
// hazard, new on V1, whose detail is the first op's.
func TestStaleOncePerSave(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	out := filepath.Join(t.TempDir(), "stale-twice.vsdx")
	res, err := Run(path, wirePlan(
		&wireOp{Value: "2", Stale: "PageSheet.PageWidth left stale"},
		&wireOp{Value: "1", Stale: "PageSheet.PageHeight left stale"},
	), Options{Out: out})
	if err != nil {
		t.Fatalf("two stale ops: %v", err)
	}
	if got := staleHazards(res); len(got) != 1 || got[0].ID != "document" || !got[0].New {
		t.Errorf("stale hazards %+v, want one new stale-values on document", got)
	}
	if got := staleHazards(res); len(got) == 1 && got[0].Detail != "PageSheet.PageWidth left stale" {
		t.Errorf("stale detail %q, want the first op's", got[0].Detail)
	}
	pkg, err := opc.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	d, err := doc.Open(pkg)
	if err != nil {
		t.Fatalf("doc.Open output: %v", err)
	}
	if !d.Recalc {
		t.Error("the output does not ask Visio to recalculate on open")
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close output: %v", err)
	}
}

// TestStaleMalformedCustom checks the A1 row: a stale write on a V1
// copy with a malformed custom.xml saves, leaves custom.xml
// byte-identical, and reports the stanza about RecalcDocument not set.
func TestStaleMalformedCustom(t *testing.T) {
	malformed := `<Properties><property name="x">`
	path := withMalformedCustom(t, malformed)
	out := filepath.Join(t.TempDir(), "malformed-out.vsdx")
	res, err := Run(path, wirePlan(&wireOp{Value: "2", Stale: "PageSheet.PageWidth left stale"}), Options{Out: out})
	if err != nil {
		t.Fatalf("stale run on malformed custom.xml: %v", err)
	}
	got := staleHazards(res)
	if len(got) != 1 || got[0].ID != "document" || !got[0].New {
		t.Errorf("stale hazards %+v, want one new stale-values on document", got)
	}
	if len(got) == 1 && got[0].Detail != "RecalcDocument not set: docProps/custom.xml cannot be read" {
		t.Errorf("stale detail %q", got[0].Detail)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("the output is missing: %v", err)
	}
	if in, outBytes := partBytes(t, path, "/docProps/custom.xml"), partBytes(t, out, "/docProps/custom.xml"); !bytes.Equal(in, outBytes) {
		t.Errorf("custom.xml changed: input %q, output %q", in, outBytes)
	}
}

// equalStrings reports whether two string slices hold equal strings in order.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalHazardKinds reports whether two hazard slices hold equal kinds
// and ids in order.
func equalHazardKinds(a, b []plan.Hazard) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].ID != b[i].ID || a[i].Detail != b[i].Detail || a[i].New != b[i].New {
			return false
		}
	}
	return true
}
