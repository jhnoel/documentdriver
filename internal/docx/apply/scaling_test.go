package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/create"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// scalingHTML is create's input of n paragraphs with formatting and a
// link each, and a heading every fifty.
func scalingHTML(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i%50 == 1 {
			fmt.Fprintf(&b, "<h2>Section %d</h2>\n", i)
		}
		fmt.Fprintf(&b, `<p>Paragraph %d has <b>bold</b>, <i>italic</i> and <a href="https://example.com/%d">a link</a> in ordinary text.</p>`+"\n", i, i)
	}
	return b.String()
}

// TestScaling is the docx half of the scaling gate (laws.Scales): create,
// read, find and apply cost at most linearly more on a document of four
// times the paragraphs.
func TestScaling(t *testing.T) {
	laws.Scales(t, "docx create", func(n int) (func() error, error) {
		in := scalingHTML(n)
		return func() error { _, err := create.Build(in); return err }, nil
	})
	doc := func(n int) (string, []byte, error) {
		path := writeDocx(t, largeBody(n))
		b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own file
		return path, b, err
	}
	ropt := read.Options{Equation: math.OMMLToLaTeX}
	laws.Scales(t, "docx read", func(n int) (func() error, error) {
		_, b, err := doc(n)
		return func() error { _, err := read.Read(b, "document", "", ropt); return err }, err
	})
	laws.Scales(t, "docx find", func(n int) (func() error, error) {
		_, b, err := doc(n)
		return func() error { _, err := read.Find(b, "ordinary text", false, "", ropt); return err }, err
	})
	for _, c := range []struct {
		name string
		spec func(mid string) ops.Spec
	}{
		{"paragraph.format", func(mid string) ops.Spec {
			return &ops.FormatParagraph{At: ops.ParagraphAddress(mid), Alignment: ops.Ptr("center")}
		}},
		{"paragraph.insert", func(mid string) ops.Spec {
			return &ops.InsertParagraph{At: ops.InsertAnchor(mid), Position: "after", HTML: ops.ParagraphContent{Items: []string{"<b>new</b> text"}}}
		}},
		{"paragraph.delete", func(mid string) ops.Spec {
			return &ops.DeleteParagraphs{At: []ops.ParagraphAddress{ops.ParagraphAddress(mid)}}
		}},
		{"paragraph.replace", func(mid string) ops.Spec {
			// The middle paragraph holds Bob's insertion, which
			// paragraph.replace refuses; the one after it does not.
			var n int
			fmt.Sscanf(mid, "%X", &n) //nolint:errcheck // the test's own address
			return &ops.ReplaceParagraph{At: ops.ParagraphAddress(fmt.Sprintf("%08X", n+1)), HTML: "new <i>text</i>"}
		}},
		{"header.set", func(string) ops.Spec {
			return &ops.SetHeader{HTML: "<p>Header <b>text</b></p>"}
		}},
		{"list.set", func(mid string) ops.Spec {
			return &ops.SetList{At: []ops.ParagraphAddress{ops.ParagraphAddress(mid)}, Format: ops.Ptr("bullet")}
		}},
		{"cell.format", func(mid string) ops.Spec {
			return &ops.FormatCell{At: midTable(mid) + "/row:1/cell:1", Format: ops.CellFormat{Fill: ops.Ptr("#ABCDEF")}}
		}},
		{"text.replace", func(mid string) ops.Spec {
			return &ops.ReplaceText{At: ops.ParagraphAddress(mid), Select: "ordinary text", With: "plain words"}
		}},
		{"text.revise (document scope)", func(mid string) ops.Spec {
			n, _ := strconv.ParseUint(mid, 16, 32) //nolint:errcheck // mid is the test's own hex
			return &ops.ReviseText{Within: ops.Ptr(fmt.Sprintf("Paragraph %d opens", n)), Edits: []ops.TextEdit{{Select: "opens", With: "starts"}}}
		}},
		{"text.format", func(mid string) ops.Spec {
			return &ops.FormatText{At: ops.ParagraphAddress(mid), Select: "ordinary", Italic: ops.Ptr(true)}
		}},
		{"bookmark.add", func(mid string) ops.Spec {
			return &ops.BookmarkAdd{At: ops.ParagraphAddress(mid), Name: "Middle", Select: ops.Ptr("ordinary")}
		}},
	} {
		laws.Scales(t, "docx apply "+c.name, func(n int) (func() error, error) {
			path, _, err := doc(n)
			if err != nil {
				return nil, err
			}
			p, d := ops.ParsePlanJSON(planOf(t, path, "track", c.spec(fmt.Sprintf("%08X", n/2))))
			if d != nil {
				return nil, fmt.Errorf("%s", d.Message)
			}
			opt := Options{Commit: p.PreviewKey(), Out: filepath.Join(t.TempDir(), "out.docx"), Now: "2026-01-01T00:00:00Z", View: viewOpt}
			return func() error {
				r, err := Run(path, p, opt)
				if err == nil && r.Outcome != Committed {
					err = fmt.Errorf("%s: %+v", r.Outcome, r.Diagnostic)
				}
				return err
			}, nil
		})
	}
	// The operations of a plan cost what they do, not the document once
	// each: a plan of one paragraph format per fifty paragraphs (20, then
	// 80) costs about four times as much on four times the paragraphs,
	// where work of operations times document (serialising the document
	// after each operation, as the first-change record once did) costs
	// sixteen. With one and four operations that work is too small a
	// part of the run to fail the ratio (measured 4.29 with it, 4.08
	// without); with 20 and 80 it gives 7.4.
	laws.Scales(t, "docx apply paragraph.format by operation count", func(n int) (func() error, error) {
		path, _, err := doc(n)
		if err != nil {
			return nil, err
		}
		var specs []ops.Spec
		for k := range n / 50 {
			specs = append(specs, &ops.FormatParagraph{At: ops.ParagraphAddress(fmt.Sprintf("%08X", 1+k*50)), Alignment: ops.Ptr("center")})
		}
		p, d := ops.ParsePlanJSON(planOf(t, path, "track", specs...))
		if d != nil {
			return nil, fmt.Errorf("%s", d.Message)
		}
		opt := Options{Commit: p.PreviewKey(), Out: filepath.Join(t.TempDir(), "out.docx"), Now: "2026-01-01T00:00:00Z", View: viewOpt}
		return func() error {
			r, err := Run(path, p, opt)
			if err == nil && r.Outcome != Committed {
				err = fmt.Errorf("%s: %+v", r.Outcome, r.Diagnostic)
			}
			return err
		}, nil
	})
}
