package archtest

import (
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// siblingSliceExceptions is the shrink-only allow-list for
// TestNoSiblingSlices, by package and function, each with its reason:
//   - internal/ooxml/edit Wrap: needs the indices of first and last
//     for the wrap range, once per operation.
//   - internal/ooxml/edit ReplaceWith: a real neighbour lookup,
//     grandfathered because the file is not W1's; fixed in E.
//   - internal/docx/ops splitRevision: the SplitAt index, once per
//     operation.
//   - internal/docx/ops isolateObject: the SplitAt index, once per
//     operation.
var siblingSliceExceptions = map[string]string{
	pkgCore + ".Wrap":                           "needs the first and last indices for the wrap range, once per operation",
	pkgCore + ".ReplaceWith":                    "a real neighbour lookup, grandfathered because the file is not W1's; fixed in E",
	module + "/internal/docx/ops.splitRevision": "SplitAt index, once per operation",
	module + "/internal/docx/ops.isolateObject": "SplitAt index, once per operation",
}

// TestNoSiblingSlices enforces AGENTS.md's sibling rule for the
// sibling-search form: a Children() or Elements() call on x.Parent()
// (or on a variable assigned from it in the same function) that the
// function then searches for that same x (slices.Index(kids, x), or a
// range over the slice whose body compares the element == x). A sibling
// question is walked over the tree's sibling links instead. Only the
// named exceptions above are allowed. internal/ooxml/tree and the frozen
// treeoracle are exempt (the tree defines the walks).
func TestNoSiblingSlices(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole module")
	}
	root := moduleRoot(t)
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
	var problems []string
	report := func(file string, pos token.Pos, msg string) {
		p := fset.Position(pos)
		problems = append(problems, file+":"+strconv.Itoa(p.Line)+": "+msg)
	}
	for _, p := range goList(t, root, "./...") {
		if p.ImportPath == pkgTree || strings.HasPrefix(p.ImportPath, pkgTree+"/") {
			continue // the tree and the frozen treeoracle define the walks
		}
		var files []*ast.File
		var names []string
		for _, n := range p.GoFiles {
			full := filepath.Join(p.Dir, n)
			f, err := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			rel, _ := filepath.Rel(root, full)
			files = append(files, f)
			names = append(names, filepath.ToSlash(rel))
		}
		if len(files) == 0 {
			continue
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			t.Errorf("type-checking %s: %v", p.ImportPath, err)
		}
		// isNodeCall reports whether e is a call of the tree method
		// named name (Parent, Children, Elements) on a *tree.Node.
		isNodeCall := func(e ast.Expr, name string) bool {
			call, ok := e.(*ast.CallExpr)
			if !ok {
				return false
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != name {
				return false
			}
			fn := treeFunc(info, sel)
			return fn != nil && fn.Name() == name
		}
		// parentSource returns the identifier x when e is x.Parent().
		parentSource := func(e ast.Expr) (string, bool) {
			if !isNodeCall(e, "Parent") {
				return "", false
			}
			sel := e.(*ast.CallExpr).Fun.(*ast.SelectorExpr)
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return "", false
			}
			return id.Name, true
		}
		for i, f := range files {
			name := names[i]
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				if _, allowed := siblingSliceExceptions[p.ImportPath+"."+fd.Name.Name]; allowed {
					continue
				}
				// Variables a loop or if init assigns from Parent()
				// climb ancestors (for p := x.Parent(); ...); they are
				// not the sibling-search pattern.
				climb := map[string]bool{}
				collectClimb := func(init ast.Stmt) {
					if as, ok := init.(*ast.AssignStmt); ok {
						for _, lhs := range as.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								climb[id.Name] = true
							}
						}
					}
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.ForStmt:
						collectClimb(n.Init)
					case *ast.IfStmt:
						collectClimb(n.Init)
					}
					return true
				})
				// parents maps a variable assigned from x.Parent() in
				// this function to x.
				parents := map[string]string{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					as, ok := n.(*ast.AssignStmt)
					if !ok || len(as.Lhs) != len(as.Rhs) {
						return true
					}
					for j, lhs := range as.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && !climb[id.Name] {
							if x, yes := parentSource(as.Rhs[j]); yes {
								parents[id.Name] = x
							}
						}
					}
					return true
				})
				// candidate is one Children()/Elements() call on
				// x.Parent() or on a variable assigned from it.
				type candidate struct {
					call     *ast.CallExpr
					sel      *ast.SelectorExpr
					method   string
					x        string
					searched bool
				}
				var candidates []*candidate
				byCall := map[*ast.CallExpr]*candidate{}
				sliceVar := map[string]*candidate{}
				// candidateOf returns the candidate a Children() or
				// Elements() expression is, when it is one.
				candidateOf := func(e ast.Expr) *candidate {
					call, ok := e.(*ast.CallExpr)
					if !ok {
						return nil
					}
					if c, yes := byCall[call]; yes {
						return c
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Children" && sel.Sel.Name != "Elements") {
						return nil
					}
					if treeFunc(info, sel) == nil {
						return nil
					}
					var x string
					switch recv := sel.X.(type) {
					case *ast.CallExpr:
						src, yes := parentSource(recv)
						if !yes {
							return nil
						}
						x = src
					case *ast.Ident:
						src, yes := parents[recv.Name]
						if !yes {
							return nil
						}
						x = src
					default:
						return nil
					}
					c := &candidate{call: call, sel: sel, method: sel.Sel.Name, x: x}
					byCall[call] = c
					candidates = append(candidates, c)
					return c
				}
				// Resolve a slice expression (a variable holding a
				// candidate slice, or the call itself) to its candidate.
				resolve := func(e ast.Expr) *candidate {
					if id, ok := e.(*ast.Ident); ok {
						return sliceVar[id.Name]
					}
					return candidateOf(e)
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.AssignStmt:
						if len(n.Lhs) == len(n.Rhs) {
							for j, lhs := range n.Lhs {
								if id, ok := lhs.(*ast.Ident); ok {
									if c := candidateOf(n.Rhs[j]); c != nil {
										sliceVar[id.Name] = c
									}
								}
							}
						}
					case *ast.CallExpr:
						// slices.Index(kids, x) searches the slice for x.
						sel, ok := n.Fun.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "Index" || len(n.Args) != 2 {
							return true
						}
						pkgID, ok := sel.X.(*ast.Ident)
						if !ok {
							return true
						}
						pkg, ok := info.Uses[pkgID].(*types.PkgName)
						if !ok || pkg.Imported().Path() != "slices" {
							return true
						}
						id, ok := n.Args[1].(*ast.Ident)
						if !ok {
							return true
						}
						if c := resolve(n.Args[0]); c != nil && c.x == id.Name {
							c.searched = true
						}
					case *ast.RangeStmt:
						// A range over the slice whose body compares
						// the element == x searches it for x.
						c := resolve(n.X)
						if c == nil {
							return true
						}
						var elem string
						if id, ok := n.Value.(*ast.Ident); ok {
							elem = id.Name
						} else if id, ok := n.Key.(*ast.Ident); ok {
							elem = id.Name
						}
						if elem == "" {
							return true
						}
						ast.Inspect(n.Body, func(m ast.Node) bool {
							bin, ok := m.(*ast.BinaryExpr)
							if !ok || bin.Op != token.EQL {
								return true
							}
							is := func(e ast.Expr, name string) bool {
								id, ok := e.(*ast.Ident)
								return ok && id.Name == name
							}
							if (is(bin.X, elem) && is(bin.Y, c.x)) || (is(bin.X, c.x) && is(bin.Y, elem)) {
								c.searched = true
							}
							return true
						})
					}
					return true
				})
				// Also collect candidates never assigned to a
				// variable (the direct slices.Index and range forms);
				// candidateOf above only registers assigned ones until
				// resolve is called on them, which the searches did.
				for _, c := range candidates {
					if c.searched {
						report(name, c.sel.Sel.Pos(), "builds "+c.method+"() of "+c.x+"'s parent and searches it for "+c.x+"; walk the siblings over the tree's links")
					}
				}
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// treeFunc is the *tree.Node method a selector calls, or nil when it is
// not a tree method.
func treeFunc(info *types.Info, sel *ast.SelectorExpr) *types.Func {
	var fn *types.Func
	if f, ok := info.Uses[sel.Sel].(*types.Func); ok {
		fn = f
	} else if s := info.Selections[sel]; s != nil {
		if f, ok := s.Obj().(*types.Func); ok {
			fn = f
		}
	}
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgTree {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	rt := sig.Recv().Type()
	if p, ok := rt.(*types.Pointer); ok {
		rt = p.Elem()
	}
	if nt, ok := rt.(*types.Named); ok && nt.Obj().Name() == "Node" {
		return fn
	}
	return nil
}
