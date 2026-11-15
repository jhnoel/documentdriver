// Package edit is the pptx write layer under operations, on the
// format-neutral edit core (internal/ooxml/edit): it resolves slide
// addresses to objects and their mc:AlternateContent branches, hands out
// Handles (the core's, the only mutators operation code uses), places
// objects, and verifies at commit, through the core and the slide units,
// that nothing outside the declared write set changed. It mutates no tree
// itself and never sees the read model.
package edit

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Ref addresses one object: its slide part, its cNvPr id as written, and
// its 1-based ordinal among the slide's objects with that id in document
// order (counting the projected mc:AlternateContent branch, as the read
// model does). Its fields are unexported: only a Workspace's resolver
// builds one, after checking that the object exists.
type Ref struct {
	part string
	id   string
	ord  int
}

// Part returns the slide part name, such as /ppt/slides/slide3.xml.
func (r Ref) Part() string { return r.part }

// ID returns the cNvPr id as written.
func (r Ref) ID() string { return r.id }

// Ord returns the 1-based ordinal among objects sharing the id.
func (r Ref) Ord() int { return r.ord }

// IsZero reports whether r is the zero Ref, which addresses nothing.
func (r Ref) IsZero() bool { return r.part == "" }

// Short returns the id as the views show it: s17, or s17~2.
func (r Ref) Short() string {
	s := "s" + r.id
	if r.ord > 1 {
		s += "~" + strconv.Itoa(r.ord)
	}
	return s
}

// String returns the full address: slide3#17, or slide3#17~2.
func (r Ref) String() string {
	return strings.TrimSuffix(path.Base(r.part), ".xml") + "#" + strings.TrimPrefix(r.Short(), "s")
}

// Alias names an object created earlier in the same plan, written $name.
// It is a separate type from Ref: an alias resolves to a Ref only after
// the operation that binds it has run.
type Alias string

// ParseAlias returns the alias in s ("$cache") and whether s is one.
func ParseAlias(s string) (Alias, bool) {
	name, ok := strings.CutPrefix(s, "$")
	if !ok || name == "" {
		return "", false
	}
	for _, r := range name {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "", false
		}
	}
	return Alias(name), true
}

// ErrBadAddress is returned for an address that does not parse.
var ErrBadAddress = errors.New("edit: bad address")

// ErrNoObject is returned when an address names no object.
var ErrNoObject = errors.New("edit: no such object")

// parseID splits an object id as written in a plan or view (s17, 17,
// s17~2, 17~2) into the cNvPr id and the ordinal.
func parseID(s string) (id string, ord int, err error) {
	s = strings.TrimPrefix(s, "s")
	id, n, dup := strings.Cut(s, "~")
	ord = 1
	if dup {
		if ord, err = strconv.Atoi(n); err != nil || ord < 1 {
			return "", 0, fmt.Errorf("%w: ordinal %q", ErrBadAddress, n)
		}
	}
	if id == "" {
		return "", 0, fmt.Errorf("%w: empty id", ErrBadAddress)
	}
	return id, ord, nil
}
