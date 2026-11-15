package html

import (
	"strings"
	"testing"
)

// TestPlaceholderInput: an element carrying a placeholder's address is
// that placeholder, whatever its rendering; its text rides along; an
// <opaque> without an address, a malformed address and a placeholder
// outside a paragraph are refused.
func TestPlaceholderInput(t *testing.T) {
	items, err := ParseFragment(`<p>a<opaque data-ref="0000000a/opaque:1" data-kind="drawing"></opaque>`+
		`<span data-field="REF x" data-ref="0000000A/opaque:2" contenteditable="false">r<b>es</b></span>`+
		`<img src="assets/i1.png" data-ref="opaque:3"></p>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		if it.Kind == ItemAtom && it.Atom.Kind == AtomOpaque {
			got = append(got, it.Atom.Ref+"="+it.Atom.Text)
		}
	}
	if strings.Join(got, ",") != "0000000A/opaque:1=,0000000A/opaque:2=res,opaque:3=" {
		t.Errorf("placeholders %v", got)
	}
	for _, s := range []string{
		`<p><opaque data-kind="drawing"></opaque></p>`,
		`<p><opaque data-ref="0000000A/opaque:0"></opaque></p>`,
		`<p><span data-ref="x/opaque:1">a</span></p>`,
		`<opaque data-ref="opaque:1"></opaque>`,
	} {
		if _, err := ParseFragment(s, nil); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
}
