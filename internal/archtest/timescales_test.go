package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestTimeScalesInScalingTests fails when a call of the laws
// time-scaling helper sits outside a Test...Scaling... function or
// inside one that calls t.Parallel: make scaling passes -run Scaling
// and runs packages one at a time, so a call elsewhere would either
// never give its verdict there or run it under parallel load (the
// failure that made time verdicts live only in make scaling). The
// package and function names are spelled in pieces below so this
// file's own text does not put archtest on make scaling's list.
func TestTimeScalesInScalingTests(t *testing.T) {
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
		if !strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if !callsTimeHelper(fn) {
				continue
			}
			name := fn.Name.Name
			if !strings.HasPrefix(name, "Test") || !strings.Contains(name, "Scaling") {
				t.Errorf("%s: %s calls the laws time-scaling helper but is not a Test...Scaling... function (make scaling -run would miss it)", rel, name)
			}
			if callsParallel(fn) {
				t.Errorf("%s: %s calls t.Parallel around the laws time-scaling helper (the verdict runs under load)", rel, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

const (
	lawsPkg    = "laws"
	timeHelper = "Time" + "Scales"
)

// callsTimeHelper reports whether fn calls the laws time-scaling helper.
func callsTimeHelper(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == lawsPkg && sel.Sel.Name == timeHelper {
				found = true
			}
		}
		return true
	})
	return found
}

// callsParallel reports whether fn calls t.Parallel.
func callsParallel(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "t" {
				found = true
			}
		}
		return true
	})
	return found
}
