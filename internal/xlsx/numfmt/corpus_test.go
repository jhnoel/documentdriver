package numfmt

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// stylesResult is one workbook's number-format counts and examples.
type stylesResult struct {
	unreadable bool
	counts     map[Kind]int
	examples   map[Kind][]string
}

// TestCorpusNumFmts streams the custom number formats (the numFmt children
// of styles.xml's numFmts element) of every workbook under XLSX_CORPUS,
// classifies each formatCode, and logs the counts per Kind with the first
// ten examples.
func TestCorpusNumFmts(t *testing.T) {
	root := os.Getenv("XLSX_CORPUS")
	if root == "" {
		t.Skip("XLSX_CORPUS not set")
	}
	var paths []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".xlsx", ".xlsm", ".xltx", ".xltm":
			paths = append(paths, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		t.Fatalf("no workbooks under %s", root)
	}

	res := corpustest.Run(t, corpustest.Files(paths, root), func(_ *testing.T, f corpustest.File, r *stylesResult) {
		r.counts = map[Kind]int{}
		r.examples = map[Kind][]string{}
		pkg, err := opc.Open(f.Path)
		if err != nil {
			r.unreadable = true
			return
		}
		defer pkg.Close() //nolint:errcheck // read-only open; close just releases the file handle
		src, ok, err := stylesSource(pkg)
		if err != nil {
			r.unreadable = true
			return
		}
		if !ok {
			return // no styles part
		}
		v := &numFmtScan{}
		if err := tree.ScanString(src, v); err != nil {
			r.unreadable = true
			return
		}
		for _, code := range v.formats {
			k := Classify(code)
			r.counts[k]++
			if len(r.examples[k]) < 10 {
				r.examples[k] = append(r.examples[k], code)
			}
		}
	})

	if corpustest.Sharded() {
		return
	}
	total := map[Kind]int{}
	examples := map[Kind][]string{}
	ran, unreadable := 0, 0
	for _, r := range res {
		if !r.Ran {
			continue
		}
		if r.Value.unreadable {
			unreadable++
			continue
		}
		ran++
		for k, n := range r.Value.counts {
			total[k] += n
			for _, ex := range r.Value.examples[k] {
				if len(examples[k]) < 10 {
					examples[k] = append(examples[k], ex)
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "xlsx numFmts: %d workbooks, %d custom formats (%d unreadable)\n", ran, sum(total), unreadable)
	for _, k := range slices.Sorted(maps.Keys(total)) {
		fmt.Fprintf(&b, "%-9s %8d", k, total[k])
		if len(examples[k]) > 0 {
			fmt.Fprintf(&b, "  e.g. %s", strings.Join(examples[k], " | "))
		}
		b.WriteByte('\n')
	}
	t.Log("\n" + b.String())
}

// stylesSource returns the styles.xml part as text, and false when the
// package has none.
func stylesSource(pkg *opc.Package) (string, bool, error) {
	if !pkg.Has("/xl/styles.xml") {
		return "", false, nil
	}
	pt, err := pkg.Part("/xl/styles.xml")
	if err != nil {
		return "", false, err
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		return "", false, err
	}
	if !ok {
		b, err := pt.Bytes()
		if err != nil {
			return "", false, err
		}
		return string(b), true, nil
	}
	return s, true, nil
}

// numFmtScan is a tree.Visitor that collects the formatCode of every
// numFmt child of the numFmts element — the custom number formats — and
// not the numFmt a dxf (differential format) carries.
type numFmtScan struct {
	stack   []string
	formats []string
}

func (v *numFmtScan) Start(n *tree.Node) error {
	if n.Local() == "numFmt" && len(v.stack) > 0 && v.stack[len(v.stack)-1] == "numFmts" {
		if code, ok := n.Attr("formatCode"); ok {
			v.formats = append(v.formats, code)
		}
	}
	v.stack = append(v.stack, n.Local())
	return nil
}

func (v *numFmtScan) End(*tree.Node) error {
	if len(v.stack) > 0 {
		v.stack = v.stack[:len(v.stack)-1]
	}
	return nil
}

func (v *numFmtScan) Leaf(*tree.Node) error { return nil }

func sum(m map[Kind]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}
