package vsd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Kind is the kind of thing an address names.
type Kind uint8

// Address kinds (docs/vsdx-contract-draft.md, section 6).
const (
	KindPage Kind = iota + 1
	KindMaster
	KindShape
	KindProp
	KindLayer
	KindField
	KindAlias
)

// Address is a parsed agent address. Container is the page or master
// id and PageDup its ~n (1 when absent); Shape the shape id and Dup its
// ~n (1 when absent); Name the row name of a prop or the layer name; N
// the 1-based number of a field.
type Address struct {
	Kind      Kind
	InMaster  bool
	Container uint32
	PageDup   int
	Shape     uint32
	Dup       int
	Name      string
	N         int
	Alias     string
}

// wantAddress names every form Parse accepts, for an error whose
// offending part names nothing more specific.
const wantAddress = "page<id>, master<id>, page<id>/s<id>, master<id>/s<id>, " +
	"page<id>/layer:<name>, page<id>/s<id>/prop:<name>, page<id>/s<id>/field:<n> or $alias"

// badAddress reports that s is not an address where want names the
// expected form.
func badAddress(s, want string) error {
	return fmt.Errorf("vsdx: bad address %q: want %s", s, want)
}

// scanUint32 scans the stored id at s[i:]: one or more ASCII digits in
// canonical form (no leading zero) that fits a uint32, returning the
// value and the index past it.
func scanUint32(s string, i int) (uint32, int, bool) {
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, start, false
	}
	digits := s[start:i]
	if len(digits) > 1 && digits[0] == '0' {
		return 0, start, false
	}
	v, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return 0, start, false
	}
	return uint32(v), i, true
}

// scanPositiveInt scans a 1-based number at s[i:] in canonical form
// (no leading zero, so "0" itself is refused), returning the value and
// the index past it.
func scanPositiveInt(s string, i int) (int, int, bool) {
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start || s[start] == '0' {
		return 0, start, false
	}
	v, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, start, false
	}
	return v, i, true
}

// PageID returns the address of a page: "page" and its stored ID
// ("page0"), and "page0~2" for the second page sharing ID 0 (dup 2;
// Visio never writes one, but a file may hold one). Chunk V2.
func PageID(id uint32, dup int) string {
	s := "page" + strconv.FormatUint(uint64(id), 10)
	if dup > 1 {
		s += "~" + strconv.Itoa(dup)
	}
	return s
}

// MasterID returns the address of a master: "master2". Chunk V2.
func MasterID(id uint32) string {
	return "master" + strconv.FormatUint(uint64(id), 10)
}

// ShapeID returns the address of a shape in a page or master: "page0/s5",
// and "page0/s5~2" for the second shape sharing id 5 (dup 2). Chunk V2.
func ShapeID(container string, id uint32, dup int) string {
	s := container + "/s" + strconv.FormatUint(uint64(id), 10)
	if dup > 1 {
		s += "~" + strconv.Itoa(dup)
	}
	return s
}

// PropID returns the address of a shape data row: "page0/s5/prop:Cost".
// Row names are kept as stored; a name holding "/" is written as is and
// parsed back by taking everything after "prop:". Chunk V2.
func PropID(shape, row string) string {
	return shape + "/prop:" + row
}

// LayerID returns the address of a page layer by its name:
// "page0/layer:Connector". Chunk V2.
func LayerID(page, name string) string {
	return page + "/layer:" + name
}

// FieldID returns the address of the n-th field (1-based, in text
// order) of a shape's text: "page0/s5/field:1". Chunk V2.
func FieldID(shape string, n int) string {
	return shape + "/field:" + strconv.Itoa(n)
}

// Glue returns a connector end's glue as read shows it: "s12" for glue
// to the whole shape (ToCell PinX), "s12:3" for connection point 3
// (ToCell Connections.X3), "s12@<ToCell>" for any other ToCell
// (read-only), "#12..." when id 12 names no shape or several. Chunk V2.
func Glue(shape uint32, toCell string, resolved bool) string {
	prefix := "s"
	if !resolved {
		prefix = "#"
	}
	base := prefix + strconv.FormatUint(uint64(shape), 10)
	if toCell == "PinX" {
		return base
	}
	if rest, ok := strings.CutPrefix(toCell, "Connections.X"); ok {
		if n, next, ok := scanPositiveInt(rest, 0); ok && next == len(rest) {
			return base + ":" + strconv.Itoa(n)
		}
	}
	return base + "@" + toCell
}

// Parse parses an address an agent typed (page, master, shape, prop,
// layer or $alias). It fails with an error that names the expected
// form. Chunk V2.
func Parse(s string) (Address, error) {
	if s == "" {
		return Address{}, badAddress(s, wantAddress)
	}
	if s[0] == '$' {
		name := s[1:]
		if !cli.ValidAlias(name) {
			return Address{}, badAddress(s, "$alias ([A-Za-z_][A-Za-z0-9_]*)")
		}
		return Address{Kind: KindAlias, Alias: name}, nil
	}

	a := Address{PageDup: 1, Dup: 1}
	var rest string
	i := 0
	if strings.HasPrefix(s, "page") {
		a.Kind = KindPage
		i = len("page")
	} else if strings.HasPrefix(s, "master") {
		a.Kind = KindMaster
		i = len("master")
	} else {
		return Address{}, badAddress(s, wantAddress)
	}
	inMaster := a.Kind == KindMaster
	shapeWant := "page<id>/s<id> (page0/s5)"
	if inMaster {
		shapeWant = "master<id>/s<id> (master2/s5)"
	}

	id, next, ok := scanUint32(s, i)
	if !ok {
		if inMaster {
			return Address{}, badAddress(s, "master<id> (master2)")
		}
		return Address{}, badAddress(s, "page<id> (page0, page5~2)")
	}
	a.Container = id
	i = next
	if i < len(s) && s[i] == '~' {
		if inMaster {
			return Address{}, badAddress(s, "master<id> (master2)")
		}
		dup, next, ok := scanPositiveInt(s, i+1)
		if !ok || dup < 2 {
			return Address{}, badAddress(s, "page<id> or page<id>~<dup> (dup from 2, page0~2)")
		}
		a.PageDup = dup
		i = next
	}
	if i == len(s) {
		return a, nil
	}
	if s[i] != '/' {
		if inMaster {
			return Address{}, badAddress(s, "master<id> (master2)")
		}
		return Address{}, badAddress(s, "page<id> (page0, page5~2)")
	}
	rest = s[i+1:]
	if rest == "" {
		if inMaster {
			return Address{}, badAddress(s, shapeWant)
		}
		return Address{}, badAddress(s, "page<id>/s<id> or page<id>/layer:<name> (page0/s5, page0/layer:Connector)")
	}
	if !inMaster && strings.HasPrefix(rest, "layer:") {
		name := rest[len("layer:"):]
		if name == "" {
			return Address{}, badAddress(s, "page<id>/layer:<name> (page0/layer:Connector)")
		}
		a.Kind = KindLayer
		a.Name = name
		return a, nil
	}
	if !strings.HasPrefix(rest, "s") {
		if inMaster {
			return Address{}, badAddress(s, shapeWant)
		}
		return Address{}, badAddress(s, "page<id>/s<id> or page<id>/layer:<name> (page0/s5, page0/layer:Connector)")
	}
	shape, next, ok := scanUint32(rest, 1)
	if !ok {
		return Address{}, badAddress(s, shapeWant)
	}
	a.Shape = shape
	j := next
	if j < len(rest) && rest[j] == '~' {
		dup, next, ok := scanPositiveInt(rest, j+1)
		if !ok || dup < 2 {
			if inMaster {
				return Address{}, badAddress(s, "master<id>/s<id> or master<id>/s<id>~<dup> (dup from 2, master2/s5~2)")
			}
			return Address{}, badAddress(s, "page<id>/s<id> or page<id>/s<id>~<dup> (dup from 2, page0/s5~2)")
		}
		a.Dup = dup
		j = next
	}
	a.InMaster = inMaster
	if j == len(rest) {
		a.Kind = KindShape
		return a, nil
	}
	if rest[j] != '/' {
		return Address{}, badAddress(s, shapeWant)
	}
	tail := rest[j+1:]
	if tail == "" {
		if inMaster {
			return Address{}, badAddress(s, "master<id>/s<id>/prop:<name> or master<id>/s<id>/field:<n> (master2/s5/prop:Cost, master2/s5/field:1)")
		}
		return Address{}, badAddress(s, "page<id>/s<id>/prop:<name> or page<id>/s<id>/field:<n> (page0/s5/prop:Cost, page0/s5/field:1)")
	}
	if strings.HasPrefix(tail, "prop:") {
		name := tail[len("prop:"):]
		if name == "" {
			if inMaster {
				return Address{}, badAddress(s, "master<id>/s<id>/prop:<name> (master2/s5/prop:Cost)")
			}
			return Address{}, badAddress(s, "page<id>/s<id>/prop:<name> (page0/s5/prop:Cost)")
		}
		a.Kind = KindProp
		a.Name = name
		return a, nil
	}
	if strings.HasPrefix(tail, "field:") {
		n, next, ok := scanPositiveInt(tail, len("field:"))
		if !ok || next != len(tail) {
			if inMaster {
				return Address{}, badAddress(s, "master<id>/s<id>/field:<n> (n from 1, master2/s5/field:1)")
			}
			return Address{}, badAddress(s, "page<id>/s<id>/field:<n> (n from 1, page0/s5/field:1)")
		}
		a.Kind = KindField
		a.N = n
		return a, nil
	}
	if inMaster {
		return Address{}, badAddress(s, "master<id>/s<id>/prop:<name> or master<id>/s<id>/field:<n> (master2/s5/prop:Cost, master2/s5/field:1)")
	}
	return Address{}, badAddress(s, "page<id>/s<id>/prop:<name> or page<id>/s<id>/field:<n> (page0/s5/prop:Cost, page0/s5/field:1)")
}
