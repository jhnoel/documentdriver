package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// TestCommittedOutputs fails when docs/docx-plan.schema.json or
// docs/docx-plan.md is stale.
func TestCommittedOutputs(t *testing.T) {
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range outputs() {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go run ./tools/schemagen", name)
		}
	}
}
