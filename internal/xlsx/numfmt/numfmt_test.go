package numfmt

import "testing"

func TestBuiltIn(t *testing.T) {
	known := map[int]string{
		0:  "General",
		1:  "0",
		2:  "0.00",
		3:  "#,##0",
		4:  "#,##0.00",
		5:  "$#,##0_);($#,##0)",
		6:  "$#,##0_);[Red]($#,##0)",
		7:  "$#,##0.00_);($#,##0.00)",
		8:  "$#,##0.00_);[Red]($#,##0.00)",
		9:  "0%",
		10: "0.00%",
		11: "0.00E+00",
		12: "# ?/?",
		13: "# ??/??",
		14: "mm-dd-yy",
		15: "d-mmm-yy",
		16: "d-mmm",
		17: "mmm-yy",
		18: "h:mm AM/PM",
		19: "h:mm:ss AM/PM",
		20: "h:mm",
		21: "h:mm:ss",
		22: "m/d/yy h:mm",
		37: "#,##0_);(#,##0)",
		38: "#,##0_);[Red](#,##0)",
		39: "#,##0.00_);(#,##0.00)",
		40: "#,##0.00_);[Red](#,##0.00)",
		45: "mm:ss",
		46: "[h]:mm:ss",
		47: "mm:ss.0",
		48: "##0.0E+0",
		49: "@",
	}
	for id, want := range known {
		got, ok := BuiltIn(id)
		if !ok || got != want {
			t.Errorf("BuiltIn(%d) = %q, %v; want %q, true", id, got, ok, want)
		}
	}
	// The ids between 22 and 37, 40 and 45, and past 49 have no built-in
	// format, as do negatives.
	for _, id := range []int{-1, 23, 36, 41, 44, 50, 9999} {
		if got, ok := BuiltIn(id); ok {
			t.Errorf("BuiltIn(%d) = %q, true; want false", id, got)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		format string
		want   Kind
	}{
		// General, number, text.
		{"General", General},
		{"general", General},
		{"GENERAL", General},
		{"0", Number},
		{"0.00", Number},
		{"#,##0", Number},
		{"#,##0.00", Number},
		{"0%", Number},
		{"0.00E+00", Number},
		{"# ?/?", Number},
		{"@", Text},
		{"@@", Text},
		{"0.00 @", Text},
		{"", Number},

		// Dates.
		{"m/d/yyyy", Date},
		{"d-mmm-yy", Date},
		{"d-mmm", Date},
		{"mmm-yy", Date},
		{"yyyy-mm-dd", Date},
		{"mm/dd/yyyy", Date},
		{"[$-409]mmm d, yyyy", Date},
		{"[$-409]d-mmm-yy", Date},
		{"mm", Date},
		{"m", Date},

		// Times, with the month/minute split.
		{"h:mm", Time},
		{"h:mm:ss", Time},
		{"hh:mm:ss", Time},
		{"h:mm AM/PM", Time},
		{"h:mm:ss AM/PM", Time},
		{"h:mm A/P", Time},
		{"mm:ss", Time},
		{"mm:ss.0", Time},
		{"m:ss", Time},
		{"h:m", Time},
		{"ss", Time},

		// Dates with times.
		{"m/d/yyyy h:mm", DateTime},
		{"mm/dd/yyyy hh:mm:ss", DateTime},
		{"d-mmm-yy h:mm AM/PM", DateTime},
		{"yyyy-mm-dd hh:mm", DateTime},

		// Elapsed time brackets.
		{"[h]:mm", Duration},
		{"[h]:mm:ss", Duration},
		{"[hh]:mm:ss", Duration},
		{"[m]", Duration},
		{"[mm]:ss", Duration},
		{"[s]", Duration},
		{"[ss]", Duration},
		{"[H]:MM", Duration},

		// Quoted literals, escapes and padding do not make a date.
		{`"d" m/d/yyyy`, Date},
		{`"d" 0.00`, Number},
		{`"m"`, Number},
		{`"General"`, Number},
		{`d\ m\ y`, Date},
		{`\m 0.00`, Number},
		{`0.00_m`, Number},
		{`0.00*m`, Number},

		// Bracket groups other than elapsed time are ignored.
		{"[Red]0.00", Number},
		{"[Red][<0]-0.00", Number},
		{"[<100]0.00", Number},
		{"[$€-x]0.00", Number},
		{"[$-409]", Number},

		// Only the first section counts.
		{"0.00;[Red]-0.00", Number},
		{"m/d/yyyy;@", Date},
		{"h:mm;[Red]h:mm", Time},
	}
	for _, tc := range tests {
		if got := Classify(tc.format); got != tc.want {
			t.Errorf("Classify(%q) = %v; want %v", tc.format, got, tc.want)
		}
	}
}

func TestKindString(t *testing.T) {
	want := map[Kind]string{
		General: "General", Number: "Number", Text: "Text", Date: "Date",
		DateTime: "DateTime", Time: "Time", Duration: "Duration",
	}
	for k, s := range want {
		if got := k.String(); got != s {
			t.Errorf("(%d).String() = %q; want %q", k, got, s)
		}
	}
	if got := Kind(99).String(); got == "" {
		t.Errorf("Kind(99).String() is empty")
	}
}
