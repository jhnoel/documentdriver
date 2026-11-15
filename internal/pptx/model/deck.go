package model

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Deck gives read access to a presentation's slides.
type Deck struct {
	pkg *opc.Package
	// PresPart is the presentation part name.
	PresPart string
	Pres     *tree.Document
	// W, H are the slide size in EMU (p:sldSz); zero if not derived, with
	// the reason in Omitted.
	W, H float64
	// Omitted lists presentation-level omissions; every slide read
	// repeats them.
	Omitted []Omission
	// Slides are the slide part names in presentation order.
	Slides []string

	media map[string]*MediaFacts // by part name, once read
}

// mediaFacts reads a media part's content type and the facts its bytes
// give, once per part.
func (d *Deck) mediaFacts(name string) (*MediaFacts, error) {
	if m, ok := d.media[name]; ok {
		return m, nil
	}
	pt, err := d.pkg.Part(name)
	if err != nil {
		return nil, err
	}
	b, err := pt.Bytes()
	if err != nil {
		return nil, err
	}
	ct, err := pt.ContentType()
	if err != nil {
		return nil, err
	}
	m := &MediaFacts{Type: ct, Media: derive.ReadMedia(b)}
	if d.media == nil {
		d.media = map[string]*MediaFacts{}
	}
	d.media[name] = m
	return m, nil
}

// relType reports whether a relationship's type t is want (a ctypes.Rel
// constant) in the transitional or the strict vocabulary.
func relType(t, want string) bool {
	return ctypes.Is(t, want)
}

// OpenDeck locates the presentation part and reads the slide list.
func OpenDeck(p *opc.Package) (*Deck, error) {
	rels, err := p.Rels("/")
	if err != nil {
		return nil, err
	}
	d := &Deck{pkg: p}
	for _, r := range rels.All() {
		if relType(r.Type, ctypes.RelOfficeDocument) && !r.External() {
			if d.PresPart, err = rels.TargetPart(r); err != nil {
				return nil, err
			}
			break
		}
	}
	if d.PresPart == "" {
		return nil, fmt.Errorf("model: no presentation part")
	}
	pt, err := p.Part(d.PresPart)
	if err != nil {
		return nil, err
	}
	if d.Pres, err = pt.Tree(); err != nil {
		return nil, err
	}
	root := d.Pres.Root()
	if sz := derive.Child(root, derive.PML, "sldSz"); sz == nil {
		d.Omitted = append(d.Omitted, Omission{"size", "presentation has no p:sldSz"})
	} else {
		cx, err1 := derive.AttrEMU(sz, "cx")
		cy, err2 := derive.AttrEMU(sz, "cy")
		if err := errors.Join(err1, err2); err != nil {
			d.Omitted = append(d.Omitted, Omission{"size", err.Error()})
		} else {
			d.W, d.H = float64(cx), float64(cy)
		}
	}
	prels, err := p.Rels(d.PresPart)
	if err != nil {
		return nil, err
	}
	lst := derive.Child(root, derive.PML, "sldIdLst")
	if lst == nil {
		return d, nil
	}
	for id := range lst.ElementsSeq() {
		rid, ok := derive.AttrNS(id, derive.REL, "id")
		if !ok {
			continue
		}
		r, ok := prels.Get(rid)
		if !ok {
			continue
		}
		name, err := prels.TargetPart(r)
		if err != nil || !p.Has(name) {
			continue
		}
		d.Slides = append(d.Slides, name)
	}
	return d, nil
}

// ShortName is a slide part's short name: /ppt/slides/slide3.xml -> slide3.
func ShortName(part string) string {
	return strings.TrimSuffix(path.Base(part), ".xml")
}

// ResolveSlide maps a slide argument (slide3, slide3.xml,
// ppt/slides/slide3.xml or /ppt/slides/slide3.xml) to a part name.
func (d *Deck) ResolveSlide(arg string) (string, error) {
	cands := []string{arg, "/" + arg}
	if !strings.Contains(arg, "/") {
		base := arg
		if !strings.HasSuffix(base, ".xml") {
			base += ".xml"
		}
		cands = append(cands, path.Join(path.Dir(firstOr(d.Slides, "/ppt/slides/x.xml")), base), "/ppt/slides/"+base)
	}
	for _, c := range cands {
		for _, s := range d.Slides {
			if strings.EqualFold(c, s) {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("no slide %q in the presentation's slide list", arg)
}

// firstOr returns the first slide part, or d when empty.
func firstOr(s []string, d string) string {
	if len(s) > 0 {
		return s[0]
	}
	return d
}

// partTree loads a part related to src by a relationship of type want (a
// ctypes.Rel constant, either vocabulary).
func (d *Deck) related(src, want string) (string, *tree.Document, error) {
	rels, err := d.pkg.Rels(src)
	if err != nil {
		return "", nil, err
	}
	for _, r := range rels.All() {
		if relType(r.Type, want) && !r.External() {
			name, err := rels.TargetPart(r)
			if err != nil {
				return "", nil, err
			}
			pt, err := d.pkg.Part(name)
			if err != nil {
				return "", nil, err
			}
			doc, err := pt.Tree()
			if err != nil {
				return "", nil, err
			}
			return name, doc, nil
		}
	}
	return "", nil, nil
}

// SlideInfo summarises one slide for the list mode.
type SlideInfo struct {
	Part    string
	Name    string
	Title   string
	Objects int
}

// List reads every slide and summarises it.
func (d *Deck) List() ([]SlideInfo, error) {
	var out []SlideInfo
	for _, s := range d.Slides {
		sl, err := d.ReadSlide(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s, err)
		}
		info := SlideInfo{Part: s, Name: sl.Name, Objects: len(sl.Objects)}
		for _, o := range sl.Objects {
			if o.Ph != nil && (o.Ph.Type == "title" || o.Ph.Type == "ctrTitle") && o.node != nil {
				info.Title = plainText(o.node)
				break
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// plainText is the text of an object's paragraphs, joined by spaces.
func plainText(n *tree.Node) string {
	body := derive.Child(n, derive.PML, "txBody")
	if body == nil {
		return ""
	}
	var paras []string
	for p := range body.ElementsSeq() {
		if !derive.Is(p, derive.DML, "p") {
			continue
		}
		var b strings.Builder
		p.Walk(func(e *tree.Node) bool {
			if derive.Is(e, derive.DML, "t") {
				b.WriteString(e.Text())
			}
			return true
		})
		if s := strings.TrimSpace(b.String()); s != "" {
			paras = append(paras, s)
		}
	}
	return strings.Join(paras, " ")
}
