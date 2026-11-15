package create

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
)

const tableSample = `<p>before</p>
<table style="width:100%;margin-left:auto;margin-right:auto;border-top:0.5pt solid #000000;"><thead>
<tr style="height:20pt;break-inside:avoid;"><th colspan="2" style="background-color:#FFEE00;vertical-align:middle;"><b>wide</b></th><td rowspan="2">tall</td></tr>
</thead><tbody>
<tr><td>a <a href="https://example.com/">link</a></td><td style="width:72pt;padding-left:5.4pt;"><p class="Quote">b<br>c</p><p>d</p></td></tr>
<tr><td><table><tr><td>nested</td></tr></table></td><td><ul><li>one</li><li>two</li></ul></td></tr>
</tbody></table>
<p>after</p>`

// TestCreateTables: read(create(html)) shows the table as written (spans,
// the style read writes, nested tables, paragraphs, lists and links in
// cells), every cell paragraph has its id, and read's markup sent back
// to create reads the same.
func TestCreateTables(t *testing.T) {
	m := roundTrip(t, tableSample,
		`<table data-table="1" id="table:1" style="width:100%;margin-left:auto;margin-right:auto;border-top:0.5pt solid #000000;">`,
		`<tr id="table:1/row:1" style="break-inside:avoid;height:20pt;">`,
		`style="background-color:#FFEE00;vertical-align:middle;" colspan="2">`,
		`<td id="table:1/row:1/cell:2" rowspan="2">`,
		`<td id="table:1/row:2/cell:2" style="width:72pt;padding-left:5.4pt;">`,
		`b<br>c</i></p>`, `data-style="Quote"`, `href="https://example.com/"`,
		`<table id="table:2" style="width:auto;">`, `nested`, `<ul data-list="1"`, `two`,
		`<p id="`, `after</p>`)
	if strings.Count(m, "<tr ") != 4 {
		t.Errorf("want 4 rows:\n%s", m)
	}
	d, err := model.OpenDocument(created(t, tableSample))
	if err != nil {
		t.Fatal(err)
	}
	if r := d.ParaIDs(); len(r.Repaired) != 0 {
		t.Errorf("paragraphs without a stored id: %d", len(r.Repaired))
	}
}

// TestCreateTableEnds: a table ending the document or a section is
// followed by an empty paragraph (a section break, and Word, need one).
func TestCreateTableEnds(t *testing.T) {
	m := roundTrip(t, `<table><tr><td>x</td></tr></table>`)
	if !tableThenParagraph.MatchString(m) {
		t.Errorf("no paragraph after the table:\n%s", m)
	}
}

var tableThenParagraph = regexp.MustCompile(`</tbody></table>\n<p id="[0-9A-F]{8}"[^>]*></p>\n</section>$`)

// TestCreateTableRefusals: a style read does not write for a table, a
// misplaced structure and a grid Word cannot hold are refused with
// nothing written.
func TestCreateTableRefusals(t *testing.T) {
	for _, c := range []struct{ in, path string }{
		{`<table style="color:#FF0000"><tr><td>x</td></tr></table>`, "html@style"},
		{`<table><tr><td style="height:2pt">x</td></tr></table>`, "html@style"},
		{`<table><tr><td colspan="64">x</td></tr></table>`, "html"},
		{`<table><tr><td class="NoSuchStyle">x</td></tr></table>`, "html"},
		{`<table><tr><td><p class="NoSuchStyle">x</p></td></tr></table>`, "html@class"},
		{`<table border="1"><tr><td>x</td></tr></table>`, "html"},
		{`<table></table>`, "html"},
	} {
		_, err := Build(c.in)
		var e *Error
		if !errors.As(err, &e) || e.Code != "invalid_operation" || e.Path != c.path {
			t.Errorf("%s: %v (%+v), want invalid_operation at %s", c.in, err, e, c.path)
		}
	}
}
