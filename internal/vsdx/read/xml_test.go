package read

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// partString returns the original string of the part part of the
// fixture name.
func partString(t *testing.T, name, part string) string {
	t.Helper()
	pkg, err := opc.Open(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("opc.Open(%s): %v", name, err)
	}
	defer pkg.Close()
	pt, err := pkg.Part(part)
	if err != nil {
		t.Fatalf("Part(%s): %v", part, err)
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		t.Fatalf("OriginalString(%s): %v", part, err)
	}
	if !ok {
		t.Fatalf("%s has no original bytes", part)
	}
	return s
}

// shapeSpan returns the bytes of the first Shape element of a part
// holding one: from its <Shape start tag to the last </Shape>.
func shapeSpan(t *testing.T, part string) string {
	t.Helper()
	start := strings.Index(part, "<Shape ")
	end := strings.LastIndex(part, "</Shape>")
	if start < 0 || end < 0 {
		t.Fatalf("part does not hold a Shape element")
	}
	return part[start : end+len("</Shape>")]
}

// TestXMLV1 checks the R3 V1 row: XML(V1, "page0/s1") is the exact
// Shape element bytes of page1.xml.
func TestXMLV1(t *testing.T) {
	got, err := XML(fixtureBytes(t, "v1-minimal"), "page0/s1")
	if err != nil {
		t.Fatalf("XML(v1-minimal, page0/s1): %v", err)
	}
	want := shapeSpan(t, partString(t, "v1-minimal", "/visio/pages/page1.xml"))
	if string(got) != want {
		t.Errorf("XML(page0/s1) = %q, want %q", got, want)
	}
}

// TestXMLMaster checks the R3 V2 row: XML(V2, "master2/s5") is
// master1.xml's Shape.
func TestXMLMaster(t *testing.T) {
	got, err := XML(fixtureBytes(t, "v2-master"), "master2/s5")
	if err != nil {
		t.Fatalf("XML(v2-master, master2/s5): %v", err)
	}
	want := shapeSpan(t, partString(t, "v2-master", "/visio/masters/master1.xml"))
	if string(got) != want {
		t.Errorf("XML(master2/s5) = %q, want %q", got, want)
	}
}

// TestXMLNested checks XML of a shape nested in a group (V2 page0/s4)
// and of the group itself (page0/s3, with its nested shapes).
func TestXMLNested(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	got, err := XML(input, "page0/s4")
	if err != nil {
		t.Fatalf("XML(page0/s4): %v", err)
	}
	if want := `<Shape ID='4' Type='Shape' MasterShape='6'/>`; string(got) != want {
		t.Errorf("XML(page0/s4) = %q, want %q", got, want)
	}
	got, err = XML(input, "page0/s3")
	if err != nil {
		t.Fatalf("XML(page0/s3): %v", err)
	}
	want := `<Shape ID='3' NameU='Pair' Name='Pair' Type='Group' Master='3'><Cell N='PinX' V='6'/><Cell N='PinY' V='6'/><Shapes><Shape ID='4' Type='Shape' MasterShape='6'/><Shape ID='5' Type='Shape' MasterShape='7'><Text>Right` + "\n" + `</Text></Shape></Shapes></Shape>`
	if string(got) != want {
		t.Errorf("XML(page0/s3) = %q, want %q", got, want)
	}
}

// TestXMLNotFound checks the R3 not-found row: XML("page0/s99") is
// not_found, as are a master that does not exist and an address that
// is not a shape; input that is not a zip is an error.
func TestXMLNotFound(t *testing.T) {
	input := fixtureBytes(t, "v2-master")
	for _, address := range []string{"page0/s99", "master9/s5", "page0", "page0/s1/prop:Cost"} {
		_, err := XML(input, address)
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeNotFound {
			t.Errorf("XML(%s) error = %v, want a not_found error", address, err)
		}
	}
	for _, in := range [][]byte{[]byte("not a zip"), {}} {
		if got, err := XML(in, "page0/s1"); err == nil || got != nil {
			t.Errorf("XML(%q) = %q, %v, want nil, an error", in, got, err)
		}
	}
}

// TestXMLUnreadable checks XML of a shape on a malformed page (V10
// page2) is unreadable_target.
func TestXMLUnreadable(t *testing.T) {
	_, err := XML(fixtureBytes(t, "v10-defects"), "page2/s1")
	var e *cli.Error
	if !errors.As(err, &e) || e.Code != cli.CodeUnreadableTarget {
		t.Fatalf("XML(page2/s1) error = %v, want an unreadable_target error", err)
	}
}

// TestScalingXML checks the scaling row: XML on Generated(1000) and
// Generated(4000) (N = 1000, 4N = 4000 shapes) allocates at most 5.5
// times as much (laws.Scales) and the best-of-5 time ratio is at most
// 8.
func TestScalingXML(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			got, err := XML(data, "page0/s1")
			if err != nil {
				return err
			}
			if len(got) == 0 {
				return fmt.Errorf("Generated(%d): XML(page0/s1) is empty", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx XML", setup)
	var costs [2]laws.Cost
	for i, n := range []int{laws.Small, laws.Large} {
		run, err := setup(n)
		if err != nil {
			t.Fatalf("setup at %d: %v", n, err)
		}
		if costs[i], err = laws.Measure(5, run); err != nil {
			t.Fatalf("at %d: %v", n, err)
		}
	}
	tm := float64(costs[1].Time) / float64(max(costs[0].Time, 1))
	t.Logf("vsdx XML: %d units best time %s, %d units best time %s: time ratio %.2f",
		laws.Small, costs[0].Time, laws.Large, costs[1].Time, tm)
	if tm > 8 {
		t.Errorf("vsdx XML time ratio %.2f for 4 times the input (budget 8): super-linear", tm)
	}
}
