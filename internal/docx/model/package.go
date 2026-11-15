// Package model is the docx read model: the package and its parts, the
// WordprocessingML helpers the read and edit sides share (paragraphs,
// runs, revision markers, paragraph identity), and the paragraph segment
// model. It is a port of docxdriver 0.5.2's package.rs, document.rs and
// segments.rs (read side only) over internal/opc and internal/ooxml/tree.
//
// Element and attribute lookups follow docxdriver's name rules: elements
// match by local name, a WordprocessingML check also accepts an element in
// no namespace, and attributes match by local name whatever their prefix.
package model

import (
	"archive/zip"
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// DocumentPart is the conventional main document part name.
const DocumentPart = "word/document.xml"

const (
	packageRelsPart   = "_rels/.rels"
	officeDocumentRel = "/officeDocument"
)

// Package is a docx package: its part names in zip order over an
// internal/opc package, each part read (and parsed) lazily on first use,
// so a read touches only the parts it needs. Part names have no leading
// slash and compare exactly. Parsed parts are the opc package's own trees
// (Part.Tree), parsed from the original bytes with nothing normalised:
// the read model and the edit side share one tree per part.
type Package struct {
	opc   *opc.Package
	names []string
	index map[string]int
	xml   map[string]*XML
	notes []Note
	// rels are the parsed relationships of each part Rels was asked
	// for, and relTargets the main document's (RelTargets): at most one
	// entry per part of the package.
	rels       map[string][]Relationship
	relTargets map[string]string
}

// Note is a fact a read could not derive because a part is damaged: the
// part (At) and why (Reason). A damaged auxiliary part disables only its
// own facet (styles, list numbering, comments, a header, an image); the
// rest of the document reads, and the note says what was left out.
type Note struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Note records a note once.
func (p *Package) Note(at, reason string) {
	for _, n := range p.notes {
		if n.At == at && n.Reason == reason {
			return
		}
	}
	p.notes = append(p.notes, Note{at, reason})
}

// Notes returns the package's leniency notes (opc.Package.Notes, at
// "package") and then the notes recorded so far, in the order first
// recorded.
func (p *Package) Notes() []Note {
	var out []Note
	for _, n := range p.opc.Notes() {
		out = append(out, Note{At: "package", Reason: n})
	}
	return append(out, p.notes...)
}

// Part is one part of a package.
type Part struct {
	Name string // without a leading slash, as stored in the zip
}

// Open reads the package in b with opc.DefaultLimits. Errors read like
// docxdriver's: "not a DOCX package: ..." for an unreadable zip.
// docxdriver decompresses every part at open, so a corrupt member it
// never uses still fails it; this reads a part only when a view needs it,
// and a part that cannot be read is left out with a Note (an intended
// difference).
func Open(b []byte) (*Package, error) {
	p, err := opc.OpenBytes(b, opc.Limits{})
	if err != nil {
		return nil, zipError(err)
	}
	return OpenPackage(p), nil
}

// OpenPackage returns the read model of an opened opc package, such as
// an edit workspace's: its views read the package's current trees.
func OpenPackage(p *opc.Package) *Package {
	pkg := &Package{opc: p, index: map[string]int{}, xml: map[string]*XML{}, rels: map[string][]Relationship{}}
	for _, name := range p.Names() {
		n := strings.TrimPrefix(name, "/")
		if _, dup := pkg.index[n]; !dup {
			pkg.index[n] = len(pkg.names)
		}
		pkg.names = append(pkg.names, n)
	}
	return pkg
}

// zipError words a package read failure as docxdriver does where the cause
// matches one of its zip errors; any other cause keeps its own text.
func zipError(err error) error {
	switch {
	case errors.Is(err, zip.ErrFormat):
		return &packageError{"not a DOCX package: invalid Zip archive: Could not find EOCD", err}
	case errors.Is(err, zip.ErrChecksum):
		return &packageError{"package I/O failed: Invalid checksum", err}
	case errors.Is(err, zip.ErrAlgorithm):
		return &packageError{"not a DOCX package: unsupported Zip archive: Compression method not supported", err}
	}
	return &packageError{"not a DOCX package: " + err.Error(), err}
}

// packageError is a package read failure: its message as docxdriver words
// it, and the cause for errors.Is and errors.As.
type packageError struct {
	msg string
	err error
}

// Error returns the package error's message.
func (e *packageError) Error() string { return e.msg }

// Unwrap returns the package error's wrapped cause.
func (e *packageError) Unwrap() error { return e.err }

// Parts returns the parts in zip order.
func (p *Package) Parts() []Part {
	out := make([]Part, len(p.names))
	for i, n := range p.names {
		out[i] = Part{Name: n}
	}
	return out
}

// Get returns the bytes of the part called name (no leading slash, exact
// case), and false if there is none or it cannot be read (a Note says
// why).
func (p *Package) Get(name string) ([]byte, bool) {
	if _, ok := p.index[name]; !ok {
		return nil, false
	}
	b, err := p.read(name)
	if err != nil {
		p.Note(name, "the part could not be read: "+zipError(err).Error())
		return nil, false
	}
	return b, true
}

// read returns the bytes of part name, or the error reading it.
func (p *Package) read(name string) ([]byte, error) {
	part, err := p.opc.Part(name)
	if err != nil {
		return nil, err
	}
	return part.Bytes()
}

// ParsePart returns a private, freshly parsed tree of the part called
// name (for a copy a view changes, such as a settled view), and false when
// the part is absent or damaged; a damaged part gets a Note.
func (p *Package) ParsePart(name string) (*XML, bool) {
	b, ok := p.Get(name)
	if !ok {
		return nil, false
	}
	x, err := ParseXML(b)
	if err != nil {
		p.Note(name, "the part is not well formed: "+strings.TrimPrefix(err.Error(), "document.xml parse failed: "))
		return nil, false
	}
	return x, true
}

// Part returns the parsed part called name (XML) for reading, and false
// when it is absent or damaged; a damaged part gets a Note.
func (p *Package) Part(name string) (*XML, bool) {
	if !p.Has(name) {
		return nil, false
	}
	x, err := p.XML(name)
	if err != nil {
		p.Note(name, "the part is not well formed: "+strings.TrimPrefix(err.Error(), "document.xml parse failed: "))
		return nil, false
	}
	return x, true
}

// XML returns the parsed part called name: the opc package's tree of it,
// parsed once from its original bytes. A part with a UTF-16 byte order
// mark is transcoded into a private tree instead (the working tree reads
// UTF-8 only, so the edit side cannot open such a part). Errors are
// ParseXML's.
func (p *Package) XML(name string) (*XML, error) {
	if x, ok := p.xml[name]; ok {
		return x, nil
	}
	if _, ok := p.index[name]; !ok {
		return nil, fmt.Errorf("%w: %s", opc.ErrNoPart, name)
	}
	b, err := p.read(name)
	if err != nil {
		return nil, zipError(err)
	}
	var x *XML
	if isUTF16(b) {
		var err error
		if x, err = ParseXML(b); err != nil {
			return nil, err
		}
	} else {
		part, err := p.opc.Part(name)
		if err != nil {
			return nil, err
		}
		d, err := part.Tree()
		if err != nil {
			return nil, parseFailure(b)
		}
		x = &XML{Doc: d}
	}
	p.xml[name] = x
	return x, nil
}

// Has reports whether the part called name exists.
func (p *Package) Has(name string) bool {
	_, ok := p.index[name]
	return ok
}

// DocumentPartName returns the main document part's name, discovered
// through the package relationships (the officeDocument relationship's
// target), or DocumentPart when the relationships are absent, unreadable
// or name a part the package does not have.
func (p *Package) DocumentPartName() string {
	if x, ok := p.Part(packageRelsPart); ok {
		name, found := "", false
		x.Doc.Node().Walk(func(n *tree.Node) bool {
			if found || !IsLocal(n, "Relationship") {
				return !found
			}
			t, ok := Attr(n, "Type")
			if !ok || !strings.HasSuffix(t, officeDocumentRel) {
				return true
			}
			target, ok := Attr(n, "Target")
			if !ok {
				return true
			}
			name, found = strings.TrimLeft(target, "/"), true
			return false
		})
		if found && p.Has(name) {
			return name
		}
	}
	return DocumentPart
}

// ErrNoDocument is returned when the package has no main document part.
var ErrNoDocument = errors.New("package has no " + DocumentPart)

// DocumentXML returns the main document part's bytes.
func (p *Package) DocumentXML() ([]byte, error) {
	b, ok := p.Get(p.DocumentPartName())
	if !ok {
		return nil, ErrNoDocument
	}
	return b, nil
}

// SiblingPart returns the part named file in the main document part's
// directory ("word/styles.xml" for "styles.xml").
func (p *Package) SiblingPart(file string) string {
	main := p.DocumentPartName()
	i := strings.LastIndexByte(main, '/')
	if i < 0 {
		return file
	}
	return main[:i] + "/" + file
}

// RelsPartFor returns the relationships part of a part
// ("word/_rels/document.xml.rels" for "word/document.xml").
func RelsPartFor(part string) string {
	i := strings.LastIndexByte(part, '/')
	if i < 0 {
		return "_rels/" + part + ".rels"
	}
	return part[:i] + "/_rels/" + part[i+1:] + ".rels"
}

// ResolveRelTarget resolves a relationship target relative to the
// directory of the part that owns the relationship ("media/image1.png"
// from "word/document.xml" is "word/media/image1.png"); an absolute target
// drops its leading slash. "." segments and empty segments are skipped,
// ".." removes the previous segment.
func ResolveRelTarget(sourcePart, target string) string {
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		return abs
	}
	var segs []string
	if i := strings.LastIndexByte(sourcePart, '/'); i >= 0 {
		segs = strings.Split(sourcePart[:i], "/")
	}
	for s := range strings.SplitSeq(target, "/") {
		switch s {
		case "", ".":
		case "..":
			if len(segs) > 0 {
				segs = segs[:len(segs)-1]
			}
		default:
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// Relationship is one Relationship element of a relationships part, its
// attributes as written (no defaults applied).
type Relationship struct {
	ID, Type, Target, TargetMode string
	HasID, HasType, HasTarget    bool
}

// External reports whether the relationship targets an external resource.
func (r Relationship) External() bool { return r.TargetMode == "External" }

// Rels parses the relationships part of part, returning nil when it is
// absent or not well formed. Every element whose local name is
// Relationship counts, in document order. The package parses each
// relationships part once; callers share the slice and must not modify
// it.
func (p *Package) Rels(part string) []Relationship {
	if rels, ok := p.rels[part]; ok {
		return rels
	}
	rels := p.parseRels(part)
	p.rels[part] = rels
	return rels
}

// parseRels returns the relationships of part, or nil when absent or not well formed.
func (p *Package) parseRels(part string) []Relationship {
	x, ok := p.Part(RelsPartFor(part))
	if !ok {
		return nil
	}
	var out []Relationship
	x.Doc.Node().Walk(func(n *tree.Node) bool {
		if !IsLocal(n, "Relationship") {
			return true
		}
		var r Relationship
		r.ID, r.HasID = Attr(n, "Id")
		r.Type, r.HasType = Attr(n, "Type")
		r.Target, r.HasTarget = Attr(n, "Target")
		r.TargetMode, _ = Attr(n, "TargetMode")
		out = append(out, r)
		return true
	})
	return out
}

// RelTargets maps relationship ids of the main document part to their
// targets as written; a later duplicate id wins. The map is built once
// and shared: callers must not modify it.
func (p *Package) RelTargets() map[string]string {
	if p.relTargets != nil {
		return p.relTargets
	}
	out := map[string]string{}
	for _, r := range p.Rels(p.DocumentPartName()) {
		if r.HasID && r.HasTarget {
			out[r.ID] = r.Target
		}
	}
	p.relTargets = out
	return out
}
