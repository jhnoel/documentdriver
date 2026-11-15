package opc

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const (
	nsA      = "http://schemas.openxmlformats.org/drawingml/2006/main"
	slide1   = "/ppt/slides/slide1.xml"
	ctStream = "[Content_Types].xml"
)

func open(t testing.TB, path string) *Package {
	t.Helper()
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// roundTripAll parses every XML part of p and checks that serialising the
// untouched tree reproduces the stored bytes. It returns the number of XML
// parts checked.
func roundTripAll(t testing.TB, p *Package) int {
	t.Helper()
	n := 0
	for _, name := range p.Names() {
		if !p.IsXML(name) {
			continue
		}
		pt, err := p.Part(name)
		if err != nil {
			t.Fatal(err)
		}
		orig, err := pt.Original()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		doc, err := pt.Tree()
		if err != nil {
			t.Errorf("%s: %s: %v", p.Path(), name, err)
			continue
		}
		if !bytes.Equal(doc.Bytes(), orig) {
			t.Errorf("%s: %s: serialisation differs", p.Path(), name)
		}
		n++
	}
	return n
}

func TestFixtureRoundTrip(t *testing.T) {
	for name, path := range testdeck.All(t) {
		t.Run(name, func(t *testing.T) {
			p := open(t, path)
			if n := roundTripAll(t, p); n < 8 {
				t.Fatalf("only %d XML parts checked", n)
			}
			if mod, err := p.Modified(); err != nil || mod {
				t.Fatalf("Modified = %v, %v after parsing only", mod, err)
			}
		})
	}
}

// setOff sets x on the first a:off in the slide (resolved by namespace, so
// it works whatever prefix the deck uses) and returns the old value.
func setOff(t testing.TB, p *Package, part, val string) string {
	t.Helper()
	pt, err := p.Part(part)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := pt.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var off *tree.Node
	for _, n := range doc.Root().FindAll(nsA, "off") {
		if n.Parent().Parent().Local() == "spPr" {
			off = n
			break
		}
	}
	if off == nil {
		t.Fatal("no shape a:off")
	}
	old, _ := off.Attr("x")
	if err := off.SetAttr("x", val); err != nil {
		t.Fatal(err)
	}
	return old
}

func TestSetAttrChangesOnlyThatAttribute(t *testing.T) {
	for name, path := range testdeck.All(t) {
		t.Run(name, func(t *testing.T) {
			before, comment := rawEntries(t, path)
			p := open(t, path)
			pt, _ := p.Part(slide1)
			orig, _ := pt.Original()
			old := setOff(t, p, slide1, "123456")
			if err := p.Save(); err != nil {
				t.Fatal(err)
			}
			after, comment2 := rawEntries(t, path)
			if comment != comment2 {
				t.Errorf("archive comment %q became %q", comment, comment2)
			}
			if len(before) != len(after) {
				t.Fatalf("%d members became %d", len(before), len(after))
			}
			for i := range before {
				b, a := before[i], after[i]
				if b.hdr.Name != a.hdr.Name {
					t.Fatalf("member order changed at %d: %s -> %s", i, b.hdr.Name, a.hdr.Name)
				}
				if "/"+b.hdr.Name == slide1 {
					continue
				}
				if d := sameEntry(b, a); d != "" {
					t.Errorf("untouched member %s changed: %s", b.hdr.Name, d)
				}
				if name == "minimal" && !bytes.Equal(b.local, a.local) {
					t.Errorf("untouched member %s: local header changed", b.hdr.Name)
				}
			}
			// The slide differs exactly in that attribute value.
			p2 := open(t, path)
			pt2, _ := p2.Part(slide1)
			got, _ := pt2.Bytes()
			i := bytes.Index(orig, []byte(`x="`+old+`"`))
			if i < 0 {
				i = bytes.Index(orig, []byte(`x='`+old+`'`))
			}
			want := append(append(bytes.Clone(orig[:i+3]), "123456"...), orig[i+3+len(old):]...)
			if !bytes.Equal(got, want) {
				t.Fatalf("slide bytes differ beyond the attribute:\n%s", got)
			}
		})
	}
}

func TestModifiedMemberKeepsHeader(t *testing.T) {
	path := testdeck.Minimal(t)
	before, _ := rawEntries(t, path)
	p := open(t, path)
	setOff(t, p, slide1, "1")
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := rawEntries(t, path)
	for i := range before {
		if "/"+before[i].hdr.Name != slide1 {
			continue
		}
		b, a := before[i].hdr, after[i].hdr
		if b.Method != a.Method || b.ModifiedDate != a.ModifiedDate || b.ModifiedTime != a.ModifiedTime || //nolint:staticcheck // SA1019: compares the raw MS-DOS fields the writer must preserve
			b.Flags != a.Flags || b.CreatorVersion != a.CreatorVersion || !bytes.Equal(b.Extra, a.Extra) {
			t.Fatalf("modified member header changed:\n%+v\n%+v", b, a)
		}
	}
}

// snapshot captures a file's bytes and modification time.
type snapshot struct {
	data  []byte
	mtime time.Time
}

func snap(t testing.TB, path string) snapshot {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot{b, info.ModTime()}
}

// ageFile sets the file's mtime into the past so that any rewrite shows.
func ageFile(t testing.TB, path string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func assertUntouched(t testing.TB, path string, s snapshot) {
	t.Helper()
	now := snap(t, path)
	if !bytes.Equal(now.data, s.data) {
		t.Fatal("file bytes changed")
	}
	if !now.mtime.Equal(s.mtime) {
		t.Fatalf("file mtime changed: %v -> %v", s.mtime, now.mtime)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("stray file %s left in directory", e.Name())
		}
	}
}

func TestNoOpSaveWritesNothing(t *testing.T) {
	cases := []struct {
		name string
		edit func(t *testing.T, p *Package)
	}{
		{"nothing", func(_ *testing.T, _ *Package) {}},
		{"parse everything", func(t *testing.T, p *Package) { roundTripAll(t, p) }},
		{"set unchanged value", func(t *testing.T, p *Package) {
			old := setOff(t, p, slide1, "1")
			setOff(t, p, slide1, old)
		}},
		{"set unchanged bytes", func(_ *testing.T, p *Package) {
			pt, _ := p.Part(slide1)
			b, _ := pt.Original()
			pt.SetBytes(b)
		}},
		{"insert then remove", func(t *testing.T, p *Package) {
			ct, err := p.ContentTypes()
			if err != nil {
				t.Fatal(err)
			}
			if err := ct.SetOverride("/x.xml", "application/x"); err != nil {
				t.Fatal(err)
			}
			ct.RemoveOverride("/x.xml")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := testdeck.Minimal(t)
			ageFile(t, path)
			s := snap(t, path)
			p := open(t, path)
			c.edit(t, p)
			if mod, _ := p.Modified(); mod {
				t.Fatal("package reports modified")
			}
			if err := p.Save(); err != nil {
				t.Fatal(err)
			}
			assertUntouched(t, path, s)
		})
	}
}

func TestMalformedEditWritesNothing(t *testing.T) {
	path := testdeck.Minimal(t)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	setOff(t, p, slide1, "42") // a valid edit in the same save
	pt, _ := p.Part("/ppt/presentation.xml")
	pt.SetBytes([]byte(`<p:presentation><unclosed></p:presentation>`))
	err := p.Save()
	if err == nil || !strings.Contains(err.Error(), "not well formed") {
		t.Fatalf("Save err = %v", err)
	}
	assertUntouched(t, path, s)
}

func TestFailureAfterWriteLeavesNoTempFile(t *testing.T) {
	path := testdeck.Minimal(t)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	setOff(t, p, slide1, "42")
	var sawTemp bool
	defer SetFaultHook(func(stage, tmp string) error {
		if stage != StageRename {
			return nil
		}
		_, err := os.Stat(tmp)
		sawTemp = err == nil && filepath.Dir(tmp) == filepath.Dir(path)
		return errors.New("injected failure before rename")
	})()
	if err := p.Save(); err == nil {
		t.Fatal("Save succeeded")
	}
	if !sawTemp {
		t.Fatal("temporary file was not written next to the target")
	}
	assertUntouched(t, path, s)
}

func TestSaveRefusesFileChangedOnDisk(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	setOff(t, p, slide1, "42")
	other := []byte("someone else's bytes")
	if err := os.WriteFile(path, other, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("Save err = %v", err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, other) {
		t.Fatal("file overwritten")
	}
}

func TestNextRelationshipID(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	rels, err := p.Rels(slide1)
	if err != nil {
		t.Fatal(err)
	}
	if got := rels.NextID(); got != "rId3" {
		t.Fatalf("NextID = %s", got)
	}
	// Types come from the deck itself, never from memory.
	imageType := rels.All()[1].Type
	id, err := rels.Add(imageType, "../media/image1.png", false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "rId3" {
		t.Fatalf("Add id = %s", id)
	}
	ext, err := rels.Add("http://example.com/rel", "https://example.com/a?b&c", true)
	if err != nil || ext != "rId4" {
		t.Fatalf("external Add = %s, %v", ext, err)
	}
	relsPart, _ := p.Part(rels.Name())
	orig, _ := relsPart.Original()
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	p2 := open(t, path)
	pt, _ := p2.Part(rels.Name())
	got, _ := pt.Bytes()
	added := `<Relationship Id="rId3" Type="` + imageType + `" Target="../media/image1.png"/>` +
		`<Relationship Id="rId4" Type="http://example.com/rel" Target="https://example.com/a?b&amp;c" TargetMode="External"/>`
	want := bytes.Replace(orig, []byte("</Relationships>"), []byte(added+"</Relationships>"), 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("rels part:\n got %s\nwant %s", got, want)
	}
	r2, _ := p2.Rels(slide1)
	rel, ok := r2.Get("rId4")
	if !ok || !rel.External() || rel.Target != "https://example.com/a?b&c" {
		t.Fatalf("Get rId4 = %+v %v", rel, ok)
	}
	if tp, _ := r2.TargetPart(r2.All()[0]); tp != "/ppt/slideLayouts/slideLayout1.xml" {
		t.Fatalf("TargetPart = %s", tp)
	}
}

func TestNextIDSkipsGapsAndOddIDs(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	rels, _ := p.Rels(slide1)
	rels.Remove("rId1")
	if _, err := rels.Add("t", "x", true); err != nil { // max(rId2)+1 = rId3; rId1 is not reused
		t.Fatal(err)
	}
	if _, ok := rels.Get("rId1"); ok {
		t.Fatal("rId1 reused")
	}
	if _, ok := rels.Get("rId3"); !ok {
		t.Fatal("rId3 missing")
	}
}

func TestAddPartAddsOverrideThroughTree(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	ct, err := p.ContentTypes()
	if err != nil {
		t.Fatal(err)
	}
	slideType, ok := ct.Lookup(slide1)
	if !ok {
		t.Fatal("no slide content type")
	}
	ctPart, _ := p.Part(ContentTypesName)
	orig, _ := ctPart.Original()
	slide1Bytes, _ := func() ([]byte, error) { pt, _ := p.Part(slide1); return pt.Original() }()
	if _, err := p.AddPart("/ppt/slides/slide2.xml", slide1Bytes, slideType); err != nil {
		t.Fatal(err)
	}
	// A PNG needs no override: the Default for png already says image/png.
	pngType, _ := ct.Default("png")
	if _, err := p.AddPart("/ppt/media/image2.png", []byte("not really a png"), pngType); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	p2 := open(t, path)
	ctp, _ := p2.Part(ContentTypesName)
	got, _ := ctp.Bytes()
	last := `<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>`
	want := bytes.Replace(orig, []byte(last), []byte(last+`<Override PartName="/ppt/slides/slide2.xml" ContentType="`+slideType+`"/>`), 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("content types:\n got %s\nwant %s", got, want)
	}
	names := p2.Names()
	if names[len(names)-2] != "/ppt/slides/slide2.xml" || names[len(names)-1] != "/ppt/media/image2.png" {
		t.Fatalf("added parts not appended in order: %v", names)
	}
	if got, _ := p2.ContentTypes(); got != nil {
		if typ, _ := got.Lookup("/ppt/slides/slide2.xml"); typ != slideType {
			t.Fatalf("slide2 type = %q", typ)
		}
	}
}

func TestRelsPartCreatedOnFirstAdd(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	const theme = "/ppt/theme/theme1.xml"
	rels, _ := p.Rels(theme)
	if len(rels.All()) != 0 || p.Has(rels.Name()) {
		t.Fatal("theme should have no rels")
	}
	if _, err := rels.Add("http://example.com/t", "../media/image1.png", false); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	p2 := open(t, path)
	pt, err := p2.Part("/ppt/theme/_rels/theme1.xml.rels")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := pt.Bytes()
	want := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://example.com/t" Target="../media/image1.png"/></Relationships>`
	if string(got) != want {
		t.Fatalf("new rels part:\n got %s\nwant %s", got, want)
	}
	ct, _ := p2.ContentTypes()
	if typ, _ := ct.Lookup(pt.Name()); typ != "application/vnd.openxmlformats-package.relationships+xml" {
		t.Fatalf("rels content type = %q", typ)
	}
}

func TestDeletePart(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	if err := p.DeletePart("/ppt/theme/theme1.xml"); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	p2 := open(t, path)
	if p2.Has("/ppt/theme/theme1.xml") {
		t.Fatal("part still present")
	}
	ct, _ := p2.ContentTypes()
	if _, ok := ct.Override("/ppt/theme/theme1.xml"); ok {
		t.Fatal("override still present")
	}
}

func TestPartLookupIsCaseInsensitive(t *testing.T) {
	p := open(t, testdeck.Minimal(t))
	if !p.Has("/PPT/Slides/Slide1.XML") || !p.Has("ppt/slides/slide1.xml") {
		t.Fatal("lookup failed")
	}
	if _, err := p.Part("/nope.xml"); !errors.Is(err, ErrNoPart) {
		t.Fatalf("err = %v", err)
	}
}

// TestUntouchedCorruptMemberIsCopied shows that members are read lazily and
// never validated unless touched: a member with corrupt compressed data
// survives an edit elsewhere byte for byte.
func TestUntouchedCorruptMemberIsCopied(t *testing.T) {
	path := testdeck.Minimal(t)
	entries, _ := rawEntries(t, path)
	file, _ := os.ReadFile(path)
	var png rawEntry
	for _, e := range entries {
		if strings.HasSuffix(e.hdr.Name, ".png") {
			png = e
		}
	}
	// Flip a byte inside the stored PNG data; its CRC no longer matches.
	i := bytes.Index(file, png.raw)
	file[i+len(png.raw)/2] ^= 0xFF
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := rawEntries(t, path)
	p := open(t, path)
	setOff(t, p, slide1, "7")
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := rawEntries(t, path)
	for k := range before {
		if before[k].hdr.Name == png.hdr.Name {
			if d := sameEntry(before[k], after[k]); d != "" {
				t.Fatalf("corrupt member changed: %s", d)
			}
		}
	}
	pt, _ := open(t, path).Part("/ppt/media/image1.png")
	if _, err := pt.Bytes(); err == nil {
		t.Fatal("expected checksum error reading the corrupt member")
	}
}

func TestSaveAs(t *testing.T) {
	path := testdeck.Quirky(t)
	s := snap(t, path)
	p := open(t, path)
	dst := filepath.Join(t.TempDir(), "copy.pptx")
	if err := p.SaveAs(dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); !bytes.Equal(b, s.data) {
		t.Fatal("no-op SaveAs is not a byte copy")
	}
	setOff(t, p, slide1, "9")
	dst2 := filepath.Join(t.TempDir(), "edited.pptx")
	if err := p.SaveAs(dst2); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, s)
	p2 := open(t, dst2)
	if n := roundTripAll(t, p2); n == 0 {
		t.Fatal("no parts")
	}
}

func TestSaveReopens(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	setOff(t, p, slide1, "1")
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	if mod, _ := p.Modified(); mod {
		t.Fatal("modified after save")
	}
	setOff(t, p, slide1, "2")
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	pt, _ := open(t, path).Part(slide1)
	b, _ := pt.Bytes()
	if !bytes.Contains(b, []byte(`<a:off x="2" y="1828800"/>`)) {
		t.Fatal("second save lost")
	}
}

func TestOpenBytesReadsLikeOpen(t *testing.T) {
	for name, path := range testdeck.All(t) {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := OpenBytes(b, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			f := open(t, path)
			if got, want := strings.Join(p.Names(), ","), strings.Join(f.Names(), ","); got != want {
				t.Fatalf("Names = %s, want %s", got, want)
			}
			if n := roundTripAll(t, p); n < 8 {
				t.Fatalf("only %d XML parts checked", n)
			}
		})
	}
	if _, err := OpenBytes([]byte("not a zip"), Limits{}); err == nil {
		t.Error("OpenBytes accepted bytes that are not a zip")
	}
}

func TestOpenBytesCannotSave(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("a.xml"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := OpenBytes(buf.Bytes(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); !errors.Is(err, ErrInMemory) {
		t.Errorf("Save = %v, want ErrInMemory", err)
	}
	if err := p.SaveAs("out.zip"); !errors.Is(err, ErrInMemory) {
		t.Errorf("SaveAs = %v, want ErrInMemory", err)
	}
	if err := p.SaveAndClose(); !errors.Is(err, ErrInMemory) {
		t.Errorf("SaveAndClose = %v, want ErrInMemory", err)
	}
}
