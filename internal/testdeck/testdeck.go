// Package testdeck builds the small .pptx fixtures used by tests from XML
// committed under testdata/decks, and locates the other committed decks. It
// is imported only by tests.
package testdeck

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Root returns the repository root.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// officeTime is the timestamp PowerPoint writes on every member.
var officeTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// member is one zip entry of a fixture.
type member struct {
	name   string
	data   []byte
	method uint16
}

// Minimal builds the hand-written minimal deck the way PowerPoint lays out
// its zips: [Content_Types].xml first, sizes in the local headers, no data
// descriptors, 1980 timestamps. It returns the path of a fresh copy in a
// temporary directory.
func Minimal(t testing.TB) string {
	t.Helper()
	ms := readDeck(t, "minimal")
	ms = append(ms, member{name: "ppt/media/image1.png", data: pngBytes(t), method: zip.Store})
	return write(t, "minimal.pptx", ms, false, "")
}

// MinimalWith builds the minimal deck with the members named in over
// (zip names, such as "ppt/slides/slide1.xml") replaced or added, stored
// uncompressed so that building is cheap enough for fuzzing. The bytes need
// not be well formed.
func MinimalWith(t testing.TB, over map[string][]byte) string {
	t.Helper()
	ms := readDeck(t, "minimal")
	ms = append(ms, member{name: "ppt/media/image1.png", data: pngBytes(t), method: zip.Store})
	for i := range ms {
		if b, ok := over[ms[i].name]; ok {
			ms[i].data, ms[i].method = b, zip.Store
		}
	}
	for name, b := range over {
		if !slices.ContainsFunc(ms, func(m member) bool { return m.name == name }) {
			ms = append(ms, member{name: name, data: b, method: zip.Store})
		}
	}
	return write(t, "minimal.pptx", ms, false, "")
}

// DeepGroupsSlide returns a slide whose deepest element is nested depth
// elements deep (p:sld is depth 1): depth-8 groups, each inside the last,
// around one shape with text. Groups are ids 2 upwards, outermost first;
// the shape's id is DeepGroupsShapeID(depth). depth must be at least 9.
func DeepGroupsSlide(depth int) []byte {
	groups := depth - 8
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
		`<p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`)
	for i := range groups {
		id := strconv.Itoa(i + 2)
		b.WriteString(`<p:grpSp><p:nvGrpSpPr><p:cNvPr id="` + id + `" name="g` + id + `"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
			`<p:grpSpPr><a:xfrm><a:off x="12700" y="12700"/><a:ext cx="9144000" cy="5143500"/><a:chOff x="0" y="0"/><a:chExt cx="9144000" cy="5143500"/></a:xfrm></p:grpSpPr>`)
	}
	id := strconv.Itoa(DeepGroupsShapeID(depth))
	b.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="deep"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="914400" y="914400"/><a:ext cx="914400" cy="457200"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>deep</a:t></a:r></a:p></p:txBody></p:sp>`)
	b.WriteString(strings.Repeat(`</p:grpSp>`, groups))
	b.WriteString(`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	return []byte(b.String())
}

// ManyShapesSlide returns a slide of n text shapes in a grid, ids 2 to
// n+1, for the scaling gate.
func ManyShapesSlide(n int) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
		`<p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`)
	for i := range n {
		id := strconv.Itoa(i + 2)
		x, y := strconv.Itoa(12700*(i%100)), strconv.Itoa(12700*(i/100))
		b.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="s` + id + `"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>` +
			`<p:spPr><a:xfrm><a:off x="` + x + `" y="` + y + `"/><a:ext cx="914400" cy="457200"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
			`<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" b="1"/><a:t>Shape ` + id + `</a:t></a:r><a:r><a:t> with text</a:t></a:r></a:p></p:txBody></p:sp>`)
	}
	b.WriteString(`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	return []byte(b.String())
}

// DeepGroupsShapeID is the id of the shape inside DeepGroupsSlide(depth).
func DeepGroupsShapeID(depth int) int { return depth - 8 + 2 }

// Quirky builds a deck whose XML exercises tokenizer edge cases (BOM, CRLF,
// single quotes, comments, PIs, CDATA, mc:AlternateContent, references,
// unusual whitespace) and whose zip uses data descriptors, extended
// timestamp extra fields, a directory entry and an archive comment.
func Quirky(t testing.TB) string {
	t.Helper()
	ms := readDeck(t, "minimal")
	for i := range ms {
		if q, ok := quirks[ms[i].name]; ok {
			ms[i].data = []byte(q)
		}
	}
	ms = append(ms,
		member{name: "ppt/media/image1.png", data: pngBytes(t), method: zip.Store},
		member{name: "customXml/item1.xml", data: []byte(customXML), method: zip.Store},
	)
	return write(t, "quirky.pptx", ms, true, "fixture comment")
}

// Diagram builds the minimal deck with its slide, layout and master
// replaced by the hand-written diagram fixture under testdata/decks/diagram:
// placeholders that inherit their boxes and text defaults, glued straight
// and elbow connectors, flipped and rotated groups, mc:AlternateContent,
// duplicate ids, custom geometry, style references and deliberate hazards.
func Diagram(t testing.TB) string {
	t.Helper()
	ms := readDeck(t, "minimal")
	over := map[string][]byte{}
	for _, m := range readDeck(t, "diagram") {
		over[m.name] = m.data
	}
	for i := range ms {
		if b, ok := over[ms[i].name]; ok {
			ms[i].data = b
		}
	}
	ms = append(ms, member{name: "ppt/media/image1.png", data: pngBytes(t), method: zip.Store})
	return write(t, "diagram.pptx", ms, false, "")
}

// SOffice returns a temporary copy of the deck LibreOffice produced from
// testdata/soffice/diagram.fodp.
func SOffice(t testing.TB) string {
	t.Helper()
	return Copy(t, filepath.Join(Root(), "testdata", "soffice", "diagram.pptx"))
}

// All returns temporary copies of every committed fixture deck, by name.
func All(t testing.TB) map[string]string {
	t.Helper()
	return map[string]string{
		"minimal": Minimal(t),
		"quirky":  Quirky(t),
		"soffice": SOffice(t),
		"diagram": Diagram(t),
	}
}

// Copy copies a file into a new temporary directory and returns the copy's
// path.
func Copy(t testing.TB, src string) string {
	t.Helper()
	b, err := os.ReadFile(src) //nolint:gosec // G304: test helper reading a fixture path the test names
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(dst, b, 0o644); err != nil { //nolint:gosec // G306: test fixture under t.TempDir()
		t.Fatal(err)
	}
	return dst
}

// Corpus returns every .pptx under $PPTX_CORPUS, or skips the test if the
// variable is unset.
func Corpus(t testing.TB) []string {
	t.Helper()
	dir := os.Getenv("PPTX_CORPUS")
	if dir == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { //nolint:gosec // G703: the corpus directory is the developer's PPTX_CORPUS
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".pptx") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

// Native returns the PowerPoint-native decks listed in
// $PPTX_CORPUS/manifest.jsonl, resolved under $PPTX_CORPUS/repos, in
// sorted order; it skips the test if the variable is unset. Decks missing
// on disk are listed anyway, for the caller to count.
func Native(t testing.TB) []string {
	t.Helper()
	dir := os.Getenv("PPTX_CORPUS")
	if dir == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.jsonl")) //nolint:gosec // G304: the manifest of the corpus the caller named in PPTX_CORPUS
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			Source string `json:"source"`
			Path   string `json:"path"`
			Native bool   `json:"powerpoint_native"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.Native {
			out = append(out, filepath.Join(dir, "repos", e.Source, e.Path))
		}
	}
	sort.Strings(out)
	return out
}

// readDeck reads the deck fixture under testdata/decks/name into sorted zip members.
func readDeck(t testing.TB, name string) []member {
	t.Helper()
	dir := filepath.Join(Root(), "testdata", "decks", name)
	var ms []member
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p) //nolint:gosec // G304: test helper reading a fixture path the test names
		if err != nil {
			return err
		}
		ms = append(ms, member{name: filepath.ToSlash(rel), data: b, method: zip.Deflate})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(ms, func(i, j int) bool {
		ci, cj := ms[i].name == "[Content_Types].xml", ms[j].name == "[Content_Types].xml"
		if ci != cj {
			return ci
		}
		return ms[i].name < ms[j].name
	})
	return ms
}

// pngBytes returns the encoded bytes of a small fixed PNG image.
func pngBytes(t testing.TB) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{0x44, 0x72, 0xC4, 0xFF})
	img.Set(1, 1, color.RGBA{0xED, 0x7D, 0x31, 0xFF})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// write zips ms into a temporary file. Office style (quirky=false) writes
// raw entries with sizes in the local header; quirky style lets
// archive/zip add data descriptors and extended timestamps.
func write(t testing.TB, name string, ms []member, quirky bool, comment string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if quirky {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: "ppt/", Modified: officeTime.Add(24 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range ms {
		var err error
		if quirky {
			var w io.Writer
			w, err = zw.CreateHeader(&zip.FileHeader{Name: m.name, Method: m.method, Modified: officeTime.Add(24 * time.Hour)})
			if err == nil {
				_, err = w.Write(m.data)
			}
		} else {
			err = writeRaw(zw, m)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if comment != "" {
		if err := zw.SetComment(comment); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: test fixture under t.TempDir()
		t.Fatal(err)
	}
	return p
}

// writeRaw writes one member raw to zw, compressing it when deflated.
func writeRaw(zw *zip.Writer, m member) error {
	body := m.data
	if m.method == zip.Deflate {
		var b bytes.Buffer
		fw, err := flate.NewWriter(&b, flate.BestCompression)
		if err != nil {
			return err
		}
		if _, err := fw.Write(m.data); err != nil {
			return err
		}
		if err := fw.Close(); err != nil {
			return err
		}
		body = b.Bytes()
	}
	fh := &zip.FileHeader{
		Name:               m.name,
		Method:             m.method,
		CRC32:              crc32.ChecksumIEEE(m.data),
		CompressedSize64:   uint64(len(body)),
		UncompressedSize64: uint64(len(m.data)),
		CreatorVersion:     45,
		ReaderVersion:      20,
	}
	// 1980-01-01 00:00 in MS-DOS format. CreateRaw writes these fields as
	// given; Modified applies only to CreateHeader and adds an extra field.
	fh.ModifiedDate, fh.ModifiedTime = 0x21, 0 //nolint:staticcheck // SA1019: see above
	w, err := zw.CreateRaw(fh)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
