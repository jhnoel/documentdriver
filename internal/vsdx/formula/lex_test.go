package formula

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TestLex checks the FL1 rows: Width*0.5, GUARD(Sheet.5!Width*0.25),
// "a""b" as one string, 1IN*DropOnPageScale, the PAR(PNT(...))
// round trip, and the reference, operator, number and string forms
// the corpus and the chunk name.
func TestLex(t *testing.T) {
	rows := []struct {
		in   string
		want []Token
	}{
		{"Width*0.5", []Token{{Kind: Ref, Text: "Width"}, {Kind: Op, Text: "*"}, {Kind: Number, Text: "0.5"}}},
		{"GUARD(Sheet.5!Width*0.25)", []Token{{Kind: Func, Text: "GUARD"}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "Sheet.5!Width"}, {Kind: Op, Text: "*"}, {Kind: Number, Text: "0.25"}, {Kind: RParen, Text: ")"}}},
		{`"a""b"`, []Token{{Kind: String, Text: `"a""b"`}}},
		{"1IN*DropOnPageScale", []Token{{Kind: Number, Text: "1IN"}, {Kind: Op, Text: "*"}, {Kind: Ref, Text: "DropOnPageScale"}}},
		{"PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1))", []Token{{Kind: Func, Text: "PAR"}, {Kind: LParen, Text: "("}, {Kind: Func, Text: "PNT"}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "Sheet.135!Connections.X1"}, {Kind: Comma, Text: ","}, {Kind: Ref, Text: "Sheet.135!Connections.Y1"}, {Kind: RParen, Text: ")"}, {Kind: RParen, Text: ")"}}},
		{"Prop.Cost", []Token{{Kind: Ref, Text: "Prop.Cost"}}},
		{"Geometry1.X2", []Token{{Kind: Ref, Text: "Geometry1.X2"}}},
		{"Char.Size[2]", []Token{{Kind: Ref, Text: "Char.Size[2]"}}},
		{"Sheet.5!Width", []Token{{Kind: Ref, Text: "Sheet.5!Width"}}},
		{"ThePage!PageHeight", []Token{{Kind: Ref, Text: "ThePage!PageHeight"}}},
		{"Connections.X1", []Token{{Kind: Ref, Text: "Connections.X1"}}},
		{"Sheet.5!Actions.Start.Checked", []Token{{Kind: Ref, Text: "Sheet.5!Actions.Start.Checked"}}},
		{"Controls.Row_1.Y", []Token{{Kind: Ref, Text: "Controls.Row_1.Y"}}},
		{"User.LockDate", []Token{{Kind: Ref, Text: "User.LockDate"}}},
		{"Inh", []Token{{Kind: Ref, Text: "Inh"}}},
		{"No Formula", []Token{{Kind: Ref, Text: "No"}, {Kind: Space, Text: " "}, {Kind: Ref, Text: "Formula"}}},
		{"1 IN", []Token{{Kind: Number, Text: "1"}, {Kind: Space, Text: " "}, {Kind: Ref, Text: "IN"}}},
		{"0.5DL", []Token{{Kind: Number, Text: "0.5DL"}}},
		{".5", []Token{{Kind: Number, Text: ".5"}}},
		{"1E-4", []Token{{Kind: Number, Text: "1E-4"}}},
		{"Width*-2.4330900243309006E-4", []Token{{Kind: Ref, Text: "Width"}, {Kind: Op, Text: "*"}, {Kind: Op, Text: "-"}, {Kind: Number, Text: "2.4330900243309006E-4"}}},
		{"100%", []Token{{Kind: Number, Text: "100"}, {Kind: Op, Text: "%"}}},
		{"0DA", []Token{{Kind: Number, Text: "0DA"}}},
		{`THEMEVAL("LineWeight",0DT)`, []Token{{Kind: Func, Text: "THEMEVAL"}, {Kind: LParen, Text: "("}, {Kind: String, Text: `"LineWeight"`}, {Kind: Comma, Text: ","}, {Kind: Number, Text: "0DT"}, {Kind: RParen, Text: ")"}}},
		{`INDEX(User.HSide,"0;3;0;1")`, []Token{{Kind: Func, Text: "INDEX"}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "User.HSide"}, {Kind: Comma, Text: ","}, {Kind: String, Text: `"0;3;0;1"`}, {Kind: RParen, Text: ")"}}},
		{"_WALKGLUE(EndTrigger,BegTrigger,WalkPreference)", []Token{{Kind: Func, Text: "_WALKGLUE"}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "EndTrigger"}, {Kind: Comma, Text: ","}, {Kind: Ref, Text: "BegTrigger"}, {Kind: Comma, Text: ","}, {Kind: Ref, Text: "WalkPreference"}, {Kind: RParen, Text: ")"}}},
		{"A+B-C/D^E&F=G", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: "+"}, {Kind: Ref, Text: "B"}, {Kind: Op, Text: "-"}, {Kind: Ref, Text: "C"}, {Kind: Op, Text: "/"}, {Kind: Ref, Text: "D"}, {Kind: Op, Text: "^"}, {Kind: Ref, Text: "E"}, {Kind: Op, Text: "&"}, {Kind: Ref, Text: "F"}, {Kind: Op, Text: "="}, {Kind: Ref, Text: "G"}}},
		{"A<>B", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: "<>"}, {Kind: Ref, Text: "B"}}},
		{"A<=B", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: "<="}, {Kind: Ref, Text: "B"}}},
		{"A>=B", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: ">="}, {Kind: Ref, Text: "B"}}},
		{"A<B", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: "<"}, {Kind: Ref, Text: "B"}}},
		{"A>B", []Token{{Kind: Ref, Text: "A"}, {Kind: Op, Text: ">"}, {Kind: Ref, Text: "B"}}},
		{"A;B", []Token{{Kind: Ref, Text: "A"}, {Kind: Semicolon, Text: ";"}, {Kind: Ref, Text: "B"}}},
		{"(BeginX+EndX)/2", []Token{{Kind: LParen, Text: "("}, {Kind: Ref, Text: "BeginX"}, {Kind: Op, Text: "+"}, {Kind: Ref, Text: "EndX"}, {Kind: RParen, Text: ")"}, {Kind: Op, Text: "/"}, {Kind: Number, Text: "2"}}},
		{"SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)", []Token{{Kind: Func, Text: "SQRT"}, {Kind: LParen, Text: "("}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "EndX"}, {Kind: Op, Text: "-"}, {Kind: Ref, Text: "BeginX"}, {Kind: RParen, Text: ")"}, {Kind: Op, Text: "^"}, {Kind: Number, Text: "2"}, {Kind: Op, Text: "+"}, {Kind: LParen, Text: "("}, {Kind: Ref, Text: "EndY"}, {Kind: Op, Text: "-"}, {Kind: Ref, Text: "BeginY"}, {Kind: RParen, Text: ")"}, {Kind: Op, Text: "^"}, {Kind: Number, Text: "2"}, {Kind: RParen, Text: ")"}}},
		{"Width*0.5 ", []Token{{Kind: Ref, Text: "Width"}, {Kind: Op, Text: "*"}, {Kind: Number, Text: "0.5"}, {Kind: Space, Text: " "}}},
		{"", nil},
		{`"abc`, []Token{{Kind: Invalid, Text: `"abc`}}},
		{"Char.Size[", []Token{{Kind: Ref, Text: "Char.Size"}, {Kind: Invalid, Text: "["}}},
		{`CONTAINERSHEETREF(1,"Swimlane")!User.VISHEADINGTEXT`, []Token{{Kind: Func, Text: "CONTAINERSHEETREF"}, {Kind: LParen, Text: "("}, {Kind: Number, Text: "1"}, {Kind: Comma, Text: ","}, {Kind: String, Text: `"Swimlane"`}, {Kind: RParen, Text: ")"}, {Kind: Invalid, Text: "!"}, {Kind: Ref, Text: "User.VISHEADINGTEXT"}}},
	}
	for _, row := range rows {
		want := make([]Token, len(row.want))
		pos := 0
		for i, tok := range row.want {
			want[i] = Token{Kind: tok.Kind, Text: tok.Text, Pos: pos}
			pos += len(tok.Text)
		}
		if pos != len(row.in) {
			t.Fatalf("row %q: want texts total %d bytes, input is %d", row.in, pos, len(row.in))
		}
		got := Lex(row.in)
		if !slices.Equal(got, want) {
			t.Errorf("Lex(%q) = %v, want %v", row.in, got, want)
		}
		if Join(got) != row.in {
			t.Errorf("Join(Lex(%q)) = %q, want the input", row.in, Join(got))
		}
	}
}

// TestLexNumberValue checks Token.NumberValue: the FL1 row 1IN is
// the number 1 with unit IN, the exponent form carries no unit, an
// unknown unit is still reported (the later layers refuse it), and a
// non-finite or non-number token is not ok.
func TestLexNumberValue(t *testing.T) {
	rows := []struct {
		text  string
		value float64
		unit  string
		ok    bool
	}{
		{"1IN", 1, "IN", true},
		{"0.5DL", 0.5, "DL", true},
		{"25.4MM", 25.4, "MM", true},
		{"90DEG", 90, "DEG", true},
		{"1E-4", 0.0001, "", true},
		{".5", 0.5, "", true},
		{"1.", 1, "", true},
		{"100", 100, "", true},
		{"1XX", 1, "XX", true},
		{"1E400", 0, "", false},
		{"", 0, "", false},
	}
	for _, row := range rows {
		got, unit, ok := (Token{Kind: Number, Text: row.text}).NumberValue()
		if got != row.value || unit != row.unit || ok != row.ok {
			t.Errorf("NumberValue(%q) = %v, %q, %v; want %v, %q, %v", row.text, got, unit, ok, row.value, row.unit, row.ok)
		}
	}
	if _, _, ok := (Token{Kind: Ref, Text: "Width"}).NumberValue(); ok {
		t.Error("NumberValue of a Ref token: ok, want not ok")
	}
	if got := Lex("1IN*DropOnPageScale")[0]; got.Kind != Number {
		t.Errorf("1IN first token kind = %v, want Number", got.Kind)
	} else if v, unit, ok := got.NumberValue(); v != 1 || unit != "IN" || !ok {
		t.Errorf("1IN number = %v, %q, %v; want 1, IN, true", v, unit, ok)
	}
}

// TestLexScaling checks the scaling row: Lex on a formula of
// laws.Small (1000) and laws.Large (4000) terms allocates at most
// 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio
// is at most 8.
func TestLexScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		var b strings.Builder
		for range n {
			b.WriteString("Width*0.5+")
		}
		b.WriteString("Width")
		formula := b.String()
		want := 4*n + 1
		return func() error {
			toks := Lex(formula)
			if len(toks) != want {
				return fmt.Errorf("Lex of %d terms: %d tokens, want %d", n, len(toks), want)
			}
			total := 0
			for _, tok := range toks {
				total += len(tok.Text)
			}
			if total != len(formula) {
				return fmt.Errorf("Lex of %d terms: token texts total %d bytes, input is %d", n, total, len(formula))
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Lex formula terms", setup)
	laws.TimeScales(t, "vsdx Lex formula terms", setup)
}

// lexCorpusDir returns the vsdx corpus directory: VSDX_CORPUS
// (directly, below corpus/vsdx when VSDX_CORPUS is a worktree root, or
// the vsdx directory of the repository corpus when unset), or "" when
// there is none.
func lexCorpusDir() string {
	if d := os.Getenv("VSDX_CORPUS"); d != "" {
		for _, cand := range []string{d, filepath.Join(d, "corpus", "vsdx"), filepath.Join(d, "vsdx")} {
			if _, err := os.Stat(filepath.Join(cand, "repos")); err == nil {
				return cand
			}
		}
		return ""
	}
	dir, err := repo.Corpus()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "vsdx")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// lexCorpusDrawings lists the drawings under dir in traversal order,
// sorted: the Visio extensions the design names for the corpus sweep.
func lexCorpusDrawings(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".vsdx", ".vsdm", ".vstx", ".vstm", ".vssx", ".vssm":
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// formulaCollector collects the F attributes of Cell elements in a
// tree.Scan visitor, resolving namespaces through a tree.NSStack
// (Scan reports nodes without their ancestors' declarations).
type formulaCollector struct {
	ns       tree.NSStack
	formulas []string
}

// Start records the namespace declarations of element n and collects
// its F when it is a Cell in a Visio main namespace.
func (c *formulaCollector) Start(n *tree.Node) error {
	c.ns.Push(n)
	if n.Local() == vsd.ElemCell && vsd.IsMain(c.ns.Space(n)) {
		if v, ok := n.Attr("F"); ok {
			c.formulas = append(c.formulas, v)
		}
	}
	return nil
}

// End drops the namespace declarations of the innermost element.
func (c *formulaCollector) End(*tree.Node) error {
	c.ns.Pop()
	return nil
}

// Leaf ignores the non-element node n.
func (c *formulaCollector) Leaf(*tree.Node) error { return nil }

// lexCorpusCounts are one file's round-trip tally.
type lexCorpusCounts struct {
	// formulas counts the Cell F values lexed.
	formulas int
	// tokens counts the tokens they lexed to.
	tokens int
	// invalid counts the file as invalid_document (not a zip package).
	invalid int
}

// TestLexCorpus checks the FL1 corpus row: concatenating the tokens
// of every Cell F of the Visio-authored corpus files gives the F
// back, and the count is logged. It skips without VSDX_CORPUS and
// fails naming the path when VSDX_CORPUS is set but holds no corpus.
func TestLexCorpus(t *testing.T) {
	dir := lexCorpusDir()
	if dir == "" {
		if d := os.Getenv("VSDX_CORPUS"); d != "" {
			t.Fatalf("VSDX_CORPUS %s holds no vsdx corpus (no repos directory under it)", d)
		}
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := lexCorpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("VSDX_CORPUS %s holds no vsdx drawings under %s", os.Getenv("VSDX_CORPUS"), dir)
	}
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *lexCorpusCounts) {
		pkg, err := opc.Open(f.Path)
		if err != nil {
			r.invalid = 1
			return
		}
		defer pkg.Close()
		for _, name := range pkg.Names() {
			if !strings.HasSuffix(strings.ToLower(name), ".xml") {
				continue
			}
			pt, err := pkg.Part(name)
			if err != nil {
				t.Errorf("Part(%s): %v", name, err)
				continue
			}
			s, ok, err := pt.OriginalString()
			if err != nil || !ok {
				t.Errorf("OriginalString(%s): %v, ok %v", name, err, ok)
				continue
			}
			coll := &formulaCollector{}
			if err := tree.ScanString(s, coll); err != nil {
				t.Errorf("scan %s: %v", name, err)
				continue
			}
			for _, formula := range coll.formulas {
				toks := Lex(formula)
				if Join(toks) != formula {
					t.Errorf("Join(Lex(%q)) = %q in %s part %s, want the F back", formula, Join(toks), f.Name, name)
				}
				pos := 0
				for _, tok := range toks {
					if tok.Pos != pos || tok.Text == "" {
						t.Errorf("Lex(%q): token %+v at %d, want a non-empty token at %d", formula, tok, tok.Pos, pos)
						break
					}
					pos += len(tok.Text)
				}
				r.formulas++
				r.tokens += len(toks)
			}
		}
	})
	var formulas, tokens, invalid int
	for _, r := range res {
		if !r.Ran {
			continue
		}
		formulas += r.Value.formulas
		tokens += r.Value.tokens
		invalid += r.Value.invalid
	}
	t.Logf("vsdx Lex corpus: %d files, %d invalid_document, %d Cell F values, %d tokens, all concatenate back", len(res), invalid, formulas, tokens)
	if !corpustest.Sharded() && formulas == 0 {
		t.Error("corpus holds no Cell F values")
	}
}

// FuzzLex feeds arbitrary strings to Lex: it must never panic, and
// the tokens must always concatenate to the input with no empty token
// and each token at its stated position (chunk FL1).
func FuzzLex(f *testing.F) {
	for _, s := range []string{
		"", "Width*0.5", "GUARD(Sheet.5!Width*0.25)", `"a""b"`, "1IN*DropOnPageScale",
		"PAR(PNT(Sheet.135!Connections.X1,Sheet.135!Connections.Y1))", "No Formula", "Char.Size[2]",
		"THEMEVAL(\"LineWeight\",0DT)", "1E-4", "Width<>Height", `"unterminated`, "Sheet.5!",
		"1E", ".5IN", "A<=B", "_WALKGLUE(x)", "Sheet.5!Actions.Start.Checked", "\xff\xfe", "1 IN",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		toks := Lex(s)
		if Join(toks) != s {
			t.Errorf("Join(Lex(%q)) = %q, want the input", s, Join(toks))
		}
		pos := 0
		for _, tok := range toks {
			if tok.Pos != pos || tok.Text == "" || tok.Text != s[pos:pos+len(tok.Text)] {
				t.Errorf("Lex(%q): token %+v, want a non-empty token at %d matching the input", s, tok, pos)
				break
			}
			if tok.Kind == Number {
				_, _, _ = tok.NumberValue()
			}
			pos += len(tok.Text)
		}
		if pos != len(s) {
			t.Errorf("Lex(%q): tokens cover %d bytes, input is %d", s, pos, len(s))
		}
	})
}
