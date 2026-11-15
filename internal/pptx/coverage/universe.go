// Package coverage measures what the tool covers of what a diagram edit
// can touch. The universe is every element and attribute path reachable
// from the design's object kinds and the slide-level context the read
// model reads, generated from the pinned ECMA-376 transitional schemas
// (GenerateUniverse). The registry (Claims) declares, reader by reader and
// codec by codec, which paths each one honours; tests in this package
// check every declaration against the read model and the plan operations
// on fixtures, and fail on a declaration the code does not honour. Build
// joins universe, registry, operation matrix and corpus counts into the
// report that tools/coverage writes to eval/coverage.
package coverage

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"slices"
)

// Item is one element or attribute path of the universe.
type Item struct {
	// Path is the prefix:local chain from the root, attributes as a last
	// "@name" step, xsd:any as "*": "p:sp/p:spPr/a:ln/@w".
	Path string `json:"path"`
	// Node is element, attribute or any.
	Node string `json:"node"`
	// Type is the XSD type, qualified (a:ST_LineWidth, xsd:boolean).
	Type string `json:"type,omitempty"`
	// Enum names the enumeration (a key of Universe.Enums) when Type is
	// one.
	Enum string `json:"enum,omitempty"`
	// Choice names the choice the element is an alternative of: a named
	// group (a:EG_FillProperties) or "<type>#choiceN".
	Choice string `json:"choice,omitempty"`
	// Min and Max are the occurrence bounds in the parent (an element in a
	// choice or optional group has Min 0; Max "unbounded" when it or its
	// group repeats). Attributes have Min 1 when required.
	Min     int     `json:"min"`
	Max     string  `json:"max"`
	Default *string `json:"default,omitempty"`
	// Root is the root path the item belongs to.
	Root string `json:"root"`
	// Object marks an object element below a group or shape tree: it is a
	// root of its own and not expanded here.
	Object bool `json:"object,omitempty"`
	// Recursive marks an element whose type already occurs on its path:
	// recorded, not expanded.
	Recursive bool `json:"recursive,omitempty"`
	// Stop says why a slide-level subtree is recorded but not expanded.
	Stop string `json:"stop,omitempty"`
	// Factored names the pseudo-root ("(a:CT_SchemeColor)") under which
	// the element's content is enumerated once for every position.
	Factored string `json:"factored,omitempty"`
}

// Universe is the generated enumeration.
type Universe struct {
	Schema string              `json:"schema"`
	SHA256 string              `json:"sha256"`
	Rule   string              `json:"recursion_rule"`
	Roots  []Root              `json:"roots"`
	Enums  map[string][]string `json:"enums"`
	// Simple gives the built-in base and bounds of every other simple
	// type an item uses.
	Simple map[string]SimpleType `json:"simple"`
	Items  []Item                `json:"items"`
}

// SimpleType is a simple type's built-in base (xsd:int...) and the
// bounds and pattern of its restriction, as written ("" when none).
type SimpleType struct {
	Base    string `json:"base"`
	Min     string `json:"min,omitempty"`
	Max     string `json:"max,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

// RecursionRule is the cap documented in the universe file.
const RecursionRule = "A complex type is not expanded below an element whose path already holds an element of the same type: " +
	"the element is recorded with recursive=true. Object elements below a group or shape tree are recorded (object=true) and expanded only as roots. " +
	"Slide transitions, timing and background are recorded (stop) but not expanded. xsd:any is one item named *. " +
	"Value types (colours, gradient and pattern fill bodies, effect containers, 3D, list-style levels, blips) are enumerated once under a pseudo-root \"(type)\"; " +
	"at each position only the element is an item, with factored naming the pseudo-root."

// LoadUniverse reads a universe file.
func LoadUniverse(path string) (*Universe, error) { return loadJSON[Universe](path) }

// loadJSON reads the JSON file at path and unmarshals it into a new T.
func loadJSON[T any](path string) (*T, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// MarshalStable writes v, a struct, as JSON with one line per top-level
// field, and one line per element of a top-level slice or map (maps in
// key order), so that diffs of the generated files show one changed line
// per changed item. HTML escaping is off.
func MarshalStable(v any) ([]byte, error) {
	raw, err := compact(v)
	if err != nil {
		return nil, err
	}
	// Re-read the top level as ordered fields: marshal of a struct keeps
	// field order, so decode the keys in order with a token stream.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if _, err := dec.Token(); err != nil { // {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("{")
	first := true
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		kb, _ := compact(kt.(string))
		b.WriteString("\n")
		b.Write(kb)
		b.WriteString(":")
		switch {
		case len(val) > 0 && val[0] == '[':
			var elems []json.RawMessage
			if err := json.Unmarshal(val, &elems); err != nil {
				return nil, err
			}
			b.WriteString("[")
			for i, e := range elems {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString("\n")
				b.Write(e)
			}
			b.WriteString("\n]")
		case len(val) > 0 && val[0] == '{':
			var m map[string]json.RawMessage
			if err := json.Unmarshal(val, &m); err != nil {
				return nil, err
			}
			keys := slices.Sorted(maps.Keys(m))
			b.WriteString("{")
			for i, k := range keys {
				if i > 0 {
					b.WriteString(",")
				}
				kb, _ := compact(k)
				b.WriteString("\n")
				b.Write(kb)
				b.WriteString(":")
				b.Write(m[k])
			}
			b.WriteString("\n}")
		default:
			b.Write(val)
		}
	}
	b.WriteString("\n}\n")
	return b.Bytes(), nil
}

// compact marshals v without HTML escaping or a trailing newline.
func compact(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// ByPath indexes the universe's items.
func (u *Universe) ByPath() map[string]*Item {
	m := make(map[string]*Item, len(u.Items))
	for i := range u.Items {
		m[u.Items[i].Path] = &u.Items[i]
	}
	return m
}
