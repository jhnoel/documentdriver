package ops

import (
	"errors"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Header and footer stories (headers.rs). A section's header or footer of
// a kind (default, first, even) is the part its w:sectPr references with
// a w:headerReference or w:footerReference of that w:type; the story is
// that part's root, opened as a region. A new story is a new part
// (word/headerN.xml), its content type, a relationship from the main
// document and the reference, placed among the section's references.

// chrome names one story kind: hdr or ftr.
type chrome struct {
	scope string // "hdr" or "ftr"
	ref   string // headerReference or footerReference
	word  string // header or footer
	rel   string
	ct    string
}

var (
	headerStory = chrome{"hdr", "headerReference", "header", ctypes.RelHeader, ctypes.CTWordprocessingmlHeader}
	footerStory = chrome{"ftr", "footerReference", "footer", ctypes.RelFooter, ctypes.CTWordprocessingmlFooter}
)

// kindOf is a story kind's w:type value; nil is the default.
func kindOf(k *ChromeKind) string {
	if k == nil {
		return "default"
	}
	return string(*k)
}

// storySection resolves a header or footer op's section (the section
// ops' resolver, section_target.go) to its w:sectPr, created for the
// body's implicit final section, and its 1-based number.
func (env *Env) storySection(at *SectionAddress) (*tree.Node, int, error) {
	t, err := env.section(at)
	if err != nil {
		return nil, 0, err
	}
	sect, err := t.ensure()
	return sect, t.n, err
}

// sectionHandle opens a region over w:sectPr sect (a unit of the main
// document).
func (env *Env) sectionHandle(sect *tree.Node) (*core.Handle, error) {
	part := env.WS.DocumentPart()
	return env.WS.Core().Range(part, "section", sect, sect)
}

// storyRef finds sect's explicit reference of story c and kind.
func storyRef(sect *tree.Node, c chrome, kind string) *tree.Node {
	for _, ch := range sect.Elements() {
		if model.IsW(ch, c.ref) {
			t, ok := model.Attr(ch, "type")
			if !ok {
				t = "default"
			}
			if t == kind {
				return ch
			}
		}
	}
	return nil
}

// storyPart resolves reference ref to its part, or "" when it names no
// part of the package.
func (env *Env) storyPart(ref *tree.Node) (string, error) {
	rels, err := env.WS.Package().Rels(env.WS.DocumentPart())
	if err != nil {
		return "", err
	}
	rel, ok := rels.Get(model.AttrOr(ref, "id"))
	if !ok || rel.External() {
		return "", nil
	}
	name, err := rels.TargetPart(rel)
	if err != nil || !env.WS.Package().Has(name) {
		return "", nil //nolint:nilerr // a dangling target names no part
	}
	return name, nil
}

// storyRoot returns part's working tree root.
func (env *Env) storyRoot(part string) (*tree.Node, error) {
	doc, err := env.WS.Tree(part)
	if err != nil {
		return nil, err
	}
	if doc.Root() == nil {
		return nil, errors.New("ops: " + part + " has no root element")
	}
	return doc.Root(), nil
}

// newStory adds an empty story of c for sect's kind: a new part, its
// relationship, and the reference placed after sect's last reference
// (references lead CT_SectPr). It returns the part.
func (env *Env) newStory(h *core.Handle, sect *tree.Node, c chrome, kind string) (string, error) {
	name := env.WS.PartName("word", c.word, ".xml")
	empty := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<w:` + c.scope + ` xmlns:w="` + model.NSW + `" xmlns:r="` + core.REL.URIs[0] + `"/>`
	rid, err := env.WS.AddPart(c.rel, c.ct, name, empty)
	if err != nil {
		return "", err
	}
	if err := env.declareRelPrefix(env.WS.DocumentPart()); err != nil {
		return "", err
	}
	nodes, err := h.Workspace().Parse(sect, []byte(`<w:`+c.ref+` w:type="`+kind+`" r:id="`+rid+`"/>`),
		map[string]string{"w": model.NSW, "r": core.REL.URIs[0]})
	if err != nil || len(nodes) != 1 {
		return "", errors.Join(errors.New("ops: a story reference did not parse"), err)
	}
	var after *tree.Node
	for _, ch := range sect.Elements() {
		if model.IsW(ch, "headerReference") || model.IsW(ch, "footerReference") {
			continue
		}
		after = ch
		break
	}
	if err := h.PlaceAt(sect, nodes[0], after); err != nil {
		return "", err
	}
	return name, nil
}

// storyContentRefs are the addresses read gives the equations and
// placeholders of a story without paragraph addresses (view's storyRef):
// equation:N and opaque:N, counted across the story in document order.
func storyContentRefs(root *tree.Node) (eqs map[string]*tree.Node, phs map[string]model.Placeholder, order []string, blocked string) {
	eqs, phs = map[string]*tree.Node{}, map[string]model.Placeholder{}
	ne, np := 0, 0
	for _, p := range model.Descendants(root, func(n *tree.Node) bool { return model.IsW(n, "p") }) {
		for _, e := range model.CurrentEquations(p) {
			ne++
			ref := "equation:" + strconv.Itoa(ne)
			eqs[ref] = e
			order = append(order, ref)
		}
		list, b := model.Placeholders(p)
		if b != "" {
			blocked = b
		}
		for _, ph := range list {
			np++
			ref := "opaque:" + strconv.Itoa(np)
			phs[ref] = ph
			order = append(order, ref)
		}
	}
	return eqs, phs, order, blocked
}

// settingsOn reports whether the settings part turns w:local on.
func (env *Env) settingsOn(local string) bool {
	part, ok := env.settingsPart()
	if !ok {
		return false
	}
	root, err := env.storyRoot(part)
	if err != nil {
		return false
	}
	c := model.ChildW(root, local)
	if c == nil {
		return false
	}
	v, _ := model.Attr(c, "val")
	return v != "0" && v != "false" && v != "off"
}

// settingsPart is the main document's settings part.
func (env *Env) settingsPart() (string, bool) {
	rels, err := env.WS.Package().Rels(env.WS.DocumentPart())
	if err != nil {
		return "", false
	}
	for _, r := range rels.All() {
		if !r.External() && ctypes.Is(r.Type, ctypes.RelSettings) {
			if name, err := rels.TargetPart(r); err == nil && env.WS.Package().Has(name) {
				return name, true
			}
		}
	}
	return "", false
}

// The track-mode refusals of the header and footer operations: Word
// tracks a story's paragraphs, never its reference, a setting or other
// blocks.
var (
	storySetGuards = []string{"refused_in_mode: kind even while even and odd headers are off",
		"a story holding a table or content control", "keeping a placeholder"}
	storyDeleteGuards = []string{"refused_in_mode: a section after the first", "a story holding a table or content control"}
)
