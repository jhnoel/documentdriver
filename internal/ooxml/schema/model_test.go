package schema

import (
	"slices"
	"testing"
)

func TestContentModel(t *testing.T) {
	m, ok := ContentModel("w:CT_P")
	if !ok {
		t.Fatal("no model for w:CT_P")
	}
	i := slices.IndexFunc(m.Children, func(p Particle) bool { return p.Prefix == "w" && p.Local == "pPr" })
	if i < 0 || m.Children[i].Max != 1 || m.Children[i].Type != "w:CT_PPr" || m.Children[i].Rank == NoRank {
		t.Errorf("w:pPr in w:CT_P: %+v", m.Children)
	}
	tbl, ok := ContentModel("w:CT_Tbl")
	if !ok || !slices.ContainsFunc(tbl.Children, func(p Particle) bool { return p.Local == "tblGrid" && p.Required }) {
		t.Errorf("w:CT_Tbl, untabulated as slots, has no model requiring w:tblGrid: %+v", tbl)
	}
	jc, _ := ContentModel("w:CT_Jc")
	if !slices.Contains(jc.Attrs, ModelAttr{"w", "val", "w:ST_Jc", true}) {
		t.Errorf("w:CT_Jc attributes %+v", jc.Attrs)
	}
	text, _ := ContentModel("w:CT_Text")
	if !slices.Contains(text.Attrs, ModelAttr{"xml", "space", "xml:space", false}) {
		t.Errorf("w:CT_Text attributes %+v", text.Attrs)
	}
	if typ, ok := RootType("w", "document"); !ok || typ != "w:CT_Document" {
		t.Errorf("RootType(document) = %q", typ)
	}
	for _, st := range []string{"r:ST_RelationshipId", "xml:space"} {
		if _, ok := SimpleType(st); !ok {
			t.Errorf("no simple type %s", st)
		}
	}
	if err := CheckValue("xml:space", "keep"); err == nil {
		t.Error("xml:space keep accepted")
	}
}
