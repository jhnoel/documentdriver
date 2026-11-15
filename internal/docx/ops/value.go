package ops

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// value is the plan's wire form as canonical JSON and the oracle mapping
// build it: plans are read by the shared parser (internal/cli) and, for
// the mapping, converted here (fromCLI).

// kind is a JSON value's kind. Numbers keep docxdriver's three
// representations: a non-negative integer that fits u64, a negative one
// that fits i64, and everything else as f64.
type kind uint8

const (
	kNull kind = iota
	kBool
	kU64
	kI64
	kF64
	kString
	kArray
	kObject
)

// value is one JSON value. Object members parsed by fromCLI are sorted
// by key.
type value struct {
	kind kind
	b    bool
	u    uint64
	i    int64
	f    float64
	s    string
	a    []*value
	m    []member
}

// member is one object entry.
type member struct {
	key string
	val *value
}

// get returns the member named key, or nil.
func (v *value) get(key string) *value {
	i, ok := slices.BinarySearchFunc(v.m, key, func(m member, k string) int { return cmp.Compare(m.key, k) })
	if !ok {
		return nil
	}
	return v.m[i].val
}

// parseJSON parses text with the shared parser into a value.
func parseJSON(text []byte) (*value, error) {
	v, err := cli.ParseJSON(text)
	if err != nil {
		return nil, err
	}
	return fromCLI(v), nil
}

// isHexDigit reports whether byte c is a hex digit.
func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// fromCLI converts a value the shared decoder parsed into this
// package's value: object members sorted by key, and a number as an
// unsigned or signed integer when it is written as one and fits, else a
// float.
func fromCLI(v *cli.Value) *value {
	switch v.Kind {
	case cli.Null:
		return &value{kind: kNull}
	case cli.Bool:
		return &value{kind: kBool, b: v.Bool}
	case cli.String:
		return &value{kind: kString, s: v.Str}
	case cli.Number:
		if strings.TrimLeft(v.Num, "-0") == "" && strings.HasPrefix(v.Num, "-") {
			return &value{kind: kF64, f: math.Copysign(0, -1)} // -0 is a float, as docxdriver reads it
		}
		if !strings.ContainsAny(v.Num, ".eE") {
			if u, err := strconv.ParseUint(v.Num, 10, 64); err == nil {
				return &value{kind: kU64, u: u}
			}
			if i, err := strconv.ParseInt(v.Num, 10, 64); err == nil {
				return &value{kind: kI64, i: i}
			}
		}
		f, _ := strconv.ParseFloat(v.Num, 64) //nolint:errcheck // the grammar was checked; out of range gives ±Inf, which the field types refuse
		return &value{kind: kF64, f: f}
	case cli.Array:
		out := &value{kind: kArray, a: make([]*value, len(v.Items))}
		for i, it := range v.Items {
			out.a[i] = fromCLI(it)
		}
		return out
	}
	out := &value{kind: kObject, m: make([]member, len(v.Members))}
	for i, m := range v.Members {
		out.m[i] = member{key: m.Key, val: fromCLI(m.Value)}
	}
	slices.SortFunc(out.m, func(a, b member) int { return strings.Compare(a.key, b.key) })
	return out
}
