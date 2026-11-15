package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// TestUnportedOpsRefuse runs every operation not ported yet (its Spec
// still embeds pending, so the list comes from the op table and porting
// an operation needs no edit here), in both modes, after an operation that changes the document: each is refused
// at the operation with operation_failed (the contract's code for an op
// that does not apply), never internal, and the file is left as it was.
func TestUnportedOpsRefuse(t *testing.T) {
	path := writeDocx(t, `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>`)
	before, err := os.ReadFile(path) //nolint:gosec // G304: the test's own file
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	base := planBase(t, path)
	for i := range ops.Ops {
		d := &ops.Ops[i]
		spec := d.New()
		if _, pending := spec.(interface{ PendingOp() }); !pending {
			continue
		}
		for _, mode := range []ops.ChangeMode{ops.ModeTrack, ops.ModeDirect} {
			first := &ops.FormatParagraph{At: "0000000A", Alignment: ops.Ptr("center")}
			p := &ops.Plan{Base: base, Author: "Ann", ChangeMode: mode,
				Ops: []ops.Op{{Name: "paragraph.format", Spec: first}, {Name: d.Name, Spec: spec}}}
			for _, opt := range []Options{{View: viewOpt}, {Commit: p.PreviewKey(), Now: "2026-01-01T00:00:00Z", View: viewOpt}} {
				r, err := Run(path, p, opt)
				if err != nil {
					t.Fatalf("%s %s: %v", d.Name, mode, err)
				}
				if r.Outcome != Rejected || r.Diagnostic.Code != "operation_failed" || r.Diagnostic.Path != "ops[1]" {
					t.Errorf("%s %s: %s %+v, want operation_failed at ops[1]", d.Name, mode, r.Outcome, r.Diagnostic)
				}
			}
		}
	}
	unchangedFile(t, path, before, st.ModTime())
}

// planBase is the base of a plan against the file at path.
func planBase(t *testing.T, path string) ops.SourceHash {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own file
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return ops.SourceHash("sha256:" + hex.EncodeToString(sum[:]))
}
