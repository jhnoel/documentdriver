package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// obj is a JSON object; the writer sorts its keys, as serde_json's
// BTreeMap does.
type obj map[string]any

// float is a JSON number written with a decimal point ("0.0"), as
// schemars writes a minimum of 0 for unsigned integers.
type float float64

// typeDocs are the type-level descriptions docxdriver's doc comments
// give, which schemars copies into the schema.
var typeDocs = map[string]string{
	"TableWidth":       "Table widths use auto, points, or percent (0 < value <= 100 for percent).",
	"ListLevel":        "One bounded level in a new multilevel list definition. Array position is level (0..8).",
	"TextEdit":         "One replacement inside a `text.revise` scope.",
	"HighlightColor":   "The exact ECMA-376 ST_HighlightColor palette; None is an explicit off override.",
	"ParagraphContent": "`paragraph.insert`'s `html`: one paragraph's inline content, or a list of them inserted as consecutive paragraphs in list order.",
}

// fixedDefs are the definitions of types with their own wire form, as
// docxdriver's generate_typed_schema writes them.
func fixedDefs() map[string]obj {
	enum := func(name string) obj {
		o := obj{"type": "string", "enum": toAny(ops.UnitEnum(name))}
		if d := typeDocs[name]; d != "" {
			o["description"] = d
		}
		return o
	}
	tagged := func(tag, variant string, props obj, required ...string) obj {
		props[tag] = obj{"enum": []any{variant}, "type": "string"}
		req := append([]string{tag}, required...)
		return obj{"additionalProperties": false, "properties": props, "required": toAny(sortStrings(req)), "type": "object"}
	}
	return map[string]obj{
		"SourceHash":       {"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
		"Points":           {"type": "number", "minimum": 0, "multipleOf": float(0.05)},
		"SignedPoints":     {"type": "number", "minimum": -100000, "maximum": 100000, "multipleOf": float(0.05)},
		"ParagraphAddress": {"type": "string", "pattern": `^(?:[A-F0-9]{8}(?:~[1-9][0-9]*)?|\$[A-Za-z_][A-Za-z0-9_]*)$`},
		"InsertAnchor":     {"type": "string", "pattern": `^(?:[A-F0-9]{8}(?:~[1-9][0-9]*)?|\$[A-Za-z_][A-Za-z0-9_]*|table:[1-9][0-9]*)$`},
		"SectionAddress":   {"type": "string", "pattern": `^section:(?:[0-9A-Fa-f]{8}(?:~[1-9][0-9]*)?|[1-9][0-9]*|final|\$[A-Za-z_][A-Za-z0-9_]*)$`},
		"CommentAddress":   {"type": "string", "pattern": "^comment:[0-9]+$"},
		"BookmarkAddress":  {"type": "string", "pattern": "^bookmark:.+$"},
		"EquationAddress":  {"type": "string", "pattern": `^(?:[A-F0-9]{8}(?:~[1-9][0-9]*)?|\$[A-Za-z_][A-Za-z0-9_]*)(?:/equation:[1-9][0-9]*)?$`},
		"RevisionAuthor":   {"type": "string", "minLength": 1},
		"RevisionScope": {"oneOf": []any{
			obj{"type": "string", "pattern": "^revision:[0-9]+$"},
			obj{"type": "array", "items": obj{"type": "string", "pattern": `^(?:[A-F0-9]{8}(?:~[1-9][0-9]*)?|\$[A-Za-z_][A-Za-z0-9_]*)$`}},
		}},
		"ParagraphContent": {"anyOf": []any{obj{"type": "string"}, obj{"type": "array", "items": obj{"type": "string"}}},
			"description": typeDocs["ParagraphContent"]},
		"ChangeMode":     enum("ChangeMode"),
		"InsertPosition": enum("InsertPosition"),
		"CommentStatus":  enum("CommentStatus"),
		"ChromeKind":     enum("ChromeKind"),
		"HighlightColor": enum("HighlightColor"),
		"TableWidth": {"description": typeDocs["TableWidth"], "oneOf": []any{
			tagged("unit", "auto", obj{}),
			tagged("unit", "points", obj{"value": ref("Points")}, "value"),
			tagged("unit", "percent", obj{"value": obj{"format": "double", "type": "number"}}, "value"),
		}},
		"LineSpacing": {"oneOf": []any{
			tagged("mode", "multiple", obj{"value": obj{"format": "double", "type": "number"}}, "value"),
			tagged("mode", "exact", obj{"value": ref("Points")}, "value"),
			tagged("mode", "at_least", obj{"value": ref("Points")}, "value"),
		}},
	}
}

// toAny converts a string slice to an any slice.
func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// sortStrings returns a sorted copy of the strings.
func sortStrings(ss []string) []string {
	out := append([]string(nil), ss...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ref returns a $ref object for the named definition.
func ref(name string) obj { return obj{"$ref": "#/$defs/" + name} }

// gen collects definitions while walking the plan's types.
type gen struct {
	defs  map[string]obj
	fixed map[string]obj
}

// planSchema builds the plan JSON Schema: the Plan definition as the
// root, with every definition it reaches.
func planSchema() obj {
	g := &gen{defs: map[string]obj{}, fixed: fixedDefs()}
	var editOps []any
	for _, d := range ops.Ops {
		o := g.structSchema(reflect.TypeOf(d.New()).Elem(), "op", d.Name)
		o["description"] = d.Description()
		editOps = append(editOps, o)
	}
	g.defs["editOp"] = obj{"oneOf": editOps}
	g.defs["Plan"] = obj{
		"additionalProperties": false,
		"properties": obj{
			"author":      obj{"type": "string"},
			"base":        g.typeSchema(reflect.TypeFor[ops.SourceHash]()),
			"change_mode": g.typeSchema(reflect.TypeFor[ops.ChangeMode]()),
			"ops":         obj{"items": ref("editOp"), "type": "array"},
		},
		"required": []any{"author", "base", "change_mode", "ops"},
		"type":     "object",
	}
	return obj{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"title":       "Plan",
		"description": "A docx plan in the agent contract's grammar (docs/contract.md). Generated from internal/docx/ops by tools/schemagen.",
		"allOf":       []any{ref("Plan")},
		"$defs":       g.defs,
	}
}

// typeSchema is the schema of a value of type t, adding definitions.
func (g *gen) typeSchema(t reflect.Type) obj {
	if _, ok := g.fixed[t.Name()]; ok && t.PkgPath() != "" {
		g.defs[t.Name()] = g.fixed[t.Name()]
		g.walkFixed(t.Name())
		return ref(t.Name())
	}
	switch t.Kind() {
	case reflect.String:
		return obj{"type": "string"}
	case reflect.Bool:
		return obj{"type": "boolean"}
	case reflect.Uint32:
		return obj{"format": "uint32", "minimum": float(0), "type": "integer"}
	case reflect.Uint8:
		return obj{"format": "uint8", "minimum": float(0), "type": "integer"}
	case reflect.Float64:
		return obj{"format": "double", "type": "number"}
	case reflect.Slice:
		return obj{"items": g.typeSchema(t.Elem()), "type": "array"}
	case reflect.Struct:
		if _, ok := g.defs[t.Name()]; !ok {
			g.defs[t.Name()] = obj{} // placeholder against cycles
			o := g.structSchema(t, "", "")
			if d := typeDocs[t.Name()]; d != "" {
				o["description"] = d
			}
			g.defs[t.Name()] = o
		}
		return ref(t.Name())
	}
	panic(fmt.Sprintf("schemagen: no schema for %s", t))
}

// walkFixed adds the definitions a fixed definition refers to.
func (g *gen) walkFixed(name string) {
	switch name {
	case "TableWidth", "LineSpacing":
		g.defs["Points"] = g.fixed["Points"]
	}
}

// structSchema is a deny_unknown_fields object; tag, when set, is an
// internally tagged enum's tag property with the single value variant.
func (g *gen) structSchema(t reflect.Type, tag, variant string) obj {
	props := obj{}
	var required []string
	if tag != "" {
		props[tag] = obj{"enum": []any{variant}, "type": "string"}
		required = append(required, tag)
	}
	for f := range t.Fields() {
		f := f
		jsonTag, ok := f.Tag.Lookup("json")
		if !ok {
			continue
		}
		name, opts, _ := strings.Cut(jsonTag, ",")
		def, hasDefault := f.Tag.Lookup("default")
		omitzero := opts == "omitzero"
		// An optional field is left out, never null: the plan parser
		// (internal/cli) refuses an explicit null everywhere, so a
		// pointer field's schema is its element's, unnullable, and
		// optionality is expressed only by omitting it from required.
		var p obj
		if f.Type.Kind() == reflect.Pointer {
			p = g.typeSchema(f.Type.Elem())
		} else {
			p = g.typeSchema(f.Type)
			if !hasDefault {
				required = append(required, name)
			} else if _, isRef := p["$ref"]; isRef && !omitzero {
				p = obj{"allOf": []any{p}}
			}
		}
		if hasDefault && !omitzero && def != "null" {
			p["default"] = defaultValue(def)
		}
		if d := f.Tag.Get("doc"); d != "" {
			p["description"] = d
		}
		props[name] = p
	}
	o := obj{"additionalProperties": false, "properties": props, "type": "object"}
	if len(required) > 0 {
		o["required"] = toAny(sortStrings(required))
	}
	return o
}

// defaultValue parses a default tag value and panics on unknown ones.
func defaultValue(text string) any {
	switch text {
	case "[]":
		return []any{}
	case "false":
		return false
	case "0":
		return 0
	case `""`:
		return ""
	}
	panic("schemagen: unknown default " + text)
}
