package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// manifestHeader starts every manifest; the oracle's --version line follows
// it as a comment when recorded.
const manifestHeader = "# file\tcase\texit\tstdout\tstderr (SHA-256, first 16 hex digits; - when empty)"

// manifestPath is the manifest of a set in the baseline.
func (c *config) manifestPath(set string) string {
	return filepath.Join(c.baseline, set+".tsv")
}

type key struct{ file, kase string }

// manifest is a set's recorded outcomes and the comment lines above them.
type manifest struct {
	comments []string
	entries  map[key]entry
}

// loadManifest reads a manifest; a missing file is an empty manifest.
func loadManifest(path string) (*manifest, error) {
	m := &manifest{entries: map[key]entry{}}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if line != manifestHeader {
				m.comments = append(m.comments, line)
			}
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("%s:%d: want 5 tab-separated fields, have %d", path, n, len(f))
		}
		exit, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: exit code: %w", path, n, err)
		}
		e := entry{file: f[0], kase: f[1], exit: exit, stdout: f[3], errh: f[4]}
		m.entries[key{e.file, e.kase}] = e
	}
	return m, sc.Err()
}

// save writes the manifest sorted by file, then by case order.
func (m *manifest) save(path string) error {
	order := map[string]int{}
	for i, k := range cases {
		order[k.Name] = i
	}
	list := make([]entry, 0, len(m.entries))
	for _, e := range m.entries {
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b entry) int {
		if c := strings.Compare(a.file, b.file); c != 0 {
			return c
		}
		return order[a.kase] - order[b.kase]
	})
	var b strings.Builder
	b.WriteString(manifestHeader + "\n")
	for _, c := range m.comments {
		b.WriteString(c + "\n")
	}
	for _, e := range list {
		b.WriteString(e.line() + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
