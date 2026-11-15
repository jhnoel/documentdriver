package edit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The styles kernel's semantics (designer rows).
func TestPlanFormat(t *testing.T) {
	tbl := StyleTable{
		NumFmts: map[int]string{164: "0.000"},
		Fonts:   []string{`<font><sz val="11"/><name val="Calibri"/></font>`},
		Fills:   []string{`<fill><patternFill patternType="none"/></fill>`, `<fill><patternFill patternType="gray125"/></fill>`},
		XFs:     []string{`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`, `<xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`},
	}
	xlsxtest.Skip(t, func() {
		if id, code, ok := NumFmtOf(tbl, 1); !ok || id != 14 || code == "" {
			t.Errorf("NumFmtOf(1) = %d %q %v", id, code, ok)
		}
		if p, err := PlanNumFmt(tbl, 0, 14); err != nil || p != (StylePlan{XF: 1}) {
			t.Errorf("PlanNumFmt reuse = %+v %v", p, err)
		}
		p, err := PlanNumFmt(tbl, 0, 22)
		if err != nil || p.XF != 2 || p.NewXF != `<xf numFmtId="22" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` {
			t.Errorf("PlanNumFmt new = %+v %v", p, err)
		}
		code := "0.000"
		if p, err := PlanFormat(tbl, 0, CellFormat{NumberFormat: &code}); err != nil || p.NewNumFmt != "" || p.NewXF != `<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` {
			t.Errorf("custom reuse = %+v %v", p, err)
		}
		other := "#,##0.0"
		if p, _ := PlanFormat(tbl, 0, CellFormat{NumberFormat: &other}); p.NewNumFmt != `<numFmt numFmtId="165" formatCode="#,##0.0"/>` {
			t.Errorf("custom new = %+v", p)
		}
		bold, fill := true, "FF0000"
		p, err = PlanFormat(tbl, 0, CellFormat{Bold: &bold, Fill: &fill})
		if err != nil || p.NewFont != `<font><b/><sz val="11"/><name val="Calibri"/></font>` ||
			p.NewFill != `<fill><patternFill patternType="solid"><fgColor rgb="FFFF0000"/><bgColor indexed="64"/></patternFill></fill>` ||
			p.NewXF != `<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>` {
			t.Errorf("bold fill = %+v %v", p, err)
		}
		bad := "middle"
		if _, err := PlanFormat(tbl, 0, CellFormat{Align: &bad}); err == nil {
			t.Error("a bad alignment was planned")
		}
	})
}
