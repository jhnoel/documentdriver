package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/laws"
)

// propertyScaling are the scaling cases of the property family's
// operations (section, document settings, comments, revision
// conversion and settling by author and by paragraph), on the large document of TestScaling: each costs at most
// linearly more on four times the paragraphs.
var propertyScaling = []struct {
	name string
	spec func(mid string) ops.Spec
	// direct runs the case in direct mode (an op track mode refuses).
	direct bool
	// pre, when set, is an operation run first in the same plan (a
	// comment for the comment operations to act on).
	pre func(mid string) ops.Spec
}{
	{"section.set_margins", func(string) ops.Spec {
		return &ops.SetPageMargins{Top: ops.Ptr(ops.Points(720)), Left: ops.Ptr(ops.Points(1800))}
	}, false, nil},
	{"section.set_layout", func(string) ops.Spec {
		return &ops.SetSectionLayout{Orientation: ops.Ptr("landscape"), Columns: ops.Ptr(uint32(2))}
	}, false, nil},
	{"section.set_first_page", func(string) ops.Spec { return &ops.SetFirstPageMode{Enabled: true} }, false, nil},
	{"document.set_even_odd_headers", func(string) ops.Spec { return &ops.SetEvenAndOddHeaders{EvenAndOdd: true} }, true, nil},
	{"revision.convert_to_formatting", func(string) ops.Spec { return &ops.ConvertRevisionsToFormatting{All: true} }, false, nil},
	{"revision.accept by author", func(string) ops.Spec {
		return &ops.AcceptRevisions{All: true, Author: ops.Ptr(ops.RevisionAuthor("Bob"))}
	}, false, nil},
	{"revision.reject by paragraph", func(mid string) ops.Spec {
		return &ops.RejectRevisions{At: &ops.RevisionScope{Paragraphs: []ops.ParagraphAddress{ops.ParagraphAddress(mid)}}}
	}, false, nil},
	{"comment.reply", func(string) ops.Spec { return &ops.CommentReply{At: "comment:0", Text: "reply"} }, false, scalingComment},
	{"comment.set_status", func(string) ops.Spec { return &ops.CommentSetStatus{At: "comment:0", Status: "resolved"} }, false, scalingComment},
	{"comment.delete", func(string) ops.Spec { return &ops.CommentDelete{At: "comment:0"} }, false, scalingComment},
}

// TestScalingProperty is the scaling gate for the property family's
// operations (laws.Scales).
func TestScalingProperty(t *testing.T) {
	for _, c := range propertyScaling {
		laws.Scales(t, "docx apply "+c.name, func(n int) (func() error, error) {
			path := writeDocx(t, largeBody(n))
			if _, err := os.Stat(path); err != nil {
				return nil, err
			}
			mode := "track"
			if c.direct {
				mode = "direct"
			}
			mid := fmt.Sprintf("%08X", n/2)
			specs := []ops.Spec{c.spec(mid)}
			if c.pre != nil {
				specs = append([]ops.Spec{c.pre(mid)}, specs...)
			}
			p, d := ops.ParsePlanJSON(planOf(t, path, mode, specs...))
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
}

// scalingComment adds a comment on the middle paragraph.
func scalingComment(mid string) ops.Spec {
	return &ops.CommentAdd{At: ops.ParagraphAddress(mid), Text: "A comment."}
}
