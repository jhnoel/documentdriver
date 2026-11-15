package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

func touch(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectAndPair(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a--source.pptx", "a--move-11.pptx", "a--move-22.pptx", "a--noop-33.pptx",
		"b--set-44.pptx", "plain.pptx", "~$a--source.pptx", "notes.txt")
	files, err := collect([]string{dir, filepath.Join(dir, "plain.pptx")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("collect: %v", files)
	}
	j := func(n string) string { return filepath.Join(dir, n) }

	got, err := pair(files, false, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []group{
		{Name: "a", Source: j("a--source.pptx"), Outputs: []string{j("a--move-11.pptx"), j("a--noop-33.pptx")}},
		{Name: "b", Outputs: []string{j("b--set-44.pptx")}},
		{Name: "plain", Source: j("plain.pptx")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pair:\n got %+v\nwant %+v", got, want)
	}

	got, _ = pair(files, true, "")
	if n := len(got[0].Outputs); n != 3 {
		t.Fatalf("--all kept %d outputs of a, want 3", n)
	}

	got, err = pair([]string{j("plain.pptx"), j("b--set-44.pptx")}, false, j("plain.pptx"))
	if err != nil || len(got) != 1 || got[0].Source != j("plain.pptx") || len(got[0].Outputs) != 1 {
		t.Fatalf("explicit source: %+v %v", got, err)
	}
	if _, err := pair([]string{j("plain.pptx")}, false, j("plain.pptx")); err == nil {
		t.Fatal("explicit source with no outputs should fail")
	}
	if _, err := pair([]string{j("a--source.pptx"), j("a.pptx")}, false, ""); err == nil {
		t.Fatal("two sources for one name should fail")
	}
}

// buildDeck writes a minimal package whose slide list order differs from
// the part numbering: slide2 is first.
func buildDeck(t *testing.T) string { return buildDeckWith(t, strings.NewReplacer()) }

// buildStrictDeck is buildDeck in the Strict conformance class.
func buildStrictDeck(t *testing.T) string {
	return buildDeckWith(t, strings.NewReplacer(
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships", "http://purl.oclc.org/ooxml/officeDocument/relationships",
		"http://schemas.openxmlformats.org/presentationml/2006/main", "http://purl.oclc.org/ooxml/presentationml/main",
	))
}

func buildDeckWith(t *testing.T, ns *strings.Replacer) string {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`,
		"ppt/presentation.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldIdLst><p:sldId id="256" r:id="rId9"/><p:sldId id="257" r:id="rId2"/><p:sldId id="258" r:id="rId3"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="/ppt/slides/slide10.xml"/><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "ppt/presentation.xml", "ppt/_rels/presentation.xml.rels"} {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(ns.Replace(parts[n]))); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "deck.pptx")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSlideMappingStrict(t *testing.T) {
	order, err := slideOrder(buildStrictDeck(t))
	if err != nil || len(order) != 3 || order[0] != "/ppt/slides/slide2.xml" {
		t.Fatalf("%v %v", order, err)
	}
}

func TestSlideMapping(t *testing.T) {
	deck := buildDeck(t)
	order, err := slideOrder(deck)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/ppt/slides/slide2.xml", "/ppt/slides/slide1.xml", "/ppt/slides/slide10.xml"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for in, idx := range map[string]int{"slide2": 1, "slide1": 2, "slides/slide10.xml": 3, "/ppt/slides/slide10.xml": 3, "Slide1": 2} {
		got, err := slideIndex(deck, in)
		if err != nil || got != idx {
			t.Errorf("slideIndex(%q) = %d, %v; want %d", in, got, err, idx)
		}
	}
	if _, err := slideIndex(deck, "slide3"); err == nil {
		t.Error("slide3 is not in the list")
	}
	if partBase("/ppt/slides/slide10.xml") != "slide10" {
		t.Error("partBase")
	}
}

func TestAppleScriptText(t *testing.T) {
	if got := officeapp.Quote(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Fatalf("quote: %s", got)
	}
	lines := officeapp.WithTimeout(7, scriptSaveAs(`/x/~$o "1".png`, "save as PNG")...)
	want := []string{
		"with timeout of 7 seconds",
		`set dst to (POSIX file "/x/~$o \"1\".png") as text`,
		`tell application "Microsoft PowerPoint" to save presentation 1 in dst as save as PNG`,
		"end timeout",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("got %q", lines)
	}
	if s := strings.Join(scriptWindowTitles(), "\n"); !strings.Contains(s, `process "Microsoft PowerPoint"`) {
		t.Fatalf("window script: %s", s)
	}
}

func TestParseOSAError(t *testing.T) {
	cases := map[string]int{
		"73:77: execution error: Microsoft PowerPoint got an error: AppleEvent timed out. (-1712)\n":               -1712,
		"0:1: execution error: Not authorized to send Apple events to Microsoft PowerPoint. (-1743)":               -1743,
		"106:130: execution error: System Events got an error: osascript is not allowed assistive access. (-1728)": -1728,
		"garbage": 0,
	}
	for in, want := range cases {
		if got := officeapp.ParseOSAError(in).Num; got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

func TestClassifyWindows(t *testing.T) {
	if s := classifyWindows([]string{"", "PowerPoint found a problem with content in x.pptx. PowerPoint can attempt to repair the presentation."}); s != statusRepair {
		t.Error(s)
	}
	if s := classifyWindows([]string{"Grant File Access"}); s != statusAccessDialog {
		t.Error(s)
	}
	if s := classifyWindows([]string{"x.pptx"}); s != "" {
		t.Error(s)
	}
}

func TestPDFPageCount(t *testing.T) {
	pdf := []byte("<< /Type /Pages /Kids [3 0 R 4 0 R] >> << /Type /Page /Parent 2 0 R >> <</Type/Page/Parent 2 0 R>>")
	if n := pdfPageCount(pdf); n != 2 {
		t.Fatalf("pages %d", n)
	}
}

// fakeApp simulates PowerPoint. behave maps a file name to how it opens:
// "clean", "dialog" (Apple events time out), "repair" (alert text
// readable), "crash", "deny" (-1743).
type fakeApp struct {
	behave    map[string]string
	assistive bool
	running   bool
	current   string
	key       string // behave key matching current (copies carry a run prefix)
	opened    []string
	quits     int
}

func (f *fakeApp) Running() (bool, error) {
	return f.running && f.behave[f.key] != "crash", nil
}

func (f *fakeApp) Open(path string) error {
	f.running = true
	f.current = filepath.Base(path)
	for k := range f.behave {
		if strings.HasSuffix(f.current, "--"+k) || f.current == k {
			f.key = k
		}
	}
	f.opened = append(f.opened, path)
	return nil
}

func (f *fakeApp) ForceQuit() error {
	f.running, f.current, f.key = false, "", ""
	f.quits++
	return nil
}

func (f *fakeApp) Run(lines []string, _ int) (string, error) {
	s := strings.Join(lines, "\n")
	b := f.behave[f.key]
	if strings.Contains(s, "System Events") {
		if !f.assistive {
			return "", &officeapp.OSAError{Num: errNoAssistive}
		}
		if b == "repair" {
			return "PowerPoint found a problem with content. PowerPoint can attempt to repair the presentation.", nil
		}
		return "", nil
	}
	switch b {
	case "clean":
		return f.current, nil
	case "deny":
		return "", &officeapp.OSAError{Num: errNotAuthorised}
	case "crash":
		return "", &officeapp.OSAError{Num: errAppNotRunning}
	case "stuck":
		return "missing value", nil
	case "other":
		return "Presentation1", nil
	}
	return "", &officeapp.OSAError{Num: errAETimeout}
}

// fakeTime makes the wait loops run instantly.
func fakeTime(t *testing.T) {
	now := time.Unix(0, 0)
	oldClock, oldSleep := clock, sleep
	clock = func() time.Time { return now }
	sleep = func(d time.Duration) { now = now.Add(d) }
	t.Cleanup(func() { clock, sleep = oldClock, oldSleep })
}

func TestOpenAndWait(t *testing.T) {
	fakeTime(t)
	for _, tc := range []struct {
		behave    string
		assistive bool
		want      string
		abort     bool
	}{
		{"clean", false, statusClean, false},
		{"dialog", false, statusDialog, false},
		{"repair", true, statusRepair, false},
		{"repair", false, statusDialog, false},
		{"crash", false, statusCrashed, false},
		{"stuck", false, statusDialog, false},
		{"other", false, statusNotListed, false},
		{"deny", false, statusError, true},
	} {
		f := &fakeApp{behave: map[string]string{"d.pptx": tc.behave}, assistive: tc.assistive}
		res, err := openAndWait(f, "/w/d.pptx", 10*time.Second)
		if res.Status != tc.want || errors.Is(err, errAbort) != tc.abort {
			t.Errorf("%s/%v: got %s %v, want %s abort=%v", tc.behave, tc.assistive, res.Status, err, tc.want, tc.abort)
		}
	}
}

func TestRunSmokeBaselineRule(t *testing.T) {
	fakeTime(t)
	src := t.TempDir()
	touch(t, src, "good--source.pptx", "good--move-1.pptx", "good--set-2.pptx", "bad--source.pptx", "bad--move-1.pptx")
	files, _ := collect([]string{src})
	groups, _ := pair(files, false, "")
	f := &fakeApp{behave: map[string]string{
		"good--source.pptx": "clean", "good--move-1.pptx": "clean", "good--set-2.pptx": "dialog",
		"bad--source.pptx": "dialog", "bad--move-1.pptx": "clean",
	}}
	work := t.TempDir()
	rd, err := newRunDir(work)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sum, err := runSmoke(f, rd, groups, 5*time.Second, reporter(&buf, true))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Opened != 4 || sum.Skipped != 1 || len(sum.Failures) != 1 || filepath.Base(sum.Failures[0]) != "good--set-2.pptx" {
		t.Fatalf("summary %+v", sum)
	}
	for _, p := range f.opened {
		if filepath.Dir(p) != work {
			t.Fatalf("opened %s, not a flat file in the work folder", p)
		}
	}
	var statuses []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, filepath.Base(r.File)+"="+r.Status)
	}
	want := []string{"bad--source.pptx=dialog", "bad--move-1.pptx=skipped", "good--source.pptx=clean", "good--move-1.pptx=clean", "good--set-2.pptx=dialog"}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("records %v", statuses)
	}
	ents, _ := os.ReadDir(rd.dir)
	if len(ents) != 0 {
		t.Fatalf("copies left behind: %v", ents)
	}
}

func TestRefusesWhilePowerPointRuns(t *testing.T) {
	f := &fakeApp{running: true}
	var out, errb bytes.Buffer
	code := run(f, []string{"smoke", t.TempDir()}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if f.quits != 0 || len(f.opened) != 0 {
		t.Fatal("touched a running PowerPoint")
	}
}

func TestUsageErrors(t *testing.T) {
	f := &fakeApp{}
	var out, errb bytes.Buffer
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"smoke"},
		{"render", "deck.pptx"},
		{"render", "deck.pptx", "--slide", "slide1"},
		{"resave", "deck.pptx"},
	} {
		if code := run(f, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if len(f.opened) != 0 {
		t.Fatal("opened PowerPoint on a usage error")
	}
}

func newTestFlagSet(sf *sessionFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	sf.register(fs)
	return fs
}

func TestParseInterleaved(t *testing.T) {
	var sf sessionFlags
	fs := newTestFlagSet(&sf)
	pos, err := parseInterleaved(fs, []string{"deck.pptx", "--timeout", "5", "x"})
	if err != nil || !reflect.DeepEqual(pos, []string{"deck.pptx", "x"}) || sf.timeout != 5 {
		t.Fatalf("%v %v %v", pos, err, sf.timeout)
	}
}

func TestRunDirFlat(t *testing.T) {
	work := t.TempDir()
	touch(t, work, "keep.pptx")
	rd, err := newRunDir(work)
	if err != nil {
		t.Fatal(err)
	}
	f := rd.file("deck.pptx")
	if filepath.Dir(f) != work || !strings.HasSuffix(f, "--deck.pptx") {
		t.Fatalf("not flat: %s", f)
	}
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "~$"+filepath.Base(f)), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rd.Close(); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(work)
	if len(ents) != 1 || ents[0].Name() != "keep.pptx" {
		t.Fatalf("left %v", ents)
	}
	if _, err := newRunDir(filepath.Join(work, "missing")); err == nil {
		t.Fatal("missing work folder must fail, not be created")
	}
}
