package edit

import "fmt"

// Branched is a write handle over one or more branch elements: a
// format's handle type, which wraps the core's Handle.
type Branched interface {
	Branches() int
}

// Reading is a value read from a document: present, or absent with the
// reason (a format's value-with-origin type).
type Reading[T any] interface {
	Get() (T, bool)
	Reason() string
}

// Codec reads, writes and shows one property of type T through handles
// of type H, its reads of type R. A format keeps its table of codecs and
// fixes H and R with a type alias.
type Codec[H Branched, T any, R Reading[T]] struct {
	// Name is the property's name in plans and views, such as "transform".
	Name string
	// Read reads the value of branch i of the handle (0 is the projected
	// branch, the one the views show): present, or absent with the
	// reason.
	Read func(h H, branch int) R
	// Write stores v in every branch.
	Write func(h H, v T) error
	// Show formats v exactly as the views show it. Two values that show
	// the same are the same value to the agent.
	Show func(v T) string
}

// Set writes want unless the handle already shows it: the round-trip
// rule. When the current value shows as want, it returns without
// touching any XML. Otherwise it writes, re-reads every branch and fails
// unless each shows want; the failure wraps inexpressible, the format's
// error for a value it cannot store so that it reads back as requested.
func Set[H Branched, T any, R Reading[T]](h H, c Codec[H, T, R], want T, inexpressible error) error {
	if cur, ok := c.Read(h, 0).Get(); ok && c.Show(cur) == c.Show(want) {
		return nil
	}
	if err := c.Write(h, want); err != nil {
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	return Check(h, c, want, inexpressible)
}

// Check fails, wrapping inexpressible, unless every branch of the handle
// shows want: the read-back half of Set, for writes made another way
// (a move relative to the stored value).
func Check[H Branched, T any, R Reading[T]](h H, c Codec[H, T, R], want T, inexpressible error) error {
	for i := range h.Branches() {
		got := c.Read(h, i)
		v, ok := got.Get()
		if !ok {
			return fmt.Errorf("%s: %w: branch %d does not read back: %s", c.Name, inexpressible, i, got.Reason())
		}
		if g, w := c.Show(v), c.Show(want); g != w {
			return fmt.Errorf("%s: %w: branch %d reads back as %s, want %s", c.Name, inexpressible, i, g, w)
		}
	}
	return nil
}
