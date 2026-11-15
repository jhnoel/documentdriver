package archtest

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

// handSortedGrandfathered lists files that violate
// TestNoHandSortedKeys and are not owned by cleanup chunk E
// (frozen from E's first run; entries only shrink).
var handSortedGrandfathered = map[string]string{
	"eval/cmd/pptxbench/main.go":       "not in cleanup chunk E's owns; hand sort left for a follow-up",
	"internal/ooxml/edit/place.go":     "not in cleanup chunk E's owns; hand sort left for a follow-up",
	"internal/pptx/coverage/parity.go": "not in cleanup chunk E's owns; hand sort left for a follow-up",
	"internal/pptx/coverage/report.go": "not in cleanup chunk E's owns; hand sort left for a follow-up",
	"internal/xlsx/edit/sheet.go":      "locals are sorted by worksheetRank, a custom schema order, not alphabetical; slices.Sorted would change patch order",
}

// TestNoHandSortedKeys fails on a range over a map with only a key
// variable whose body is a single append of the key to a slice,
// followed in the same block by sort.Strings, sort.Slice,
// slices.Sort or slices.SortFunc of that slice. Use
// slices.Sorted(maps.Keys(m)) instead.
func TestNoHandSortedKeys(t *testing.T) {
	scan := dupUnits(t)
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			ast.Inspect(f, func(n ast.Node) bool {
				block, ok := n.(*ast.BlockStmt)
				if !ok {
					return true
				}
				for idx := 0; idx+1 < len(block.List); idx++ {
					rng, ok := block.List[idx].(*ast.RangeStmt)
					if !ok || rng.Value != nil || rng.Body == nil || len(rng.Body.List) != 1 {
						continue
					}
					keyID, ok := rng.Key.(*ast.Ident)
					if !ok || keyID.Name == "_" {
						continue
					}
					if typ := u.info.TypeOf(rng.X); typ != nil {
						if _, isMap := typ.Underlying().(*types.Map); !isMap {
							continue
						}
					}
					assign, ok := rng.Body.List[0].(*ast.AssignStmt)
					if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
						continue
					}
					sliceID, ok := assign.Lhs[0].(*ast.Ident)
					if !ok {
						continue
					}
					call, ok := assign.Rhs[0].(*ast.CallExpr)
					if !ok || len(call.Args) != 2 {
						continue
					}
					appendID, ok := call.Fun.(*ast.Ident)
					if !ok || appendID.Name != "append" {
						continue
					}
					if arg, ok := call.Args[0].(*ast.Ident); !ok || arg.Name != sliceID.Name {
						continue
					}
					if arg, ok := call.Args[1].(*ast.Ident); !ok || arg.Name != keyID.Name {
						continue
					}
					expr, ok := block.List[idx+1].(*ast.ExprStmt)
					if !ok {
						continue
					}
					sortCall, ok := expr.X.(*ast.CallExpr)
					if !ok || len(sortCall.Args) == 0 {
						continue
					}
					if arg, ok := sortCall.Args[0].(*ast.Ident); !ok || arg.Name != sliceID.Name {
						continue
					}
					isSort := dupCall(u, sortCall, "sort", "Strings") || dupCall(u, sortCall, "sort", "Slice") ||
						dupCall(u, sortCall, "slices", "Sort") || dupCall(u, sortCall, "slices", "SortFunc")
					if !isSort {
						continue
					}
					violations = append(violations, dupViolation{rel, dupLine(scan.fset, rng.Pos()), "hand-sorted map keys: use slices.Sorted(maps.Keys(m))"})
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestNoHandSortedKeys", violations, map[string]string{}, handSortedGrandfathered)
}

// asciiGrandfathered lists files that violate TestASCIIHelpers and
// are not owned by cleanup chunk E (frozen from E's first run;
// entries only shrink).
var asciiGrandfathered = map[string]string{
	"internal/docx/view/compact.go": "byte-typed isASCIIAlpha (ascii.IsAlpha takes a rune); not in any cleanup chunk's owns",
}

// TestASCIIHelpers fails on a non-test function declaration named
// asciiLower, asciiLowerByte, asciiLowerRune, asciiEqualFold,
// isASCII, isASCIIDigit, isASCIIAlpha, isASCIIAlnum or isASCIIHex
// (any case) outside internal/ascii. Use internal/ascii instead.
func TestASCIIHelpers(t *testing.T) {
	scan := dupUnits(t)
	banned := map[string]bool{
		"asciilower": true, "asciilowerbyte": true, "asciilowerrune": true,
		"asciiequalfold": true, "isascii": true, "isasciidigit": true,
		"isasciialpha": true, "isasciialnum": true, "isasciihex": true,
	}
	allow := map[string]string{"internal/ascii/": "the one ASCII helper package"}
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if dupExempt(rel, allow) {
				continue
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if banned[strings.ToLower(fd.Name.Name)] {
					violations = append(violations, dupViolation{rel, dupLine(scan.fset, fd.Pos()), "function " + fd.Name.Name + ": use internal/ascii"})
				}
			}
		}
	}
	dupCheckViolations(t, "TestASCIIHelpers", violations, allow, asciiGrandfathered)
}
