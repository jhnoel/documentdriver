package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
)

// TestParseOverlay checks ParseOverlay: the empty value selects
// nothing, ids, sites and glue select, spaces are trimmed, and any
// other word is an error naming it with ids, sites or glue.
func TestParseOverlay(t *testing.T) {
	for _, c := range []struct {
		value string
		want  Overlay
	}{
		{"", Overlay{}},
		{"ids", Overlay{IDs: true}},
		{"sites", Overlay{Sites: true}},
		{"glue", Overlay{Glue: true}},
		{"ids,sites,glue", Overlay{IDs: true, Sites: true, Glue: true}},
		{"ids, sites , glue", Overlay{IDs: true, Sites: true, Glue: true}},
		{"ids,", Overlay{IDs: true}},
	} {
		got, err := ParseOverlay(c.value)
		if err != nil || got != c.want {
			t.Errorf("ParseOverlay(%q) = %+v, %v; want %+v", c.value, got, err, c.want)
		}
	}
	for _, value := range []string{"bogus", "id", "IDS", "ids,bogus", "ids sites"} {
		if _, err := ParseOverlay(value); err == nil || !strings.Contains(err.Error(), "unknown overlay") {
			t.Errorf("ParseOverlay(%q) error = %v; want an unknown overlay error", value, err)
		}
	}
	if _, err := ParseOverlay("bogus"); err == nil || err.Error() != `unknown overlay "bogus": ids, sites or glue` {
		t.Errorf("ParseOverlay(bogus) error = %v, want the usage message", err)
	}
}

// TestParseScale checks ParseScale: a positive number parses, and
// zero, a negative, text and empty are the usage error.
func TestParseScale(t *testing.T) {
	for _, c := range []struct {
		value string
		want  float64
	}{
		{"1", 1},
		{"1.5", 1.5},
		{"0.5", 0.5},
		{"2e0", 2},
	} {
		got, err := ParseScale(c.value)
		if err != nil || got != c.want {
			t.Errorf("ParseScale(%q) = %v, %v; want %v", c.value, got, err, c.want)
		}
	}
	for _, value := range []string{"", "0", "-1", "bogus", "1x", " 1"} {
		if _, err := ParseScale(value); err == nil || err.Error() != "--scale is a positive number" {
			t.Errorf("ParseScale(%q) error = %v; want --scale is a positive number", value, err)
		}
	}
}

// TestResult checks the shared render result: its JSON has saved,
// px, size formatted with geom.FormatNum as read formats it, scale,
// hidden and key, in that order.
func TestResult(t *testing.T) {
	result := Result{Saved: "x.png", Px: [2]int{612, 792},
		Size:  [2]json.Number{json.Number(geom.FormatNum(612)), json.Number(geom.FormatNum(792))},
		Scale: 1, Key: []KeyEntry{}}
	b, err := cli.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"saved":"x.png","px":[612,792],"size":[612,792],"scale":1,"hidden":false,"key":[]}`
	if string(b) != want {
		t.Errorf("Result = %s, want %s", b, want)
	}
	result.Key = []KeyEntry{{N: 1, ID: "page0/s1"}}
	b, err = cli.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"key":[{"n":1,"id":"page0/s1"}]`) {
		t.Errorf("Result with key = %s, want a key with n and id", b)
	}
	// A size that is not a whole number is shown to two decimals.
	result.Size = [2]json.Number{json.Number(geom.FormatNum(612.345)), json.Number(geom.FormatNum(792.3))}
	b, err = cli.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"size":[612.35,792.3]`) {
		t.Errorf("Result with fractional size = %s, want size [612.35,792.3]", b)
	}
}
