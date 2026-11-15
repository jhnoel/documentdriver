package laws

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// TestKept: an owned unit may change, merge into another owned unit or
// go (leaving the whitespace around it, as a removal does); an unowned
// unit that changes is caught.
func TestKept(t *testing.T) {
	a := docx(t, `<w:p><w:r><w:t>one</w:t></w:r></w:p>`, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`, `<w:p><w:r><w:t>three</w:t></w:r></w:p>`)
	has := func(s string) func(e *tree.Node) bool {
		return func(e *tree.Node) bool { return wNS.Is(e, "p") && strings.Contains(string(e.Bytes()), s) }
	}
	merged := docx(t, `<w:p><w:r><w:t>onetwo</w:t></w:r></w:p>`, ``, `<w:p><w:r><w:t>three</w:t></w:r></w:p>`)
	both := func(e *tree.Node) bool { return has("one")(e) || has("two")(e) }
	if err := Kept(a, merged, docPart, blocks{}, both); err != nil {
		t.Fatalf("two owned units merging was refused: %v", err)
	}
	gone := docx(t, ``, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`, `<w:p><w:r><w:t>three</w:t></w:r></w:p>`)
	if err := Kept(a, gone, docPart, blocks{}, has("one")); err != nil {
		t.Fatalf("an owned unit going was refused: %v", err)
	}
	if err := Kept(a, merged, docPart, blocks{}, has("one")); err == nil || !strings.Contains(err.Error(), "untargeted w:p changed or moved") {
		t.Fatalf("an unowned unit merged away went unnoticed: %v", err)
	}
	changed := docx(t, `<w:p><w:r><w:t>one</w:t></w:r></w:p>`, `<w:p><w:r><w:t>two</w:t></w:r></w:p>`, `<w:p><w:r><w:t>THREE</w:t></w:r></w:p>`)
	if err := Kept(a, changed, docPart, blocks{}, has("one")); err == nil {
		t.Fatal("a changed unowned unit went unnoticed")
	}
}
