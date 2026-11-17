package emit

import (
	"strings"
	"testing"
)

// TestConnectorXMLGolden checks the golden README call: ConnectorXML(3,
// End{3, 9.5, glued to 1 point 2}, End{5, 9.5, glued to 2 point 4}) is
// connector.xml and the two lines of connects.txt (chunk K4 row).
func TestConnectorXMLGolden(t *testing.T) {
	shape, connects, ok := ConnectorXML(3,
		End{X: 3, Y: 9.5, Glued: true, Target: 1, Point: 2},
		End{X: 5, Y: 9.5, Glued: true, Target: 2, Point: 4})
	if !ok {
		t.Fatalf("ConnectorXML(ends 3,9.5 and 5,9.5) ok = false, want true")
	}
	if want := golden(t, "connector.xml"); string(shape) != want {
		t.Errorf("ConnectorXML shape = %s\nwant %s", shape, want)
	}
	lines := strings.Split(strings.TrimSuffix(golden(t, "connects.txt"), "\n"), "\n")
	if len(connects) != len(lines) {
		t.Fatalf("ConnectorXML connects = %d rows, want %d", len(connects), len(lines))
	}
	for i, want := range lines {
		if string(connects[i]) != want {
			t.Errorf("ConnectorXML connects[%d] = %s\nwant %s", i, connects[i], want)
		}
	}
}

// TestConnectorXMLFreeBegin checks that a free begin (not Glued) writes
// BeginX/BeginY with V only and no Connect for it (chunk K4 row).
func TestConnectorXMLFreeBegin(t *testing.T) {
	shape, connects, ok := ConnectorXML(3,
		End{X: 3, Y: 9.5},
		End{X: 5, Y: 9.5, Glued: true, Target: 2, Point: 4})
	if !ok {
		t.Fatalf("ConnectorXML(free begin) ok = false, want true")
	}
	s := string(shape)
	for _, want := range []string{
		"<Cell N='BeginX' V='3'/>",
		"<Cell N='BeginY' V='9.5'/>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("ConnectorXML(free begin) = %s, want it to contain %s", s, want)
		}
	}
	if strings.Contains(s, "BeginX' V='3' F=") || strings.Contains(s, "BeginY' V='9.5' F=") {
		t.Errorf("ConnectorXML(free begin) = %s, want BeginX/BeginY with V only and no formula", s)
	}
	if len(connects) != 1 {
		t.Fatalf("ConnectorXML(free begin) connects = %d rows, want 1 (End only)", len(connects))
	}
	if want := "<Connect FromSheet='3' FromCell='EndX' FromPart='12' ToSheet='2' ToCell='Connections.X4' ToPart='103'/>"; string(connects[0]) != want {
		t.Errorf("ConnectorXML(free begin) connects[0] = %s\nwant %s", connects[0], want)
	}
}

// TestConnectorXMLCoinciding checks that coinciding ends give ok false
// (chunk K4 row).
func TestConnectorXMLCoinciding(t *testing.T) {
	if shape, connects, ok := ConnectorXML(3, End{X: 3, Y: 9.5}, End{X: 3, Y: 9.5}); ok {
		t.Errorf("ConnectorXML(coinciding ends) = %s, %v, true; want ok false", shape, connects)
	}
}
