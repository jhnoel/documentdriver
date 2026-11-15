package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// fileHelper is what bookFile needs of its caller: *testing.T, or
// *testing.F before a fuzz target runs (so the fuzzer writes its
// fixture once, outside the target).
type fileHelper interface {
	Helper()
	Fatal(args ...any)
	TempDir() string
}

// bookFile writes a small workbook (sheet Budget: A1 "Item", B1 1200,
// B2 =B1*2 with cached 2400, A3 a date) to a temporary file and returns
// its path. Authored for these tests; the layout follows
// internal/xlsx/read's fixtures.
func bookFile(t fileHelper) string {
	t.Helper()
	const (
		decl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
		ns   = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
		rel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
		ct   = "application/vnd.openxmlformats-officedocument.spreadsheetml."
		rels = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", decl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="` + ct + `sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="` + ct + `worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="` + ct + `styles+xml"/></Types>`},
		{"_rels/.rels", decl + rels + `<Relationship Id="rId1" Type="` + rel + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", decl + `<workbook ` + ns + `><sheets><sheet name="Budget" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", decl + rels + `<Relationship Id="rId1" Type="` + rel + `worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="` + rel + `styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", decl + `<styleSheet ` + ns + `><fonts count="1"><font/></fonts><fills count="1"><fill/></fills><borders count="1"><border/></borders>` +
			`<cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0"/><xf numFmtId="14" applyNumberFormat="1"/></cellXfs></styleSheet>`},
		{"xl/worksheets/sheet1.xml", decl + `<worksheet ` + ns + `><dimension ref="A1:B3"/><sheetData>` +
			`<row r="1"><c r="A1" t="inlineStr"><is><t>Item</t></is></c><c r="B1"><v>1200</v></c></row>` +
			`<row r="2"><c r="B2"><f>B1*2</f><v>2400</v></c></row><row r="3"><c r="A3" s="1"><v>45352</v></c></row></sheetData></worksheet>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "book.xlsx")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReadFindCLI: the read and find command lines once chunk 7 wires
// them: exit codes, error codes and paths, and one JSON document whose
// first key is base.
func TestReadFindCLI(t *testing.T) {
	book := bookFile(t)
	for _, c := range []struct {
		args []string
		exit int
		code string // error code, or "" on success
		path string // error path
		has  string // a substring of stdout on success
	}{
		{[]string{"read", book}, cli.ExitOK, "", "", `"sheets":[{"id":"sheet:Budget"`},
		{[]string{"read", book, "--kind", "workbook"}, cli.ExitOK, "", "", `"sheets"`},
		{[]string{"read", book, "--sheet", "sheet:Budget"}, cli.ExitOK, "", "", `"cells":{"A1":"Item","B1":1200,"B2":{"f":"=B1*2","v":2400},"A3":{"date":"2024-03-01"}}`},
		{[]string{"read", book, "--range", "Budget!B1:B2", "--view", "values"}, cli.ExitOK, "", "", `"cells":{"B1":1200,"B2":2400}`},
		{[]string{"read", book, "--range", "Budget!B1:B2", "--view", "formulas", "--group"}, cli.ExitOK, "", "", `"cells":{"B2":"=B1*2"}`},
		{[]string{"read", book, "--sheet", "sheet:Budget", "--limit", "2"}, cli.ExitOK, "", "", `"truncated":{"next":"Budget!A2:B3"}`},
		{[]string{"read", book, "--sheet", "sheet:Budget", "--limit", "0"}, cli.ExitOK, "", "", `"A3"`},
		{[]string{"read", book, "--sheet", "sheet:Budget", "--pretty"}, cli.ExitOK, "", "", "\n  \"sheet\": \"sheet:Budget\""},
		{[]string{"find", book, "Item"}, cli.ExitOK, "", "", `"matches":[{"cell":"Budget!A1","value":"Item"}]`},
		{[]string{"find", book, "B1", "--view", "formulas"}, cli.ExitOK, "", "", `{"cell":"Budget!B2","value":"=B1*2"}`},
		{[]string{"find", book, "ITEM", "--ignore-case", "--sheet", "sheet:Budget"}, cli.ExitOK, "", "", `"Budget!A1"`},
		{[]string{"find", book, "1", "--limit", "1"}, cli.ExitOK, "", "", `"truncated":{"after":"Budget!B1"}`},
		{[]string{"read", book, "--limit", "-1", "--sheet", "sheet:Budget"}, cli.ExitUsage, cli.CodeUsage, "--limit", ""},
		{[]string{"read", book, "--limit", "ten", "--sheet", "sheet:Budget"}, cli.ExitUsage, cli.CodeUsage, "--limit", ""},
		{[]string{"read", book, "--view", "values"}, cli.ExitUsage, cli.CodeUsage, "--view", ""},
		{[]string{"read", book, "--sheet", "sheet:Budget", "--group"}, cli.ExitUsage, cli.CodeUsage, "--group", ""},
		{[]string{"read", book, "--kind", "workbook", "--sheet", "sheet:Budget"}, cli.ExitUsage, cli.CodeUsage, "--kind", ""},
		{[]string{"read", book, "--kind", "names"}, cli.ExitFailed, cli.CodeInvalidOperation, "--kind", ""},
		{[]string{"read", book, "--kind", "comments"}, cli.ExitFailed, cli.CodeInvalidOperation, "--kind", ""},
		// --kind is checked first: an unimplemented kind wins over a
		// usage fault with --sheet.
		{[]string{"read", book, "--kind", "tables", "--sheet", "sheet:Budget"}, cli.ExitFailed, cli.CodeInvalidOperation, "--kind", ""},
		{[]string{"read", book, "--detail", "styles", "--sheet", "sheet:Budget"}, cli.ExitFailed, cli.CodeInvalidOperation, "--detail", ""},
		{[]string{"read", book, "--sheet", "Budget"}, cli.ExitFailed, cli.CodeInvalidOperation, "--sheet", ""},
		{[]string{"read", book, "--sheet", "sheet:Nope"}, cli.ExitFailed, cli.CodeNotFound, "--sheet", ""},
		{[]string{"read", book, "--range", "Budget!$A$1"}, cli.ExitFailed, cli.CodeInvalidOperation, "--range", ""},
		{[]string{"read", filepath.Join(t.TempDir(), "absent.xlsx")}, cli.ExitFailed, cli.CodeIO, "", ""},
		{[]string{"find", filepath.Join(t.TempDir(), "absent.xlsx"), "x"}, cli.ExitFailed, cli.CodeIO, "", ""},
	} {
		code, out, errs := runCLI(t, c.args...)
		if code != c.exit {
			t.Errorf("xlsx %v: exit %d, want %d (%+v)", c.args[:1], code, c.exit, errs)
			continue
		}
		if c.code != "" {
			if len(errs) != 1 || errs[0].Code != c.code || errs[0].Path != c.path {
				t.Errorf("xlsx %v: errors %+v, want %s at %q", c.args, errs, c.code, c.path)
			}
			continue
		}
		var v map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Errorf("xlsx %v: stdout is not one JSON object: %v", c.args, err)
		}
		if !strings.HasPrefix(strings.TrimLeft(out, "{\n "), `"base"`) || !strings.Contains(out, c.has) {
			t.Errorf("xlsx %v: stdout %s lacks %s or does not start with base", c.args, out, c.has)
		}
	}
}

// TestReadHelp: the help texts state what the contract relies on.
func TestReadHelp(t *testing.T) {
	for cmd, wants := range map[string][]string{
		"read": {"--sheet", "--range", "--view", "--group", "not in row-major order", "--limit", "truncated.next", "values_stale", "spill", `"array":"cse"`, "height zero", "base"},
		"find": {"--ignore-case", "--sheet", "--view", "--limit", "truncated.after", `{"cell":"Budget!B2","value":...}`},
	} {
		code, out, _ := runCLI(t, "help", cmd)
		for _, w := range wants {
			if code != cli.ExitOK || !strings.Contains(out, w) {
				t.Errorf("help %s lacks %q", cmd, w)
			}
		}
	}
}
