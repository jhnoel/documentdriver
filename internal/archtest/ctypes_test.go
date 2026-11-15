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

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// ctypesAllowed lists where content type and relationship type strings may
// be spelled out: the table itself and the generator that fills it from
// Office-authored files. Directory entries end in "/".
var ctypesAllowed = map[string]string{
	"internal/ooxml/ctypes/":      "the table, generated from Office-authored files",
	"tools/ctypesgen/":            "the generator that builds the table",
	"internal/docx/model/mime.go": "docxdriver 0.5.2's asset-manifest media type labels (its output vocabulary, compared by the parity oracle); image/bmp and image/webp have no Office-authored source",
}

// typeContains are fragments only a content type or relationship type
// contains; typePrefixes are the media types the engine writes.
var (
	typeContains = []string{
		"application/vnd.openxmlformats",
		"application/vnd.ms-",
		"/relationships/", // every relationship type; the relationships namespaces end without the slash
	}
	typePrefixes = []string{"image/", "audio/", "video/"}
)

// isTypeLiteral reports whether s is, or embeds, a content type or
// relationship type: a fragment from typeContains, a media-type prefix, or
// a value of the ctypes table.
func isTypeLiteral(s string, table map[string]bool) bool {
	for _, c := range typeContains {
		if strings.Contains(s, c) {
			return true
		}
	}
	for _, p := range typePrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return table[s]
}

func ctypesTable() map[string]bool {
	m := map[string]bool{}
	for _, e := range ctypes.ContentTypes() {
		m[e.Value] = true
	}
	for _, e := range ctypes.RelTypes() {
		m[e.Value] = true
	}
	return m
}

// TestContentTypesFromTable fails when non-test Go code outside
// internal/ooxml/ctypes and its generator spells a content type or
// relationship type as a string literal. Types come from the table, which
// is generated from Office-authored files, never from memory. Test files
// are exempt: their fixtures are XML written as text, and they may name a
// type to check what the code wrote. Import paths such as image/png are
// not literals of this kind.
func TestContentTypesFromTable(t *testing.T) {
	root := moduleRoot(t)
	table := ctypesTable()
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
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || ctypesExempt(rel) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		imports := map[*ast.BasicLit]bool{}
		for _, im := range f.Imports {
			imports[im.Path] = true
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || imports[lit] {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && isTypeLiteral(s, table) {
				t.Errorf("%s:%d: content or relationship type spelled as a literal: use a constant from internal/ooxml/ctypes", rel, fset.Position(lit.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func ctypesExempt(rel string) bool {
	for p := range ctypesAllowed {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// TestContentTypeLiteralDetector proves the rule's detector flags what it
// should and leaves ordinary strings alone.
func TestContentTypeLiteralDetector(t *testing.T) {
	table := ctypesTable()
	for _, s := range []string{
		"application/vnd.openxmlformats-officedocument.presentationml.slide+xml",
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships/image",
		"http://purl.oclc.org/ooxml/officeDocument/relationships/slideLayout",
		`<Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"/>`,
		"image/svg+xml",
		ctypes.CTApplicationXml,
	} {
		if !isTypeLiteral(s, table) {
			t.Errorf("%q not detected", s)
		}
	}
	for _, s := range []string{
		"http://schemas.openxmlformats.org/officeDocument/2006/relationships", // a namespace, not a type
		"http://schemas.openxmlformats.org/presentationml/2006/main",
		"slideLayout", "png", "text/plain", "",
	} {
		if isTypeLiteral(s, table) {
			t.Errorf("%q wrongly detected", s)
		}
	}
}
