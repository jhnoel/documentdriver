package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// loadProvenance reads and decodes the provenance record at path.
func loadProvenance(path string) (*Provenance, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a fixed repo path
	if err != nil {
		return nil, err
	}
	var p Provenance
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}
