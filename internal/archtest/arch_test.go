// Package archtest checks the module's architecture rules for every format:
// which code may write to disk, which may mutate XML trees, which may see
// the read model, which may import a format's packages, and that the
// shared engine depends on no format. It type-checks
// every package of the module from source, so the rules follow method
// identity, not names.
package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/jhnoel/documentdriver"

const (
	pkgTree     = module + "/internal/ooxml/tree"
	pkgOPC      = module + "/internal/opc"
	pkgPlan     = module + "/internal/plan"
	pkgOps      = module + "/internal/pptx/ops"
	pkgCore     = module + "/internal/ooxml/edit"
	pkgEdit     = module + "/internal/pptx/ops/edit"
	pkgModel    = module + "/internal/pptx/model"
	pkgRender   = module + "/internal/pptx/render"
	pkgOracle   = module + "/eval/oracle"
	pkgLaws     = module + "/internal/laws"
	pkgCorpus   = module + "/internal/corpustest"
	pkgDocxEdit = module + "/internal/docx/edit"
	pkgXlsxEdit = module + "/internal/xlsx/edit"
	pkgXMLScan  = module + "/internal/ooxml/xmlscan"
)

// formats are the per-format trees under internal/. Nothing outside
// internal/<format> imports it except that format's CLI (cmd/<format>) and
// the dev-only harnesses under eval/ and tools/: the shared engine
// (internal/opc, internal/ooxml, ...) never depends on a format, and one
// format never depends on the other.
var formats = []string{"pptx", "docx", "xlsx"}

// importsFormat reports the format whose packages path belongs to, or "".
func importsFormat(path string) string {
	for _, f := range formats {
		root := module + "/internal/" + f
		if path == root || strings.HasPrefix(path, root+"/") {
			return f
		}
	}
	return ""
}

// mayImportFormat reports whether package pkg may import format f.
func mayImportFormat(pkg, f string) bool {
	for _, prefix := range []string{"/internal/" + f, "/cmd/" + f, "/eval", "/tools"} {
		root := module + prefix
		if pkg == root || strings.HasPrefix(pkg, root+"/") {
			return true
		}
	}
	return false
}

// treeMutators are the *tree.Node methods that change a document.
var treeMutators = set("SetAttr", "SetAttrNS", "RemoveAttr", "InsertAt", "AppendChild",
	"InsertBefore", "InsertAfter", "RemoveChild", "Remove", "ReplaceChild", "InsertFragment",
	"ReplaceWithFragment", "SetText", "Rename")

// treeInserters are the mutators that add elements. In the edit core
// they are used only in placeFiles: Handle.Ensure, which places children
// by the schema's order, the placed inserts beside it (AppendEach,
// AppendExt, ReplaceChildren, ReplaceRun), which say which occurrence of
// a repeated slot is meant, and unit placement (Insert and Move at a
// Position from SlotEnd or next to an existing sibling), and the
// structural verbs of structure.go. No other code inserts.
var treeInserters = set("InsertAt", "AppendChild", "InsertBefore", "InsertAfter", "ReplaceChild",
	"InsertFragment", "ReplaceWithFragment")

// placeFiles are the edit core's files that may insert elements.
var placeFiles = set("internal/ooxml/edit/place.go", "internal/ooxml/edit/unit.go", "internal/ooxml/edit/structure.go")

// sharedEngine are the shared engine's package trees. Neither they nor
// anything they import depends on a format (internal/pptx,
// internal/docx).
var sharedEngine = []string{"internal/opc", "internal/ooxml", "internal/plan", "internal/ids", "internal/htmlx", "internal/cli", "internal/laws", "internal/ascii", "internal/geom", "internal/casefold"}

// savers are the opc methods that write a package to disk. Only plan
// transactions call them (Commit, CommitTo, Snapshot), after the
// write-set verifier.
var savers = map[string]map[string]bool{
	"Package": set("Save", "SaveAs", "SaveAndClose"),
}

// partWriters are the opc methods that add, delete or replace a part
// wholesale, bypassing the tree. Operations call them inside a plan
// transaction, which holds them to the declared write set.
var partWriters = map[string]map[string]bool{
	"Package": set("AddPart", "DeletePart"),
	"Part":    set("SetBytes"),
}

// patchers are the opc methods that make a part's content a splice
// patch (internal/ooxml/splice), the one way besides the edit core to
// change XML: whole-node spans of a large part's original string
// (a worksheet's sheetData, shared strings), never parsed into a tree.
var patchers = map[string]map[string]bool{
	"Part": set("SetPatch"),
}

// xlsxTreeMakers are the calls that parse XML into a tree. Under
// internal/xlsx a worksheet's sheetData and the shared strings are
// never parsed into a tree (docs/design.md, xlsx: Engine rule); they are
// read with tree.Scan and written as splices. The calls cannot name the
// part statically, so only the files of xlsxTreeFiles, which parse the
// small parts (workbook, styles, tables, drawings), may make them.
var xlsxTreeMakers = map[string]map[string]bool{
	"Part": set("Tree"),
	"":     set("Parse", "ParseString", "ParseFragment"),
}

// The rules' exceptions. Each names the files it covers and why.
var (
	// Packages whose own code may mutate trees: the tree itself, the
	// package layer (content types, relationships) and the edit core,
	// whose Handle and unit placement are the only mutators a format's
	// edit layer and operations use. The format edit layers
	// (internal/pptx/ops/edit) mutate only through the core.
	//
	// eval/oracle is the one eval package with this exception: it writes
	// the reference solutions by editing slide XML directly through opc and
	// tree, by design independent of internal/pptx/ops, because the references
	// are what the M3 operations will be graded against. No other eval
	// package may mutate trees or write packages (eval/grade only reads).
	mutatorPkgs = set(pkgTree, pkgOPC, pkgCore, pkgOracle)
	// Test files that mutate trees directly. tree, opc and edit core tests
	// test those packages' own mutation; plan, pptx edit and ops tests
	// make declared and undeclared changes to prove the commit-time
	// verifier holds them to the write set; render_test builds a
	// two-slide fixture deck; xmlscan's tests hold EditTag to
	// tree.Node.SetAttr and RemoveAttr on throwaway trees.
	mutatorTests = set(pkgTree, pkgOPC, pkgCore, pkgEdit, pkgPlan, pkgOps, pkgRender, pkgOracle, pkgXMLScan)
	// Packages whose own code may save a package: opc defines the
	// writes, plan transactions commit them.
	saverPkgs = set(pkgOPC, pkgPlan, pkgOracle) // oracle: see mutatorPkgs
	// Test files that save packages directly: opc and plan tests test the
	// save path; render_test saves its fixture deck.
	saverTests = set(pkgOPC, pkgPlan, pkgRender, pkgOracle)
	// Packages whose own code may add, delete or replace parts: opc
	// defines them, pptx operations (pictures and media) and the docx
	// edit layer (comment and header parts) and the xlsx edit layer
	// (sheet.add's worksheet, the calcChain the recalculation policy
	// removes) call them inside a plan transaction.
	partWriterPkgs = set(pkgOPC, pkgOps, pkgOracle, pkgDocxEdit, pkgXlsxEdit)
	// Test files that write parts directly: opc tests, ops tests whose
	// deliberately misbehaving operations write undeclared parts, and
	// render_test, which builds its fixture deck.
	partWriterTests = set(pkgOPC, pkgOps, pkgRender, pkgOracle)
	// Packages whose own code may set a splice patch on a part: opc
	// defines it, and the xlsx edit layer's Sheet flushes its overlay
	// into one (internal/xlsx/edit/sheet.go), declared spliced.
	patcherPkgs = set(pkgOPC, pkgXlsxEdit)
	// Test files that set patches: opc tests the streamed write, plan
	// tests DeclareSpliced against patched parts, the xlsx edit layer's
	// tests flush overlays.
	patcherTests = set(pkgOPC, pkgPlan, pkgXlsxEdit)
	// xlsxTreeFiles are the non-test files under internal/xlsx that may
	// parse a part into a tree, each only for small parts: integrity's
	// checker.tree (workbook, styles, tables, relationships), and read's
	// loaders of the workbook, styles, xl/metadata.xml and table parts,
	// and the xlsx edit layer's small parts (workbook, styles, tables).
	xlsxTreeFiles = set("internal/xlsx/integrity/check.go", "internal/xlsx/edit/smallparts.go",
		"internal/xlsx/read/book.go", "internal/xlsx/read/styles.go",
		"internal/xlsx/read/metadata.go", "internal/xlsx/read/tables.go")
)

func set(s ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range s {
		m[x] = true
	}
	return m
}

// listed is the part of `go list -json` output used here.
type listed struct {
	Dir          string
	ImportPath   string
	Export       string
	Standard     bool
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string

	ForTest string
}

func goList(t *testing.T, root string, args ...string) []listed {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list", "-json"}, args...)...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	var pkgs []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// checked is one type-checked set of files.
type checked struct {
	pkg   string // import path of the package under test
	test  bool   // the files are _test.go files
	files []*ast.File
	names []string // file names relative to the module root
	info  *types.Info
}

func TestArchitecture(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole module")
	}
	root := moduleRoot(t)
	// Export data for every dependency, test dependencies included.
	exports := map[string]string{}
	for _, p := range goList(t, root, "-deps", "-test", "-export", "./...") {
		if p.Export != "" && p.ForTest == "" {
			exports[p.ImportPath] = p.Export
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok {
			return nil, errors.New("no export data for " + path)
		}
		return os.Open(f)
	})
	mods := goList(t, root, "./...")
	var units []checked
	for _, p := range mods {
		parse := func(names []string) ([]*ast.File, []string) {
			var fs []*ast.File
			var rel []string
			for _, n := range names {
				full := filepath.Join(p.Dir, n)
				f, err := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
				if err != nil {
					t.Fatal(err)
				}
				r, _ := filepath.Rel(root, full)
				fs = append(fs, f)
				rel = append(rel, filepath.ToSlash(r))
			}
			return fs, rel
		}
		check := func(path string, files []*ast.File) *types.Info {
			info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
			conf := types.Config{Importer: imp, Error: func(error) {}}
			if _, err := conf.Check(path, fset, files, info); err != nil {
				t.Errorf("type-checking %s: %v", path, err)
			}
			return info
		}
		gf, gn := parse(append(append([]string{}, p.GoFiles...), p.CgoFiles...))
		tf, tn := parse(p.TestGoFiles)
		// Non-test files on their own, then with the in-package tests
		// (whose findings are attributed to the test files only).
		units = append(units, checked{pkg: p.ImportPath, files: gf, names: gn, info: check(p.ImportPath, gf)})
		if len(tf) > 0 {
			all := append(append([]*ast.File{}, gf...), tf...)
			units = append(units, checked{pkg: p.ImportPath, test: true, files: tf, names: tn, info: check(p.ImportPath, all)})
		}
		if len(p.XTestGoFiles) > 0 {
			xf, xn := parse(p.XTestGoFiles)
			units = append(units, checked{pkg: p.ImportPath, test: true, files: xf, names: xn, info: check(p.ImportPath+"_test", xf)})
		}
	}

	var problems []string
	report := func(file string, pos token.Pos, msg string) {
		p := fset.Position(pos)
		problems = append(problems, file+":"+strconv.Itoa(p.Line)+": "+msg)
	}
	for _, u := range units {
		for i, f := range u.files {
			name := u.names[i]
			for _, is := range f.Imports {
				path := strings.Trim(is.Path.Value, `"`)
				if path == "encoding/xml" && !strings.HasPrefix(name, "eval/") {
					report(name, is.Pos(), "imports encoding/xml; OOXML goes through internal/ooxml/tree")
				}
				if path == pkgLaws && !u.test {
					report(name, is.Pos(), "imports internal/laws, the law harness, outside a test")
				}
				if path == pkgCorpus && !u.test {
					report(name, is.Pos(), "imports internal/corpustest, the corpus runner, outside a test")
				}
				if fm := importsFormat(path); fm != "" && !mayImportFormat(u.pkg, fm) {
					report(name, is.Pos(), "imports "+path+"; only internal/"+fm+", cmd/"+fm+", eval and tools may import internal/"+fm)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				fn, ok := u.info.Uses[sel.Sel].(*types.Func)
				if !ok || fn.Pkg() == nil {
					return true
				}
				recv := ""
				if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
					rt := sig.Recv().Type()
					if p, ok := rt.(*types.Pointer); ok {
						rt = p.Elem()
					}
					if nt, ok := rt.(*types.Named); ok {
						recv = nt.Obj().Name()
					}
				}
				if strings.HasPrefix(name, "internal/xlsx/") && !u.test && !xlsxTreeFiles[name] {
					treeMaker := (fn.Pkg().Path() == pkgOPC && xlsxTreeMakers[recv][fn.Name()] && recv != "") ||
						(fn.Pkg().Path() == pkgTree && recv == "" && xlsxTreeMakers[""][fn.Name()])
					if treeMaker {
						report(name, sel.Sel.Pos(), "parses a part into a tree ("+fn.Name()+") under internal/xlsx outside xlsxTreeFiles; worksheets and shared strings are scanned (tree.Scan) and spliced")
					}
				}
				switch fn.Pkg().Path() {
				case pkgTree:
					if recv == "Node" && treeMutators[fn.Name()] && !allowed(u, mutatorPkgs, mutatorTests) {
						report(name, sel.Sel.Pos(), "calls tree mutator (*Node)."+fn.Name()+" outside opc and the edit core (internal/ooxml/edit)")
					}
					if recv == "Node" && treeInserters[fn.Name()] && u.pkg == pkgCore && !u.test && !placeFiles[name] {
						report(name, sel.Sel.Pos(), "inserts with (*Node)."+fn.Name()+" outside the edit core's place.go and unit.go (Ensure, the placed inserts and unit placement)")
					}
				case pkgOPC:
					switch {
					case recv == "" && (fn.Name() == "SetFaultHook" || fn.Name() == "SetFaultHookFor"):
						// internal/laws is the test-only law harness
						// (imported only by tests, below).
						if !u.test && u.pkg != pkgLaws {
							report(name, sel.Sel.Pos(), "calls opc.SetFaultHook outside tests")
						}
					case savers[recv][fn.Name()]:
						if !allowed(u, saverPkgs, saverTests) {
							report(name, sel.Sel.Pos(), "calls opc "+recv+"."+fn.Name()+" outside internal/plan; writes go through plan transactions")
						}
					case patchers[recv][fn.Name()]:
						if !allowed(u, patcherPkgs, patcherTests) {
							report(name, sel.Sel.Pos(), "calls opc "+recv+"."+fn.Name()+" outside opc; splice patches are set only by the xlsx edit layer, inside plan transactions")
						}
					case partWriters[recv][fn.Name()]:
						if !allowed(u, partWriterPkgs, partWriterTests) {
							report(name, sel.Sel.Pos(), "calls opc "+recv+"."+fn.Name()+" outside internal/pptx/ops; parts change only inside plan transactions")
						}
					}
				}
				return true
			})
		}
	}

	// Write code (internal/pptx/ops and everything it imports) never sees the
	// read model, so the model cannot be serialised back.
	for _, p := range goList(t, root, "-deps", module+"/internal/pptx/ops/...") {
		if p.ImportPath == pkgModel {
			problems = append(problems, "internal/pptx/ops depends on internal/pptx/model: the read model is never serialised back")
		}
	}

	// The shared engine depends on no format, not even indirectly.
	var engine []string
	for _, e := range sharedEngine {
		engine = append(engine, "./"+e+"/...")
	}
	for _, p := range goList(t, root, append([]string{"-deps"}, engine...)...) {
		if f := importsFormat(p.ImportPath); f != "" {
			problems = append(problems, "the shared engine ("+strings.Join(sharedEngine, ", ")+") depends on "+p.ImportPath+"; it never depends on a format")
		}
	}

	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

func allowed(u checked, pkgs, tests map[string]bool) bool {
	if u.test {
		return tests[u.pkg]
	}
	return pkgs[u.pkg]
}
