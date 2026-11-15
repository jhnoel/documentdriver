package numfmt

import "testing"

// classifySeeds are format strings that exercise each classification rule:
// they seed FuzzClassify and its committed seed corpus.
var classifySeeds = []string{
	"General", "0.00", "#,##0.00;[Red]-#,##0.00", "@", "0.00 @",
	"m/d/yyyy", "d-mmm-yy", "mm/dd/yyyy", "yyyy-mm-dd",
	"h:mm", "h:mm:ss", "h:mm AM/PM", "mm:ss", "m:ss", "h:m",
	"m/d/yyyy h:mm", "d-mmm-yy h:mm AM/PM",
	"[h]:mm", "[h]:mm:ss", "[mm]:ss", "[s]",
	`"d" m/d/yyyy`, `"m"`, `d\ m\ y`, "0.00_m", "0.00*m",
	"[Red]0.00", "[Red][<0]-0.00", "[$-409]mmm d, yyyy", "[$€-x]0.00",
	"m/d/yyyy;@", "0.00;[Red]-0.00",
}

// FuzzClassify: Classify never panics and always reports a known kind,
// whatever the format string (a format code is untrusted input).
func FuzzClassify(f *testing.F) {
	for _, s := range classifySeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, format string) {
		k := Classify(format)
		if k < General || k > Duration {
			t.Fatalf("Classify(%q) = %v, outside the known kinds", format, k)
		}
	})
}
