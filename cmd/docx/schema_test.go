package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestHelpSchema: `docx help schema` prints the committed plan JSON
// Schema byte for byte (tools/schemagen's test keeps that file current
// with the operation table), and it is a JSON object naming every
// operation of the table.
func TestHelpSchema(t *testing.T) {
	code, out, errs := runCLI(t, "help", "schema")
	if code != 0 {
		t.Fatalf("help schema: %d %+v", code, errs)
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(root, "docs", "docx-plan.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Fatal("help schema is not docs/docx-plan.schema.json")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(out), &schema); err != nil {
		t.Fatalf("help schema is not JSON: %v", err)
	}
	if schema["$schema"] == nil {
		t.Errorf("no $schema: %.200s", out)
	}
	for _, d := range ops.Ops {
		if !strings.Contains(out, `"`+d.Name+`"`) {
			t.Errorf("help schema does not name %s", d.Name)
		}
	}
	_, help, _ := runCLI(t, "help")
	if !strings.Contains(help, "|schema|") {
		t.Errorf("help does not list schema:\n%s", help)
	}
}
