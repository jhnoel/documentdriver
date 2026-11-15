package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoAbsoluteHomePaths fails when a tracked text file contains an
// absolute home or temp path. Everything in the repository is
// repo-relative: tools find the repository root at run time
// (internal/repo) and take overrides from flags or environment variables.
//
// Exempt: files under testdata/ or fixtures/ (recorded real-world data),
// and the files in allowedPathMentions, which name these prefixes as
// search terms of leak checks, not as paths.
func TestNoAbsoluteHomePaths(t *testing.T) {
	root := moduleRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	// Built from pieces so this file does not contain the prefixes itself.
	needles := []string{"/" + "Users/", "/" + "home/", "/" + "private/var/"}
	for rel := range strings.SplitSeq(string(out), "\x00") {
		if rel == "" || exempt(rel) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			continue // deleted in the working tree, or binary
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, n := range needles {
				if strings.Contains(line, n) {
					t.Errorf("%s:%d contains %q: use a repo-relative path, or an override flag/env var", rel, i+1, n)
				}
			}
		}
	}
}

// allowedPathMentions name the prefixes as terms the bench leak checks
// look for in agent output and prompts, not as paths.
var allowedPathMentions = set(
	"eval/cmd/pptxbench/pi.go",
	"eval/cmd/pptxbench/run.go",
	"eval/cmd/pptxbench/run_test.go",
	"eval/README.md",
)

func exempt(rel string) bool {
	if allowedPathMentions[rel] {
		return true
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if seg == "testdata" || seg == "fixtures" {
			return true
		}
	}
	return false
}
