package soffice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("test"), 0700); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "extra")
	if err := os.WriteFile(extra, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		env, name, want string
		extra           []string
	}{{"override", "tool", "override", nil}, {"", "tool", tool, []string{extra}}, {"", "missing", extra, []string{dir, extra}}, {"", "missing", "", nil}} {
		if got := Find(tc.env, tc.name, tc.extra...); got != tc.want {
			t.Errorf("Find = %q, want %q", got, tc.want)
		}
	}
}
