package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestEveryFunctionDocumented fails when a function or method in a
// tracked, non-test, non-generated Go file has no doc comment. Exported
// or not: a helper's doc comment is what the next agent searches for
// (the semantic function search embeds signature plus doc comment), and a
// helper nobody can find gets written again.
//
// docExemptDirs lists the package directories that still had undocumented
// functions when the rule landed (2026-11-15). The list only shrinks: a
// directory listed here that is now fully documented fails the test, so
// its line must be deleted. A directory not listed must stay fully
// documented. Never add a directory.
func TestEveryFunctionDocumented(t *testing.T) {
	root := moduleRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	missing := map[string][]string{}
	for rel := range strings.SplitSeq(string(out), "\x00") {
		if rel == "" || strings.HasSuffix(rel, "_test.go") || strings.Contains("/"+rel, "/testdata/") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, rel), nil, parser.ParseComments)
		if err != nil {
			continue // deleted in the working tree; a build error is go vet's to report
		}
		if ast.IsGenerated(f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || (fd.Doc != nil && strings.TrimSpace(fd.Doc.Text()) != "") {
				continue
			}
			dir := path.Dir(rel)
			missing[dir] = append(missing[dir], fmt.Sprintf("%s: %s", rel, funcName(fd)))
		}
	}
	dirs := make([]string, 0, len(missing))
	for d := range missing {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if slices.Contains(docExemptDirs, d) {
			continue
		}
		for _, m := range missing[d] {
			t.Errorf("%s has no doc comment: add one (a sentence starting with its name)", m)
		}
	}
	for _, d := range docExemptDirs {
		if len(missing[d]) == 0 {
			t.Errorf("docExemptDirs lists %s, which is now fully documented: delete its line (the list only shrinks)", d)
		}
	}
}

// funcName is a function's name, with its receiver type for a method.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	if ix, ok := t.(*ast.IndexListExpr); ok {
		t = ix.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// docExemptDirs are the package directories not yet fully documented,
// frozen when TestEveryFunctionDocumented landed; delete a line once its
// directory is documented, never add one.
var docExemptDirs = []string{
	"internal/ooxml/tree/internal/treeoracle",
}
