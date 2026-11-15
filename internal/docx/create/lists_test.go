package create

import (
	"strings"
	"testing"
)

// TestCreateLists: each list is a numbering definition of its own, its
// paragraphs members at their levels, with its start and format; the
// document reads back as those lists (markers and all), and read's
// markup sent back to create reads the same.
func TestCreateLists(t *testing.T) {
	m := roundTrip(t,
		`<ol start="3"><li>a<ol type="a"><li>b</li><li><h2>c</h2></li></ol></li><li>d</li></ol>`+
			`<ul><li>e<ul><li>f<ol type="I"><li>g</li></ol></li></ul></li></ul><p>h</p>`+
			`<ol type="A" start="2"><li>i</li></ol><ol type="i"><li>j</li></ol>`+
			`<table><tr><td><ol><li>in a cell</li></ol></td></tr></table>`,
		`<ol data-list="1" data-level="0" start="3" type="1"><li data-marker="3."><p id="`,
		`<ol data-list="2" data-level="1" start="1" type="a"><li data-marker="a."><p id="`,
		`<li data-marker="b."><h2 id="`, `<li data-marker="4.">`,
		`<ul data-list="3" data-level="0"><li data-marker="•">`, `<ul data-list="4" data-level="1">`,
		`type="I"><li data-marker="I.">`, `start="2" type="A"><li data-marker="B.">`,
		`type="i"><li data-marker="i.">`, `in a cell`)
	if strings.Count(m, "<li ") != 10 {
		t.Errorf("want 10 list items:\n%s", m)
	}
}

// TestCreateListRefusals: a list item of several paragraphs is refused
// (each paragraph needs its own <li>).
func TestCreateListRefusals(t *testing.T) {
	if _, err := Build(`<ol><li><p>a</p><p>b</p></li></ol>`); err == nil {
		t.Error("a list item of two paragraphs was accepted")
	}
}
