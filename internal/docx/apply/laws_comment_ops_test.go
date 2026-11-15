package apply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
)

// commentOpSuite is the change-mode law of comment.reply,
// comment.set_status and comment.delete: comments are not tracked, so
// both modes write the same.
func commentOpSuite(name, opsJSON string) *laws.ModeSuite {
	s := commentSuite(name, opsJSON)
	s.Name = name
	return s
}

// commentIDs are the ids of the comments the comments read of path
// lists, roots and replies.
func commentIDs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	seen := map[string]bool{}
	// The w:comment records' ids, from comments.xml (whatever its name).
	for _, m := range regexp.MustCompile(`<w:comment w:id="(\d+)"`).FindAllSubmatch(zipText(t, b), -1) {
		if id := string(m[1]); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// zipText is the concatenated text of a package's comment parts.
func zipText(t *testing.T, b []byte) []byte {
	t.Helper()
	var out []byte
	for name, data := range zipEntries(t, b) {
		if regexp.MustCompile(`(?i)comments\.xml$`).MatchString(name) {
			out = append(out, data...)
		}
	}
	return out
}

// TestCommentOpLaws runs the change-mode law of comment.reply,
// comment.set_status (both statuses) and comment.delete on the first and
// last comment of every law input with comments; resolving twice writes
// nothing the second time, and a reply goes with its thread.
func TestCommentOpLaws(t *testing.T) {
	eachInput(t, sectionLawInputs(t), func(t *testing.T, in string, _ int) {
		ids := commentIDs(t, in)
		if len(ids) == 0 {
			return
		}
		for _, id := range []string{ids[0], ids[len(ids)-1]} {
			at := `"at":"comment:` + id + `"`
			for name, p := range map[string]string{
				"reply":   `[{"op":"comment.reply",` + at + `,"text":"A reply & more."}]`,
				"resolve": `[{"op":"comment.set_status",` + at + `,"status":"resolved"}]`,
				"reopen":  `[{"op":"comment.set_status",` + at + `,"status":"open"}]`,
				"delete":  `[{"op":"comment.delete",` + at + `}]`,
				"thread":  `[{"op":"comment.reply",` + at + `,"text":"r"},{"op":"comment.delete",` + at + `}]`,
			} {
				commentOpSuite("comment "+name+" "+id, p).Check(t, in)
			}
			dir := t.TempDir()
			once, twice := filepath.Join(dir, "once.docx"), filepath.Join(dir, "twice.docx")
			resolve := `[{"op":"comment.set_status",` + at + `,"status":"resolved"}]`
			err := runPlan(laws.Private(t, in), once, laws.Direct, resolve)
			var r refusal
			if errors.As(err, &r) {
				continue
			}
			if err != nil {
				t.Errorf("%s: %v", filepath.Base(in), err)
				continue
			}
			if err := runPlan(once, twice, laws.Direct, resolve); err != nil {
				t.Errorf("%s: resolve again: %v", filepath.Base(in), err)
			} else if string(mustRead(t, once)) != string(mustRead(t, twice)) {
				t.Errorf("%s: resolving a resolved comment wrote changes", filepath.Base(in))
			}
			thread := filepath.Join(dir, "thread.docx")
			if err := runPlan(laws.Private(t, in), thread, laws.Direct, `[{"op":"comment.reply",`+at+`,"text":"r"},{"op":"comment.delete",`+at+`}]`); err == nil {
				if got, want := len(commentIDs(t, thread)), len(commentIDs(t, in)); got >= want {
					t.Errorf("%s: deleting comment %s with its reply left %d comments of %d", filepath.Base(in), id, got, want)
				}
			}
		}
	})
}
