package read

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// lawFixtures are the designer-committed fixtures of chunks.md (V1, V2
// and V5): every law row of L1a reads each of them.
var lawFixtures = []string{"v1-minimal", "v2-master", "v5-glue"}

// lawAddrs returns every address Document names: its pages in order,
// then its masters.
func lawAddrs(t *testing.T, input []byte) []string {
	t.Helper()
	sum, err := Document(input, "B")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	addrs := make([]string, 0, len(sum.Pages)+len(sum.Masters))
	for _, p := range sum.Pages {
		addrs = append(addrs, p.ID)
	}
	for _, m := range sum.Masters {
		addrs = append(addrs, m.ID)
	}
	return addrs
}

// parsePagePage parses the Page result in b into its pageJSON form.
func parsePagePage(t *testing.T, b []byte) pageJSON {
	t.Helper()
	var p pageJSON
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("Unmarshal Page: %v", err)
	}
	return p
}

// TestLawsDeterministicRead checks the L1a determinism row: two reads
// of each fixture (V1, V2, V5) are byte-identical: Document, Find and
// every Page (each page and master).
func TestLawsDeterministicRead(t *testing.T) {
	for _, fx := range lawFixtures {
		t.Run(fx, func(t *testing.T) {
			input := fixtureBytes(t, fx)
			sum1, err := Document(input, "B")
			if err != nil {
				t.Fatalf("Document: %v", err)
			}
			sum2, err := Document(input, "B")
			if err != nil {
				t.Fatalf("Document again: %v", err)
			}
			b1, err := cli.Marshal(sum1)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			b2, err := cli.Marshal(sum2)
			if err != nil {
				t.Fatalf("Marshal again: %v", err)
			}
			if !bytes.Equal(b1, b2) {
				t.Errorf("two Documents differ:\n%s\n%s", b1, b2)
			}
			f1, err := Find(input, "B", FindRequest{Query: ""})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			f2, err := Find(input, "B", FindRequest{Query: ""})
			if err != nil {
				t.Fatalf("Find again: %v", err)
			}
			fb1, err := cli.Marshal(f1)
			if err != nil {
				t.Fatalf("Marshal Find: %v", err)
			}
			fb2, err := cli.Marshal(f2)
			if err != nil {
				t.Fatalf("Marshal Find again: %v", err)
			}
			if !bytes.Equal(fb1, fb2) {
				t.Errorf("two Finds differ:\n%s\n%s", fb1, fb2)
			}
			for _, addr := range lawAddrs(t, input) {
				p1, err := Page(input, "B", addr, Options{})
				if err != nil {
					t.Fatalf("Page(%s): %v", addr, err)
				}
				p2, err := Page(input, "B", addr, Options{})
				if err != nil {
					t.Fatalf("Page(%s) again: %v", addr, err)
				}
				if !bytes.Equal(p1, p2) {
					t.Errorf("two Page(%s) reads differ", addr)
				}
			}
		})
	}
}

// subsetErr reports whether got is a subset of full with equal
// objects: every object of got occurs in full with an equal value,
// in full order.
func subsetErr(full, got pageJSON) error {
	i := 0
	for _, o := range got.Objects {
		found := false
		for ; i < len(full.Objects); i++ {
			if full.Objects[i].ID != o.ID {
				continue
			}
			if !found && !reflect.DeepEqual(full.Objects[i], o) {
				return fmt.Errorf("object %s differs from the full read", o.ID)
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("object %s is not in the full read", o.ID)
		}
		i++
	}
	return nil
}

// TestLawsFiltersAreSubsets checks the L1a filter row: the --ids and
// --region outputs of each fixture (V1, V2, V5) are subsets of the
// full read with equal objects.
func TestLawsFiltersAreSubsets(t *testing.T) {
	for _, fx := range lawFixtures {
		t.Run(fx, func(t *testing.T) {
			input := fixtureBytes(t, fx)
			for _, addr := range lawAddrs(t, input) {
				full := parsePagePage(t, mustPage(t, input, addr, Options{}))
				if len(full.Objects) == 0 {
					continue
				}
				byIDs := parsePagePage(t, mustPage(t, input, addr, Options{IDs: []string{full.Objects[0].ID}}))
				if err := subsetErr(full, byIDs); err != nil {
					t.Errorf("Page(%s, --ids %s): %v", addr, full.Objects[0].ID, err)
				}
				if len(full.Objects[0].Box) == 4 {
					r := regionOf(t, full.Objects[0])
					byRegion := parsePagePage(t, mustPage(t, input, addr, Options{Region: &r}))
					if err := subsetErr(full, byRegion); err != nil {
						t.Errorf("Page(%s, --region): %v", addr, err)
					}
					if len(byRegion.Objects) == 0 {
						t.Errorf("Page(%s, --region at %s's own box): no objects, want %s", addr, full.Objects[0].ID, full.Objects[0].ID)
					}
				}
			}
		})
	}
}

// mustPage reads addr of input with o and fails the test on an error.
func mustPage(t *testing.T, input []byte, addr string, o Options) []byte {
	t.Helper()
	b, err := Page(input, "B", addr, o)
	if err != nil {
		t.Fatalf("Page(%s): %v", addr, err)
	}
	return b
}

// regionOf returns the view region of obj's box (pt, the default
// units).
func regionOf(t *testing.T, obj objectJSON) [4]float64 {
	t.Helper()
	var r [4]float64
	for i, n := range obj.Box {
		v, err := strconv.ParseFloat(string(n), 64)
		if err != nil {
			t.Fatalf("box of %s: %v", obj.ID, err)
		}
		r[i] = v
	}
	return r
}

// TestLawsXMLRoundTrip checks the L1a XML row: every object id of
// every fixture (V1, V2, V5) parses as a shape address and round-
// trips through XML: XML returns the shape bytes naming that shape.
func TestLawsXMLRoundTrip(t *testing.T) {
	for _, fx := range lawFixtures {
		t.Run(fx, func(t *testing.T) {
			input := fixtureBytes(t, fx)
			for _, addr := range lawAddrs(t, input) {
				for _, obj := range parsePagePage(t, mustPage(t, input, addr, Options{})).Objects {
					a, err := vsd.Parse(obj.ID)
					if err != nil {
						t.Errorf("Parse(%q): %v", obj.ID, err)
						continue
					}
					if a.Kind != vsd.KindShape {
						t.Errorf("Parse(%q) kind = %v, want KindShape", obj.ID, a.Kind)
						continue
					}
					b, err := XML(input, obj.ID)
					if err != nil {
						t.Errorf("XML(%q): %v", obj.ID, err)
						continue
					}
					if len(b) == 0 {
						t.Errorf("XML(%q) is empty", obj.ID)
					}
					id := strconv.FormatUint(uint64(a.Shape), 10)
					if !bytes.Contains(b, []byte("ID='"+id+"'")) && !bytes.Contains(b, []byte(`ID="`+id+`"`)) {
						t.Errorf("XML(%q) = %q, want the bytes of shape %s", obj.ID, b, id)
					}
				}
			}
		})
	}
}

// TestLawsHashUnchanged checks the L1a write row: reading a fixture
// (V1, V2, V5) leaves the file unchanged: its bytes and sha256 are
// equal before and after Document, Page, Find and XML.
func TestLawsHashUnchanged(t *testing.T) {
	for _, fx := range lawFixtures {
		t.Run(fx, func(t *testing.T) {
			path := vsdxtest.Fixture(t, fx)
			before, err := os.ReadFile(path) //nolint:gosec // G304: path built by vsdxtest from a fixture name
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			hash := cli.Hash(before)
			if _, err := Document(before, "B"); err != nil {
				t.Fatalf("Document: %v", err)
			}
			for _, addr := range lawAddrs(t, before) {
				p := parsePagePage(t, mustPage(t, before, addr, Options{}))
				for _, obj := range p.Objects {
					if _, err := XML(before, obj.ID); err != nil {
						t.Fatalf("XML(%q): %v", obj.ID, err)
					}
				}
			}
			if _, err := Find(before, "B", FindRequest{Query: ""}); err != nil {
				t.Fatalf("Find: %v", err)
			}
			after, err := os.ReadFile(path) //nolint:gosec // G304: path built by vsdxtest from a fixture name
			if err != nil {
				t.Fatalf("read %s again: %v", path, err)
			}
			if !bytes.Equal(before, after) || cli.Hash(after) != hash {
				t.Errorf("read changed %s: hash %s, want %s", path, cli.Hash(after), hash)
			}
		})
	}
}

// TestLawsScalingSubset checks the scaling row: Page plus the --ids
// subset proof on Generated(1000) and Generated(4000) (N = 1000,
// 4N = 4000 shapes) allocates at most 5.5 times as much (laws.Scales)
// and the best-of-5 time ratio is at most 8.
func TestLawsScalingSubset(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			fullB, err := Page(data, "B", "page0", Options{})
			if err != nil {
				return err
			}
			var full pageJSON
			if err := json.Unmarshal(fullB, &full); err != nil {
				return err
			}
			if len(full.Objects) == 0 {
				return fmt.Errorf("Generated(%d): no objects", n)
			}
			gotB, err := Page(data, "B", "page0", Options{IDs: []string{full.Objects[0].ID}})
			if err != nil {
				return err
			}
			var got pageJSON
			if err := json.Unmarshal(gotB, &got); err != nil {
				return err
			}
			return subsetErr(full, got)
		}, nil
	}
	laws.Scales(t, "vsdx laws subset", setup)
	scalingTimeRatio(t, "vsdx laws subset", setup)
}
