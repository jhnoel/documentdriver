package read

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

const (
	nsW   = model.NSW
	nsW14 = model.NSW14
	// docHead opens a document element declaring w and w14.
	docHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`
	docTail = `</w:body></w:document>`
)

// part is one zip member of a test package.
type part struct{ name, body string }

// buildDocx zips parts in order, stored, with a fixed time, so the bytes
// (and the source hash that seeds paragraph ids) are stable.
func buildDocx(t testing.TB, parts ...part) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Store, Modified: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func docOnly(t testing.TB, body string) []byte {
	return buildDocx(t, part{"word/document.xml", docHead + body + docTail})
}

func encodeResult(t *testing.T, r Result) string {
	t.Helper()
	s, err := EncodeResult(r, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// refused returns the contract error a read or find failed with.
func refused(t *testing.T, err error) *cli.Error {
	t.Helper()
	var e *cli.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a contract error", err)
	}
	return e
}

// Ported from bookmarks.rs: reads_cross_paragraph_point_and_deleted_text.
func TestBookmarksCrossParagraphPointAndDeletedText(t *testing.T) {
	x, err := model.ParseXML([]byte(`<w:document xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `"><w:body>
<w:p w14:paraId="11111111"><w:r><w:t xml:space="preserve">one </w:t></w:r><w:bookmarkStart w:id="1" w:name="Span"/><w:r><w:t>two</w:t></w:r><w:del w:id="9" w:author="a"><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>
<w:bookmarkStart w:id="2" w:name="_Between"/>
<w:p w14:paraId="22222222"><w:r><w:t>three</w:t></w:r><w:bookmarkEnd w:id="1"/><w:bookmarkStart w:id="3" w:name="Point"/><w:bookmarkEnd w:id="3"/></w:p>
<w:bookmarkEnd w:id="2"/>
</w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	idx := x.ResolveParagraphIDs(model.DocumentPart, [32]byte{})
	entries := walkStory(x, model.DocumentPart, func(n *tree.Node) *string {
		if id, ok := idx.IDs[n]; ok {
			return &id.ID
		}
		return nil
	})
	if len(entries) != 3 {
		t.Fatalf("%d entries", len(entries))
	}
	check := func(i int, name, text, start, end string) {
		t.Helper()
		e := entries[i]
		if e.name != name || e.text.String() != text || deref(e.start) != start || deref(e.end) != end {
			t.Errorf("entry %d = %s %q %s-%s, want %s %q %s-%s", i, e.name, e.text.String(), deref(e.start), deref(e.end), name, text, start, end)
		}
	}
	check(0, "Span", "two\nthree", "11111111", "22222222")
	check(1, "_Between", "three", "22222222", "22222222")
	check(2, "Point", "", "22222222", "22222222")
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestEncodeMatchesSerde(t *testing.T) {
	v := Object{
		"b": []any{}, "a": Object{}, "s": "q\"\\\b\f\n\r\t\x01\x7f/<é😀 ", "n": nil,
		"i": int64(-3), "u": uint32(4), "t": true, "l": []any{1, "x"},
	}
	compact, err := Encode(v, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{},"b":[],"i":-3,"l":[1,"x"],"n":null,"s":"q\"\\\b\f\n\r\t\u0001` + "\x7f/<é😀 " + `","t":true,"u":4}`
	if compact != want {
		t.Errorf("compact:\n got %s\nwant %s", compact, want)
	}
	pretty, err := Encode(Object{"a": []any{1, Object{"k": "v"}}, "e": []any{}}, true)
	if err != nil {
		t.Fatal(err)
	}
	wantPretty := "{\n  \"a\": [\n    1,\n    {\n      \"k\": \"v\"\n    }\n  ],\n  \"e\": []\n}"
	if pretty != wantPretty {
		t.Errorf("pretty:\n%s\nwant\n%s", pretty, wantPretty)
	}
	if _, err := Encode(Object{"x": 1.5}, false); err == nil {
		t.Error("Encode accepted a float")
	}
}

func TestRustLower(t *testing.T) {
	for in, want := range map[string]string{
		"ABC":      "abc",
		"İstanbul": "i̇stanbul",
		"ΟΔΟΣ":     "οδος",
		"ΟΔΟΣ ΑΒ":  "οδος αβ",
		"Σ":        "σ",
		"ΑΣ.":      "ας.",
		"ΑΣΑ":      "ασα",
		"A'Σ":      "a'ς",
		"ẞ":        "ß",
	} {
		if got := rustLower(in); got != want {
			t.Errorf("rustLower(%q) = %q, want %q", in, got, want)
		}
	}
	if got := rustUpper("aßﬁ0"); got != "ASSFI0" {
		t.Errorf("rustUpper = %q", got)
	}
}

// TestStrictReads: an ISO Strict document reads like a transitional one
// (docxdriver refuses it at open; editing it is refused per operation).
func TestStrictReads(t *testing.T) {
	strict := buildDocx(t, part{"word/document.xml", `<w:document xmlns:w="` + model.NSStrictW + `"><w:body>` +
		`<w:p><w:r><w:t>strict</w:t><w:tab/><w:t>text</w:t></w:r></w:p></w:body></w:document>`})
	r, err := Find(strict, "strict", false, "markup", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encodeResult(t, r), `"text":"strict\ttext"`) {
		t.Errorf("find on Strict: %s", encodeResult(t, r))
	}
	d, err := model.OpenDocument(strict)
	if err != nil || !d.IsStrict() {
		t.Errorf("IsStrict: %v", err)
	}
}

func TestRejections(t *testing.T) {
	for _, c := range []struct {
		name  string
		input []byte
		want  string
	}{
		{"not a zip", []byte("plain text"), "not a DOCX package: invalid Zip archive: Could not find EOCD"},
		{"no document", buildDocx(t, part{"word/other.xml", "<x/>"}), "package has no word/document.xml"},
		{"malformed", buildDocx(t, part{"word/document.xml", "<w:document>"}), "document.xml parse failed: "},
	} {
		t.Run(c.name, func(t *testing.T) {
			// A file that cannot be opened at all is invalid_document,
			// whatever is asked of it.
			for _, kind := range []string{"styles", "document"} {
				_, err := Read(c.input, kind, "", Options{})
				if e := refused(t, err); e.Code != cli.CodeInvalidDocument || !strings.HasPrefix(e.Message, c.want) {
					t.Errorf("read %s: %v, want invalid_document %q", kind, e, c.want)
				}
			}
			_, err := Find(c.input, "", false, "markup", Options{})
			if e := refused(t, err); e.Code != cli.CodeInvalidDocument || !strings.HasPrefix(e.Message, c.want) {
				t.Errorf("find: %v", e)
			}
		})
	}
	_, err := Read(docOnly(t, `<w:p/>`), "bogus", "", Options{})
	if e := refused(t, err); e.Code != cli.CodeUsage || e.Path != "--kind" {
		t.Errorf("unknown kind: %v", e)
	}
	if _, err := Read(docOnly(t, `<w:p/>`), "document_ui", "", Options{}); refused(t, err).Code != cli.CodeUsage {
		t.Errorf("document_ui is not a read kind of the contract: %v", err)
	}
}

// TestResultShape: every result is one object with base first, notes
// always present, snake_case keys and no null.
func TestResultShape(t *testing.T) {
	in := docOnly(t, `<w:p w14:paraId="00000001"><w:r><w:t>a</w:t></w:r></w:p>`)
	for _, kind := range Kinds {
		r, err := Read(in, kind, "", Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := encodeResult(t, r)
		if !strings.HasPrefix(got, `{"base":"`+SourceHash(in)+`",`) || !strings.Contains(got, `"notes":[]`) {
			t.Errorf("%s: base first and notes: %s", kind, got)
		}
		if strings.Contains(got, "null") || strings.Contains(got, `"source"`) || strings.Contains(got, `"outcome"`) {
			t.Errorf("%s: a null, a source or an outcome in %s", kind, got)
		}
		if m := regexp.MustCompile(`"[a-z]+[A-Z][A-Za-z]*":`).FindString(got); m != "" {
			t.Errorf("%s: camelCase key %s", kind, m)
		}
	}
	r, err := Read(in, "document", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	pretty, err := EncodeResult(r, true)
	if err != nil || !strings.HasPrefix(pretty, "{\n  \"base\": \"sha256:") || !strings.HasSuffix(pretty, "\n}") {
		t.Errorf("pretty: %v\n%s", err, pretty)
	}
}

func TestFindViewsAndCase(t *testing.T) {
	in := docOnly(t, `<w:p w14:paraId="00000001"><w:r><w:t>ΟΔΟΣ one</w:t></w:r><w:del w:id="1"><w:r><w:delText> gone</w:delText></w:r></w:del></w:p>`+
		`<w:p w14:paraId="00000002"><w:ins w:id="2"><w:r><w:t>added</w:t></w:r></w:ins></w:p>`)
	count := func(query string, ignore bool, view string) int {
		t.Helper()
		r, err := Find(in, query, ignore, view, Options{})
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		return len(r.Value["matches"].([]any))
	}
	for _, c := range []struct {
		query  string
		ignore bool
		view   string
		want   int
	}{
		{"gone", false, "markup", 1}, {"gone", false, "final", 0}, {"gone", false, "original", 1},
		{"added", false, "original", 0}, {"added", false, "final", 1},
		{"οδος", true, "markup", 1}, {"οδος", false, "markup", 0}, {"", false, "markup", 2},
	} {
		if got := count(c.query, c.ignore, c.view); got != c.want {
			t.Errorf("find %q ignore=%v %s: %d matches, want %d", c.query, c.ignore, c.view, got, c.want)
		}
	}
}

func TestEquationsNeedConverter(t *testing.T) {
	in := docOnly(t, `<w:p><m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><m:t>x</m:t></m:r></m:oMath></w:p>`)
	if _, err := Find(in, "", false, "markup", Options{}); !errors.Is(err, ErrNoEquation) {
		t.Fatalf("err = %v, want ErrNoEquation", err)
	}
	r, err := Find(in, "", false, "markup", Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeResult(t, r)
	if !strings.Contains(got, `"equations":[{"display":false,"editable":true,"id":"4AAD0223/equation:1","index":1,"latex":"x"}]`) {
		t.Errorf("equations missing: %s", got)
	}
}

// TestEquationLossInEntries: an equation whose LaTeX leaves out content
// lists it in equations[].loss; a lossless one has no loss key.
func TestEquationLossInEntries(t *testing.T) {
	const m = `xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`
	in := docOnly(t, `<w:p><m:oMath `+m+`><m:r><m:t>x</m:t></m:r></m:oMath>`+
		`<m:oMath `+m+`><m:r><m:t>y</m:t></m:r><w:ins w:id="1" w:author="a"><m:r><m:t>z</m:t></m:r></w:ins></m:oMath></w:p>`)
	r, err := Find(in, "", false, "markup", Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeResult(t, r)
	want := `"equations":[{"display":false,"editable":true,"id":"66C9DB0F/equation:1","index":1,"latex":"x"},` +
		`{"display":false,"editable":true,"id":"66C9DB0F/equation:2","index":2,"latex":"yz","loss":["w:ins"]}]`
	if !strings.Contains(got, want) {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

// docxdriver counts every AlternateContent branch and nested story text in
// comment anchors; the port counts only the segment space, where the
// drawing is one object character (model.LeafText).
func TestCommentLocatorsUseSegmentSpace(t *testing.T) {
	in := buildDocx(t,
		part{"word/document.xml", docHead + `<w:p><w:r><w:t>ab</w:t></w:r><w:commentRangeStart w:id="0"/>` +
			`<w:r><mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006">` +
			`<mc:Choice Requires="wps"><w:t>choice text</w:t></mc:Choice><mc:Fallback><w:t>c</w:t></mc:Fallback></mc:AlternateContent>` +
			`<w:drawing><w:txbxContent><w:p><w:r><w:t>nested</w:t></w:r></w:p></w:txbxContent></w:drawing></w:r>` +
			`<w:commentRangeEnd w:id="0"/></w:p>` + docTail},
		part{"word/_rels/document.xml.rels", commentRels("comments.xml")},
		part{"word/comments.xml", `<w:comments xmlns:w="` + nsW + `"><w:comment w:id="0"><w:p><w:r><w:t>x</w:t></w:r></w:p></w:comment></w:comments>`},
	)
	r, err := Read(in, "comments", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeResult(t, r); !strings.Contains(got, `"end":4,"occurrence":1,"select":"c￼","start":2`) {
		t.Errorf("anchor not in segment space: %s", got)
	}
}

// docxdriver drops a story whose relationship target needs resolving.
func TestStoryTargetsResolve(t *testing.T) {
	in := buildDocx(t,
		part{"word/document.xml", docHead + `<w:p/>` + docTail},
		part{"word/_rels/document.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="./sub/../header1.xml"/></Relationships>`},
		part{"word/header1.xml", `<w:hdr xmlns:w="` + nsW + `"><w:p><w:bookmarkStart w:id="1" w:name="H"/><w:r><w:t>h</w:t></w:r><w:bookmarkEnd w:id="1"/></w:p></w:hdr>`},
	)
	r, err := Read(in, "bookmarks", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeResult(t, r); !strings.Contains(got, `"part":"word/header1.xml"`) {
		t.Errorf("header bookmark missing: %s", got)
	}
}

// commentRels is a document relationships part naming the comment parts
// at the given targets: comments, then commentsExtended and commentsIds.
func commentRels(targets ...string) string {
	types := []string{
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments",
		"http://schemas.microsoft.com/office/2011/relationships/commentsExtended",
		"http://schemas.microsoft.com/office/2016/09/relationships/commentsIds",
	}
	var b strings.Builder
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, t := range targets {
		b.WriteString(`<Relationship Id="rId` + string(rune('1'+i)) + `" Type="` + types[i] + `" Target="` + t + `"/>`)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

// Comment parts are found through the document's relationships, under
// whatever names they have; docxdriver reads fixed names only.
func TestCommentPartsByRelationship(t *testing.T) {
	body := docHead + `<w:p><w:commentRangeStart w:id="7"/><w:r><w:t>anchored</w:t></w:r><w:commentRangeEnd w:id="7"/></w:p>` + docTail
	comments := func(text string) string {
		return `<w:comments xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `"><w:comment w:id="7" w:author="A"><w:p w14:paraId="20000000"><w:r><w:t>` + text + `</w:t></w:r></w:p></w:comment></w:comments>`
	}
	extended := `<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml"><w15:commentEx w15:paraId="20000000" w15:done="1"/></w15:commentsEx>`
	ids := `<w16cid:commentsIds xmlns:w16cid="http://schemas.microsoft.com/office/word/2016/wordml/cid"><w16cid:commentId w16cid:paraId="20000000" w16cid:durableId="1234ABCD"/></w16cid:commentsIds>`
	renamed := buildDocx(t,
		part{"word/document.xml", body},
		part{"word/_rels/document.xml.rels", commentRels("notes/remarks.xml", "../word/notes/remarksEx.xml", "/word/notes/remarksIds.xml")},
		part{"word/notes/remarks.xml", comments("renamed")},
		part{"word/notes/remarksEx.xml", extended},
		part{"word/notes/remarksIds.xml", ids},
		// Orphans at the conventional names: no relationship names them.
		part{"word/comments.xml", comments("orphan")},
	)
	r, err := Read(renamed, "comments", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := encodeResult(t, r)
	for _, w := range []string{`"text":"renamed"`, `"status":"resolved"`, `"durable_id":"1234ABCD"`, `"id":"comment:`, `"select":"`} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %s in %s", w, got)
		}
	}
	if strings.Contains(got, "orphan") {
		t.Errorf("read an orphaned comments part: %s", got)
	}

	orphan := buildDocx(t, part{"word/document.xml", body}, part{"word/comments.xml", comments("orphan")})
	r, err = Read(orphan, "comments", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeResult(t, r); !strings.Contains(got, `"comments":[]`) {
		t.Errorf("orphaned comments part listed: %s", got)
	}

	malformed := buildDocx(t,
		part{"word/document.xml", body},
		part{"word/_rels/document.xml.rels", commentRels("c.xml")},
		part{"word/c.xml", "<w:comments>"},
	)
	// The comments listing targets the damaged part: it alone fails.
	_, err = Read(malformed, "comments", "", Options{})
	if e := refused(t, err); e.Code != cli.CodeUnreadableTarget || !strings.HasPrefix(e.Detail, "word/c.xml: XML parse failed: parse error at") {
		t.Errorf("malformed renamed part: %+v", e)
	}
}

// TestUnitsAsRead: Units gives the tables, sections and comments of a
// read of the document kind, as encoded.
func TestUnitsAsRead(t *testing.T) {
	corpustest.Each(t, fixtureFiles(t), func(t *testing.T, f corpustest.File) {
		path := f.Path
		b, err := os.ReadFile(path) //nolint:gosec // G304: a fixture
		if err != nil {
			t.Fatal(err)
		}
		res, err := Read(b, "document", "", Options{Equation: math.OMMLToLaTeX})
		if err != nil {
			return
		}
		d, err := model.OpenDocument(b)
		if err != nil {
			t.Fatal(err)
		}
		full, units := res.Object(), Units(d)
		for _, k := range []string{"tables", "sections", "comments"} {
			x, err1 := Encode(full[k], false)
			y, err2 := Encode(units[k], false)
			if err1 != nil || err2 != nil || x != y {
				t.Errorf("%s: %s differs:\nread  %s\nunits %s", path, k, x, y)
			}
		}
	})
}

// fixtureFiles are unitFixtures as corpus files (internal/corpustest),
// named relative to DOCX_CORPUS or the repository, to run in parallel.
func fixtureFiles(t *testing.T) []corpustest.File {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	return corpustest.Files(unitFixtures(t), os.Getenv("DOCX_CORPUS"), root)
}

// unitFixtures are the repository's docx fixtures and, when DOCX_CORPUS
// is set, the corpus documents.
func unitFixtures(t *testing.T) []string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, g := range []string{
		filepath.Join(root, "testdata", "docx", "*.docx"),
		filepath.Join(root, "tools", "oracle", "testdata", "write", "*.docx"),
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m...)
	}
	if dir := os.Getenv("DOCX_CORPUS"); dir != "" {
		m, err := filepath.Glob(filepath.Join(dir, "*", "*.docx"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m...)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures")
	}
	return out
}
