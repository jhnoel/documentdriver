package shift

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestElementTableComplete(t *testing.T) {
	model, ok := schema.ContentModel("x:CT_Worksheet")
	if !ok {
		t.Fatal("missing worksheet model")
	}
	seen := map[string]bool{}
	for _, child := range model.Children {
		if child.Local == "sheetData" {
			continue
		}
		seen[child.Local] = true
		if _, ok := elementRules[child.Local]; !ok {
			t.Errorf("unclassified %s", child.Local)
		}
	}
	for name, rule := range elementRules {
		if !seen[name] {
			t.Errorf("extra classification %s", name)
		}
		t.Run(name, func(t *testing.T) {
			input := "<" + name + "/>"
			switch rule {
			case "cols":
				input = `<cols><col min="1" max="1"/></cols>`
			case "cf":
				input = `<conditionalFormatting sqref="A1"/>`
			case "dv":
				input = `<dataValidations><dataValidation sqref="A1"/></dataValidations>`
			case "refs":
				switch name {
				case "mergeCells":
					input = `<mergeCells><mergeCell ref="A1:B1"/></mergeCells>`
				case "hyperlinks":
					input = `<hyperlinks><hyperlink ref="A1"/></hyperlinks>`
				}
			case "breaks":
				input = "<" + name + `><brk id="1"/></` + name + ">"
			}
			e := formula.Edit{Sheet: "S", Kind: formula.InsertRows, At: 5, Count: 1}
			got, _, err := Element(name, input, formula.Sheets{Own: "S"}, e)
			if err != nil || got != input {
				t.Fatalf("identity: got %q, %v; want %q", got, err, input)
			}
		})
	}
}

func TestElementChunk(t *testing.T) {
	e := formula.Edit{Sheet: "S", Kind: formula.DeleteRows, At: 2, Count: 1}
	cases := []struct{ local, in, want string }{
		{"mergeCells", `<mergeCells count="2"><mergeCell ref="A1:A2"/><mergeCell ref="C3:D3"/></mergeCells>`, `<mergeCells count="1"><mergeCell ref="C2:D2"/></mergeCells>`},
		{"hyperlinks", `<hyperlinks><hyperlink ref="A2"/><hyperlink ref="B3"/></hyperlinks>`, `<hyperlinks><hyperlink ref="B2"/></hyperlinks>`},
		{"rowBreaks", `<rowBreaks count="2" manualBreakCount="1"><brk id="2" man="1"/><brk id="4"/></rowBreaks>`, `<rowBreaks count="1" manualBreakCount="0"><brk id="3"/></rowBreaks>`},
	}
	for _, tc := range cases {
		t.Run(tc.local, func(t *testing.T) {
			got, _, err := Element(tc.local, tc.in, formula.Sheets{Own: "S"}, e)
			if err != nil || got != tc.want {
				t.Fatalf("got %q %v; want %q", got, err, tc.want)
			}
		})
	}
	_, _, err := Element("protectedRanges", `<protectedRanges><protectedRange sqref="A3"/></protectedRanges>`, formula.Sheets{Own: "S"}, e)
	var refused *Refused
	if !errors.As(err, &refused) {
		t.Fatalf("expected refusal: %v", err)
	}
}
