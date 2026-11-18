package read

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// nullPath returns the JSON path of the first null in b, or "" when b
// holds none. docs/contract.md §4: a fact the tool cannot derive is
// omitted, never null.
func nullPath(t *testing.T, b []byte) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var stack []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			t.Fatalf("output is not JSON: %v", err)
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				stack = append(stack, string(v))
			} else {
				stack = stack[:len(stack)-1]
			}
		case nil:
			return strings.Join(stack, "")
		}
	}
}

// noNullDrawing checks the summary, every page and master at every
// detail level, and find, of one drawing; it reports the first null
// per output.
func noNullDrawing(t *testing.T, name string, input []byte) {
	t.Helper()
	sum, err := Document(input, "sha256:x")
	if err != nil {
		return // a refused drawing prints an errors object, not a result
	}
	b, err := json.Marshal(sum)
	if err != nil {
		t.Fatalf("%s: summary: %v", name, err)
	}
	if p := nullPath(t, b); p != "" {
		t.Errorf("%s: summary has a null", name)
	}
	var addrs []string
	for _, pg := range sum.Pages {
		addrs = append(addrs, pg.ID)
	}
	for _, m := range sum.Masters {
		addrs = append(addrs, m.ID)
	}
	for _, a := range addrs {
		for _, detail := range []string{"", "sites", "full"} {
			for _, units := range []string{"pt", "in"} {
				out, err := Page(input, "sha256:x", a, Options{Detail: detail, Units: units})
				if err != nil {
					continue
				}
				if bytes.Contains(out, []byte("null")) && nullPath(t, out) != "" {
					t.Errorf("%s: read %s --detail %q --units %s has a JSON null near %s", name, a, detail, units, nullContext(out))
				}
			}
		}
	}
	for _, q := range []string{"", "e", "1"} {
		res, err := Find(input, "sha256:x", FindRequest{Query: q})
		if err != nil {
			continue
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("%s: find: %v", name, err)
		}
		if nullPath(t, b) != "" {
			t.Errorf("%s: find %q has a JSON null near %s", name, q, nullContext(b))
		}
	}
}

// nullContext shows the bytes around the first "null" token.
func nullContext(b []byte) string {
	i := bytes.Index(b, []byte(":null"))
	if i < 0 {
		i = bytes.Index(b, []byte("null"))
	}
	lo, hi := max(0, i-80), min(len(b), i+20)
	return string(b[lo:hi])
}

// TestNoNullInReadOrFind is the law: no vsdx read or find output, at
// any detail level or unit, contains a JSON null. It runs on every
// fixture, and on every corpus drawing when the corpus is present.
func TestNoNullInReadOrFind(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "testdata", "vsdx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "common" || e.Name() == "golden" || e.Name() == "create" {
			continue
		}
		b, err := os.ReadFile(vsdxtest.Fixture(t, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		noNullDrawing(t, e.Name(), b)
	}
	dir := vsdxCorpus()
	if dir == "" {
		return
	}
	for _, p := range corpusDrawings(dir) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		noNullDrawing(t, filepath.Base(p), b)
	}
}

// TestDateNotInDateFormHasNoValue is the M2 row on
// vsdx-js/tests/Connectors.vsdx: page0/s40/prop:StartDate is a date
// row whose value is not stored in date form, so read shows it with
// no value key (never "value":null). It skips only when the corpus is
// absent; with the corpus present a missing file fails.
func TestDateNotInDateFormHasNoValue(t *testing.T) {
	dir := vsdxCorpus()
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	path := filepath.Join(dir, "repos", "vsdx-js", "tests", "Connectors.vsdx")
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("corpus file: %v", err)
	}
	out, err := Page(input, "sha256:x", "page0", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Objects []struct {
			ID    string                       `json:"id"`
			Props []map[string]json.RawMessage `json:"props"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(out, &page); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range page.Objects {
		for _, p := range o.Props {
			if string(p["id"]) != `"page0/s40/prop:StartDate"` {
				continue
			}
			found = true
			if string(p["type"]) != `"date"` {
				t.Errorf("StartDate type = %s, want \"date\"", p["type"])
			}
			if v, ok := p["value"]; ok {
				t.Errorf("StartDate has value %s, want no value key", v)
			}
		}
	}
	if !found {
		t.Fatal("page0/s40/prop:StartDate not in read --page page0")
	}
}
