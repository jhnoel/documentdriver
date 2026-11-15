package read

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// fuzzSeedDocs are small packages exercising every reader: numbering,
// styles, comments, revisions in body and header, bookmarks, images,
// equations and alternate content.
func fuzzSeedDocs(t testing.TB) [][]byte {
	return [][]byte{richDoc(t, true), richDoc(t, false), docOnly(t, `<w:p/>`), []byte("PK")}
}

// richDoc builds the rich seed; with bare, one w:t sits directly in its
// paragraph, outside any run (docxdriver 0.5.2 panics listing its
// revisions).
func richDoc(t testing.TB, bare bool) []byte {
	bareText := `<w:r><w:t>bare</w:t></w:r>`
	if bare {
		bareText = `<w:t>bare</w:t>`
	}
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/header" Target="header1.xml"/>` +
		`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/a.png"/>` +
		`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>` +
		`<Relationship Id="rId5" Type="http://schemas.microsoft.com/office/2011/relationships/commentsExtended" Target="commentsExtended.xml"/>` +
		`</Relationships>`
	body := `<w:p w14:paraId="00000001"><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr><w:rPr><w:del w:id="5"/></w:rPr></w:pPr>` +
		`<w:bookmarkStart w:id="0" w:name="B"/><w:commentRangeStart w:id="0"/><w:r><w:t>ΑΣ one</w:t><w:tab/></w:r>` +
		`<w:ins w:id="1" w:author="a"><w:r><w:t>in</w:t></w:r></w:ins><w:del w:id="2"><w:r><w:delText>out</w:delText></w:r></w:del>` +
		`<w:commentRangeEnd w:id="0"/><m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:r><m:t>x</m:t></m:r></m:oMath></w:p>` +
		`<w:p><w:pPr><w:pStyle w:val="L"/><w:pPrChange w:id="3"><w:pPr/></w:pPrChange></w:pPr>` + bareText + `<w:bookmarkEnd w:id="0"/>` +
		`<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><mc:Fallback><w:r><w:t>fb</w:t></w:r></mc:Fallback></mc:AlternateContent></w:p>` +
		`<w:tbl><w:tr><w:trPr><w:ins w:id="4"/></w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl>` +
		`<w:sectPr><w:headerReference w:type="default" r:id="rId2"/></w:sectPr>`
	numbering := `<w:numbering xmlns:w="` + nsW + `"><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="3"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="%1."/></w:lvl>` +
		`<w:lvl w:ilvl="1"><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%1.%2)"/><w:lvlRestart w:val="1"/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="1"><w:startOverride w:val="2"/></w:lvlOverride></w:num></w:numbering>`
	styles := `<w:styles xmlns:w="` + nsW + `"><w:style w:type="paragraph" w:styleId="L" w:default="1"><w:name w:val="List"/><w:basedOn w:val="N"/><w:next w:val="L"/><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style></w:styles>`
	comments := `<w:comments xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `"><w:comment w:id="0" w:author="A" w:date="2000-01-01T00:00:00Z"><w:p w14:paraId="10000000"><w:r><w:t>note</w:t></w:r></w:p></w:comment>` +
		`<w:comment w:id="1" w:author="B"><w:p w14:paraId="10000001"><w:r><w:t>reply</w:t></w:r></w:p></w:comment></w:comments>`
	extended := `<w15:commentsEx xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml"><w15:commentEx w15:paraId="10000000" w15:done="1"/><w15:commentEx w15:paraId="10000001" w15:paraIdParent="10000000"/></w15:commentsEx>`
	header := `<w:hdr xmlns:w="` + nsW + `" xmlns:w14="` + nsW14 + `"><w:p w14:paraId="abcdef01"><w:bookmarkStart w:id="9" w:name="H"/><w:ins w:id="7"><w:r><w:t>head</w:t></w:r></w:ins><w:bookmarkEnd w:id="9"/></w:p></w:hdr>`
	return buildDocx(t,
		part{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="/word/document.xml"/></Relationships>`},
		part{"word/document.xml", docHead + body + docTail},
		part{"word/_rels/document.xml.rels", rels},
		part{"word/numbering.xml", numbering},
		part{"word/styles.xml", styles},
		part{"word/comments.xml", comments},
		part{"word/commentsExtended.xml", extended},
		part{"word/header1.xml", header},
		part{"word/media/a.png", "\x89PNG"},
		part{"word/media/b.PNG", "\x89PNG"},
	)
}

// FuzzRead runs every read kind (the document kinds in every view) and
// find on mutated packages: reading
// must never panic, whatever the bytes.
func FuzzRead(f *testing.F) {
	for _, d := range fuzzSeedDocs(f) {
		f.Add(d)
	}
	opt := Options{
		Equation: math.OMMLToLaTeX,
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		// A refusal is a contract error; anything else is a bug.
		contract := func(err error) {
			var e *cli.Error
			if err != nil && !errors.As(err, &e) {
				t.Fatal(err)
			}
		}
		for _, kind := range []string{"styles", "comments", "revisions", "bookmarks", "assets"} {
			_, err := Read(b, kind, "", opt)
			contract(err)
		}
		// The HTML projection, in every view, as agent markup and UI.
		for _, kind := range []string{"document"} {
			for _, view := range []string{"", "final", "original"} {
				_, err := Read(b, kind, view, opt)
				contract(err)
			}
		}
		for _, view := range []string{"markup", "final", "original"} {
			_, err := Find(b, "", true, view, opt)
			contract(err)
		}
	})
}

// TestSeedDocOutputs pins what the readers derive from the richest seed.
// The w:tab counts as one character in the locators (model.LeafText).
func TestSeedDocOutputs(t *testing.T) {
	d := fuzzSeedDocs(t)[0]
	opt := Options{Equation: func(*tree.Node) (string, bool, []math.Loss) { return "M", false, nil }}
	for _, c := range []struct {
		kind string
		want []string
	}{
		{"revisions", []string{`"kind":"ins","locator":"p1:7-9"`, `"kind":"del","locator":"p1:9-12"`, `"kind":"del","locator":"p1:13-13"`, `"kind":"pPrChange","locator":"p2:0-0"`, `"locator":"row"`, `"part":"word/header1.xml"`}},
		{"comments", []string{`"status":"resolved"`, `"id":"comment:0"`, `"replies":[{"author":"B","date":"","id":"comment:1"`, `"text":"reply"`, `"select":"`}},
		{"bookmarks", []string{`"name":"B"`, `"text":"ΑΣ one\tin\nbare"`, `"part":"word/header1.xml"`, `"start":"ABCDEF01"`}},
		{"styles", []string{`{"based_on":"N","default":true,"id":"L","name":"List","next":"L","type":"paragraph"}`}},
		{"assets", []string{`"parts":["word/media/a.png","word/media/b.PNG"]`, `"data_url":"data:image/png;base64,iVBORw=="`}},
	} {
		r, err := Read(d, c.kind, "", opt)
		if err != nil {
			t.Fatal(err)
		}
		got := encodeResult(t, r)
		for _, w := range c.want {
			if !contains(got, w) {
				t.Errorf("%s: missing %s in\n%s", c.kind, w, got)
			}
		}
	}
	r, err := Find(d, "", false, "markup", opt)
	if err != nil {
		t.Fatal(err)
	}
	got := encodeResult(t, r)
	for _, w := range []string{`"number":"III.b)"`, `"list":{"continuation_paragraph":false,"continued":false,"format":"lowerLetter","level":1,"num_id":"1","start":2,"value":2}`, `"style":"L"`, `"latex":"M"`, `"text":"barefb"`} {
		if !contains(got, w) {
			t.Errorf("find: missing %s in\n%s", w, got)
		}
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// authored are the fixtures under testdata/docx, built by richDoc; the
// parity oracle runs them as its "authored" set.
var authored = map[string]bool{"rich.docx": false, "rich-bare-text.docx": true}

// TestAuthoredFixtures keeps testdata/docx equal to what richDoc builds;
// DOCX_UPDATE_FIXTURES=1 rewrites the files.
func TestAuthoredFixtures(t *testing.T) {
	for name, bare := range authored {
		path := filepath.Join("..", "..", "..", "testdata", "docx", name)
		want := richDoc(t, bare)
		if os.Getenv("DOCX_UPDATE_FIXTURES") == "1" {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale: run DOCX_UPDATE_FIXTURES=1 go test ./internal/docx/read -run TestAuthoredFixtures", path)
		}
	}
}
