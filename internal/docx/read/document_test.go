package read

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

var (
	oracleMath = regexp.MustCompile(`<math[^>]*>.*?</math>`)
	goEquation = regexp.MustCompile(`<equation[^>]*>.*?</equation>`)
	offsetKey  = regexp.MustCompile(`"(start|end)": \d+`)
	textKey    = regexp.MustCompile(`"(select|selectable_text|marked_text)": "[^"]*"`)
	// A placeholder the projection marks (docs/contract.md, section 7)
	// around content docxdriver shows bare: an intended difference.
	placeholderSpan = regexp.MustCompile(`<span data-ref=\\"[^"\\]*opaque:\d+\\" data-kind=\\"[a-z-]+\\">(.*?)</span>`)
)

func latexStub(*tree.Node) (string, bool, []math.Loss) { return "x^{2}<1", false, nil }

// TestDocumentMatchesOracle compares the document read of the authored
// rich.docx with the recorded docxdriver output put into the contract's
// shape (FromOracle), equations aside: the Go projection renders them as
// LaTeX in <equation> (an intended change).
func TestDocumentMatchesOracle(t *testing.T) {
	b := richDoc(t, false)
	for _, c := range []struct{ name, view string }{
		{"read-document", ""}, {"read-document-final", "final"}, {"read-document-original", "original"},
	} {
		oracle, err := os.ReadFile(filepath.Join("..", "..", "..", "tools", "oracle", "baseline", "authored", "rich.docx", c.name+".stdout"))
		if err != nil {
			t.Fatal(err)
		}
		r, err := Read(b, "document", c.view, Options{Equation: latexStub})
		if err != nil {
			t.Fatal(err)
		}
		// The header's paragraph has an address docxdriver does not show
		// (known difference 16).
		if !strings.Contains(fmt.Sprint(r.Value["markup"]), `<p id="ABCDEF01">`) {
			t.Errorf("%s: the header paragraph has no id", c.name)
		}
		r.Value = WithoutStoryAddresses(r.Value).(Object)
		got, err := EncodeResult(r, true)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, `/equation:1\">x^{2}&lt;1</equation>`) {
			t.Errorf("%s: no LaTeX equation in\n%s", c.name, got)
		}
		// The oracle's result in the contract's shape (FromOracle): the
		// content must be the same.
		wantObj, err := FromOracle("document", oracle)
		if err != nil {
			t.Fatal(err)
		}
		base := wantObj["base"].(string)
		delete(wantObj, "base")
		rest, err := Encode(wantObj, true)
		if err != nil {
			t.Fatal(err)
		}
		want := "{\n  \"base\": \"" + base + "\"," + strings.TrimPrefix(rest, "{")
		// An equation counts as one U+FFFC and a tab as "\t" in selectable
		// text and anchor offsets (docxdriver counts neither; an intended
		// difference), so those characters and the offsets are masked.
		g := strings.ReplaceAll(goEquation.ReplaceAllString(got, "EQ"), model.EquationChar, "")
		g = placeholderSpan.ReplaceAllString(g, "$1")
		g = textKey.ReplaceAllStringFunc(g, func(m string) string { return strings.ReplaceAll(m, `\t`, "") })
		g = offsetKey.ReplaceAllString(g, `"$1": N`)
		w := offsetKey.ReplaceAllString(oracleMath.ReplaceAllString(want, "EQ"), `"$1": N`)
		if g != w {
			t.Errorf("%s differs from the oracle:\ngot  %s\nwant %s", c.name, g, w)
		}
	}
}

// TestDocumentNeedsConverter: equations without a converter are an error,
// not silently dropped.
func TestDocumentNeedsConverter(t *testing.T) {
	_, err := Read(richDoc(t, false), "document", "", Options{})
	if !errors.Is(err, ErrNoEquation) {
		t.Fatalf("err = %v, want ErrNoEquation", err)
	}
	r, err := Read(docOnly(t, `<w:p><w:r><w:t>plain</w:t></w:r></w:p>`), "document", "", Options{})
	if err != nil {
		t.Fatalf("plain document: %v %+v", err, r)
	}
}

// TestSettledViews: the final view joins a paragraph whose mark is
// deleted into the next and keeps the next one's address; the original
// view drops an inserted paragraph that has nothing to join.
func TestSettledViews(t *testing.T) {
	b := docOnly(t, `<w:p w14:paraId="00000001"><w:pPr><w:rPr><w:del w:id="1" w:author="a"/></w:rPr></w:pPr><w:r><w:t>one</w:t></w:r></w:p>`+
		`<w:p w14:paraId="00000002"><w:r><w:t>two</w:t></w:r></w:p>`+
		`<w:p w14:paraId="00000003"><w:pPr><w:rPr><w:ins w:id="2" w:author="a"/></w:rPr></w:pPr><w:ins w:id="3" w:author="a"><w:r><w:t>new</w:t></w:r></w:ins></w:p>`+
		`<w:p w14:paraId="00000004"><w:r><w:t>end</w:t></w:r></w:p>`)
	for _, c := range []struct{ view, want string }{
		{"final", `<p id=\"00000002\">onetwo</p>\n<p id=\"00000003\">new</p>\n<p id=\"00000004\">end</p>`},
		{"original", `<p id=\"00000001\">one</p>\n<p id=\"00000002\">two</p>\n<p id=\"00000004\">end</p>`},
		{"", `<p id=\"00000001\" data-break-del=\"1\">one</p>`},
	} {
		r, err := Read(b, "document", c.view, Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := encodeResult(t, r)
		if !strings.Contains(got, c.want) {
			t.Errorf("view %q: missing %s in\n%s", c.view, c.want, got)
		}
	}
}
