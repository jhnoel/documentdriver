package integrity

import (
	"archive/zip"
	"bytes"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// mutations damage one XML part in ways the checks see.
var mutations = []func(string) string{
	func(s string) string { return s + " " }, // a byte change no check sees
	func(s string) string {
		return strings.Replace(s, "</w:body>", `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r></w:p></w:body>`, 1)
	},
	func(s string) string {
		return strings.Replace(s, "</w:body>", `<w:p><w:pPr><w:jc w:val="nowhere"/></w:pPr><w:r><w:commentReference w:id="99"/><w:footnoteReference w:id="99"/></w:r></w:p></w:body>`, 1)
	},
	func(s string) string { return strings.Replace(s, `w:id="`, `w:id="9`, 1) },
	func(s string) string { return strings.Replace(s, `Id="rId1"`, `Id="rId1x"`, 1) },
	func(s string) string { return s[:len(s)/2] },
}

// gateAgrees checks that Gate's answer on (input, output) is that of
// comparing Check of both packages.
func gateAgrees(t *testing.T, label string, input, output []byte) bool {
	t.Helper()
	want := Introduced(Check(input), Check(output))
	var got []Finding
	if err := Gate(input, output); err != nil {
		var g *GateFailure
		if !asGateFailure(err, &g) {
			t.Fatalf("%s: %v", label, err)
		}
		got = g.Introduced
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: Gate introduced %q, full check %q", label, got, want)
	}
	// The plan gate checks its input lazily; over a workspace of the
	// output (whose trees account for none of the input's parts) it
	// gives the eager answer.
	if pkg, err := opc.OpenBytes(output, opc.DefaultLimits()); err == nil {
		if w, err := docxedit.New(pkg, docxedit.Options{}); err == nil {
			lazyAgrees(t, label, input, output, w.Core(), nil)
		}
	}
	l := withDefaults(opc.Limits{})
	in, out := unzip(input, l), unzip(output, l)
	scope, scoped := changedScope(in, out)
	if !scoped {
		t.Errorf("%s: not checked by scope", label)
	}
	// The scoped findings are complete on the changed parts: the edit
	// result's hazards are read from them (GateEditReport).
	inScope := func(f *Findings) map[Finding]int {
		m := map[Finding]int{}
		for _, k := range f.Kinds() {
			if scope[k.Part] {
				m[k] = f.Count(k)
			}
		}
		return m
	}
	if got, full := inScope(check(out, scope)), inScope(check(out, nil)); !maps.Equal(got, full) {
		t.Errorf("%s: scoped findings on the changed parts %v, full check %v", label, got, full)
	}
	return len(want) > 0
}

// TestGateMatchesFullCheck mutates each part of the rule packages and,
// when the corpus is there, the document part of some of its documents.
func TestGateMatchesFullCheck(t *testing.T) {
	refused := 0
	for _, c := range ruleCases {
		input := zipParts(t, c.parts)
		for i, p := range c.parts {
			if p.name == contentTypesName || !isXMLName(p.name) {
				continue
			}
			for k, m := range mutations {
				parts := slices.Clone(c.parts)
				parts[i].content = m(p.content)
				if gateAgrees(t, c.name+"/"+p.name+"/"+string(rune('0'+k)), input, zipParts(t, parts)) {
					refused++
				}
			}
		}
	}
	if refused < 50 {
		t.Errorf("only %d edits refused: the mutations test little", refused)
	}
	files := parityFiles(t)
	if len(files) == 0 {
		return
	}
	corpusRefused := 0
	defer func() {
		if corpusRefused == 0 {
			t.Error("no corpus edit was refused")
		}
	}()
	for i, key := range slices.Sorted(maps.Keys(files)) {
		if i%25 != 0 {
			continue
		}
		data, err := os.ReadFile(files[key])
		if err != nil {
			t.Fatal(err)
		}
		for k, m := range mutations[1:4] {
			out, ok := rewritePart(t, data, "word/document.xml", m)
			if ok {
				if gateAgrees(t, key+"/"+string(rune('1'+k)), data, out) {
					corpusRefused++
				}
			}
		}
	}
}

// rewritePart rewrites one entry of a zip, or reports false when the
// package cannot be read or has no such entry.
func rewritePart(t *testing.T, data []byte, name string, m func(string) string) ([]byte, bool) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	found := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		b, err := io.ReadAll(rc)
		if cerr := rc.Close(); err != nil || cerr != nil {
			return nil, false
		}
		if f.Name == name {
			b, found = []byte(m(string(b))), true
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), found
}
