package opc

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ContentTypesName is the part name of the content types stream.
const ContentTypesName = "/[Content_Types].xml"

// ErrNoPart is returned when a part does not exist.
var ErrNoPart = errors.New("opc: no such part")

// Package is an open OPC package. Members are read from the zip file only
// when asked for, and parsed into trees only on demand. Edits stay in memory
// until Save.
type Package struct {
	path    string
	file    *os.File
	info    os.FileInfo
	zr      *zip.Reader
	members []*member // zip order, then added members
	byName  map[string]*member

	ct   *ContentTypes
	rels map[string]*Rels

	budget *budget // decompression limits and bytes read so far

	// notes are pre-existing package defects the package was opened
	// despite (see Notes).
	notes []string
}

// Notes returns the pre-existing defects the package was opened despite,
// in the order met: a zip entry whose part name another entry already
// has (the first entry is the part; the later one is copied through on
// save, untouched), a content types stream whose root is not Types (it
// reads as declaring nothing). A defect the tool did not introduce never
// stops an edit or a save; only a file that is not a zip, has no main
// part or trips a safety limit is refused.
func (p *Package) Notes() []string { return append([]string(nil), p.notes...) }

// member is one zip entry, or a part added since the package was opened.
type member struct {
	zf     *zip.File // nil for an added member
	name   string    // part name, with a leading "/"
	budget *budget   // the package's; reads of zf count against it

	orig   string // decompressed original bytes, once read; a parsed tree shares them
	loaded bool

	doc      *tree.Document // parsed on demand
	data     []byte         // replacement bytes (SetBytes, AddPart)
	replaced bool
	// patch, when set, is the part's content: the original with the
	// patch's replacements (SetPatch). It excludes doc and data.
	patch   *splice.Patch
	deleted bool
	// dropped is set by DropContents: the content is gone.
	dropped bool
}

// Open opens the package at path with DefaultLimits. Only the zip central
// directory and the local file headers are read.
func Open(path string) (*Package, error) {
	return OpenWithLimits(path, Limits{})
}

// OpenWithLimits opens the package at path, bounding what reading it may
// cost by l (zero fields take their DefaultLimits value). Exceeding a
// limit is an error wrapping ErrLimit, at open or when a part is read.
func OpenWithLimits(path string, l Limits) (*Package, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	p, err := openReader(f, info.Size(), path, path, l)
	if err != nil {
		f.Close()
		return nil, err
	}
	p.file, p.info = f, info
	return p, nil
}

// OpenBytes opens a package held in memory, bounded by l like
// OpenWithLimits. b is retained, not copied, and must not be modified
// afterwards. The package reads like one opened from a file, but it has no
// file to save to: Save, SaveAs and SaveAndClose return ErrInMemory.
func OpenBytes(b []byte, l Limits) (*Package, error) {
	return openReader(bytes.NewReader(b), int64(len(b)), "", "(in memory)", l)
}

// ErrInMemory is returned when saving a package opened with OpenBytes.
var ErrInMemory = errors.New("opc: package opened from memory cannot be saved")

// openReader reads the zip directory of r, the package at path ("" for one
// held in memory); name labels errors.
func openReader(r io.ReaderAt, size int64, path, name string, l Limits) (*Package, error) {
	l = l.withDefaults()
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("opc: %s: %w", name, err)
	}
	if err := checkEntries(zr, l); err != nil {
		return nil, fmt.Errorf("opc: %s: %w", name, err)
	}
	p := &Package{path: path, zr: zr, byName: map[string]*member{}, rels: map[string]*Rels{}, budget: &budget{limits: l}}
	for _, zf := range zr.File {
		if strings.HasSuffix(zf.Name, "/") {
			continue // directory entry: kept on save, not a part
		}
		m := &member{zf: zf, name: "/" + zf.Name, budget: p.budget}
		key := PartKey(m.name)
		if _, dup := p.byName[key]; dup {
			// The first entry wins; this one is not a part, and save
			// copies it through as it is.
			p.notes = append(p.notes, "duplicate part name "+m.name+": the first entry is used")
			continue
		}
		p.byName[key] = m
		p.members = append(p.members, m)
	}
	return p, nil
}

// Close releases the underlying file. Unsaved edits are discarded.
func (p *Package) Close() error {
	if p.file == nil {
		return nil
	}
	err := p.file.Close()
	p.file = nil
	return err
}

// Path returns the file the package was opened from.
func (p *Package) Path() string { return p.path }

// Names returns the names of all parts, including [Content_Types].xml and
// relationship parts, in zip order followed by added parts.
func (p *Package) Names() []string {
	var out []string
	for _, m := range p.members {
		if !m.deleted {
			out = append(out, m.name)
		}
	}
	return out
}

// PartKey normalises a part name for lookup and declaration: a leading
// slash, lower case (part names compare ASCII-case-insensitively).
func PartKey(name string) string {
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return strings.ToLower(name)
}

// Has reports whether the part exists.
func (p *Package) Has(name string) bool {
	m, ok := p.byName[PartKey(name)]
	return ok && !m.deleted
}

// Part returns the part with the given name ("/ppt/slides/slide1.xml"; the
// leading slash is optional).
func (p *Package) Part(name string) (*Part, error) {
	m, ok := p.byName[PartKey(name)]
	if !ok || m.deleted {
		return nil, fmt.Errorf("%w: %s", ErrNoPart, name)
	}
	return &Part{pkg: p, m: m}, nil
}

// AddPart adds a new part with the given bytes. If contentType is not empty
// and the content types stream does not already give the part that type
// through its extension's Default, an Override is added.
func (p *Package) AddPart(name string, data []byte, contentType string) (*Part, error) {
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	if err := checkPartName(name); err != nil {
		return nil, err
	}
	if contentType != "" {
		// Refused before anything changes: no part is added that the
		// content types stream cannot register.
		if err := p.RegistersTypes(); err != nil {
			return nil, err
		}
	}
	key := PartKey(name)
	if m, ok := p.byName[key]; ok && !m.deleted {
		return nil, fmt.Errorf("opc: part %s already exists", name)
	}
	if contentType != "" {
		ct, err := p.ContentTypes()
		if err != nil {
			return nil, err
		}
		if got, _ := ct.Default(extension(name)); got != contentType {
			if err := ct.SetOverride(name, contentType); err != nil {
				return nil, err
			}
		}
	}
	m := &member{name: name, data: bytes.Clone(data), replaced: true, loaded: true}
	if old, ok := p.byName[key]; ok {
		// Re-adding a deleted part: write it in the old member's place.
		old.deleted = false
		old.doc, old.patch = nil, nil
		old.data, old.replaced = m.data, true
		return &Part{pkg: p, m: old}, nil
	}
	p.byName[key] = m
	p.members = append(p.members, m)
	return &Part{pkg: p, m: m}, nil
}

// DeletePart removes a part and its content-type Override, if any. It does
// not touch relationships that point at the part.
func (p *Package) DeletePart(name string) error {
	m, ok := p.byName[PartKey(name)]
	if !ok || m.deleted {
		return fmt.Errorf("%w: %s", ErrNoPart, name)
	}
	if PartKey(name) == PartKey(ContentTypesName) {
		return errors.New("opc: cannot delete the content types stream")
	}
	ct, err := p.ContentTypes()
	if err != nil {
		return err
	}
	ct.RemoveOverride(m.name)
	m.deleted = true
	m.doc, m.patch = nil, nil
	delete(p.rels, PartKey(m.name))
	return nil
}

// checkPartName returns an error when name is an invalid part name.
func checkPartName(name string) error {
	if name == "/" || strings.HasSuffix(name, "/") || strings.Contains(name, "//") ||
		strings.Contains(name, "\\") || strings.Contains(name, "/./") || strings.Contains(name, "/../") {
		return fmt.Errorf("opc: invalid part name %q", name)
	}
	return nil
}

// extension returns the extension after the last dot in the final path element.
func extension(name string) string {
	base := name[strings.LastIndexByte(name, '/')+1:]
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}
	return ""
}

// Part is one part of a package.
type Part struct {
	pkg *Package
	m   *member
}

// Name returns the part name, with a leading slash.
func (pt *Part) Name() string { return pt.m.name }

// ContentType returns the part's content type from the content types
// stream.
func (pt *Part) ContentType() (string, error) {
	ct, err := pt.pkg.ContentTypes()
	if err != nil {
		return "", err
	}
	t, _ := ct.Lookup(pt.m.name)
	return t, nil
}

// original returns the member's original bytes, caching them, or an error after sealing.
func (m *member) original() (string, error) {
	if m.dropped {
		return "", fmt.Errorf("opc: %s: the package's contents were dropped after sealing", m.name)
	}
	if m.loaded {
		return m.orig, nil
	}
	b, err := m.budget.read(m.zf)
	if err != nil {
		return "", fmt.Errorf("opc: %s: %w", m.name, err)
	}
	m.orig, m.loaded = b, true
	return b, nil
}

// current returns the member's bytes as they would be saved. fresh
// reports whether the slice is a new copy, the caller's to keep.
func (m *member) current() (b []byte, fresh bool, err error) {
	switch {
	case m.doc != nil:
		if !m.doc.Modified() {
			return []byte(m.doc.OriginalString()), true, nil
		}
		return m.doc.Bytes(), true, nil
	case m.replaced:
		return m.data, false, nil
	case m.patch != nil:
		return []byte(m.patch.String()), true, nil
	}
	s, err := m.original()
	return []byte(s), true, err
}

// payload is what a changed member writes: bytes, or a patch streamed
// over the original (SetPatch), never materialised.
type payload struct {
	data  []byte
	patch *splice.Patch
}

// changed reports whether the member's bytes differ from the original, and
// returns what to write.
func (m *member) changed() (bool, payload, error) {
	if m.zf == nil {
		b, _, err := m.current()
		return true, payload{data: b}, err
	}
	if m.patch != nil {
		return !m.patch.Empty() && !sameAsOriginal(m.patch), payload{patch: m.patch}, nil
	}
	if !m.replaced && (m.doc == nil || !m.doc.Modified()) {
		return false, payload{}, nil
	}
	cur, _, err := m.current()
	if err != nil {
		return false, payload{}, err
	}
	orig, err := m.original()
	if err != nil {
		return false, payload{}, err
	}
	if string(cur) == orig {
		return false, payload{}, nil
	}
	return true, payload{data: cur}, nil
}

// sameAsOriginal reports whether a non-empty patch's result is its
// original all the same (an insertion and a deletion that cancel out),
// comparing as it streams, without a copy.
func sameAsOriginal(p *splice.Patch) bool {
	if p.Len() != len(p.Original()) {
		return false
	}
	c := &cmpWriter{want: p.Original()}
	_, err := p.WriteTo(c)
	return err == nil && c.at == len(c.want)
}

// cmpWriter compares what is written to it with want, failing at the
// first difference.
type cmpWriter struct {
	want string
	at   int
}

var errDiffers = errors.New("differs")

// Write compares b with the expected bytes, failing at the first difference.
func (c *cmpWriter) Write(b []byte) (int, error) {
	if len(b) > len(c.want)-c.at || string(b) != c.want[c.at:c.at+len(b)] {
		return 0, errDiffers
	}
	c.at += len(b)
	return len(b), nil
}

// Bytes returns the part's current bytes: the serialised tree if it was
// parsed, otherwise the replacement or original bytes.
func (pt *Part) Bytes() ([]byte, error) {
	b, fresh, err := pt.m.current()
	if err != nil {
		return nil, err
	}
	if !fresh {
		b = bytes.Clone(b)
	}
	return b, nil
}

// Original returns the part's bytes as stored in the file when the package
// was opened, a copy that is the caller's. Added parts have none.
func (pt *Part) Original() ([]byte, error) {
	if pt.m.zf == nil {
		return nil, nil
	}
	s, err := pt.m.original()
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// OriginalString is Original without the copy; ok is false for an added
// part.
func (pt *Part) OriginalString() (s string, ok bool, err error) {
	if pt.m.zf == nil {
		return "", false, nil
	}
	s, err = pt.m.original()
	return s, err == nil, err
}

// Tree parses the part on first use and returns its tree. Edits to the tree
// are saved with the package.
func (pt *Part) Tree() (*tree.Document, error) {
	m := pt.m
	if m.doc != nil {
		return m.doc, nil
	}
	if m.patch != nil {
		return nil, fmt.Errorf("%w: %s", ErrSpliced, m.name)
	}
	var doc *tree.Document
	var err error
	switch {
	case m.replaced:
		doc, err = tree.Parse(m.data)
	default:
		var s string
		if s, err = m.original(); err != nil {
			return nil, err
		}
		doc, err = tree.ParseString(s) // shares the original's bytes
	}
	if err != nil {
		return nil, fmt.Errorf("opc: %s: %w", m.name, err)
	}
	m.doc = doc
	return doc, nil
}

// SetBytes replaces the part's content, discarding any parsed tree or
// patch (and the package's cached view of it, for content types and
// relationships).
func (pt *Part) SetBytes(b []byte) {
	pt.m.doc, pt.m.patch = nil, nil
	pt.m.data = bytes.Clone(b)
	pt.m.replaced = true
	pt.dropCached()
}

// ErrSpliced is returned by Tree for a part whose content is a patch:
// it is never parsed into a tree.
var ErrSpliced = errors.New("opc: part is edited as a patch and has no tree")

// SetPatch makes the part's content the patch p over its original
// string: the part is changed when p is not empty, and saving streams
// p's WriteTo into the zip writer, so the part's bytes are never held
// twice. p must be over the part's original (a patch made from
// OriginalString); the caller may keep adding to it until the package
// is saved, and a later SetPatch, SetBytes or DeletePart replaces it.
// Saving checks it with Check instead of re-tokenising the result. An
// added part has no original to patch, and a part already replaced or
// whose tree was modified is refused rather than losing those edits; an
// unmodified tree is dropped.
func (pt *Part) SetPatch(p *splice.Patch) error {
	m := pt.m
	switch {
	case m.zf == nil:
		return fmt.Errorf("opc: %s: an added part has no original to patch", m.name)
	case m.replaced || (m.doc != nil && m.doc.Modified()):
		return fmt.Errorf("opc: %s: already changed; a patch would discard the change", m.name)
	}
	orig, err := m.original()
	if err != nil {
		return err
	}
	if p.Original() != orig {
		return fmt.Errorf("opc: %s: the patch is not over the part's original", m.name)
	}
	m.doc, m.patch = nil, p
	pt.dropCached()
	return nil
}

// Forget drops the part's cached original bytes, so that a caller that
// visits many large parts one at a time (a search over every worksheet)
// holds one at a time. The next read decompresses the member again, and
// counts against the package's read limits again. It does nothing and
// reports false for a part a save or a reader may still need: added,
// replaced, patched or deleted, or parsed into a tree (whose nodes share
// the bytes; an unmodified tree is not dropped either, since callers may
// hold its nodes). A string the caller still holds stays valid.
func (pt *Part) Forget() bool {
	m := pt.m
	if m.zf == nil || m.replaced || m.patch != nil || m.deleted || m.doc != nil {
		return false
	}
	m.orig, m.loaded = "", false
	return true
}

// Patch returns the part's patch (SetPatch), or nil.
func (pt *Part) Patch() *splice.Patch { return pt.m.patch }

// dropCached drops the package's parsed view of the part when it is the
// content types stream or a relationships part.
func (pt *Part) dropCached() {
	p := pt.pkg
	if PartKey(pt.m.name) == PartKey(ContentTypesName) {
		p.ct = nil
	}
	for k, r := range p.rels {
		if PartKey(r.name) == PartKey(pt.m.name) {
			delete(p.rels, k)
		}
	}
}

// Modified reports whether the part's bytes differ from the original.
func (pt *Part) Modified() (bool, error) {
	ch, _, err := pt.m.changed()
	return ch, err
}

// Modified reports whether saving would change the file.
func (p *Package) Modified() (bool, error) {
	for _, m := range p.members {
		if m.deleted {
			if m.zf != nil {
				return true, nil
			}
			continue
		}
		ch, _, err := m.changed()
		if err != nil || ch {
			return ch, err
		}
	}
	return false, nil
}

// ChangedParts returns the names of the parts whose saved bytes would
// differ from the original: modified, added and deleted parts, in zip
// order followed by added parts.
func (p *Package) ChangedParts() ([]string, error) {
	return p.ChangedPartsExcept(nil)
}

// ChangedPartsExcept is ChangedParts without the parts skip (when not
// nil) reports, which it does not compare. Only a part whose tree was
// modified or whose bytes were replaced is serialised and compared, so a
// caller that skips the parts it already knew changed (the first
// operation to change each, asked after every operation) serialises each
// part about once, not once per call.
func (p *Package) ChangedPartsExcept(skip func(name string) bool) ([]string, error) {
	var out []string
	for _, m := range p.members {
		if skip != nil && skip(m.name) {
			continue
		}
		if m.deleted {
			if m.zf != nil {
				out = append(out, m.name)
			}
			continue
		}
		ch, _, err := m.changed()
		if err != nil {
			return nil, err
		}
		if ch {
			out = append(out, m.name)
		}
	}
	return out, nil
}
