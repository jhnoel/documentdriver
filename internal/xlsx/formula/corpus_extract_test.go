package formula

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Namespaces of the elements that hold formulas (transitional and strict
// SpreadsheetML, the Excel 2010 "xm" formula, and DrawingML charts).
const (
	nsSML       = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsSMLStrict = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	nsXM        = "http://schemas.microsoft.com/office/excel/2006/main"
	nsChart     = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	nsChartStr  = "http://purl.oclc.org/ooxml/drawingml/chart"
)

// corpusFormula is one formula found in a corpus workbook.
type corpusFormula struct {
	Part string
	// Where is the cell (worksheet formulas), the name (defined names) or
	// the element's position otherwise.
	Where string
	// Kind is cell, shared-child, data-table, name, cf, dv, x14, chart or
	// table.
	Kind string
	Text string
	// T, Ref and SI are the cell formula's attributes; CM says its cell
	// has cell metadata (a dynamic array).
	T, Ref, SI string
	CM         bool
}

// corpusFile is what one workbook holds.
type corpusFile struct {
	// Excel says docProps/app.xml names Microsoft Excel as the
	// application (not an "Excel Compatible" writer).
	Excel    bool
	Formulas []corpusFormula
	// Unreadable is set when the package does not open; ScanFaults
	// lists parts that do not tokenise (the formulas read before the
	// fault are kept).
	Unreadable string
	ScanFaults []string
}

// corpusFiles lists the workbooks under XLSX_CORPUS, skipping the test
// when it is unset.
func corpusFiles(t *testing.T) (string, []corpustest.File) {
	t.Helper()
	root := os.Getenv("XLSX_CORPUS")
	if root == "" {
		t.Skip("XLSX_CORPUS not set")
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".xlsx", ".xlsm", ".xltx", ".xltm":
			if d.Type().IsRegular() {
				paths = append(paths, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	slices.Sort(paths)
	if len(paths) == 0 {
		t.Fatalf("no workbooks under %s", root)
	}
	return root, corpustest.Files(paths, root)
}

// readCorpusFile extracts every formula of one workbook.
func readCorpusFile(path string) (out corpusFile) {
	pkg, err := opc.Open(path)
	if err != nil {
		return corpusFile{Unreadable: err.Error()}
	}
	defer func() {
		if err := pkg.Close(); err != nil && out.Unreadable == "" {
			out.Unreadable = "close: " + err.Error()
		}
	}()
	for _, name := range pkg.Names() {
		lower := strings.ToLower(name)
		if lower == "/docprops/app.xml" {
			out.Excel = isExcelApp(pkg, name)
			continue
		}
		if !strings.HasPrefix(lower, "/xl/") || !strings.HasSuffix(lower, ".xml") {
			continue
		}
		pt, err := pkg.Part(name)
		if err != nil {
			continue
		}
		b, err := pt.Bytes()
		if err != nil {
			continue
		}
		v := &extractor{part: name}
		if err := tree.Scan(b, v); err != nil {
			out.ScanFaults = append(out.ScanFaults, name+": "+err.Error())
		}
		out.Formulas = append(out.Formulas, v.out...)
	}
	return out
}

func isExcelApp(pkg *opc.Package, name string) bool {
	pt, err := pkg.Part(name)
	if err != nil {
		return false
	}
	b, err := pt.Bytes()
	if err != nil {
		return false
	}
	s := string(b)
	_, after, ok := strings.Cut(s, "<Application>")
	if !ok {
		return false
	}
	app := after
	if j := strings.IndexByte(app, '<'); j >= 0 {
		app = app[:j]
	}
	return strings.HasPrefix(app, "Microsoft") && strings.Contains(app, "Excel") && !strings.Contains(app, "Compatible")
}

// extractor is a tree.Visitor collecting formula texts.
type extractor struct {
	part  string
	stack []string // local names of open elements
	// ns holds the namespace declarations in scope, innermost last, and
	// nsDepth how many each open element pushed (Scan keeps no parents,
	// so Node.Space cannot resolve a prefix).
	ns      []nsDecl
	nsDepth []int
	cell    string
	cm      bool
	in      *corpusFormula
	text    strings.Builder
	n       int
	out     []corpusFormula
}

type nsDecl struct{ prefix, uri string }

func (v *extractor) space(prefix string) string {
	for _, v0 := range slices.Backward(v.ns) {
		if v0.prefix == prefix {
			return v0.uri
		}
	}
	return ""
}

func isSML(space string) bool { return space == nsSML || space == nsSMLStrict }

func (v *extractor) parent() string {
	if len(v.stack) < 2 {
		return ""
	}
	return v.stack[len(v.stack)-2]
}

func (v *extractor) Start(n *tree.Node) error {
	pushed := 0
	for k, val := range n.AttrsSeq() {
		switch {
		case k == "xmlns":
			v.ns = append(v.ns, nsDecl{"", val})
			pushed++
		case strings.HasPrefix(k, "xmlns:"):
			v.ns = append(v.ns, nsDecl{k[len("xmlns:"):], val})
			pushed++
		}
	}
	v.nsDepth = append(v.nsDepth, pushed)
	sp, local := v.space(n.Prefix()), n.Local()
	v.stack = append(v.stack, local)
	if isSML(sp) && local == "c" {
		v.cell, _ = n.Attr("r")
		_, v.cm = n.Attr("cm")
	}
	kind := ""
	switch {
	case isSML(sp) && local == "f" && v.parent() == "c":
		kind = "cell"
	case isSML(sp) && local == "definedName":
		kind = "name"
	case isSML(sp) && local == "formula" && v.parent() == "cfRule":
		kind = "cf"
	case isSML(sp) && (local == "formula1" || local == "formula2"):
		kind = "dv"
	case isSML(sp) && (local == "calculatedColumnFormula" || local == "totalsRowFormula"):
		kind = "table"
	case sp == nsXM && local == "f":
		kind = "x14"
	case (sp == nsChart || sp == nsChartStr) && local == "f":
		kind = "chart"
	}
	if kind == "" {
		return nil
	}
	v.n++
	f := corpusFormula{Part: v.part, Kind: kind}
	switch kind {
	case "cell":
		f.Where, f.CM = v.cell, v.cm
		f.T, _ = n.Attr("t")
		f.Ref, _ = n.Attr("ref")
		f.SI, _ = n.Attr("si")
	case "name":
		f.Where, _ = n.Attr("name")
		if s, ok := n.Attr("localSheetId"); ok {
			f.Where += "@" + s
		}
		if r, ok := n.Attr("refersTo"); ok {
			// An externalLink's cached name: the formula is an attribute.
			f.Kind, f.Text = "external-name", r
			v.out = append(v.out, f)
			return nil
		}
	default:
		f.Where = local + "#" + strconv.Itoa(v.n)
	}
	v.in = &f
	v.text.Reset()
	return nil
}

func (v *extractor) Leaf(n *tree.Node) error {
	if v.in != nil && (n.Kind() == tree.TextNode || n.Kind() == tree.CDataNode) {
		v.text.WriteString(n.Data())
	}
	return nil
}

func (v *extractor) End(*tree.Node) error {
	if v.in != nil && len(v.stack) > 0 {
		f := *v.in
		f.Text = v.text.String()
		switch {
		case f.Kind == "cell" && f.T == "shared" && f.Text == "":
			f.Kind = "shared-child"
		case f.Kind == "cell" && f.T == "dataTable":
			f.Kind = "data-table"
		case f.Text == "":
			// No formula text: a member of a volatile array range
			// (<f ca="1"/>), or an empty optional element.
			f.Kind = "empty " + f.Kind
		}
		v.out = append(v.out, f)
		v.in = nil
	}
	v.stack = v.stack[:len(v.stack)-1]
	v.ns = v.ns[:len(v.ns)-v.nsDepth[len(v.nsDepth)-1]]
	v.nsDepth = v.nsDepth[:len(v.nsDepth)-1]
	return nil
}
