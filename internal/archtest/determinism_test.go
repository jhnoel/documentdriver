package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// nondeterministicImports are the packages that produce random values;
// fresh ids and the clock come from internal/ids instead.
var nondeterministicImports = set("math/rand", "math/rand/v2", "crypto/rand")

// determinismAllowed lists the non-test code that may use randomness or
// the wall clock, with the reason. Everything else takes an ids.Source.
// Directory entries end in "/".
var determinismAllowed = map[string]string{
	"internal/ids/":             "the one source of ids and time",
	"eval/cmd/pptxbench/":       "bench harness: run timings, wall-clock run stamps and the task-order shuffle seed; none reaches an output deck",
	"tools/pptoracle/":          "PowerPoint oracle driver: timeouts and per-run work directory names; no deck output",
	"tools/internal/officeapp/": "Office driver: timeouts and polling; no document output",
	"tools/word-smoke/":         "Word smoke driver: open timeouts and polling; no document output",
	"tools/excel-smoke/":        "Excel smoke driver: open timeouts and polling; no workbook output",
	"internal/laws/":            "test-only law harness (imported only by tests): seeded random values for the laws, an aged mtime on its private copies and the scaling gate's timings; no tool output",
}

// TestDeterminismSources fails when non-test code imports math/rand,
// math/rand/v2 or crypto/rand, or reads the clock with time.Now, outside
// internal/ids and the justified entries of determinismAllowed. Output
// must be a function of the input, the plan and an ids.Source.
func TestDeterminismSources(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == ".claude" || n == ".worktrees" || n == "testdata" || n == "corpus" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || determinismExempt(rel) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		timeName := ""
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if nondeterministicImports[p] {
				t.Errorf("%s imports %s: take an ids.Source instead", rel, p)
			}
			if p == "time" {
				timeName = "time"
				if im.Name != nil {
					timeName = im.Name.Name
				}
			}
		}
		if timeName == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Now" {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == timeName && x.Obj == nil {
					t.Errorf("%s:%d: time.Now: take the time from an ids.Source", rel, fset.Position(sel.Pos()).Line)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func determinismExempt(rel string) bool {
	for p := range determinismAllowed {
		if rel == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(rel, p)) {
			return true
		}
	}
	return false
}
