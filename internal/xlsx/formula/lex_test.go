package formula

import (
	"strings"
	"testing"
)

func kinds(toks []Token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t.Kind.String())
		b.WriteByte(':')
		b.WriteString(t.Text)
	}
	return b.String()
}

func TestLex(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"SUM(A1:B2)", "func:SUM lparen:( ref:A1:B2 rparen:)"},
		{"LOG10(1E5)", "func:LOG10 lparen:( number:1E5 rparen:)"},
		{"$A$1+A$2*$B3", "ref:$A$1 op:+ ref:A$2 op:* ref:$B3"},
		{"A:A 1:1", "ref:A:A space:  ref:1:1"},
		{"Sheet1!A1", "ref:Sheet1!A1"},
		{"'My Sheet'!$A$1", "ref:'My Sheet'!$A$1"},
		{"'It''s'!A1", "ref:'It''s'!A1"},
		{"Jan:Mar!B2", "ref:Jan:Mar!B2"},
		{"'Jan 1:Mar'!B2", "ref:'Jan 1:Mar'!B2"},
		{"'a':'b'!B2", "ref:'a':'b'!B2"},
		{"[1]Sheet1!A1", "ref:[1]Sheet1!A1"},
		{"'[1]My Sheet'!A1", "ref:'[1]My Sheet'!A1"},
		{"[1]!Rate", "name:[1]!Rate"},
		{"Sheet2!#REF!", "ref:Sheet2!#REF!"},
		{"#REF!+#N/A", "error:#REF! op:+ error:#N/A"},
		{"Sheet1!Rate", "name:Sheet1!Rate"},
		{"!A1", "ref:!A1"},
		{"_xlfn._xlws.SORT(A1:A3)", "func:_xlfn._xlws.SORT lparen:( ref:A1:A3 rparen:)"},
		{"_xlfn.LET(_xlpm.x,1,_xlpm.x)", "func:_xlfn.LET lparen:( name:_xlpm.x comma:, number:1 comma:, name:_xlpm.x rparen:)"},
		{"_xlfn.ANCHORARRAY(A1)", "func:_xlfn.ANCHORARRAY lparen:( ref:A1 rparen:)"},
		{"Sales[[#This Row],[Qty]]", "structref:Sales[[#This Row],[Qty]]"},
		{"T[Col'[x']]", "structref:T[Col'[x']]"},
		{"{1,2;-3,\"a\"\"b\"}", "lbrace:{ number:1 comma:, number:2 semicolon:; op:- number:3 comma:, string:\"a\"\"b\" rbrace:}"},
		{"TRUE<>FALSE()", "bool:TRUE op:<> func:FALSE lparen:( rparen:)"},
		{"A1>=.5%", "ref:A1 op:>= number:.5 op:%"},
		{"XFE1+A1B", "name:XFE1 op:+ name:A1B"},
		{"A1:B2C", "ref:A1 op:: name:B2C"},
		{"A1#", "ref:A1 hash:#"},
		{"@A1", "at:@ ref:A1"},
		{"\"open", "invalid:\"open"},
		{"'no bang'", "invalid:'no bang'"},
		{"[open", "invalid:[open"},
		{"Sheet・1!A1", "ref:Sheet・1!A1"},
		{"A1<>““", "ref:A1 op:<> name:““"},
		{"\xff", "invalid:\xff"},
	} {
		toks := Lex(tc.in)
		if got := kinds(toks); got != tc.want {
			t.Errorf("Lex(%q)\n got %s\nwant %s", tc.in, got, tc.want)
		}
		if Join(toks) != tc.in {
			t.Errorf("Lex(%q) does not join back", tc.in)
		}
	}
}

func TestSplitQualified(t *testing.T) {
	for _, tc := range []struct {
		in                string
		book, first, last string
		hasBook, quoted   bool
		rest              string
	}{
		{"A1", "", "", "", false, false, "A1"},
		{"Sheet1!A1", "", "Sheet1", "", false, false, "A1"},
		{"'It''s'!A1", "", "It's", "", false, true, "A1"},
		{"Jan:Mar!A1", "", "Jan", "Mar", false, false, "A1"},
		{"'Jan 1:Mar'!A1", "", "Jan 1", "Mar", false, true, "A1"},
		{"'a':'b'!A1", "", "a", "b", false, true, "A1"},
		{"[1]Sheet1!A1", "1", "Sheet1", "", true, false, "A1"},
		{"'[1]My Sheet'!A1", "1", "My Sheet", "", true, true, "A1"},
		{"[1]!Rate", "1", "", "", true, false, "Rate"},
		{"!A1", "", "", "", false, false, "A1"},
	} {
		p, rest, ok := SplitQualified(tc.in)
		if !ok || p.Book != tc.book || p.First != tc.first || p.Last != tc.last || p.HasBook != tc.hasBook || p.Quoted != tc.quoted || rest != tc.rest {
			t.Errorf("SplitQualified(%q) = %+v, %q, %v", tc.in, p, rest, ok)
		}
	}
}

func TestSplitPrefix(t *testing.T) {
	for in, want := range map[string][2]string{
		"_xlfn._xlws.SORT": {PrefixWorksheet, "SORT"},
		"_xlfn.XLOOKUP":    {PrefixFuture, "XLOOKUP"},
		"_xlpm.x":          {PrefixParam, "x"},
		"SUM":              {"", "SUM"},
		"_xlfn.":           {"", "_xlfn."},
	} {
		p, r := SplitPrefix(in)
		if p != want[0] || r != want[1] {
			t.Errorf("SplitPrefix(%q) = %q, %q", in, p, r)
		}
	}
}

func TestNeedsQuote(t *testing.T) {
	for s, want := range map[string]bool{
		"Sheet1": false, "Data_2024": false, "Résumé": false, "My Sheet": true, "2024": true,
		"A1": true, "R1C1": true, "R": true, "It's": true, "a-b": true, "": true, ".x": true,
	} {
		if got := NeedsQuote(s); got != want {
			t.Errorf("NeedsQuote(%q) = %v", s, got)
		}
	}
	if got := SheetPrefix("It's", ""); got != "'It''s'!" {
		t.Errorf("SheetPrefix = %q", got)
	}
	if got := SheetPrefix("Jan", "Mar 2"); got != "'Jan:Mar 2'!" {
		t.Errorf("SheetPrefix 3-D = %q", got)
	}
}
