package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// faultOp is a test-only operation that writes a fault on purpose: an
// invalid w:jc on the paragraph At (declared, through a region, as any
// operation edits).
type faultOp struct{ At string }

func (f *faultOp) Run(env *ops.Env) error {
	part, _, err := env.Main()
	if err != nil {
		return err
	}
	p, ok := env.WS.Registry().Paragraph(f.At)
	if !ok {
		return &ops.Rejection{Code: "not_found", Field: "at", Msg: "no paragraph " + f.At}
	}
	h, err := env.WS.Core().Range(part, "fault", p, p)
	if err != nil {
		return err
	}
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	jc, err := h.EnsureIn(ppr, docxedit.W("jc"))
	if err != nil {
		return err
	}
	return h.SetNodeAttr(jc, "w:val", "bogus")
}

// TestGateRefusesAtTheOp: an operation that writes a fault of a kind the
// input already has elsewhere is refused with invalid_output at that
// operation's index, not at the earlier operation that changed the same
// part first, and nothing is saved, in both modes. The fault is the very
// element the input already has (w:jc w:val="bogus"): written fresh, it
// is no copy of that one, so it is refused.
func TestGateRefusesAtTheOp(t *testing.T) {
	body := `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000B"><w:r><w:t>b</w:t></w:r></w:p>` +
		`<w:p w14:paraId="0000000C"><w:pPr><w:jc w:val="bogus"/></w:pPr><w:r><w:t>c</w:t></w:r></w:p>`
	for _, mode := range []string{"track", "direct"} {
		path := writeDocx(t, body)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(before)
		p := &ops.Plan{Base: ops.SourceHash("sha256:" + hex.EncodeToString(sum[:])), Author: "Ann", ChangeMode: ops.ChangeMode(mode), Ops: []ops.Op{
			{Name: "paragraph.format", Spec: &ops.FormatParagraph{At: "0000000B", Alignment: ops.Ptr("right")}},
			{Name: "test.fault", Spec: &faultOp{At: "0000000A"}},
		}}
		for _, opt := range []Options{{View: viewOpt}, {Commit: p.PreviewKey(), Now: "2026-01-01T00:00:00Z", View: viewOpt}} {
			r, err := Run(path, p, opt)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != Rejected || r.Diagnostic.Code != "invalid_output" || r.Diagnostic.Path != "ops[1]" {
				t.Errorf("%s: %s %+v", mode, r.Outcome, r.Diagnostic)
			}
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:errcheck // compared
			t.Errorf("%s: a refused plan wrote the file", mode)
		}
		// Without the fault the same plan saves.
		p.Ops = p.Ops[:1]
		if r, err := Run(path, p, Options{Commit: p.PreviewKey(), Now: "2026-01-01T00:00:00Z", View: viewOpt}); err != nil || r.Outcome != Committed {
			t.Errorf("%s: %v %+v", mode, err, r)
		}
	}
}

// brokenPartOp is a test-only operation that adds a part that is not
// well formed, through the workspace as an operation adds a part.
type brokenPartOp struct{ Name string }

func (b *brokenPartOp) Run(env *ops.Env) error {
	if _, err := env.WS.AddPart(ctypes.RelFooter, ctypes.CTWordprocessingmlFooter, b.Name, `<?xml version="1.0"?><w:ftr xmlns:w="urn:w"><w:p></w:ftr>`); err != nil {
		return err
	}
	return nil
}

// TestGateRefusesMalformedPart: the seal does not tokenise the changed
// parts (the gate parses each), so a part that is not well formed is
// refused by the gate as invalid_output at the operation that wrote it,
// not failed as an internal error, and nothing is saved.
func TestGateRefusesMalformedPart(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(before)
	p := &ops.Plan{Base: ops.SourceHash("sha256:" + hex.EncodeToString(sum[:])), Author: "Ann", ChangeMode: "direct", Ops: []ops.Op{
		{Name: "paragraph.format", Spec: &ops.FormatParagraph{At: "0000000A", Alignment: ops.Ptr("right")}},
		{Name: "test.broken", Spec: &brokenPartOp{Name: "/word/broken.xml"}},
	}}
	for _, opt := range []Options{{View: viewOpt}, {Commit: p.PreviewKey(), Now: "2026-01-01T00:00:00Z", View: viewOpt}} {
		r, err := Run(path, p, opt)
		if err != nil {
			t.Fatalf("internal error: %v", err)
		}
		if r.Outcome != Rejected || r.Diagnostic.Code != "invalid_output" || r.Diagnostic.Path != "ops[1]" ||
			!strings.Contains(r.Diagnostic.Message, "word/broken.xml: xml: ") {
			t.Errorf("%s %+v", r.Outcome, r.Diagnostic)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:errcheck // compared
		t.Error("a refused plan wrote the file")
	}
}
