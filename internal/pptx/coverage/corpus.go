package coverage

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Counts are occurrence counts of universe paths in a corpus.
type Counts struct {
	// Corpus describes what was counted.
	Corpus string `json:"corpus"`
	Decks  int    `json:"decks"`
	// Missing are listed decks not found on disk; Unreadable failed to
	// open or parse.
	Missing    int `json:"missing"`
	Unreadable int `json:"unreadable"`
	Slides     int `json:"slides"`
	Objects    int `json:"objects"`
	// Paths counts each universe path: elements by occurrence, attributes
	// by occurrence. Paths under a factored type count under its
	// pseudo-root, summed over every position.
	Paths map[string]int64 `json:"paths"`
	// ObjectPaths are the counts found in objects (shapes, connectors,
	// groups, pictures, graphic frames, content parts), without
	// slide-level context, layouts, masters, presentation and themes.
	ObjectPaths map[string]int64 `json:"object_paths"`
	// Values counts the values of enumerated attributes, by path.
	Values map[string]map[string]int64 `json:"values"`
	// Outside counts elements and attributes found where the universe has
	// no item: extensions in other namespaces, mc:AlternateContent
	// wrappers, content below recursive items and schema violations. Only
	// the outermost unknown element of a subtree is counted.
	Outside map[string]int64 `json:"outside"`
}

// NativeDecks lists the PowerPoint-native decks of the manifest, resolved
// under corpus/repos.
func NativeDecks(manifest, corpus string) ([]string, error) {
	f, err := os.Open(manifest) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e struct {
			Source string `json:"source"`
			Path   string `json:"path"`
			Native bool   `json:"powerpoint_native"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		if e.Native {
			out = append(out, filepath.Join(corpus, "repos", e.Source, e.Path))
		}
	}
	sort.Strings(out)
	return out, sc.Err()
}

// counter walks parts against the universe.
type counter struct {
	items map[string]*Item
	c     *Counts
	obj   bool // inside an object
	// objOnly restricts enumerated values and outside counts to objects
	// too (Paths and ObjectPaths are always both kept): a profile of
	// objects must not count a theme's or a master's values.
	objOnly bool
}

// outside counts an element or attribute the universe has no item for.
func (k *counter) outside(path string) {
	if k.objOnly && !k.obj {
		return
	}
	k.c.Outside[path]++
}

// count increments the path's total and object counts.
func (k *counter) count(path string) {
	k.c.Paths[path]++
	if k.obj {
		k.c.ObjectPaths[path]++
	}
}

// CountCorpus counts universe paths in the given decks: every object on
// every slide (objects in groups included, the mc:AlternateContent branch
// the read model projects), the slide-level items of slides, the colour
// map overrides of layouts, the colour map and text styles of masters,
// the slide size and default text style of the presentation, and the
// colour, font and format schemes of themes. Layout and master
// placeholders are not counted as objects.
func CountCorpus(u *Universe, decks []string, label string) (*Counts, error) {
	return countDecks(u, decks, label, false)
}

// countDecks counts universe paths across the given decks.
func countDecks(u *Universe, decks []string, label string, objOnly bool) (*Counts, error) {
	k := &counter{objOnly: objOnly, items: u.ByPath(), c: &Counts{Corpus: label, Paths: map[string]int64{}, ObjectPaths: map[string]int64{}, Values: map[string]map[string]int64{}, Outside: map[string]int64{}}}
	for _, d := range decks {
		if _, err := os.Stat(d); err != nil {
			k.c.Missing++
			continue
		}
		if err := k.deck(d); err != nil {
			k.c.Unreadable++
			continue
		}
		k.c.Decks++
	}
	return k.c, nil
}

// deck counts universe paths in the deck file at path.
func (k *counter) deck(path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	type parsed struct {
		name string
		doc  *tree.Document
	}
	var docs []parsed
	for _, f := range zr.File {
		n := f.Name
		if !strings.HasPrefix(n, "ppt/") || !strings.HasSuffix(n, ".xml") || strings.Contains(n, "_rels/") {
			continue
		}
		dir := filepath.Dir(n)
		switch dir {
		case "ppt", "ppt/slides", "ppt/slideLayouts", "ppt/slideMasters", "ppt/theme":
		default:
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		doc, err := tree.Parse(b)
		if err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
		docs = append(docs, parsed{n, doc})
	}
	// Count only after every part parsed, so an unreadable deck adds
	// nothing.
	for _, p := range docs {
		root := p.doc.Root()
		if root == nil {
			continue
		}
		switch {
		case derive.Is(root, derive.PML, "sld"):
			k.c.Slides++
			k.walk(root, "")
		case derive.Is(root, derive.PML, "sldLayout"):
			k.sub(root, "p:sldLayout", "clrMapOvr")
		case derive.Is(root, derive.PML, "sldMaster"):
			k.sub(root, "p:sldMaster", "clrMap")
			k.sub(root, "p:sldMaster", "txStyles")
		case derive.Is(root, derive.PML, "presentation"):
			k.sub(root, "p:presentation", "sldSz")
			k.sub(root, "p:presentation", "defaultTextStyle")
		case derive.Is(root, derive.DML, "theme"):
			te := derive.Child(root, derive.DML, "themeElements")
			k.sub(te, "a:theme/a:themeElements", "clrScheme")
			k.sub(te, "a:theme/a:themeElements", "fontScheme")
			k.sub(te, "a:theme/a:themeElements", "fmtScheme")
		}
	}
	return nil
}

// sub counts the first child named local of parent (in parent's own
// namespace family) as the root parentPath/prefix:local.
func (k *counter) sub(parent *tree.Node, parentPath, local string) {
	if parent == nil {
		return
	}
	for c := range parent.ElementsSeq() {
		if c.Local() == local && c.Space() == parent.Space() {
			k.walk(c, parentPath)
			return
		}
	}
}

// qname is an element's or attribute's name with the universe's prefix
// for its namespace (strict namespaces fold onto transitional ones), or
// its own prefixed name for namespaces outside the universe.
func qname(space, local, written string) string {
	if p, ok := canonicalPrefix(space); ok {
		return p + ":" + local
	}
	return written
}

var strictPrefix = map[string]string{
	"http://purl.oclc.org/ooxml/presentationml/main":          "p",
	"http://purl.oclc.org/ooxml/drawingml/main":               "a",
	"http://purl.oclc.org/ooxml/officeDocument/relationships": "r",
	"http://purl.oclc.org/ooxml/officeDocument/sharedTypes":   "s",
}

// canonicalPrefix returns the universe prefix for a namespace URI.
func canonicalPrefix(uri string) (string, bool) {
	if p, ok := nsPrefix[uri]; ok {
		return p, true
	}
	p, ok := strictPrefix[uri]
	return p, ok
}

// walk counts element e found below parentPath, whose own path is
// parentPath/qname (or qname alone when parentPath is "").
func (k *counter) walk(e *tree.Node, parentPath string) {
	if derive.Is(e, derive.MC, "AlternateContent") {
		k.outside(join(parentPath, "mc:AlternateContent"))
		if b := derive.SelectBranch(e); b.Node != nil {
			for c := range b.Node.ElementsSeq() {
				k.walk(c, parentPath)
			}
		}
		return
	}
	name := qname(e.Space(), e.Local(), e.Name())
	path := join(parentPath, name)
	it := k.items[path]
	if it == nil {
		k.outside(path)
		return
	}
	if parentPath == "" && objectElems[path] {
		k.c.Objects++
		was := k.obj
		k.obj = true
		defer func() { k.obj = was }()
	}
	k.count(path)
	if it.Object {
		// An object in a group or shape tree: counted again as a root.
		k.walk(e, "")
		return
	}
	if it.Recursive || it.Stop != "" {
		return
	}
	base := path
	if it.Factored != "" {
		base = it.Factored
		k.count(base)
	}
	for _, a := range e.Attrs() {
		if a.Name == "xmlns" || strings.HasPrefix(a.Name, "xmlns:") {
			continue
		}
		an := a.Name
		if pfx, local, ok := strings.Cut(a.Name, ":"); ok {
			uri, _ := e.LookupPrefix(pfx)
			an = qname(uri, local, a.Name)
		}
		ap := base + "/@" + an
		ai := k.items[ap]
		if ai == nil {
			k.outside(ap)
			continue
		}
		k.count(ap)
		if ai.Enum != "" && (k.obj || !k.objOnly) {
			m := k.c.Values[ap]
			if m == nil {
				m = map[string]int64{}
				k.c.Values[ap] = m
			}
			m[a.Value]++
		}
	}
	for c := range e.ElementsSeq() {
		if anyItem := k.items[base+"/*"]; anyItem != nil && k.items[join(base, qname(c.Space(), c.Local(), c.Name()))] == nil {
			// Content of an xsd:any (extension bodies, graphic data).
			k.count(base + "/*")
			continue
		}
		k.walk(c, base)
	}
}

// join appends name to parent with a slash.
func join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// LoadCounts reads a counts file; a missing file gives empty counts.
func LoadCounts(path string) (*Counts, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if os.IsNotExist(err) {
		return &Counts{Paths: map[string]int64{}, ObjectPaths: map[string]int64{}, Values: map[string]map[string]int64{}, Outside: map[string]int64{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Counts
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
