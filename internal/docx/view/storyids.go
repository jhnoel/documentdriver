package view

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// storyRels are the relationship types of the main document's other
// stories, in the order their paragraphs are counted (the edit layer's
// StoryParts).
var storyRels = []string{ctypes.RelHeader, ctypes.RelFooter, ctypes.RelFootnotes, ctypes.RelEndnotes, ctypes.RelComments}

// storyParaIDs gives the paragraphs of the main document's other stories
// (headers, footers, footnotes, endnotes, comments) the addresses the
// text and bookmark operations resolve them by: the stored w14:paraId in
// upper case, "~n" for the n-th paragraph of those stories sharing it
// (counted across the parts in storyRels order, each in document order).
// A paragraph whose address a body paragraph holds has none here: the
// body paragraph wins. A paragraph without a stored id has no address.
// Ord is -1: the markup gives such a paragraph no data-ord.
func (r *renderer) storyParaIDs(body map[*tree.Node]model.ParaID) map[*tree.Node]model.ParaID {
	out := map[*tree.Node]model.ParaID{}
	for _, p := range r.storyParagraphs(body) {
		out[p.Node] = model.ParaID{ID: p.ID, Ord: -1}
	}
	return out
}

// StoryParagraph is a paragraph of one of the main document's other
// stories with its address (storyParaIDs) and its part.
type StoryParagraph struct {
	Node     *tree.Node
	ID, Part string
	// Comment is set for a paragraph of the comments part.
	Comment bool
}

// StoryParagraphs lists the addressed paragraphs of d's other stories
// (headers, footers, footnotes, endnotes, comments), in the order their
// addresses are counted.
func StoryParagraphs(d *model.Document) []StoryParagraph {
	r := &renderer{doc: d, pkg: d.Pkg, main: d.XML, parsed: map[string]*model.XML{}}
	return r.storyParagraphs(d.ParaIDs().IDs)
}

// storyParagraphs lists addressed paragraphs of other stories excluding body addresses.
func (r *renderer) storyParagraphs(body map[*tree.Node]model.ParaID) []StoryParagraph {
	taken := make(map[string]bool, len(body))
	for _, id := range body {
		taken[id.ID] = true
	}
	main := r.pkg.DocumentPartName()
	rels := r.pkg.Rels(main)
	var parts []string
	comments := map[string]bool{}
	for _, kind := range storyRels {
		for _, rel := range rels {
			if rel.External() || !rel.HasTarget || !ctypes.Is(rel.Type, kind) {
				continue
			}
			name := model.ResolveRelTarget(main, rel.Target)
			if name != main && !slices.Contains(parts, name) && r.pkg.Has(name) {
				parts = append(parts, name)
				comments[name] = kind == ctypes.RelComments
			}
		}
	}
	var out []StoryParagraph
	seen := map[string]int{}
	for _, name := range parts {
		x, ok := r.part(name)
		if !ok || x.Root() == nil {
			continue
		}
		x.Root().Walk(func(n *tree.Node) bool {
			if !model.IsW(n, "p") {
				return true
			}
			v, ok := model.Attr(n, "paraId")
			if !ok {
				return true
			}
			base := strings.ToUpper(v)
			seen[base]++
			addr := base
			if k := seen[base]; k > 1 {
				addr = base + "~" + strconv.Itoa(k)
			}
			if !taken[addr] {
				out = append(out, StoryParagraph{Node: n, ID: addr, Part: name, Comment: comments[name]})
			}
			return true
		})
	}
	return out
}

// storyRegion matches the markup of the other stories: a header, a
// footer, the notes.
var storyRegion = regexp.MustCompile(`(?s)<header[ >].*?</header>|<footer[ >].*?</footer>|<aside data-notes=.*?</aside>`)

// storyAddress matches what storyParaIDs adds to that markup: a
// paragraph's id and the paragraph prefix of an equation's data-ref.
var storyAddress = regexp.MustCompile(` id="[0-9A-F]{8}(?:~[0-9]+)?"|(data-ref=")[0-9A-F]{8}(?:~[0-9]+)?/`)

// WithoutStoryAddresses removes from markup the addresses of other
// stories' paragraphs (storyParaIDs), which docxdriver's markup does not
// carry: the oracle comparison's normalisation of the Go side (known
// difference 16 in tools/oracle/baseline/README.md).
func WithoutStoryAddresses(markup string) string {
	return storyRegion.ReplaceAllStringFunc(markup, func(region string) string {
		return storyAddress.ReplaceAllString(region, "$1")
	})
}
