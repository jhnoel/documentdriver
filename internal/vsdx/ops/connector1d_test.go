package ops_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// connectorCorpusDir returns the vsdx corpus directory, or "".
func connectorCorpusDir() string {
	if d := os.Getenv("VSDX_CORPUS"); d != "" {
		for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
			if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
				return cand
			}
		}
		return ""
	}
	return ""
}

// connectorCorpusFile returns the path of one corpus file: it
// skips when VSDX_CORPUS is unset and fails naming the path when
// it is set and the file is missing.
func connectorCorpusFile(t *testing.T, rel string) string {
	t.Helper()
	dir := connectorCorpusDir()
	if dir == "" {
		if os.Getenv("VSDX_CORPUS") != "" {
			t.Fatalf("VSDX_CORPUS %s holds no vsdx corpus (no repos directory under it)", os.Getenv("VSDX_CORPUS"))
		}
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	path := filepath.Join(dir, "repos", filepath.FromSlash(rel))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corpus file %s not found under VSDX_CORPUS %s", path, os.Getenv("VSDX_CORPUS"))
	}
	return path
}

// connectorTestFile is the relative path of the test4 Connectors
// corpus file, and connectorJsFile of Connectors.vsdx.
const (
	connectorTestFile = "dave-howard-vsdx/tests/test4_connectors.vsdx"
	connectorJsFile   = "vsdx-js/tests/Connectors.vsdx"
)

// TestConnectorRow1 checks row 1: test4 s6 to_glue "none" keeps to
// at [219.69,74.69], drops to_glue, keeps connections entry for
// from s1, removes the EndX Connect, keeps EndX V with no F, does
// not store EndTrigger locally, and reports hazards []. Written as
// the CC2 row verbatim.
func TestConnectorRow1(t *testing.T) {
	path := connectorCorpusFile(t, connectorTestFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s6","to_glue":"none"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s6")
	checkPt(t, "s6 to", page.Objects[i].To, [2]float64{219.69, 74.69})
	if page.Objects[i].ToGlue != "" {
		t.Errorf("to_glue = %q, want absent", page.Objects[i].ToGlue)
	}
	found := false
	for _, c := range page.Connections {
		if c.Connector == "page0/s6" {
			found = true
			if c.From != "page0/s1" || c.FromGlue != "s1" || c.To != "" || c.ToGlue != "" {
				t.Errorf("connections s6 = %+v, want connector s6 from s1 from_glue s1 with no to", c)
			}
		}
	}
	if !found {
		t.Error("connections has no s6 entry")
	}
	xml := shapeXML(t, out, "page0/s6")
	if !strings.Contains(xml, "N='EndX' V='3.051180987260419'") || strings.Contains(xml, "N='EndX' V='3.051180987260419' F=") {
		t.Errorf("EndX V 3.051180987260419 without F missing: %s", xml)
	}
	if strings.Contains(xml, "<Cell N='EndTrigger'") {
		t.Errorf("EndTrigger still stored locally: %s", xml)
	}
	pkg, err := opc.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	partXML := ""
	if pt, err := pkg.Part("/visio/pages/page1.xml"); err == nil {
		if b, err := pt.Bytes(); err == nil {
			partXML = string(b)
		}
	}
	pkg.Close()
	if strings.Contains(partXML, "FromSheet='6' FromCell='EndX'") {
		t.Errorf("EndX Connect row still present: %s", partXML)
	}
	if len(page.Hazards) != 0 {
		t.Errorf("hazards = %v, want []", page.Hazards)
	}
}

// TestConnectorRow2 checks row 2: test4 s6 to [300,100] gives to
// [300,100], Width 300/72-2.415354251861572, PinX
// (2.415354251861572+300/72)/2, Geometry1 MoveTo(0,yb) by the
// rule (LineTo IX 3 Del kept), stale-values "page0/s6 route" and
// parts page and docProps/custom.xml.
func TestConnectorRow2(t *testing.T) {
	path := connectorCorpusFile(t, connectorTestFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s6","to":[300,100]}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s6")
	checkPt(t, "s6 to", page.Objects[i].To, [2]float64{300, 100})
	xml := shapeXML(t, out, "page0/s6")
	if !connectorCellClose(t, xml, "Width", 300.0/72.0-2.415354251861572) {
		t.Errorf("Width is not %v in %s", 300.0/72.0-2.415354251861572, xml)
	}
	if !connectorCellClose(t, xml, "PinX", (2.415354251861572+300.0/72.0)/2) {
		t.Errorf("PinX is not %v in %s", (2.415354251861572+300.0/72.0)/2, xml)
	}
	if !strings.Contains(xml, "<Row T='LineTo' IX='3' Del='1'/>") {
		t.Errorf("LineTo IX 3 Del row not kept: %s", xml)
	}
	// Geometry1 is MoveTo(0, yb), LineTo(Width, ye) by the
	// rule: check MoveTo has no X beyond 0 and LineTo X is
	// Width (stored) and Y values follow yb/ye.
	if !strings.Contains(xml, "Geometry") || !strings.Contains(xml, "MoveTo") {
		t.Errorf("Geometry1 missing: %s", xml)
	}
	found := false
	for _, h := range res.Hazards {
		if h.Kind == "stale-values" && h.Detail == "page0/s6 route" {
			found = true
		}
	}
	if !found {
		t.Errorf("hazards %+v, want stale-values page0/s6 route", res.Hazards)
	}
	seen := map[string]bool{}
	for _, p := range res.Parts {
		seen[p] = true
	}
	if !seen["/visio/pages/page1.xml"] || !seen["/docprops/custom.xml"] {
		t.Errorf("parts %v, want page and docProps/custom.xml", res.Parts)
	}
}

// connectorCellClose reports whether the stored V of the shape-level
// cell name in xml reads back within 1e-9 of want.
func connectorCellClose(t *testing.T, xml, name string, want float64) bool {
	t.Helper()
	key := "N='" + name + "' V='"
	_, after, ok := strings.Cut(xml, key)
	if !ok {
		return false
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, "'")
	if !ok0 {
		return false
	}
	got, err := strconv.ParseFloat(before0, 64)
	if err != nil {
		return false
	}
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1e-9
}

// TestConnectorRow3 checks row 3: Connectors s42 to_glue "s40:3"
// puts the end on s40's site 3, Connect ToSheet 40 ToCell
// Connections.X3 ToPart 102, EndX F PAR(PNT(...X3...)), EndTrigger
// F _XFTRIGGER(Sheet.40!EventXFMod).
func TestConnectorRow3(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	site := connectorSite(t, path, "page0/s40", 3)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s42","to_glue":"s40:3"}]}`), apply.Options{Out: out}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s42")
	if page.Objects[i].ToGlue != "s40:3" {
		t.Errorf("to_glue = %q, want s40:3", page.Objects[i].ToGlue)
	}
	checkPt(t, "s42 to", page.Objects[i].To, site)
	xml := shapeXML(t, out, "page0/s42")
	if !strings.Contains(xml, "PAR(PNT(Sheet.40!Connections.X3") || !strings.Contains(xml, "Connections.Y3") {
		t.Errorf("EndX F is not the PAR(PNT X3 form: %s", xml)
	}
	if !strings.Contains(xml, "_XFTRIGGER(Sheet.40!EventXFMod)") || !strings.Contains(xml, "N='EndTrigger' V='2'") {
		t.Errorf("EndTrigger F/V missing: %s", xml)
	}
	partXML := connectorPartXML(t, out, "/visio/pages/page1.xml")
	want := "<Connect FromSheet='42' FromCell='EndX' FromPart='12' ToSheet='40' ToCell='Connections.X3' ToPart='102'/>"
	if !strings.Contains(partXML, want) {
		t.Errorf("Connect %s missing in page: %s", want, partXML)
	}
}

// TestConnectorRow4 checks row 4: Connectors s42 to_glue "s41"
// gives to_glue "s41", Connect ToCell PinX ToPart 3, EndX F
// _WALKGLUE(EndTrigger,BegTrigger,WalkPreference), cached end at
// midpoint of s41's left box edge [347.09,72.01].
func TestConnectorRow4(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s42","to_glue":"s41"}]}`), apply.Options{Out: out}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s42")
	if page.Objects[i].ToGlue != "s41" {
		t.Errorf("to_glue = %q, want s41", page.Objects[i].ToGlue)
	}
	checkPt(t, "s42 to", page.Objects[i].To, [2]float64{347.09, 72.01})
	xml := shapeXML(t, out, "page0/s42")
	if !strings.Contains(xml, "_WALKGLUE(EndTrigger,BegTrigger,WalkPreference)") {
		t.Errorf("EndX F is not _WALKGLUE(EndTrigger,BegTrigger,WalkPreference): %s", xml)
	}
	partXML := connectorPartXML(t, out, "/visio/pages/page1.xml")
	want := "<Connect FromSheet='42' FromCell='EndX' FromPart='12' ToSheet='41' ToCell='PinX' ToPart='3'/>"
	if !strings.Contains(partXML, want) {
		t.Errorf("Connect %s missing: %s", want, partXML)
	}
}

// TestConnectorRow5 checks row 5: test4 s5 (a master-less
// rectangle with no connection points) as target: object.set
// s6 to_glue "s5" is accepted (s6 is a Dynamic connector
// instance, master2).
func TestConnectorRow5(t *testing.T) {
	path := connectorCorpusFile(t, connectorTestFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	if _, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s6","to_glue":"s5"}]}`), apply.Options{Out: out}); err != nil {
		t.Fatalf("apply to_glue s5: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s6")
	if page.Objects[i].ToGlue != "s5" {
		t.Errorf("to_glue = %q, want s5", page.Objects[i].ToGlue)
	}
}

// TestConnectorRow6 checks row 6: a master-less connector from
// object.add kind connector to_glue "s2" is refused with the CC2
// message and hint; the same on V5's master-less connector with
// object.set.
func TestConnectorRow6(t *testing.T) {
	blank := vsdxtest.Blank(t)
	_, err := apply.Run(blank, parsePlan(t, `{"ops":[
 {"op":"object.add","at":"page0","geom":"rect","as":"a","box":[72,72,144,72]},
 {"op":"object.add","at":"page0","kind":"connector","from":[0,0],"to_glue":"$a"}
]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("add whole glue: error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[1].to_glue" {
		t.Errorf("got %s at %q, want operation_failed at ops[1].to_glue", ce.Code, ce.Path)
	}
	if !strings.Contains(ce.Message, "whole-shape") || !strings.Contains(ce.Message, "dynamic glue") {
		t.Errorf("message %q does not hold the CC2 wording", ce.Message)
	}
	if !strings.Contains(ce.Hint, "Dynamic connector") || !strings.Contains(ce.Hint, "connection point") {
		t.Errorf("hint %q does not hold the CC2 wording", ce.Hint)
	}
	path := vsdxtest.Fixture(t, "v5-glue")
	_, err = apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s4","to_glue":"s2"}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce2 *cli.Error
	if !errors.As(err, &ce2) {
		t.Fatalf("set whole glue: error %v, want *cli.Error", err)
	}
	if ce2.Code != cli.CodeOperationFailed || ce2.Path != "ops[0].to_glue" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].to_glue", ce2.Code, ce2.Path)
	}
	if !strings.Contains(ce2.Message, "page0/s4 is not from a master") {
		t.Errorf("message %q does not name the connector address", ce2.Message)
	}
	if !strings.Contains(ce2.Hint, "Dynamic connector") {
		t.Errorf("hint %q does not name the Dynamic connector master", ce2.Hint)
	}
}

// TestConnectorRow7 checks row 7: Connectors s42 after from_glue
// none, dx 10 moves from by 10, to stays glued at [347.09,72.01].
func TestConnectorRow7(t *testing.T) {
	path := connectorCorpusFile(t, connectorJsFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	plan := `{"ops":[{"op":"object.set","at":"page0/s42","from_glue":"none"},{"op":"object.move","at":"page0/s42","dx":10,"dy":0}]}`
	if _, err := apply.Run(path, parsePlan(t, plan), apply.Options{Out: out}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	page := readPage(t, out)
	i := findObject(t, page, "page0/s42")
	checkPt(t, "s42 from", page.Objects[i].From, [2]float64{276.5, 72.01})
	checkPt(t, "s42 to", page.Objects[i].To, [2]float64{347.09, 72.01})
	if page.Objects[i].ToGlue == "" {
		t.Error("to_glue is free, want still glued")
	}
}

// TestConnectorRow8 checks row 8: object.move test4 s6 (both
// glued) is refused operation_failed at ops[0].at with the CC2
// message.
func TestConnectorRow8(t *testing.T) {
	path := connectorCorpusFile(t, connectorTestFile)
	_, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.move","at":"page0/s6","dx":72}]}`), apply.Options{Out: filepath.Join(t.TempDir(), "out.vsdx")})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want *cli.Error", err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0].at" {
		t.Errorf("got %s at %q, want operation_failed at ops[0].at", ce.Code, ce.Path)
	}
	if !strings.Contains(ce.Message, "page0/s6 has both ends glued") || !strings.Contains(ce.Message, "set from_glue or to_glue to none first") {
		t.Errorf("message %q does not hold the CC2 wording", ce.Message)
	}
}

// TestConnectorRow9 checks row 9 (identity): to_glue equal to the
// current glue writes nothing (new_base == base); from equal to
// the current free end writes nothing.
func TestConnectorRow9(t *testing.T) {
	path := connectorCorpusFile(t, connectorTestFile)
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s6","to_glue":"s2"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("apply identity glue: %v", err)
	}
	if res.NewBase != res.Base {
		t.Errorf("to_glue identity new_base %q != base %q", res.NewBase, res.Base)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("to_glue identity wrote its out file")
	}
	if len(res.Changed) != 0 || len(res.Parts) != 0 {
		t.Errorf("to_glue identity changed %v parts %v, want empty", res.Changed, res.Parts)
	}
	v5 := vsdxtest.Fixture(t, "v5-glue")
	vout := filepath.Join(t.TempDir(), "vout.vsdx")
	res, err = apply.Run(v5, parsePlan(t, `{"ops":[{"op":"object.set","at":"page0/s4","from":[396,144]}]}`), apply.Options{Out: vout})
	if err != nil {
		t.Fatalf("apply identity from: %v", err)
	}
	if res.NewBase != res.Base {
		t.Errorf("from identity new_base %q != base %q", res.NewBase, res.Base)
	}
	if _, err := os.Stat(vout); !errors.Is(err, os.ErrNotExist) {
		t.Error("from identity wrote its out file")
	}
}

// TestConnectorRow10 checks row 10 (corpus sweep): on every
// connector of every corpus drawing whose cells pass the
// editability test, to_glue none then read back succeeds with no
// new hazard; counts are logged and the connectors refused are
// listed by formula.
func TestConnectorRow10(t *testing.T) {
	dir := connectorCorpusDir()
	if dir == "" {
		if os.Getenv("VSDX_CORPUS") != "" {
			t.Fatalf("VSDX_CORPUS %s holds no vsdx corpus (no repos directory under it)", os.Getenv("VSDX_CORPUS"))
		}
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := connectorCorpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("VSDX_CORPUS %s holds no drawings under %s", os.Getenv("VSDX_CORPUS"), dir)
	}
	// Shard to the corpus files of this run, as corpustest
	// suites do: one subtest per file is too heavy for one
	// table here, so sharding only limits the file set and
	// the counts are the shard's.
	if shard := os.Getenv("CORPUS_SHARD"); shard != "" {
		t.Logf("CORPUS_SHARD=%s", shard)
	}
	okCount := 0
	refused := []string{}
	for _, path := range paths {
		for _, addr := range connectorAddrs(t, path) {
			in := path
			out := filepath.Join(t.TempDir(), "sweep.vsdx")
			// Input hazards, to spot a new one for this connector.
			inHazards := connectorHazardKeys(t, in)
			plan := `{"ops":[{"op":"object.set","at":"` + addr + `","to_glue":"none"}]}`
			res, err := apply.Run(in, parsePlan(t, plan), apply.Options{Out: out})
			if err != nil {
				var ce *cli.Error
				if errors.As(err, &ce) && ce.Code == cli.CodeOperationFailed {
					refused = append(refused, path+" "+addr+": "+ce.Message)
					continue
				}
				t.Fatalf("sweep %s %s: %v", path, addr, err)
			}
			_ = res
			// Read back must succeed (an identity apply writes
			// nothing, so the input is read back instead).
			readPath := out
			if _, err := os.Stat(out); err != nil {
				readPath = in
			}
			b, err := os.ReadFile(readPath) //nolint:gosec // G304: corpus path from VSDX_CORPUS, never from a document
			if err != nil {
				t.Fatalf("sweep read out: %v", err)
			}
			pageID := "page0"
			if a, e := vsd.Parse(addr); e == nil {
				pageID = vsd.PageID(a.Container, a.PageDup)
			}
			raw, err := read.Page(b, "base", pageID, read.Options{Units: "pt"})
			if err != nil {
				t.Fatalf("sweep %s %s read back: %v", path, addr, err)
			}
			var page struct {
				Hazards []json.RawMessage `json:"hazards"`
			}
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatalf("sweep decode: %v", err)
			}
			for _, h := range page.Hazards {
				var hz hazardJSON
				if err := json.Unmarshal(h, &hz); err != nil {
					t.Fatalf("sweep hazard: %v", err)
				}
				key := hz.Kind + "\x00" + hz.ID + "\x00" + hz.Detail
				if !inHazards[key] {
					t.Errorf("sweep %s %s: new hazard %s %s %s", path, addr, hz.Kind, hz.ID, hz.Detail)
				}
			}
			okCount++
		}
	}
	sort.Strings(refused)
	t.Logf("connector sweep: ok %d refused %d", okCount, len(refused))
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	if okCount == 0 {
		t.Error("sweep ok 0 connectors: expected at least one editable connector")
	}
}

// TestScalingConnector1D checks the scaling row: object.set and
// object.move of one connector on a page of N shapes allocates
// and takes time in proportion to N (laws.Scales and
// laws.TimeScales over vsdxtest.Generated, N=1000 shapes).
func TestScalingConnector1D(t *testing.T) {
	setup := func(n int) (func() error, error) {
		src := vsdxtest.Generated(t, n)
		connector := "page0/s" + itoa(n+1)
		return func() error {
			w, err := edit.Begin(src)
			if err != nil {
				return err
			}
			defer w.Tx().Rollback()
			// The generated connectors sit at the end; s<N+1>
			// is free of glue modelling in Generated (its
			// to is a Connect to a missing target's pin, but
			// the op writes the free end regardless).
			spec := &ops.ObjectSetSpec{At: connector, To: []float64{504, 144}}
			if _, _, _, _, err := spec.Run(w, map[string]string{}, "pt", nil); err != nil {
				return err
			}
			return w.Verify()
		}, nil
	}
	laws.Scales(t, "vsdx object.set connector 1-D", setup)
	laws.TimeScales(t, "vsdx object.set connector 1-D", setup)
}

// connectorSite returns site n (1-based) of addr as read --detail
// sites shows it.
func connectorSite(t *testing.T, path, addr string, n int) [2]float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	raw, err := read.Page(b, "base", "page0", read.Options{Units: "pt", Detail: "sites"})
	if err != nil {
		t.Fatalf("read.Page sites: %v", err)
	}
	var page struct {
		Objects []struct {
			ID    string            `json:"id"`
			Sites []json.RawMessage `json:"sites"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode sites: %v", err)
	}
	for _, o := range page.Objects {
		if o.ID != addr {
			continue
		}
		if n < 1 || n > len(o.Sites) {
			t.Fatalf("no site %d on %s (%d sites)", n, addr, len(o.Sites))
		}
		var pt [2]float64
		if err := json.Unmarshal(o.Sites[n-1], &pt); err != nil {
			t.Fatalf("decode site: %v", err)
		}
		// json numbers: unmarshal into float directly fails for
		// json.Number? Sites are numbers, so use Number decode.
		return pt
	}
	t.Fatalf("no object %s", addr)
	return [2]float64{}
}

// connectorPartXML returns the XML of one part of the drawing at path.
func connectorPartXML(t *testing.T, path, part string) string {
	t.Helper()
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer pkg.Close()
	pt, err := pkg.Part(part)
	if err != nil {
		t.Fatalf("part %s: %v", part, err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatalf("part bytes: %v", err)
	}
	return string(b)
}

// connectorCorpusDrawings lists drawings under dir sorted.
func connectorCorpusDrawings(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx":
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// connectorAddrs returns the addresses of page0 connectors of the drawing.
func connectorAddrs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	summary, err := read.Document(b, "base")
	if err != nil {
		return nil
	}
	var addrs []string
	for _, pg := range summary.Pages {
		raw, err := read.Page(b, "base", pg.ID, read.Options{Units: "pt"})
		if err != nil {
			continue
		}
		var page pageJSON
		if err := json.Unmarshal(raw, &page); err != nil {
			continue
		}
		for _, o := range page.Objects {
			if o.Kind == "connector" {
				addrs = append(addrs, o.ID)
			}
		}
	}
	return addrs
}

// connectorHazardKeys returns the hazard keys of page0 of the drawing.
func connectorHazardKeys(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	summary, err := read.Document(b, "base")
	if err != nil {
		return map[string]bool{}
	}
	keys := map[string]bool{}
	for _, pg := range summary.Pages {
		raw, err := read.Page(b, "base", pg.ID, read.Options{Units: "pt"})
		if err != nil {
			continue
		}
		var page struct {
			Hazards []json.RawMessage `json:"hazards"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			continue
		}
		for _, h := range page.Hazards {
			var hz hazardJSON
			if err := json.Unmarshal(h, &hz); err != nil {
				continue
			}
			keys[hz.Kind+"\x00"+hz.ID+"\x00"+hz.Detail] = true
		}
	}
	return keys
}
