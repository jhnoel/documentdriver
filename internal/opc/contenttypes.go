package opc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ContentTypes is the [Content_Types].xml stream, edited through its tree.
// Element and attribute names are matched in the namespace of the stream's
// own root element.
type ContentTypes struct {
	doc  *tree.Document
	root *tree.Node
	ns   string
	bad  bool // the root is not Types: reads find nothing, writes fail
}

// ErrContentTypes is returned when an operation would have to register a
// part's type in a content types stream whose root is not Types. The
// stream itself is never a reason to fail a save: a plan that adds no
// part copies it through byte for byte. Operations that add parts check
// RegistersTypes before doing any work.
var ErrContentTypes = errors.New("opc: the content types stream's root is not Types, so no part can be registered in it")

// RegistersTypes reports, before any work is done, whether parts can be
// registered in the package's content types stream: nil, or an error
// wrapping ErrContentTypes for a stream whose root is not Types (or one
// that does not parse). An operation that would add a part calls it first
// and refuses up front.
func (p *Package) RegistersTypes() error {
	ct, err := p.ContentTypes()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrContentTypes, err)
	}
	if ct.bad {
		return ErrContentTypes
	}
	return nil
}

// ContentTypes parses and returns the package's content types stream.
func (p *Package) ContentTypes() (*ContentTypes, error) {
	if p.ct != nil {
		return p.ct, nil
	}
	pt, err := p.Part(ContentTypesName)
	if err != nil {
		return nil, err
	}
	doc, err := pt.Tree()
	if err != nil {
		return nil, err
	}
	root := doc.Root()
	if root == nil || root.Local() != "Types" {
		// A pre-existing defect: the stream reads as declaring nothing,
		// and writing into it is refused (ErrContentTypes).
		name := "(none)"
		if root != nil {
			name = "<" + root.Name() + ">"
		}
		p.notes = append(p.notes, "content types root is "+name+", not Types: no content types are read from it")
		p.ct = &ContentTypes{doc: doc, bad: true}
		return p.ct, nil
	}
	p.ct = &ContentTypes{doc: doc, root: root, ns: root.Space()}
	return p.ct, nil
}

// Default returns the content type registered for a file extension
// (compared case-insensitively).
func (c *ContentTypes) Default(ext string) (string, bool) {
	if c.bad {
		return "", false
	}
	for _, e := range c.root.Elements() {
		if !e.Is(c.ns, "Default") {
			continue
		}
		if x, _ := e.Attr("Extension"); strings.EqualFold(x, ext) {
			v, _ := e.Attr("ContentType")
			return v, true
		}
	}
	return "", false
}

// override returns the Override element for name, or nil when absent or the stream is bad.
func (c *ContentTypes) override(name string) *tree.Node {
	if c.bad {
		return nil
	}
	key := PartKey(name)
	for _, e := range c.root.Elements() {
		if !e.Is(c.ns, "Override") {
			continue
		}
		if pn, _ := e.Attr("PartName"); PartKey(pn) == key {
			return e
		}
	}
	return nil
}

// Override returns the content type given to a part by an Override.
func (c *ContentTypes) Override(name string) (string, bool) {
	if e := c.override(name); e != nil {
		return e.Attr("ContentType")
	}
	return "", false
}

// Lookup returns a part's content type: its Override, else the Default for
// its extension.
func (c *ContentTypes) Lookup(name string) (string, bool) {
	if t, ok := c.Override(name); ok {
		return t, true
	}
	return c.Default(extension(name))
}

// SetOverride sets a part's Override, adding one after the last Override
// (or the last child) if there is none.
func (c *ContentTypes) SetOverride(name, contentType string) error {
	if c.bad {
		return ErrContentTypes
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	if e := c.override(name); e != nil {
		return e.SetAttr("ContentType", contentType)
	}
	return c.insert("Override", "PartName", name, contentType)
}

// SetDefault sets the Default for an extension, adding one after the last
// Default if there is none.
func (c *ContentTypes) SetDefault(ext, contentType string) error {
	if c.bad {
		return ErrContentTypes
	}
	for _, e := range c.root.Elements() {
		if x, _ := e.Attr("Extension"); e.Is(c.ns, "Default") && strings.EqualFold(x, ext) {
			return e.SetAttr("ContentType", contentType)
		}
	}
	return c.insert("Default", "Extension", ext, contentType)
}

// insert adds <local key="val" ContentType="ct"/> after the last sibling of
// the same kind, else after the last element child.
func (c *ContentTypes) insert(local, key, val, ct string) error {
	el, err := tree.NewElement(c.ns, c.root.Prefix(), local)
	if err != nil {
		return err
	}
	if err := el.SetAttr(key, val); err != nil {
		return err
	}
	if err := el.SetAttr("ContentType", ct); err != nil {
		return err
	}
	return insertAfterLast(c.root, el, func(e *tree.Node) bool { return e.Is(c.ns, local) })
}

// RemoveDefault removes the Default for an extension (compared
// case-insensitively). It reports whether there was one.
func (c *ContentTypes) RemoveDefault(ext string) bool {
	if c.bad {
		return false
	}
	for _, e := range c.root.Elements() {
		if x, _ := e.Attr("Extension"); e.Is(c.ns, "Default") && strings.EqualFold(x, ext) {
			return e.Remove() == nil
		}
	}
	return false
}

// RemoveOverride removes a part's Override. It reports whether there was
// one.
func (c *ContentTypes) RemoveOverride(name string) bool {
	if e := c.override(name); e != nil {
		return e.Remove() == nil
	}
	return false
}

// insertAfterLast inserts el after the last element child of parent that
// satisfies like, else after the last element child, else as the first
// child.
func insertAfterLast(parent, el *tree.Node, like func(*tree.Node) bool) error {
	var last, lastAny *tree.Node
	for e := parent.LastChild(); e != nil && last == nil; e = e.PrevSibling() {
		if e.Kind() != tree.ElementNode {
			continue
		}
		if lastAny == nil {
			lastAny = e
		}
		if like(e) {
			last = e
		}
	}
	switch {
	case last != nil:
		return parent.InsertAfter(el, last)
	case lastAny != nil:
		return parent.InsertAfter(el, lastAny)
	}
	return parent.AppendChild(el)
}
