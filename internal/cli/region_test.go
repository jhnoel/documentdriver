package cli

import (
	"testing"
)

// TestParseRegion checks ParseRegion: four numbers with optional
// spaces parse, and any other count or a non-number is an error
// with the usage message the CLIs report.
func TestParseRegion(t *testing.T) {
	for _, c := range []struct {
		value string
		want  [4]float64
	}{
		{"1,2,3,4", [4]float64{1, 2, 3, 4}},
		{"0,0,10,10", [4]float64{0, 0, 10, 10}},
		{"1, 2, 3, 4", [4]float64{1, 2, 3, 4}},
		{"-1.5,2.25,3,4e0", [4]float64{-1.5, 2.25, 3, 4}},
	} {
		got, err := ParseRegion(c.value)
		if err != nil || got != c.want {
			t.Errorf("ParseRegion(%q) = %v, %v; want %v", c.value, got, err, c.want)
		}
	}
	for _, value := range []string{"", "1,2,3", "1,2,3,4,5", "1,2,x,4", "1,2,3,"} {
		if _, err := ParseRegion(value); err == nil {
			t.Errorf("ParseRegion(%q) succeeded, want an error", value)
		}
	}
	if _, err := ParseRegion("1,2,3"); err == nil || err.Error() != "--region is x,y,w,h" {
		t.Errorf("ParseRegion(1,2,3) error = %v, want --region is x,y,w,h", err)
	}
	if _, err := ParseRegion("1,2,x,4"); err == nil || err.Error() != `--region is four numbers: "x" is not one` {
		t.Errorf("ParseRegion(1,2,x,4) error = %v, want the four numbers message", err)
	}
}
