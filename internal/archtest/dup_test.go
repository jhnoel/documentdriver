package archtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// dupScan is the type-checked, non-test view of the module shared by
// the E1 rules (like TestArchitecture's units, built once).
type dupScan struct {
	root  string
	fset  *token.FileSet
	units []checked
}

// dupViolation is one rule hit, with its file (module-relative, slash
// separated) and line.
type dupViolation struct {
	file string
	line int
	msg  string
}

var (
	dupOnce  sync.Once
	dupBuilt *dupScan
)

// dupUnits returns the shared type-checked units, building them once.
func dupUnits(t *testing.T) *dupScan {
	t.Helper()
	dupOnce.Do(func() {
		root := moduleRoot(t)
		dupBuilt = dupBuildUnits(t, root)
	})
	return dupBuilt
}

// dupBuildUnits type-checks every package's non-test files from
// source, as TestArchitecture does.
func dupBuildUnits(t *testing.T, root string) *dupScan {
	t.Helper()
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
	var units []checked
	for _, p := range goList(t, root, "./...") {
		names := append(append([]string{}, p.GoFiles...), p.CgoFiles...)
		if len(names) == 0 {
			continue
		}
		var files []*ast.File
		var rel []string
		for _, n := range names {
			full := filepath.Join(p.Dir, n)
			f, err := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			r, _ := filepath.Rel(root, full)
			files = append(files, f)
			rel = append(rel, filepath.ToSlash(r))
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			t.Errorf("type-checking %s: %v", p.ImportPath, err)
		}
		units = append(units, checked{pkg: p.ImportPath, files: files, names: rel, info: info})
	}
	return &dupScan{root: root, fset: fset, units: units}
}

// dupExempt reports whether rel is allowed by allow (directory
// entries end in "/").
func dupExempt(rel string, allow map[string]string) bool {
	for p := range allow {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, p) {
				return true
			}
		} else if rel == p {
			return true
		}
	}
	return false
}

// dupGrandfatherKey returns the grandfather key covering rel, if any.
func dupGrandfatherKey(rel string, grandfather map[string]string) (string, bool) {
	keys := make([]string, 0, len(grandfather))
	for k := range grandfather {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, p) {
				return p, true
			}
		} else if rel == p {
			return p, true
		}
	}
	return "", false
}

// dupCheckViolations reports violations not allowed or grandfathered,
// and fails when a grandfathered entry no longer violates (the lists
// only shrink).
func dupCheckViolations(t *testing.T, rule string, violations []dupViolation, allow, grandfather map[string]string) {
	t.Helper()
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		if violations[i].line != violations[j].line {
			return violations[i].line < violations[j].line
		}
		return violations[i].msg < violations[j].msg
	})
	used := map[string]bool{}
	seen := map[dupViolation]bool{}
	for _, v := range violations {
		if dupExempt(v.file, allow) || seen[v] {
			continue
		}
		seen[v] = true
		if key, ok := dupGrandfatherKey(v.file, grandfather); ok {
			used[key] = true
			continue
		}
		t.Errorf("%s: %s:%d: %s", rule, v.file, v.line, v.msg)
	}
	keys := make([]string, 0, len(grandfather))
	for k := range grandfather {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !used[k] {
			t.Errorf("%s: grandfathered entry %q (%s) no longer violates; delete it", rule, k, grandfather[k])
		}
	}
}

// dupLine returns the line of pos in fset.
func dupLine(fset *token.FileSet, pos token.Pos) int {
	return fset.Position(pos).Line
}

// dupCall reports whether the call node n calls pkgPath.Name, by
// type info.
func dupCall(u checked, n ast.Node, pkgPath, name string) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := u.info.Uses[sel.Sel].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == pkgPath && fn.Name() == name
}

// xmlLexerAllowed lists where XML tokens, entities and escapers may
// be spelled: xmlscan itself, the validating tokenizer and its
// frozen oracle, the splice Writer (which emits markup), HTML
// (another grammar), and the formula grammar ('"' strings, '\”
// sheet names, '<'/'>' operators). Directory entries end in "/".
var xmlLexerAllowed = map[string]string{
	"internal/ooxml/xmlscan/":                  "the one XML lexer and codec",
	"internal/ooxml/tree/":                     "the validating tokenizer itself",
	"internal/ooxml/tree/internal/treeoracle/": "the frozen oracle",
	"internal/ooxml/splice/writer.go":          "the splice Writer emits markup",
	"internal/htmlx/":                          "HTML, another grammar",
	"internal/xlsx/formula/":                   "formula grammar: '\"' strings, '\\'' sheet names, '<'/'>' operators",
}

// xmlLexerGrandfathered lists files that violate TestOneXMLLexer
// and pre-date xmlscan (frozen from E1's first run on the post-M7
// tree; entries only shrink).
var xmlLexerGrandfathered = map[string]string{
	"internal/xlsx/shift/sheetdata.go":      "sdEscape keeps '>' (Q1 follow-up F1); pre-dates xmlscan",
	"internal/ooxml/edit/place.go":          "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/testdeck/quirks.go":           "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/html/types.go":           "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/html/input.go":           "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/math/xml.go":             "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/math/write_omml.go":      "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/view/render.go":          "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/view/surface.go":         "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/edit/comments.go":        "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/docx/integrity/xmlerr.go":     "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/pptx/ops/prop/path.go":        "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/pptx/ops/text/body.go":        "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/pptx/ops/text/build.go":       "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/pptx/view/svg.go":             "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/ooxml/splice/check.go":        "pre-dates xmlscan; docx/pptx consolidation pass",
	"internal/xlsx/edit/cell.go":            "builds the closing tag of a new element; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/edit/sheet.go":           "builds closing tags of new elements; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/edit/stylekernel.go":     "builds the closing tag of a new element; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/ops/colskernel.go":       "builds the closing tag of a new <cols>; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/ops/mergekernel.go":      "builds closing tags of new elements; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/ops/run_sheet_rename.go": "builds the closing tag of the renamed root; pre-dates xmlscan (no shared writer)",
	"internal/xlsx/xlsxtest/xlsxtest.go":    "test support builds markup; its namespace constants are the M9b justified keep",
}

// TestOneXMLLexer fails on a second XML lexer or codec: a token or
// entity literal, a strings.NewReplacer escaper with a "&"
// argument, an xlsx token delimiter literal, or an xlsx
// quote-tracking scan. Use internal/ooxml/xmlscan instead.
func TestOneXMLLexer(t *testing.T) {
	scan := dupUnits(t)
	exact := map[string]bool{"<!--": true, "-->": true, "<![CDATA[": true, "]]>": true}
	contains := []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;", "&#"}
	xlsxExact := map[string]bool{"</": true, "<!": true, "<?": true, "?>": true}
	var violations []dupViolation
	add := func(file string, pos token.Pos, msg string) {
		violations = append(violations, dupViolation{file, dupLine(scan.fset, pos), msg})
	}
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if strings.HasPrefix(rel, "eval/") || strings.HasPrefix(rel, "tools/") || dupExempt(rel, xmlLexerAllowed) {
				continue
			}
			imports := map[*ast.BasicLit]bool{}
			for _, im := range f.Imports {
				imports[im.Path] = true
			}
			isXlsx := strings.HasPrefix(rel, "internal/xlsx/") || strings.HasPrefix(rel, "cmd/xlsx/")
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
					if exact[v] {
						add(rel, x.Pos(), "XML token literal "+strconv.Quote(v)+": use internal/ooxml/xmlscan")
					} else {
						for _, c := range contains {
							if strings.Contains(v, c) {
								add(rel, x.Pos(), "string literal containing "+strconv.Quote(c)+": use internal/ooxml/xmlscan")
								break
							}
						}
					}
					if isXlsx && xlsxExact[v] {
						add(rel, x.Pos(), "tag delimiter literal "+strconv.Quote(v)+": use internal/ooxml/xmlscan")
					}
				case *ast.CallExpr:
					if dupCall(u, x, "strings", "NewReplacer") {
						for _, arg := range x.Args {
							if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if av, err := strconv.Unquote(lit.Value); err == nil && av == "&" {
									add(rel, x.Pos(), "strings.NewReplacer escaper with a \"&\" argument: use internal/ooxml/xmlscan")
								}
							}
						}
					}
				case *ast.FuncDecl:
					if isXlsx && x.Body != nil && dupQuoteTracking(x.Body) {
						add(rel, x.Pos(), "quote-tracking scan (both quote runes and '<' or '>'): use internal/ooxml/xmlscan")
					}
				case *ast.FuncLit:
					if isXlsx && x.Body != nil && dupQuoteTracking(x.Body) {
						add(rel, x.Pos(), "quote-tracking scan (both quote runes and '<' or '>'): use internal/ooxml/xmlscan")
					}
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestOneXMLLexer", violations, xmlLexerAllowed, xmlLexerGrandfathered)
}

// dupQuoteTracking reports whether body holds rune literals for
// both '\” and '"' and for '<' or '>'.
func dupQuoteTracking(body *ast.BlockStmt) bool {
	var single, dbl, lt, gt bool
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.CHAR {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil || len([]rune(v)) != 1 {
			return true
		}
		switch []rune(v)[0] {
		case '\'':
			single = true
		case '"':
			dbl = true
		case '<':
			lt = true
		case '>':
			gt = true
		}
		return true
	})
	return single && dbl && (lt || gt)
}

// jsonAllowed lists where encoding/json may be called: the cli
// package (cli.Marshal, cli.WriteJSON) and CLI glue under cmd/.
// Directory entries end in "/".
var jsonAllowed = map[string]string{
	"internal/cli/": "cli.Marshal and cli.WriteJSON live here",
	"cmd/":          "CLI glue",
}

// jsonGrandfathered lists files that call encoding/json.Marshal,
// MarshalIndent or NewEncoder and pre-date the consolidation
// (frozen from E1's first run on the post-M7 tree; entries only
// shrink).
var jsonGrandfathered = map[string]string{
	"internal/plan/result.go":            "pre-dates the consolidation; JSON through cli (E1-b)",
	"internal/pptx/apply/apply.go":       "pre-dates the consolidation; JSON through cli (E1-b)",
	"internal/pptx/show/show.go":         "pre-dates the consolidation; JSON through cli (E1-b)",
	"internal/pptx/view/json.go":         "pre-dates the consolidation; JSON through cli (E1-b)",
	"internal/pptx/coverage/universe.go": "pre-dates the consolidation; JSON through cli (E1-b)",
	"internal/testdeck/plan.go":          "pre-dates the consolidation; JSON through cli (E1-b)",
}

// TestJSONThroughCLI fails on a call of encoding/json.Marshal,
// MarshalIndent or NewEncoder (by type info) outside
// internal/cli and cmd/. Use cli.Marshal or cli.WriteJSON.
func TestJSONThroughCLI(t *testing.T) {
	scan := dupUnits(t)
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if strings.HasPrefix(rel, "eval/") || strings.HasPrefix(rel, "tools/") || dupExempt(rel, jsonAllowed) {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				for _, name := range []string{"Marshal", "MarshalIndent", "NewEncoder"} {
					if dupCall(u, n, "encoding/json", name) {
						violations = append(violations, dupViolation{rel, dupLine(scan.fset, n.Pos()), "calls encoding/json." + name + ": use cli.Marshal or cli.WriteJSON"})
					}
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestJSONThroughCLI", violations, jsonAllowed, jsonGrandfathered)
}

// cloneAllowed lists directories whose functions may be exact
// clones for TestNoExactClones. Directory entries end in "/".
var cloneAllowed = map[string]string{
	"internal/ooxml/tree/internal/treeoracle/": "the frozen oracle",
}

// clonePairAllowed lists exact-clone pairs that may stay, as
// "fileA:FuncA=fileB:FuncB" (in that sorted order) to a reason
// (frozen from E1's first run on the post-M7 tree; entries only
// shrink).
var clonePairAllowed = map[string]string{ //nolint:gosec // G101: pair keys name tokenizer.go files, no credential
	"cmd/pptx/main.go:main=cmd/xlsx/main.go:main":                                                        "per-CLI main glue keep; docx/pptx consolidation pass",
	"internal/docx/html/input.go:set=internal/pptx/ops/text/html.go:set":                                 "docx/pptx side left by this pass; docx/pptx consolidation pass",
	"internal/docx/integrity/zip.go:countingReader.ReadByte=internal/opc/limits.go:countReader.ReadByte": "docx side left by this pass; docx consolidation pass",
	"internal/docx/math/xml.go:escapeText=internal/docx/view/render.go:escapeText":                       "docx side left by this pass; docx consolidation pass",
	"internal/docx/math/xml.go:localName=internal/docx/model/xml.go:attrLocal":                           "docx side left by this pass; docx consolidation pass",
	"internal/docx/ops/run_paragraph_format.go:qname=internal/docx/revise/settle.go:wName":               "docx side left by this pass; docx consolidation pass",
	"internal/ooxml/tree/chars.go:allDigits=internal/ooxml/xmlscan/codec.go:allDigits":                   "tokenizer keeps its private validating helper (S1b); xmlscan's decoder is the shared codec",
	"internal/ooxml/tree/chars.go:allHex=internal/ooxml/xmlscan/codec.go:allHex":                         "tokenizer keeps its private validating helper (S1b); xmlscan's decoder is the shared codec",
	"internal/opc/save.go:samePath=internal/plan/tx.go:sameFile":                                         "plan imports opc, so opc cannot import plan's helper; shared-engine keep",
	"internal/pptx/coverage/universe.go:compact=internal/pptx/view/json.go:marshal":                      "pptx side left by this pass; docx/pptx consolidation pass",
}

// TestNoExactClones fails when two functions of six or more body
// lines, in different files, hash the same after local identifiers
// are renamed by position. Reuse the shared helper instead of
// copying the body.
// dupCloneEntry is one function in a clone group.
type dupCloneEntry struct {
	file  string
	name  string
	line  int
	hash  string
	lines int
}

func TestNoExactClones(t *testing.T) {
	files, fset := dupParseFiles(t)
	byHash := map[string][]dupCloneEntry{}
	for _, df := range files {
		if strings.HasPrefix(df.rel, "eval/") || strings.HasPrefix(df.rel, "tools/") || dupExempt(df.rel, cloneAllowed) {
			continue
		}
		for _, decl := range df.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := dupFuncName(fd)
			// M9b ruling: xlsxtest keeps its own colName
			// (formula's internal tests import xlsxtest and
			// sml imports formula, so either import from
			// xlsxtest is a cycle).
			if df.rel == "internal/xlsx/xlsxtest/xlsxtest.go" && name == "colName" {
				continue
			}
			hash, lines, ok := dupCloneHash(fset, fd)
			if !ok || lines < 6 {
				continue
			}
			byHash[hash] = append(byHash[hash], dupCloneEntry{df.rel, name, dupLine(fset, fd.Pos()), hash, lines})
		}
	}
	seenPairs := map[string]bool{}
	var pairs [][2]dupCloneEntry
	for _, group := range byHash {
		sort.Slice(group, func(i, j int) bool {
			if group[i].file != group[j].file {
				return group[i].file < group[j].file
			}
			return group[i].name < group[j].name
		})
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				if group[i].file == group[j].file {
					continue
				}
				pairs = append(pairs, [2]dupCloneEntry{group[i], group[j]})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		ki, kj := dupPairKey(pairs[i]), dupPairKey(pairs[j])
		return ki < kj
	})
	for _, p := range pairs {
		key := dupPairKey(p)
		seenPairs[key] = true
		if _, ok := clonePairAllowed[key]; ok {
			continue
		}
		t.Errorf("TestNoExactClones: %s:%d: exact clone of %s:%s (%s:%s): reuse the shared helper instead of copying the body",
			p[0].file, p[0].line, p[1].file, p[1].name, p[0].file, p[0].name)
	}
	keys := make([]string, 0, len(clonePairAllowed))
	for k := range clonePairAllowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !seenPairs[k] {
			t.Errorf("TestNoExactClones: allowed pair %q (%s) no longer clones; delete it", k, clonePairAllowed[k])
		}
	}
}

// dupFile is one freshly parsed non-test file for clone hashing.
type dupFile struct {
	rel  string
	file *ast.File
}

// dupParseFiles parses every non-test .go file of the module.
func dupParseFiles(t *testing.T) ([]dupFile, *token.FileSet) {
	t.Helper()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	var out []dupFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "corpus" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		out = append(out, dupFile{rel, f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, fset
}

// dupFuncName returns Recv.Name for a method and Name otherwise.
func dupFuncName(fd *ast.FuncDecl) string {
	name := fd.Name.Name
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return name
	}
	typ := fd.Recv.List[0].Type
	for {
		switch x := typ.(type) {
		case *ast.StarExpr:
			typ = x.X
		case *ast.Ident:
			return x.Name + "." + name
		case *ast.IndexExpr:
			typ = x.X
		case *ast.IndexListExpr:
			typ = x.X
		default:
			return name
		}
	}
}

// dupCloneHash hashes fd's body as go/printer prints it, with
// local identifiers (receiver, parameters, results, locals)
// renamed by position, and returns the line count of the printed
// body.
func dupCloneHash(fset *token.FileSet, fd *ast.FuncDecl) (string, int, bool) {
	var locals []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || name == "_" || seen[name] {
			return
		}
		seen[name] = true
		locals = append(locals, name)
	}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			for _, n := range field.Names {
				add(n.Name)
			}
		}
	}
	if fd.Recv != nil {
		addFields(fd.Recv)
	}
	addFields(fd.Type.Params)
	addFields(fd.Type.Results)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			for _, nm := range x.Names {
				add(nm.Name)
			}
		case *ast.TypeSpec:
			add(x.Name.Name)
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						add(id.Name)
					}
				}
			}
		case *ast.RangeStmt:
			if id, ok := x.Key.(*ast.Ident); ok {
				add(id.Name)
			}
			if id, ok := x.Value.(*ast.Ident); ok {
				add(id.Name)
			}
		case *ast.FuncLit:
			addFields(x.Type.Params)
			addFields(x.Type.Results)
		}
		return true
	})
	canon := make(map[string]string, len(locals))
	for i, name := range locals {
		canon[name] = "v" + strconv.Itoa(i)
	}
	skip := map[*ast.Ident]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			skip[x.Sel] = true
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		case *ast.StructType:
			if x.Fields != nil {
				for _, field := range x.Fields.List {
					for _, nm := range field.Names {
						skip[nm] = true
					}
				}
			}
		case *ast.LabeledStmt:
			skip[x.Label] = true
		case *ast.BranchStmt:
			if x.Label != nil {
				skip[x.Label] = true
			}
		}
		return true
	})
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !skip[id] {
			if repl, ok := canon[id.Name]; ok {
				id.Name = repl
			}
		}
		return true
	})
	var buf bytes.Buffer
	conf := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := conf.Fprint(&buf, fset, fd.Body); err != nil {
		return "", 0, false
	}
	printed := strings.TrimSpace(buf.String())
	if printed == "" {
		return "", 0, false
	}
	sum := sha256.Sum256([]byte(printed))
	return hex.EncodeToString(sum[:]), strings.Count(printed, "\n") + 1, true
}

// dupPairKey returns the canonical "fileA:FuncA=fileB:FuncB" key
// for a sorted pair.
func dupPairKey(p [2]dupCloneEntry) string {
	return p[0].file + ":" + p[0].name + "=" + p[1].file + ":" + p[1].name
}

// xlsxVocabGrandfathered lists files that violate
// TestXlsxVocabulary (frozen from E1's first run on the post-M7
// tree; entries only shrink).
var xlsxVocabGrandfathered = map[string]string{
	"internal/xlsx/read/workbook.go":     "name-note subject built as \"name:\"+name; left by M6 (read)",
	"internal/xlsx/xlsxtest/xlsxtest.go": "M9b justified keep: xlsxtest cannot import sml or formula (import cycle), so its namespace constants and colName stay",
}

// TestXlsxVocabulary fails on a SpreadsheetML namespace literal
// outside internal/xlsx/sml, a "sheet:"/"name:" prefix built
// outside internal/xlsx/model, or a "% 26" column-letter
// expression outside the formula column-letter home. Use sml, model
// and formula instead.
func TestXlsxVocabulary(t *testing.T) {
	scan := dupUnits(t)
	fragments := []string{"spreadsheetml/2006/main", "purl.oclc.org/ooxml/spreadsheetml", "officeDocument/2006/relationships"}
	allow := map[string]string{}
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if !strings.HasPrefix(rel, "internal/xlsx/") && !strings.HasPrefix(rel, "cmd/xlsx/") {
				continue
			}
			imports := map[*ast.BasicLit]bool{}
			for _, im := range f.Imports {
				imports[im.Path] = true
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind != token.STRING || imports[x] {
						return true
					}
					if strings.HasPrefix(rel, "internal/xlsx/sml/") {
						return true
					}
					v, err := strconv.Unquote(x.Value)
					if err != nil {
						return true
					}
					for _, frag := range fragments {
						if strings.Contains(v, frag) {
							violations = append(violations, dupViolation{rel, dupLine(scan.fset, x.Pos()), "SpreadsheetML namespace literal containing " + strconv.Quote(frag) + ": use internal/xlsx/sml"})
							break
						}
					}
				case *ast.BinaryExpr:
					if x.Op == token.ADD {
						if !strings.HasPrefix(rel, "internal/xlsx/model/") {
							if lit, ok := dupUnparen(x.X).(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if v, err := strconv.Unquote(lit.Value); err == nil && (v == "sheet:" || v == "name:") {
									violations = append(violations, dupViolation{rel, dupLine(scan.fset, x.Pos()), "builds a " + strconv.Quote(v) + " unit id with +: use model.SheetID or model.NameID"})
								}
							}
						}
					}
					if x.Op == token.REM {
						if rel != "internal/xlsx/formula/a1.go" && rel != "internal/xlsx/formula/area.go" {
							if dupIs26(x.X) || dupIs26(x.Y) {
								violations = append(violations, dupViolation{rel, dupLine(scan.fset, x.Pos()), "% expression with the literal 26: use formula.ColumnName or formula.AppendColumnName"})
							}
						}
					}
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestXlsxVocabulary", violations, allow, xlsxVocabGrandfathered)
}

// dupUnparen strips parentheses from e.
func dupUnparen(e ast.Expr) ast.Expr {
	for {
		if p, ok := e.(*ast.ParenExpr); ok {
			e = p.X
			continue
		}
		return e
	}
}

// dupIs26 reports whether e is the integer literal 26.
func dupIs26(e ast.Expr) bool {
	lit, ok := dupUnparen(e).(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	v, err := strconv.ParseInt(lit.Value, 10, 64)
	return err == nil && v == 26
}

// sofficeAllowed lists where a process may be started: the soffice
// package itself. Directory entries end in "/".
var sofficeAllowed = map[string]string{
	"internal/soffice/": "the one place that starts LibreOffice",
}

// TestProcessesThroughSoffice fails on a call of os/exec.Command,
// CommandContext or LookPath (by type info) outside
// internal/soffice, tools/ and eval/. Use internal/soffice.
func TestProcessesThroughSoffice(t *testing.T) {
	scan := dupUnits(t)
	grandfathered := map[string]string{}
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if strings.HasPrefix(rel, "eval/") || strings.HasPrefix(rel, "tools/") || dupExempt(rel, sofficeAllowed) {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				for _, name := range []string{"Command", "CommandContext", "LookPath"} {
					if dupCall(u, n, "os/exec", name) {
						violations = append(violations, dupViolation{rel, dupLine(scan.fset, n.Pos()), "calls os/exec." + name + ": use internal/soffice"})
					}
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestProcessesThroughSoffice", violations, sofficeAllowed, grandfathered)
}

// zipAllowed lists where a zip may be written: internal/opc.
// Directory entries end in "/".
var zipAllowed = map[string]string{
	"internal/opc/": "opc builds zips (BuildZip and the package save)",
}

// zipGrandfathered lists files that call archive/zip.NewWriter and
// pre-date the consolidation (frozen from E1's first run on the
// post-M7 tree; entries only shrink).
var zipGrandfathered = map[string]string{
	"internal/docx/create/template.go": "pre-dates the consolidation; docx zip consolidation pass (E1-f)",
	"internal/testdeck/testdeck.go":    "pre-dates the consolidation; docx zip consolidation pass (E1-f)",
}

// TestZipThroughOPC fails on a call of archive/zip.NewWriter (by
// type info) outside internal/opc, tools/ and eval/. Use
// opc.BuildZip.
func TestZipThroughOPC(t *testing.T) {
	scan := dupUnits(t)
	var violations []dupViolation
	for _, u := range scan.units {
		for i, f := range u.files {
			rel := u.names[i]
			if strings.HasPrefix(rel, "eval/") || strings.HasPrefix(rel, "tools/") || dupExempt(rel, zipAllowed) {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if dupCall(u, n, "archive/zip", "NewWriter") {
					violations = append(violations, dupViolation{rel, dupLine(scan.fset, n.Pos()), "calls archive/zip.NewWriter: use opc.BuildZip"})
				}
				return true
			})
		}
	}
	dupCheckViolations(t, "TestZipThroughOPC", violations, zipAllowed, zipGrandfathered)
}
