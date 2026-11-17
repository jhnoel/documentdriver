package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// vsdxVocabFragments are the Visio fragments allowed only under internal/vsdx/vsd/.
var vsdxVocabFragments = []string{"schemas.microsoft.com/office/visio", "prop:", "layer:", "field:"}

// vsdxVocabPrefixes are the literals banned as the left operand of + outside internal/vsdx/vsd/.
var vsdxVocabPrefixes = map[string]bool{"page": true, "master": true, "s": true, "/s": true}

// vsdxVocabViolations reports TestVsdxVocabulary violations in f,
// named by its module-relative slash path rel.
func vsdxVocabViolations(fset *token.FileSet, rel string, f *ast.File) []dupViolation {
	if strings.HasPrefix(rel, "internal/vsdx/vsd/") || strings.HasSuffix(rel, "_test.go") {
		return nil
	}
	if !strings.HasPrefix(rel, "internal/vsdx/") && !strings.HasPrefix(rel, "cmd/vsdx/") {
		return nil
	}
	imports := map[*ast.BasicLit]bool{}
	for _, im := range f.Imports {
		imports[im.Path] = true
	}
	var violations []dupViolation
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind != token.STRING || imports[x] {
				return true
			}
			v, err := strconv.Unquote(x.Value)
			if err != nil {
				return true
			}
			for _, frag := range vsdxVocabFragments {
				if strings.Contains(v, frag) {
					violations = append(violations, dupViolation{rel, dupLine(fset, x.Pos()), "Visio vocabulary literal containing " + strconv.Quote(frag) + ": use internal/vsdx/vsd"})
					break
				}
			}
		case *ast.BinaryExpr:
			if x.Op != token.ADD {
				return true
			}
			if lit, ok := dupUnparen(x.X).(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil && vsdxVocabPrefixes[v] {
					violations = append(violations, dupViolation{rel, dupLine(fset, x.Pos()), "builds a Visio address with " + strconv.Quote(v) + " +: use internal/vsdx/vsd"})
				}
			}
		}
		return true
	})
	return violations
}

// TestVsdxVocabulary fails on a Visio namespace or address literal
// outside internal/vsdx/vsd/, or a binary + whose left operand is
// "page", "master", "s" or "/s" outside internal/vsdx/vsd/.
// Use internal/vsdx/vsd instead.
func TestVsdxVocabulary(t *testing.T) {
	// Row 1: the current tree passes.
	scan := dupUnits(t)
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			violations = append(violations, vsdxVocabViolations(scan.fset, u.names[i], f)...)
		}
	}
	dupCheckViolations(t, "TestVsdxVocabulary", violations, map[string]string{}, map[string]string{})

	// Rows 2-4: source text "page" + x, AST built from text, nothing
	// written. Row 2 fails naming internal/vsdx/doc/bad.go; the same
	// source under internal/vsdx/vsd/ and in a _test.go file passes.
	type row struct {
		name     string
		file     string
		src      string
		wantFail bool
	}
	const src = `package doc

func Bad(x string) string { return "page" + x }
`
	rows := []row{
		{name: "row-2-bad-go", file: "internal/vsdx/doc/bad.go", src: src, wantFail: true},
		{name: "row-3-vsd-exempt", file: "internal/vsdx/vsd/bad.go", src: src, wantFail: false},
		{name: "row-4-test-exempt", file: "internal/vsdx/doc/bad_test.go", src: src, wantFail: false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, r.file, r.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			got := vsdxVocabViolations(fset, r.file, f)
			if r.wantFail {
				if len(got) == 0 {
					t.Errorf("%s: want a violation naming it, got none", r.file)
				}
				for _, v := range got {
					if v.file != r.file {
						t.Errorf("violation file = %q, want %q", v.file, r.file)
					}
				}
			} else if len(got) != 0 {
				t.Errorf("%s: want no violations, got %v", r.file, got)
			}
		})
	}
}
