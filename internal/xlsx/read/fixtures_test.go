package read

import (
	"fmt"
	"strings"
)

// The cellXfs of the value fixtures, by index: the numFmtId each uses.
const (
	xfGeneral  = 0 // 0 General
	xfDate     = 1 // 14 m/d/yyyy
	xfDateTime = 2 // 22 m/d/yyyy h:mm
	xfTime     = 3 // 21 h:mm:ss
	xfDuration = 4 // 164 [h]:mm
	xfCustomDT = 5 // 165 yyyy-mm-dd hh:mm
	xfPercent  = 6 // 10 0.00%
	xfText     = 7 // 49 @
)

func valueStyles(f fx) fx {
	f.numFmts = map[int]string{164: "[h]:mm", 165: "yyyy-mm-dd hh:mm"}
	f.xfs = []int{0, 14, 22, 21, 164, 165, 10, 49}
	return f
}

// valuesBook has every value form on sheet Values.
func valuesBook() fx {
	return valueStyles(fx{
		sst: []string{
			`<t>Item</t>`,
			`<r><rPr><b/></rPr><t>Va</t></r><r><t xml:space="preserve">lue </t></r><rPh sb="0" eb="1"><t>ignored</t></rPh>`,
			`<t>=not formula</t>`,
			`<t>a_x000D_b</t>`,
		},
		sheets: []fxSheet{{name: "Values", rows: `` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>99</v></c></row>` +
			`<row r="2"><c r="A2"><v>1200</v></c><c r="B2"><v>0.1</v></c><c r="C2"><v>1E-7</v></c><c r="D2" t="n"><v>-2.5</v></c><c r="E2"><v>NaN</v></c><c r="F2"><v>abc</v></c><c r="G2"><v>12345678901234567890</v></c></row>` +
			`<row r="3"><c r="A3" t="b"><v>1</v></c><c r="B3" t="b"><v>0</v></c><c r="C3" t="b"><v>2</v></c><c r="D3" t="e"><v>#DIV/0!</v></c>` +
			`<c r="E3" t="str"><v>x_x0009_y</v></c><c r="F3" t="inlineStr"><is><r><t>in</t></r><r><t>line</t></r><rPh><t>no</t></rPh></is></c><c r="G3" t="inlineStr"><is><t>=x</t></is></c><c r="H3" t="zzz"><v>1</v></c></row>` +
			fmt.Sprintf(`<row r="4"><c r="A4" s="%d"><v>45352</v></c><c r="B4" s="%d"><v>45352.5625</v></c><c r="C4" s="%d"><v>0.5625</v></c><c r="D4" s="%d"><v>1.5</v></c>`, xfDate, xfDateTime, xfTime, xfDuration) +
			fmt.Sprintf(`<c r="E4" s="%d"><v>60</v></c><c r="F4" s="%d"><v>61</v></c><c r="G4" s="%d"><v>0.25</v></c><c r="H4" s="%d"><v>5</v></c><c r="I4" s="99"><v>5</v></c><c r="J4" s="%d"><v>-1</v></c><c r="K4" s="%d"><v>45352.75</v></c></row>`, xfDate, xfDate, xfPercent, xfText, xfDate, xfCustomDT) +
			fmt.Sprintf(`<row r="5"><c r="A5" t="d"><v>2024-03-01T00:00:00</v></c><c r="B5" t="d"><v>2024-03-01T13:30:00</v></c><c r="C5" t="d" s="%d"><v>1899-12-31T13:30:00</v></c><c r="D5" t="d"><v>garbage</v></c><c r="E5" s="%d"/><c r="F5" t="d" s="%d"><v>2024-03-01</v></c></row>`, xfTime, xfDate, xfDateTime) +
			`<row r="6"><c r="A6" t="e" vm="1"><v>#VALUE!</v></c><c r="B6" t="str"><v></v></c><c r="C6" t="s"/></row>`,
		}},
	})
}

// valuesWant are the expected cells of valuesBook's Values sheet in the
// cells view (also the values view: no cell has a formula).
var valuesWant = map[string]string{
	"A1": `"Item"`, "B1": `"Value "`, "C1": `{"text":"=not formula"}`, "D1": `"a\rb"`,
	"A2": `1200`, "B2": `0.1`, "C2": `1e-7`, "D2": `-2.5`, "G2": `12345678901234567000`,
	"A3": `true`, "B3": `false`, "D3": `{"error":"#DIV/0!"}`, "E3": `"x\ty"`, "F3": `"inline"`, "G3": `{"text":"=x"}`,
	"A4": `{"date":"2024-03-01"}`, "B4": `{"date":"2024-03-01T13:30:00"}`, "C4": `{"time":"13:30:00"}`, "D4": `1.5`,
	"E4": `60`, "F4": `{"date":"1900-03-01"}`, "G4": `0.25`, "H4": `5`, "I4": `5`, "J4": `-1`, "K4": `{"date":"2024-03-01T18:00:00"}`,
	"A5": `{"date":"2024-03-01"}`, "B5": `{"date":"2024-03-01T13:30:00"}`, "C5": `{"time":"13:30:00"}`, "D5": `"garbage"`, "F5": `{"date":"2024-03-01T00:00:00"}`,
	"A6": `{"error":"#VALUE!"}`, "B6": `""`,
	// Left out: E1 (index out of range), E2 (NaN), F2 (not a number),
	// C3 (bool 2), H3 (unknown type), E5 (style only), C6 (t="s" with no v).
}

// formulasBook has the formula forms on sheet Calc.
func formulasBook() fx {
	return fx{sheets: []fxSheet{{name: "Calc", rows: `` +
		`<row r="1"><c r="A1"><f>B1+C1</f><v>3</v></c><c r="B1"><f>_xlfn.XLOOKUP(1,A:A,B:B)</f></c><c r="C1" t="str"><f>"a"&amp;"b"</f><v>ab</v></c><c r="D1" t="e"><f>1/0</f><v>#DIV/0!</v></c></row>` +
		`<row r="2"><c r="A2"><f t="shared" ref="A2:A4" si="0">B2*2</f><v>2</v></c><c r="B2"><f t="shared" si="5"/><v>1</v></c><c r="C2"><f>_xlfn.SINGLE(A1:A3)</f><v>0</v></c><c r="D2"><f>SUM(_xlfn.ANCHORARRAY(E5))</f><v>6</v></c><c r="E2"><f></f><v>7</v></c></row>` +
		`<row r="3"><c r="A3"><f t="shared" si="0"/><v>4</v></c><c r="B3"><f>_xlfn.LET(_xlpm.x,1,_xlpm.x+1)</f><v>2</v></c></row>` +
		`<row r="4"><c r="A4"><f t="shared" si="0"/><v>6</v></c><c r="B4" t="b"><f>TRUE()</f><v>1</v></c></row>`,
	}}}
}

var formulasWant = map[string]string{
	"A1": `{"f":"=B1+C1","v":3}`, "B1": `{"f":"=XLOOKUP(1,A:A,B:B)"}`, "C1": `{"f":"=\"a\"&\"b\"","v":"ab"}`, "D1": `{"f":"=1/0","v":{"error":"#DIV/0!"}}`,
	"A2": `{"f":"=B2*2","v":2}`, "B2": `1`, "C2": `{"f":"=@A1:A3","v":0}`, "D2": `{"f":"=SUM(E5#)","v":6}`, "E2": `7`,
	"A3": `{"f":"=B3*2","v":4}`, "B3": `{"f":"=LET(x,1,x+1)","v":2}`,
	"A4": `{"f":"=B4*2","v":6}`, "B4": `{"f":"=TRUE()","v":true}`,
}

// spillBook has a dynamic spill, a CSE array, a single-cell CSE, a data
// table, and an array whose metadata block does not resolve.
func spillBook() fx {
	return fx{metadata: dynamicMetadata, sheets: []fxSheet{{name: "Spill", rows: `` +
		`<row r="1"><c r="A1" cm="1"><f t="array" ref="A1:A3">_xlfn.SEQUENCE(3)</f><v>1</v></c><c r="C1"><f t="array" ref="C1:D2">A1:B2*2</f><v>2</v></c><c r="D1"><v>4</v></c>` +
		`<c r="F1"><f t="array" ref="F1">SUM(A1:A3*1)</f><v>6</v></c><c r="K1" cm="9"><f t="array" ref="K1:K2">_xlfn.SEQUENCE(2)</f><v>1</v></c><c r="M1" cm="2"><f t="array" ref="M1:M2">X()</f><v>1</v></c></row>` +
		`<row r="2"><c r="A2"><v>2</v></c><c r="C2"><v>6</v></c><c r="D2"><v>8</v></c><c r="H2"><f t="dataTable" ref="H2:I3" dt2D="1" dtr="1" r1="A1" r2="B1"/><v>5</v></c><c r="I2"><v>6</v></c><c r="K2"><v>2</v></c><c r="M2"><v>2</v></c></row>` +
		`<row r="3"><c r="A3"><v>3</v></c><c r="H3"><v>7</v></c><c r="I3"><v>8</v></c></row>`,
	}}}
}

var spillWantCells = map[string]string{
	"A1": `{"f":"=SEQUENCE(3)","v":1}`, "A2": `{"v":2,"spill":"Spill!A1"}`, "A3": `{"v":3,"spill":"Spill!A1"}`,
	"C1": `{"f":"=A1:B2*2","v":2,"array":"cse"}`, "D1": `{"v":4,"spill":"Spill!C1","array":"cse"}`,
	"C2": `{"v":6,"spill":"Spill!C1","array":"cse"}`, "D2": `{"v":8,"spill":"Spill!C1","array":"cse"}`,
	"F1": `{"f":"=SUM(A1:A3*1)","v":6,"array":"cse"}`,
	"H2": `{"v":5,"array":"data_table"}`, "I2": `{"v":6,"spill":"Spill!H2","array":"data_table"}`,
	"H3": `{"v":7,"spill":"Spill!H2","array":"data_table"}`, "I3": `{"v":8,"spill":"Spill!H2","array":"data_table"}`,
	"K1": `{"f":"=SEQUENCE(2)","v":1}`, "K2": `{"v":2,"spill":"Spill!K1"}`,
	"M1": `{"f":"=X()","v":1,"array":"cse"}`, "M2": `{"v":2,"spill":"Spill!M1","array":"cse"}`,
}

var spillWantValues = map[string]string{
	"A1": `1`, "A2": `2`, "A3": `3`, "C1": `2`, "D1": `4`, "C2": `6`, "D2": `8`, "F1": `6`,
	"H2": `5`, "I2": `6`, "H3": `7`, "I3": `8`, "K1": `1`, "K2": `2`, "M1": `1`, "M2": `2`,
}

var spillWantFormulas = map[string]string{
	"A1": `"=SEQUENCE(3)"`, "A2": `{"spill":"Spill!A1"}`, "A3": `{"spill":"Spill!A1"}`,
	"C1": `{"f":"=A1:B2*2","array":"cse"}`, "D1": `{"spill":"Spill!C1","array":"cse"}`,
	"C2": `{"spill":"Spill!C1","array":"cse"}`, "D2": `{"spill":"Spill!C1","array":"cse"}`,
	"F1": `{"f":"=SUM(A1:A3*1)","array":"cse"}`,
	"H2": `{"array":"data_table"}`, "I2": `{"spill":"Spill!H2","array":"data_table"}`,
	"H3": `{"spill":"Spill!H2","array":"data_table"}`, "I3": `{"spill":"Spill!H2","array":"data_table"}`,
	"K1": `"=SEQUENCE(2)"`, "K2": `{"spill":"Spill!K1"}`,
	"M1": `{"f":"=X()","array":"cse"}`, "M2": `{"spill":"Spill!M1","array":"cse"}`,
}

// addressBook has sheets whose names need quoting, defined names and a
// table, for the address forms.
func addressBook() fx {
	rows := func(sheet string) string {
		var b strings.Builder
		for r := 1; r <= 4; r++ {
			fmt.Fprintf(&b, `<row r="%d"><c r="A%d"><v>%d</v></c><c r="B%d"><v>%d</v></c><c r="C%d"><v>%d</v></c></row>`, r, r, r*10+1, r, r*10+2, r, r*10+3)
		}
		_ = sheet
		return b.String()
	}
	return fx{
		sheets: []fxSheet{
			{name: "Budget", rows: `<row r="1"><c r="A1" t="inlineStr"><is><t>Region</t></is></c><c r="B1" t="inlineStr"><is><t>Amount</t></is></c></row>` +
				`<row r="2"><c r="A2" t="inlineStr"><is><t>N</t></is></c><c r="B2"><v>5</v></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>S</t></is></c><c r="B3"><v>7</v></c></row>` +
				`<row r="4"><c r="A4" t="inlineStr"><is><t>Total</t></is></c><c r="B4"><f>SUBTOTAL(109,B2:B3)</f><v>12</v></c></row>`,
				tables: []string{strings.Replace(table("Sales", "A1:B4", "Region", "Amount"), `ref="A1:B4">`, `ref="A1:B4" totalsRowCount="1">`, 1)}},
			{name: "Q1 Budget", rows: rows("Q1 Budget")},
			{name: "Bob's", rows: rows("Bob's")},
			{name: "2024", rows: rows("2024")},
			{name: "Chart1", kind: "chartsheet"},
			{name: "Gone", kind: "missing"},
			{name: "Broken", raw: xmlDecl + `<worksheet ` + xmlnsS + `><sheetData><row r="1"><c r="A1"><v>1</v></sheetData></worksheet>`},
		},
		definedNames: `<definedNames><definedName name="Const" hidden="1">0.5</definedName><definedName name="Local" localSheetId="0">Budget!$B$2:$B$3</definedName>` +
			`<definedName name="Rate">Budget!$B$2</definedName><definedName name="Union">Budget!$A$1,Budget!$B$2</definedName>` +
			`<definedName name="Quoted">'Q1 Budget'!$A:$A</definedName><definedName name="Rows">Budget!$2:$3</definedName><definedName name="Ref">Budget!#REF!</definedName>` +
			`<definedName name="_xlnm.Print_Area" localSheetId="0">Budget!$A$1:$B$4</definedName><definedName name="Bad" localSheetId="42">Budget!$A$1</definedName></definedNames>`,
	}
}

// bigBook is a sheet "Big" of rows x cols numbers, cell (r, c) = r*100+c.
func bigBook(rows, cols int) fx {
	var b strings.Builder
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for c := 1; c <= cols; c++ {
			fmt.Fprintf(&b, `<c r="%s%d"><v>%d</v></c>`, colName(c), r, r*100+c)
		}
		b.WriteString(`</row>`)
	}
	return fx{sheets: []fxSheet{{name: "Big", rows: b.String()}}}
}

func colName(c int) string {
	s := ""
	for c > 0 {
		c--
		s = string(rune('A'+c%26)) + s
		c /= 26
	}
	return s
}
