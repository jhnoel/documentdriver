package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bannedImports are the imports no file in the module may have. unsafe
// defeats the memory safety every parser of untrusted input relies on; "C"
// (cgo) and runtime/cgo pull in a C toolchain and make builds
// non-reproducible and non-portable (the wasip1 build, CGO_ENABLED=0).
var bannedImports = map[string]string{
	"unsafe":      "no unsafe anywhere",
	"C":           "no cgo anywhere",
	"runtime/cgo": "no cgo anywhere",
}

// TestNoUnsafeNoCgo fails on any import of unsafe or "C" in any .go file of
// the module, tests included. It reads every file regardless of build
// constraints (so a file for another OS or a build tag cannot hide one) and
// skips only dot and underscore directories, testdata and vendor, which the
// go tool ignores too.
func TestNoUnsafeNoCgo(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, v := range bannedImportsOf(t, fset, path) {
			t.Errorf("%s:%d imports %q: %s", rel, v.line, v.path, bannedImports[v.path])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type violation struct {
	path string
	line int
}

// bannedImportsOf parses only the import block of path.
func bannedImportsOf(t *testing.T, fset *token.FileSet, path string) []violation {
	t.Helper()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return nil
	}
	var out []violation
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if _, bad := bannedImports[p]; bad {
			out = append(out, violation{p, fset.Position(imp.Pos()).Line})
		}
	}
	return out
}

// TestBannedImportsDetected checks the rule itself: an unsafe file and a
// cgo test file must both be flagged.
func TestBannedImportsDetected(t *testing.T) {
	dir := t.TempDir()
	src := map[string]string{
		"a.go":      "package a\n\nimport \"unsafe\"\n\nvar _ unsafe.Pointer\n",
		"b_test.go": "package a\n\n/*\n#include <stdio.h>\n*/\nimport \"C\"\n",
	}
	fset := token.NewFileSet()
	var got []string
	for n, s := range src {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, v := range bannedImportsOf(t, fset, p) {
			got = append(got, v.path)
		}
	}
	if len(got) != 2 {
		t.Fatalf("flagged %v, want unsafe and C", got)
	}
}
