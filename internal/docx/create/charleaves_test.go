package create

import "testing"

// TestCreateCharLeavesRoundTrip: read writes a non-breaking hyphen as
// U+2011 and a tab (w:tab or w:ptab) as a data-tab span. create accepts
// both, so the markup read writes round-trips through create.
func TestCreateCharLeavesRoundTrip(t *testing.T) {
	roundTrip(t,
		`<p>a`+"\u2011"+`b<span data-tab="true">`+"\t"+`</span>c</p>`,
		"a"+"\u2011"+"b",
		`<span data-tab="true">`+"\t"+`</span>`,
	)
}
