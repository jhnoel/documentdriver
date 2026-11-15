package coverage

import (
	"sort"
	"strings"
	"testing"
)

// seenStatus maps a probe outcome to the read status a claim must
// declare for it.
var seenStatus = map[string]Status{
	seenModelled: ReadModelled,
	seenOmitted:  OmittedWithReason,
	seenRaw:      PreservedRaw,
}

// TestReadClaimsMatchBehaviour checks the read registry against the read
// scan in both directions: a path whose change the views show must be
// claimed with exactly the status the probe saw, and a path claimed as
// read-modelled, omitted or raw must show that change. Non-goal claims
// are allowed only where the views show nothing or raw XML.
func TestReadClaimsMatchBehaviour(t *testing.T) {
	eff := effective(t)
	var bad []string
	paths := make([]string, 0, len(eff))
	for p := range eff {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		seen := eff[p]
		c := Decide(Read, p)
		want, observed := seenStatus[seen]
		switch {
		case observed && c == nil:
			bad = append(bad, "undeclared "+string(want)+": "+p)
		case observed && c.Status == NonGoal && want == PreservedRaw:
			// A non-goal shown as raw XML: the claim stands.
		case observed && c.Status != want:
			bad = append(bad, "declared "+string(c.Status)+" by "+c.By+", seen "+string(want)+": "+p)
		case !observed && c != nil && c.Status != NonGoal:
			bad = append(bad, "declared "+string(c.Status)+" by "+c.By+", seen nothing: "+p)
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d read claims disagree with the probes:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
