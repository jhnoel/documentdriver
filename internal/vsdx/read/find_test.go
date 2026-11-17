package read

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// checkMatches fails the test when res.Matches is not want.
func checkMatches(t *testing.T, res *FindResult, want []Match) {
	t.Helper()
	if res == nil {
		t.Fatal("Find result is nil")
	}
	if !reflect.DeepEqual(res.Matches, want) {
		t.Errorf("Matches = %#v, want %#v", res.Matches, want)
	}
}

// TestFindV2 checks the R3 V2 rows: find "Process" gives
// [{page0/s1, "Process"}], "review" with ignore case gives
// [{page0/s2, "Review"}], "12.5" gives [{page0/s2/prop:Cost, "12.5"}],
// "Left" gives [{page0/s4, "Left"}], and the empty query gives s1,
// s1's Cost, s2, s2's Cost, s4, s5 in page then paint order.
func TestFindV2(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	for _, tc := range []struct {
		name  string
		query string
		icase bool
		want  []Match
	}{
		{"Process", "Process", false, []Match{{At: "page0/s1", Text: "Process"}}},
		{"review ignore case", "review", true, []Match{{At: "page0/s2", Text: "Review"}}},
		{"12.5", "12.5", false, []Match{{At: "page0/s2/prop:Cost", Text: "12.5"}}},
		{"Left", "Left", false, []Match{{At: "page0/s4", Text: "Left"}}},
		{"empty", "", false, []Match{
			{At: "page0/s1", Text: "Process"},
			{At: "page0/s1/prop:Cost", Text: "0"},
			{At: "page0/s2", Text: "Review"},
			{At: "page0/s2/prop:Cost", Text: "12.5"},
			{At: "page0/s4", Text: "Left"},
			{At: "page0/s5", Text: "Right"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Find(input, "B", FindRequest{Query: tc.query, IgnoreCase: tc.icase})
			if err != nil {
				t.Fatalf("Find(%q): %v", tc.query, err)
			}
			if res.Base != "B" {
				t.Errorf("Base = %q, want B", res.Base)
			}
			if res.Matches == nil || res.Notes == nil {
				t.Errorf("Matches or Notes is nil, want non-nil lists")
			}
			checkMatches(t, res, tc.want)
			if res.Truncated != nil {
				t.Errorf("Truncated = %#v, want nil", res.Truncated)
			}
		})
	}
}

// TestFindLimit checks the R3 limit row: limit 1 on two matches gives
// one match plus truncated.after naming it, and exactly the limit is
// not a cut.
func TestFindLimit(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	res, err := Find(input, "B", FindRequest{Query: "", Limit: 1})
	if err != nil {
		t.Fatalf("Find(limit 1): %v", err)
	}
	checkMatches(t, res, []Match{{At: "page0/s1", Text: "Process"}})
	if res.Truncated == nil || res.Truncated.After != "page0/s1" {
		t.Errorf("Truncated = %#v, want after page0/s1", res.Truncated)
	}
	found := false
	for _, n := range res.Notes {
		if n.At == "page0/s1" {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want a note at page0/s1", res.Notes)
	}
	res, err = Find(input, "B", FindRequest{Query: "", Limit: 6})
	if err != nil {
		t.Fatalf("Find(limit 6): %v", err)
	}
	if len(res.Matches) != 6 {
		t.Errorf("len(Matches) = %d, want 6", len(res.Matches))
	}
	if res.Truncated != nil {
		t.Errorf("Truncated = %#v at exactly the limit, want nil", res.Truncated)
	}
}

// TestFindPage checks find with --page: page0 searches one page, a
// page that does not exist is not_found, and input that is not a zip
// is an error.
func TestFindPage(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	res, err := Find(input, "B", FindRequest{Query: "Process", Page: "page0"})
	if err != nil {
		t.Fatalf("Find(--page page0): %v", err)
	}
	checkMatches(t, res, []Match{{At: "page0/s1", Text: "Process"}})
	for _, page := range []string{"page9", "master2", "bogus"} {
		_, err := Find(input, "B", FindRequest{Query: "Process", Page: page})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeNotFound {
			t.Errorf("Find(--page %s) error = %v, want a not_found error", page, err)
		}
	}
	for _, in := range [][]byte{[]byte("not a zip"), {}} {
		if res, err := Find(in, "B", FindRequest{Query: "Process"}); err == nil || res != nil {
			t.Errorf("Find(%q) = %v, %v, want nil, an error", in, res, err)
		}
	}
}

// TestFindUnreadablePage checks find with --page on a malformed page
// (V10 page2) is unreadable_target, and find without --page skips it
// with a note and still finds page0's shapes.
func TestFindUnreadablePage(t *testing.T) {
	input := fixtureBytes(t, "v10-defects")
	_, err := Find(input, "B", FindRequest{Query: "First", Page: "page2"})
	var e *cli.Error
	if !errors.As(err, &e) || e.Code != cli.CodeUnreadableTarget {
		t.Fatalf("Find(--page page2) error = %v, want an unreadable_target error", err)
	}
	res, err := Find(input, "B", FindRequest{Query: "First"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	checkMatches(t, res, []Match{{At: "page0/s7", Text: "First"}})
	found := false
	for _, n := range res.Notes {
		if n.At == "page2" {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want a note at page2", res.Notes)
	}
}

// TestScalingFind checks the scaling row: Find on Generated(1000) and
// Generated(4000) (N = 1000, 4N = 4000 shapes) allocates at most 5.5
// times as much (laws.Scales) and the laws.TimeScales time ratio is at most
// 8.
func TestScalingFind(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			res, err := Find(data, "B", FindRequest{Query: "Box"})
			if err != nil {
				return err
			}
			if len(res.Matches) != n {
				return fmt.Errorf("Generated(%d): len(Matches) = %d, want %d", n, len(res.Matches), n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Find", setup)
	laws.TimeScales(t, "vsdx Find", setup)
}
