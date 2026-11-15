package ops

import "testing"

// TestInitials checks comments.rs initials_from_author: the first ASCII
// letter of each word, upper-cased, at most four.
func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Ann Lee": "AL", "  ann   b. lee ": "ABL", "Émile Zola": "MZ", "1st 2nd": "SN",
		"a b c d e": "ABCD", "": "", "李 雷": "",
	} {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}
