package ops_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The op test harness (frozen: chunks use these helpers and add tests
// only in their own *_chunk_test.go). It runs ops directly on an
// edit.Workspace, without apply, the recalculation policy or the gate, so
// an op's tests depend only on its own chunk and the chunks it calls.

// run is the outcome of running ops on a book.
type run struct {
	t   *testing.T
	ws  *edit.Workspace
	pkg *opc.Package
	env []*ops.Env
	// err is the first op's error (nil when all applied); at its index.
	err error
	at  int
}

// runOps opens b, runs specs in order (aliases resolved as apply does)
// and flushes the workspace. It stops at the first op that fails.
func runOps(t *testing.T, b xlsxtest.Book, specs ...ops.Spec) *run {
	t.Helper()
	path, err := b.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() }) //nolint:errcheck // a test's package
	ws, err := edit.New(pkg)
	if err != nil {
		t.Fatal(err)
	}
	r := &run{t: t, ws: ws, pkg: pkg, at: -1}
	aliases := map[string]string{}
	for i, s := range specs {
		env := &ops.Env{WS: ws, Index: i}
		r.env = append(r.env, env)
		err := ops.ResolveAliases(s, aliases)
		if err == nil {
			err = s.Run(env)
		}
		if err != nil {
			r.err, r.at = ops.FromEdit(err, ""), i
			return r
		}
		if a := ops.AliasOf(s); a != "" {
			aliases[a] = env.Created
		}
	}
	if err := ws.Flush(); err != nil {
		t.Fatal(err)
	}
	return r
}

// ok fails the test when an op failed.
func (r *run) ok() *run {
	r.t.Helper()
	if r.err != nil {
		r.t.Fatalf("ops[%d]: %v", r.at, r.err)
	}
	return r
}

// refused checks that op at was refused with code at field.
func (r *run) refused(at int, code, field string) {
	r.t.Helper()
	var rej *ops.Rejection
	if !errors.As(r.err, &rej) {
		r.t.Fatalf("got %v, want a %s refusal at ops[%d].%s", r.err, code, at, field)
	}
	if r.at != at || rej.Code != code || rej.Field != field {
		r.t.Fatalf("got %s at ops[%d].%s (%s), want %s at ops[%d].%s", rej.Code, r.at, rej.Field, rej.Msg, code, at, field)
	}
}

// part returns a part's text as the ops left it.
func (r *run) part(name string) string {
	r.t.Helper()
	pt, err := r.pkg.Part(name)
	if err != nil {
		r.t.Fatal(err)
	}
	if p := pt.Patch(); p != nil {
		if err := p.Check(); err != nil {
			r.t.Fatalf("%s: patch does not check: %v", name, err)
		}
		return p.String()
	}
	b, err := pt.Bytes()
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

// sheet returns worksheet n's text (1-based, xlsxtest's numbering).
func (r *run) sheet(n int) string {
	return r.part("/xl/worksheets/sheet" + string(rune('0'+n)) + ".xml")
}

// sealed returns the working copy as a package.
func (r *run) sealed() []byte {
	r.t.Helper()
	s, err := r.pkg.Seal()
	if err != nil {
		r.t.Fatal(err)
	}
	return s.Bytes()
}

// cells reads range rg of the working copy in the cells view, as a map
// of A1 key to the cell's JSON.
func (r *run) cells(rg string) map[string]string {
	r.t.Helper()
	res, err := read.Cells(r.sealed(), read.CellsRequest{Range: rg}, read.Options{})
	if err != nil {
		r.t.Fatalf("read %s: %v", rg, err)
	}
	b, err := cli.Marshal(res.Cells)
	if err != nil {
		r.t.Fatal(err)
	}
	return splitCells(r.t, string(b))
}

// splitCells splits read's cells object {"A1":v,...} into its members.
func splitCells(t *testing.T, obj string) map[string]string {
	t.Helper()
	v, perr := cli.ParseJSON([]byte(obj))
	if perr != nil {
		t.Fatal(perr)
	}
	out := map[string]string{}
	for _, m := range v.Members {
		out[m.Key] = strings.TrimSpace(obj[m.Value.Off:m.Value.End])
	}
	return out
}

// book is a one-sheet workbook named Budget with rows as its sheetData.
func book(rows string) xlsxtest.Book {
	return xlsxtest.Book{XFs: []int{0, 14, 22, 21, 10}, Sheets: []xlsxtest.Sheet{{Name: "Budget", Rows: rows, Dimension: "A1"}}}
}

// parse decodes a plan's JSON text, failing the test on errors.
func parse(t *testing.T, text string) *ops.Plan {
	t.Helper()
	p, errs := ops.ParsePlan([]byte(text))
	if errs != nil {
		t.Fatalf("%s: %v", text, errs)
	}
	return p
}

// parseErr decodes a plan's JSON text and returns its first error.
func parseErr(text string) *cli.Error {
	_, errs := ops.ParsePlan([]byte(text))
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}
