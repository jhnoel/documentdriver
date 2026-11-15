package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// runCLI runs the command line. A failure must leave stdout empty and put
// exactly one errors object on stderr; a success must leave stderr empty.
func runCLI(t *testing.T, args ...string) (code int, stdout string, errs []cli.Error) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	if code == 0 {
		if errb.Len() != 0 {
			t.Errorf("xlsx %v: stderr on success: %s", args, errb.String())
		}
		return 0, out.String(), nil
	}
	if out.Len() != 0 {
		t.Errorf("xlsx %v: stdout on failure: %s", args, out.String())
	}
	var v struct {
		Errors []cli.Error `json:"errors"`
	}
	dec := json.NewDecoder(&errb)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil || len(v.Errors) == 0 || dec.More() {
		t.Fatalf("xlsx %v: stderr is not one errors object: %v", args, err)
	}
	return code, "", v.Errors
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "--version")
	if code != cli.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var v struct {
		Tool     string `json:"tool"`
		Version  string `json:"version"`
		Contract int    `json:"contract"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Tool != "xlsx" || v.Version != version || v.Contract != cli.Contract || !strings.HasSuffix(out, "}\n") {
		t.Errorf("--version = %q", out)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		code, out, _ := runCLI(t, args...)
		if code != cli.ExitOK || !strings.Contains(out, "usage: xlsx <command>") || !strings.Contains(out, "reads and edits Excel workbooks") {
			t.Errorf("xlsx %v: exit %d, %q", args, code, out)
		}
		for _, c := range []string{"read", "find", "apply", "create"} {
			if !strings.Contains(out, "  "+c+" ") {
				t.Errorf("xlsx %v does not list %s", args, c)
			}
		}
	}
	for _, c := range []string{"read", "find", "apply", "create"} {
		if code, out, _ := runCLI(t, "help", c); code != cli.ExitOK || !strings.Contains(out, "usage: xlsx "+c) {
			t.Errorf("help %s: exit %d, %q", c, code, out)
		}
		if code, out, _ := runCLI(t, c, "--help"); code != cli.ExitOK || !strings.Contains(out, "usage: xlsx "+c) {
			t.Errorf("%s --help: exit %d, %q", c, code, out)
		}
	}
}

func TestValuesHelp(t *testing.T) {
	code, out, _ := runCLI(t, "help", "values")
	for _, want := range []string{`{"date":"2024-03-01"}`, `{"time":"13:30:00"}`, `{"error":"#N/A"}`, `{"text":"=literal"}`, `"spill"`, `"array":"cse"`, "[h]:mm"} {
		if code != cli.ExitOK || !strings.Contains(out, want) {
			t.Errorf("values help: exit %d, lacks %s", code, want)
		}
	}
}

// TestRefusals: a wrong command line is a usage error (exit 2); there
// are no xml, render or html help. apply and create end to end are in
// apply_test.go. Read and find are
// wired (chunk 7), so their rows here are command lines that are wrong
// (a --view the cli grammar refuses) and refuse before any file is
// read; the well-formed read and find rows are in TestReadFindCLI.

func TestRefusals(t *testing.T) {
	for _, c := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"read", "book.xlsx", "--view", "grid"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"find", "book.xlsx", "Total", "--view", "cells"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"xml", "book.xlsx", "Sheet1!A1"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"render", "book.xlsx"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"read"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"read", "book.xlsx", "--sheet", "sheet:Budget", "--view", "grid"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"read", "book.xlsx", "--view", "grid"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"find", "book.xlsx", "x", "--view", "cells"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"apply", "book.xlsx", "plan.json", "--changed", "some"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"help", "html"}, cli.ExitUsage, cli.CodeUsage},
		{[]string{"help", "nope.op"}, cli.ExitUsage, cli.CodeUsage},
	} {
		code, _, errs := runCLI(t, c.args...)
		if code != c.exit || len(errs) != 1 || errs[0].Code != c.code {
			t.Errorf("xlsx %v: exit %d, errors %+v; want exit %d, %s", c.args, code, errs, c.exit, c.code)
		}
	}
}
