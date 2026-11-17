package ops

import "testing"

// TestExtraRules checks the extension point for rules: a rule added to
// extraRules runs after the op's own rules, and the first broken one
// is reported.
func TestExtraRules(t *testing.T) {
	saved := extraRules
	t.Cleanup(func() { extraRules = saved })
	spec := &PageRenameSpec{At: "page0", Name: "X"}
	if _, _, bad := checkSpec(spec); bad {
		t.Fatalf("page.rename page0 X broke a rule before any extra rule")
	}
	extraRules = append(extraRules, func(s Spec) (string, string, bool) {
		if _, ok := s.(*PageRenameSpec); ok {
			return "extra", "name", true
		}
		return "", "", false
	})
	if msg, field, bad := checkSpec(spec); !bad || msg != "extra" || field != "name" {
		t.Errorf("checkSpec = (%q, %q, %v), want the extra rule", msg, field, bad)
	}
	if msg, _, bad := checkSpec(&PageRenameSpec{At: "page0"}); !bad || msg == "extra" {
		t.Errorf("checkSpec(no name) = %q, want the op's own rule first", msg)
	}
}
