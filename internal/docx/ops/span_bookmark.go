package ops

import (
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/span"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Bookmarks (bookmarks.rs): w:bookmarkStart and w:bookmarkEnd pairs
// matched by w:id, in every story. docxdriver edits body bookmarks only;
// here a bookmark in a header, footer, note or comment is edited in its
// part, as read lists it. Word does not track bookmark changes, so every
// operation applies directly in both modes.

// knownBookmark is one w:bookmarkStart of some story.
type knownBookmark struct {
	name, id, part string
	start          *tree.Node
}

// bookmarks returns every bookmark start of every story: the main
// document first, then the other stories (bookmarks.rs all_known).
func (env *Env) bookmarks() ([]knownBookmark, error) {
	parts, err := env.WS.StoryParts()
	if err != nil {
		return nil, err
	}
	var out []knownBookmark
	for _, part := range parts {
		doc, err := env.WS.Tree(part)
		if err != nil || doc.Root() == nil {
			continue // an unreadable story holds no bookmark an op can name
		}
		doc.Root().Walk(func(n *tree.Node) bool {
			if model.IsW(n, "bookmarkStart") {
				out = append(out, knownBookmark{model.AttrOr(n, "name"), model.AttrOr(n, "id"), part, n})
			}
			return true
		})
	}
	return out, nil
}

// levenshtein is the edit distance between a and b, in characters.
func levenshtein(a, b string) int {
	bs := []rune(b)
	row := make([]int, len(bs)+1)
	for j := range row {
		row[j] = j
	}
	for i, ca := range []rune(a) {
		prev := row[0]
		row[0] = i + 1
		for j, cb := range bs {
			sub := prev
			if ca != cb {
				sub++
			}
			prev = row[j+1]
			row[j+1] = min(sub, row[j]+1, row[j+1]+1)
		}
	}
	return row[len(bs)]
}

// closeNames are up to five names close to want, closest first; hidden
// names only for a hidden query (bookmarks.rs close_matches).
func closeNames(known []knownBookmark, want string) []string {
	lw := strings.ToLower(want)
	limit := max(utf8.RuneCountInString(want)/3, 2)
	type scored struct {
		d    int
		name string
	}
	var s []scored
	for _, k := range known {
		if strings.HasPrefix(k.name, "_") && !strings.HasPrefix(want, "_") {
			continue
		}
		l := strings.ToLower(k.name)
		d := levenshtein(lw, l)
		related := strings.Contains(l, lw) || strings.Contains(lw, l)
		if d <= limit || (related && l != "") {
			s = append(s, scored{d, k.name})
		}
	}
	sort.Slice(s, func(i, j int) bool { return s[i].d < s[j].d || s[i].d == s[j].d && s[i].name < s[j].name })
	var out []string
	for _, x := range s {
		if len(out) == 5 {
			break
		}
		if len(out) == 0 || out[len(out)-1] != x.name {
			out = append(out, x.name)
		}
	}
	return out
}

// findBookmark returns the bookmark named name: an exact match first, then
// one matching ignoring case (Word treats names case-insensitively),
// the main document before the other stories. No bookmark is not_found
// at at, with the close names.
func (env *Env) findBookmark(name string) (knownBookmark, []knownBookmark, error) {
	known, err := env.bookmarks()
	if err != nil {
		return knownBookmark{}, nil, err
	}
	for _, k := range known {
		if k.name == name {
			return k, known, nil
		}
	}
	lower := strings.ToLower(name)
	for _, k := range known {
		if strings.ToLower(k.name) == lower {
			return k, known, nil
		}
	}
	hint := ""
	if m := closeNames(known, name); len(m) > 0 {
		q := make([]string, len(m))
		for i, n := range m {
			q[i] = strconv.Quote(n)
		}
		hint = "close matches: " + strings.Join(q, ", ")
	} else {
		visible := 0
		for _, k := range known {
			if !strings.HasPrefix(k.name, "_") {
				visible++
			}
		}
		hint = "the document has " + strconv.Itoa(visible) + " visible bookmark"
		if visible != 1 {
			hint += "s"
		}
	}
	return knownBookmark{}, known, reject("not_found", "at", "no bookmark named %s; %s — read kind=\"bookmarks\" lists every bookmark", strconv.Quote(name), hint)
}

// unusedName refuses name when another bookmark than except (nil: any)
// uses it, ignoring case (bookmarks.rs ensure_unused).
func unusedName(known []knownBookmark, name, field string, except *tree.Node) error {
	lower := strings.ToLower(name)
	for _, k := range known {
		if strings.ToLower(k.name) != lower || k.start == except {
			continue
		}
		where := ""
		if strings.TrimPrefix(k.part, "/") != "word/document.xml" {
			where = " in " + strings.TrimPrefix(k.part, "/")
		}
		return reject("operation_failed", field, "bookmark name %s is already used by bookmark %s%s — names are unique in the document, ignoring case; choose another name, or bookmark.move / bookmark.rename / bookmark.delete the existing one", strconv.Quote(name), strconv.Quote(k.name), where)
	}
	return nil
}

// storyHandle opens a region over the story of part that holds n (the
// body, or the part's root).
func (env *Env) storyHandle(part string) (*core.Handle, error) {
	doc, err := env.WS.Tree(part)
	if err != nil {
		return nil, err
	}
	x := &model.XML{Doc: doc}
	root := x.Body()
	if root == nil {
		root = x.Root()
	}
	if root == nil {
		return nil, errors.New("bookmark: the part has no root")
	}
	return env.Story(part, root), nil
}

// removeBookmark removes the bookmark's start and every end with its id
// in its part.
func removeBookmark(h *core.Handle, b knownBookmark) error {
	root := b.start
	for root.Parent() != nil && root.Parent().Kind() != tree.DocumentNode {
		root = root.Parent()
	}
	ends := model.Descendants(root, func(n *tree.Node) bool {
		return model.IsW(n, "bookmarkEnd") && model.AttrOr(n, "id") == b.id
	})
	for _, n := range append(ends, b.start) {
		if err := h.RemoveNode(n); err != nil {
			return err
		}
	}
	return nil
}

// zeroWidth are the markers a bookmark marker may climb past without
// changing the text it bookmarks.
var zeroWidth = []string{"bookmarkStart", "bookmarkEnd", "commentRangeStart", "commentRangeEnd",
	"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd", "permStart", "permEnd", "proofErr"}

// textlessSibling is a sibling that occupies no text: properties and the
// zero-width markers (bookmarks.rs is_textless).
func textlessSibling(n *tree.Node) bool {
	if !model.IsElement(n) {
		return true
	}
	l := n.Local()
	return strings.HasSuffix(l, "Pr") || l == "fldData" || slices.ContainsFunc(zeroWidth, func(m string) bool { return model.IsW(n, m) })
}

// siblingSide reports whether some sibling of n on one side carries
// content: the nodes before n when start is set, the nodes after it
// otherwise. A marker may sit beside n at this level only when the
// whole side is properties and markers. It follows the tree's sibling
// links: no slice of the parent's children is built.
func siblingSide(n *tree.Node, start bool) bool {
	if start {
		for s := n.PrevSibling(); s != nil; s = s.PrevSibling() {
			if !textlessSibling(s) {
				return true
			}
		}
	} else {
		for s := n.NextSibling(); s != nil; s = s.NextSibling() {
			if !textlessSibling(s) {
				return true
			}
		}
	}
	return false
}

// markerAnchor is where a marker goes for unit n at a selection edge
// (bookmarks.rs marker_anchor): n, or the outermost wrapper n begins
// (start) or ends, below paragraph p. Markup-compatibility branches are
// never entered: the marker goes beside the whole mc:AlternateContent.
func markerAnchor(p, n *tree.Node, start bool) *tree.Node {
	var path []*tree.Node
	for a := n; a != nil && a != p; a = a.Parent() {
		path = append(path, a)
	}
	if slices.ContainsFunc(path, isAlternate) {
		return path[len(path)-1]
	}
	for n.Parent() != nil && n.Parent() != p {
		if !siblingSide(n, start) {
			n = n.Parent()
			continue
		}
		break
	}
	return n
}

// placeBookmark puts a bookmark's markers (w:id id, w:name name) in
// paragraph p of part, around the whole paragraph or the select span
// (bookmarks.rs place).
func (env *Env) placeBookmark(part string, p *tree.Node, pid, id, name string, sel *string, occurrence *uint32) error {
	h, err := env.WS.Core().Range(part, "paragraph "+pid, p, p)
	if err != nil {
		return err
	}
	marker := func(at *tree.Node, start bool) (*tree.Node, error) {
		s := `<w:bookmarkEnd w:id="` + string(tree.EscapeText(id)) + `"/>`
		if start {
			s = `<w:bookmarkStart w:id="` + string(tree.EscapeText(id)) + `" w:name="` + string(tree.EscapeText(name)) + `"/>`
		}
		nodes, err := h.Workspace().Parse(at, []byte(s), map[string]string{"w": model.NSW})
		if err != nil || len(nodes) != 1 {
			return nil, errors.Join(errors.New("bookmark: a marker does not parse"), err)
		}
		return nodes[0], nil
	}
	if sel == nil {
		var first *tree.Node
		for _, c := range p.Elements() {
			if !model.IsW(c, "pPr") {
				first = c
				break
			}
		}
		st, err := marker(p, true)
		if err != nil {
			return err
		}
		if err := h.PlaceAt(p, st, first); err != nil {
			return err
		}
		en, err := marker(p, false)
		if err != nil {
			return err
		}
		return h.PlaceAt(p, en, nil)
	}
	cs, ce, _, err := selectOne(p, pid, *sel, occurrence)
	if err != nil {
		return err
	}
	s, err := span.Read(p).Select(model.Current, cs, ce)
	if err != nil {
		return err
	}
	iso, err := span.Isolate(h, s, func() (string, error) { return env.WS.RevisionID(part) })
	if err != nil {
		if errors.Is(err, span.ErrRange) {
			return err
		}
		return reject("operation_failed", "select", "blocked in paragraph %s: the selection cannot be isolated: %s", pid, err.Error())
	}
	if len(iso.Units) == 0 {
		return reject("operation_failed", "select", "blocked in paragraph %s: the selection covers no text", pid)
	}
	first := markerAnchor(p, iso.Units[0], true)
	last := markerAnchor(p, iso.Units[len(iso.Units)-1], false)
	st, err := marker(first.Parent(), true)
	if err != nil {
		return err
	}
	if err := h.PlaceAt(first.Parent(), st, first); err != nil {
		return err
	}
	en, err := marker(last.Parent(), false)
	if err != nil {
		return err
	}
	return h.PlaceAt(last.Parent(), en, model.NextElement(last))
}
