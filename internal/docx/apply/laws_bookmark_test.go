package apply

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// listedBookmark is one entry of read --kind bookmarks.
type listedBookmark struct {
	Name    string `json:"name"`
	OOXMLID string `json:"ooxml_id"`
	Part    string `json:"part"`
	Start   string `json:"start"`
	Text    string `json:"text"`
}

// listBookmarks reads the bookmarks listing of the document at path.
func listBookmarks(path string) ([]listedBookmark, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: a law input
	if err != nil {
		return nil, err
	}
	res, err := read.Read(b, "bookmarks", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		return nil, err
	}
	s, err := read.Encode(res.Object(), false)
	if err != nil {
		return nil, err
	}
	var out struct {
		Bookmarks []listedBookmark `json:"bookmarks"`
	}
	return out.Bookmarks, json.Unmarshal([]byte(s), &out)
}

// bookmarkRenameSuite is bookmark.rename's value suite: a bookmark's name,
// by part and w:id.
func bookmarkRenameSuite() *laws.Suite[string] {
	read := func(path string) ([]laws.Value[string], error) {
		bs, err := listBookmarks(path)
		if err != nil {
			return nil, err
		}
		// A rename names its bookmark by name: targets are the visible
		// bookmarks whose name no other uses, ignoring case (a hidden one
		// cannot be given its own name back).
		count := map[string]int{}
		for _, b := range bs {
			count[strings.ToLower(b.Name)]++
		}
		var out []laws.Value[string]
		for _, b := range bs {
			if strings.HasPrefix(b.Name, "_") || count[strings.ToLower(b.Name)] > 1 {
				continue
			}
			out = append(out, laws.Value[string]{Target: laws.Target{Part: b.Part, Addr: b.Part + "#" + b.OOXMLID}, V: b.Name})
		}
		return out, nil
	}
	return &laws.Suite[string]{
		Name: "bookmark.rename",
		Read: read,
		Show: func(v string) string { return v },
		Set:  func(t laws.Target, v string) laws.Op { return textSet{t.Addr, v} },
		Vary: func(rng *rand.Rand, _ laws.Target, cur string) string {
			return "Renamed" + string(rune('A'+rng.IntN(26))) + strings.TrimLeft(cur, "_")
		},
		Apply: func(src, dst string, opsIn []laws.Op) error {
			vals, err := read(src)
			if err != nil {
				return err
			}
			cur := map[string]string{}
			for _, v := range vals {
				cur[v.Target.Addr] = v.V
			}
			var list []map[string]any
			for _, o := range opsIn {
				s := o.(textSet)
				if s.at == "" {
					list = append(list, map[string]any{"op": "bookmark.rename", "at": "bookmark:NoSuchBookmark", "name": "X"})
					continue
				}
				list = append(list, map[string]any{"op": "bookmark.rename", "at": "bookmark:" + cur[s.at], "name": s.text})
				cur[s.at] = s.text
			}
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			return runInPlace(src, dst, laws.Direct, string(b))
		},
		Refused: func(err error) bool {
			var r refusal
			return errors.As(err, &r)
		},
		Units:  docxedit.Units(),
		Owns:   ownsBookmark,
		Fail:   func() laws.Op { return textSet{} },
		Budget: 6,
	}
}

// TestBookmarkLaws runs the bookmark operations' laws: bookmark.rename's
// value laws, and the change-mode law (bookmarks are not tracked: both
// modes write the same) for adding one around a paragraph and a word,
// renaming, moving and deleting the document's first bookmark.
func TestBookmarkLaws(t *testing.T) {
	// The span law inputs, each with its paragraph ids stored.
	rep := reportInputs(t, spanInputs(t), func(t *testing.T, in string, i int, rep *laws.Report) {
		in = storedIDs(t, in)
		bookmarkRenameSuite().Check(t, in, uint64(i), rep)
		plans := map[string]string{}
		ids := bodyParagraphIDs(t, in)
		if len(ids) > 0 {
			plans["add paragraph"] = `[{"op":"bookmark.add","at":"` + ids[len(ids)-1] + `","name":"LawWhole"}]`
		}
		if pid, word := uniqueWord(t, in); word != "" {
			plans["add word"] = `[{"op":"bookmark.add","at":"` + pid + `","name":"LawWord","select":"` + word + `"}]`
			if bs, err := listBookmarks(in); err == nil && len(bs) > 0 {
				at := `"at":"bookmark:` + bs[0].Name + `"`
				plans["rename"] = `[{"op":"bookmark.rename",` + at + `,"name":"LawRenamed"}]`
				plans["delete"] = `[{"op":"bookmark.delete",` + at + `}]`
				plans["move"] = `[{"op":"bookmark.move",` + at + `,"to":"` + pid + `","select":"` + word + `"}]`
			}
		}
		for name, ops := range plans {
			spanSuite("bookmark "+name, ops, laws.Same).Check(t, in)
		}
	})
	t.Logf("bookmark.rename: %s", rep)
}

// ownsBookmark reports whether unit e's own content (nested units
// masked) holds the start of the bookmark t names (part#id).
func ownsBookmark(t laws.Target, e *tree.Node) bool {
	_, id, _ := strings.Cut(t.Addr, "#")
	units := docxedit.Units()
	found := false
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, c := range n.Children() {
			if found || c.Kind() != tree.ElementNode || units.IsUnit(c) {
				continue
			}
			if c.Local() == "bookmarkStart" && model.AttrOr(c, "id") == id {
				found = true
				return
			}
			walk(c)
		}
	}
	walk(e)
	return found
}
