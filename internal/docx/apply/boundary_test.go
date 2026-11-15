package apply

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

const boundaryBody = `<w:p w14:paraId="0000000A"><w:r><w:t>a</w:t></w:r></w:p>` +
	`<w:p w14:paraId="0000000B"><w:pPr><w:sectPr><w:headerReference w:type="default" r:id="rId9"/><w:pgSz w:w="100" w:h="200"/></w:sectPr></w:pPr><w:r><w:t>b</w:t></w:r></w:p>` +
	`<w:p w14:paraId="0000000C"><w:r><w:t>c</w:t></w:r></w:p>`

// TestSectionBoundary: adding a boundary copies the section it splits
// (with the break type), removing one merges into the next section,
// which takes the stories it lacked; track mode refuses both.
func TestSectionBoundary(t *testing.T) {
	path := writeDocx(t, strings.ReplaceAll(boundaryBody, `r:id`, `w:dummy`))
	c := commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_boundary","at":"0000000A","break_type":"continuous"}]`))
	if c.Outcome != Committed {
		t.Fatalf("add: %+v", c.Diagnostic)
	}
	got := mainXML(t, path)
	if !strings.Contains(got, `<w:p w14:paraId="0000000A"><w:pPr><w:sectPr><w:headerReference w:type="default" w:dummy="rId9"/><w:type w:val="continuous"/><w:pgSz w:w="100" w:h="200"/></w:sectPr></w:pPr>`) {
		t.Errorf("add: %s", got)
	}
	c = commit(t, path, opsText(t, path, "direct", `[{"op":"section.set_boundary","at":"0000000B","remove":true}]`))
	if c.Outcome != Committed {
		t.Fatalf("remove: %+v", c.Diagnostic)
	}
	got = mainXML(t, path)
	if strings.Count(got, "<w:sectPr") != 2 || !strings.Contains(got, `<w:sectPr><w:headerReference w:type="default" w:dummy="rId9"/></w:sectPr></w:body>`) {
		t.Errorf("remove: %s", got)
	}
	r, err := runText(path, opsText(t, path, "track", `[{"op":"section.set_boundary","at":"0000000C"}]`), Options{View: viewOpt})
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != Rejected || r.Diagnostic.Code != "refused_in_mode" {
		t.Errorf("track: %+v", r.Diagnostic)
	}
}

// TestSectionBoundaryLaws: direct only, on the write fixtures and law
// inputs, adding a boundary after the first body paragraph.
func TestSectionBoundaryLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) == 0 {
			return
		}
		s := replaceSuite("section.set_boundary", `[{"op":"section.set_boundary","at":"`+ids[0]+`"}]`)
		s.Class = laws.OnlyDirect
		s.Refused = func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		}
		s.Check(t, in)
	})
}
