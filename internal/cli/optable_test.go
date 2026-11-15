package cli

import (
	"encoding/json"
	"testing"
)

type opTestDef struct {
	name string
	spec any
}
type opTestA struct{}
type opTestB struct{}

func TestOpIndex(t *testing.T) {
	defs := []opTestDef{{"a", &opTestA{}}, {"b", &opTestB{}}, {"a", &opTestB{}}}
	idx := NewOpIndex(defs, func(d *opTestDef) string { return d.name }, func(d *opTestDef) any { return d.spec })
	for _, tc := range []struct {
		name  string
		index int
	}{{"a", 0}, {"b", 1}, {"missing", -1}} {
		got, ok := idx.Lookup(tc.name)
		if ok != (tc.index >= 0) || ok && got != &defs[tc.index] {
			t.Errorf("Lookup(%q) = %v, %v", tc.name, got, ok)
		}
	}
	for _, tc := range []struct {
		spec  any
		index int
	}{{&opTestA{}, 0}, {&opTestB{}, 1}, {42, -1}} {
		got, ok := idx.Of(tc.spec)
		if ok != (tc.index >= 0) || ok && got != &defs[tc.index] {
			t.Errorf("Of(%T) = %v, %v", tc.spec, got, ok)
		}
	}
}
func TestValidAlias(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{{"a", true}, {"_a1", true}, {"", false}, {"1a", false}, {"a-b", false}, {"é", false}} {
		if ValidAlias(tc.name) != tc.valid {
			t.Errorf("ValidAlias(%q)", tc.name)
		}
	}
}
func TestNonNil(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{{NonNil([]int(nil)), "[]"}, {NonNilMap(map[string]int(nil)), "{}"}, {NonNil([]int{1}), "[1]"}, {NonNilMap(map[string]int{"x": 1}), `{"x":1}`}} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Errorf("marshal = %s, %v", b, err)
		}
	}
}
