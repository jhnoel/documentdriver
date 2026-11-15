package cli

import "reflect"

// OpIndex finds the entries of a format's operation table by name and
// by the type of their Spec, in constant time. Build it once from the
// table (for example with sync.OnceValue; no init side effects). On a
// repeated name or Spec type the first entry wins, as a linear scan
// would answer; each format's table test forbids repeats.
type OpIndex[D any] struct {
	defs   []D
	byName map[string]int
	byType map[reflect.Type]int
}

// NewOpIndex indexes defs: name returns an entry's operation name, spec
// a zero Spec value of the entry (its New), whose dynamic type keys
// Of. defs is kept, not copied; the pointers Lookup and Of return point
// into it.
func NewOpIndex[D any](defs []D, name func(*D) string, spec func(*D) any) *OpIndex[D] {
	x := &OpIndex[D]{defs: defs, byName: make(map[string]int, len(defs)), byType: make(map[reflect.Type]int, len(defs))}
	for i := range defs {
		if _, dup := x.byName[name(&defs[i])]; !dup {
			x.byName[name(&defs[i])] = i
		}
		t := reflect.TypeOf(spec(&defs[i]))
		if _, dup := x.byType[t]; !dup {
			x.byType[t] = i
		}
	}
	return x
}

// Lookup returns the entry of the operation named name.
func (x *OpIndex[D]) Lookup(name string) (*D, bool) {
	i, ok := x.byName[name]
	if !ok {
		return nil, false
	}
	return &x.defs[i], true
}

// Of returns the entry whose Spec has spec's dynamic type.
func (x *OpIndex[D]) Of(spec any) (*D, bool) {
	i, ok := x.byType[reflect.TypeOf(spec)]
	if !ok {
		return nil, false
	}
	return &x.defs[i], true
}

// ValidAlias reports whether name is an alias name a plan's `as` may
// bind: [A-Za-z_][A-Za-z0-9_]*.
func ValidAlias(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// NonNil returns s, or an empty slice when s is nil, so that a result
// field marshals as [] and never null (the contract's lists).
func NonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// NonNilMap returns m, or an empty map when m is nil, so that it
// marshals as {} and never null.
func NonNilMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return m
}
