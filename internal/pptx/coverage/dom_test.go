package coverage

import (
	"bytes"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// el is a small mutable copy of an XML element for building probe
// fixtures. The probes never mutate tree nodes (internal/archtest allows
// that only to the edit layer); they copy a parsed part into els, change
// the copy and serialise it.
type el struct {
	name  string // as written, prefix:local
	attrs [][2]string
	kids  []*el
	text  string // text content of a leaf
}

func fromTree(n *tree.Node) *el {
	e := &el{name: n.Name()}
	for _, a := range n.Attrs() {
		e.attrs = append(e.attrs, [2]string{a.Name, a.Value})
	}
	for _, c := range n.Elements() {
		e.kids = append(e.kids, fromTree(c))
	}
	if len(e.kids) == 0 {
		e.text = n.Text()
	}
	return e
}

func parseEl(src string) *el {
	doc, err := tree.Parse([]byte(src))
	if err != nil {
		panic(err)
	}
	return fromTree(doc.Root())
}

func (e *el) clone() *el {
	c := &el{name: e.name, text: e.text}
	c.attrs = append(c.attrs, e.attrs...)
	for _, k := range e.kids {
		c.kids = append(c.kids, k.clone())
	}
	return c
}

func (e *el) attr(name string) (string, bool) {
	for _, a := range e.attrs {
		if a[0] == name {
			return a[1], true
		}
	}
	return "", false
}

func (e *el) setAttr(name, v string) {
	for i, a := range e.attrs {
		if a[0] == name {
			e.attrs[i][1] = v
			return
		}
	}
	e.attrs = append(e.attrs, [2]string{name, v})
}

func (e *el) removeAttr(name string) {
	for i, a := range e.attrs {
		if a[0] == name {
			e.attrs = append(e.attrs[:i], e.attrs[i+1:]...)
			return
		}
	}
}

func (e *el) child(name string) *el {
	for _, k := range e.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

func (e *el) removeChild(c *el) {
	for i, k := range e.kids {
		if k == c {
			e.kids = append(e.kids[:i], e.kids[i+1:]...)
			return
		}
	}
}

func (e *el) write(b *bytes.Buffer) {
	b.WriteString("<" + e.name)
	for _, a := range e.attrs {
		b.WriteString(" " + a[0] + `="` + esc(a[1], true) + `"`)
	}
	if len(e.kids) == 0 && e.text == "" {
		b.WriteString("/>")
		return
	}
	b.WriteString(">")
	b.WriteString(esc(e.text, false))
	for _, k := range e.kids {
		k.write(b)
	}
	b.WriteString("</" + e.name + ">")
}

func (e *el) xml() []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	e.write(&b)
	return b.Bytes()
}

var (
	textEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

func esc(s string, attr bool) string {
	if attr {
		return attrEsc.Replace(s)
	}
	return textEsc.Replace(s)
}
