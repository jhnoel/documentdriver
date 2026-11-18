package cli

import (
	"fmt"
	"reflect"
	"strings"
)

// OpDoc is one operation in the reference a format's op table generates.
type OpDoc struct {
	// Name is the operation name; Summary one line or paragraph about it.
	Name, Summary string
	// Fields are its plan fields.
	Fields []FieldDoc
	// Rules are its validation rules, as sentences.
	Rules []string
}

// FieldDoc is one field of an operation.
type FieldDoc struct {
	Name, Type, Doc string
	Required        bool
	Default         string
	Enum            []string
}

// FieldDocs describes the plan fields of a spec struct type from its
// tags: json (the name), doc, default and enum (comma-separated; an
// entry "a=b" documents a). typeName names a field's type for the
// reference; nil uses TypeName.
func FieldDocs(t reflect.Type, typeName func(reflect.Type) string) []FieldDoc {
	if typeName == nil {
		typeName = TypeName
	}
	var out []FieldDoc
	for _, f := range Fields(t) {
		if f.Name == "op" {
			continue
		}
		fd := FieldDoc{Name: f.Name, Type: typeName(f.Type), Doc: f.Tag.Get("doc"), Required: f.Required || f.Needed}
		if def, ok := f.Tag.Lookup("default"); ok && def != "null" {
			fd.Default = def
		}
		if enum := f.Tag.Get("enum"); enum != "" {
			for e := range strings.SplitSeq(enum, ",") {
				name, _, _ := strings.Cut(e, "=")
				fd.Enum = append(fd.Enum, name)
			}
		}
		out = append(out, fd)
	}
	return out
}

// HelpTyper is a plan field type that names its own help type, for a
// field that takes more than one JSON form ("none" or an object).
type HelpTyper interface{ HelpType() string }

var helpTyperType = reflect.TypeFor[HelpTyper]()

// TypeName names a Go type as a plan reader sees it.
func TypeName(t reflect.Type) string {
	if t.Kind() != reflect.Pointer && t.Implements(helpTyperType) {
		return reflect.Zero(t).Interface().(HelpTyper).HelpType()
	}
	if reflect.PointerTo(t).Implements(optionalType) {
		if f, ok := t.FieldByName("V"); ok {
			return TypeName(f.Type)
		}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return TypeName(t.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		return "list of " + TypeName(t.Elem())
	case reflect.Array:
		return fmt.Sprintf("list of %d %ss", t.Len(), TypeName(t.Elem()))
	case reflect.Map, reflect.Struct:
		return "object"
	}
	return "value"
}

// OpsHelp is the `help ops` text: every operation on one line with its
// fields, required ones first and optional ones in brackets.
func OpsHelp(tool, intro string, ops []OpDoc) string {
	var b strings.Builder
	if intro != "" {
		b.WriteString(strings.TrimSpace(intro) + "\n\n")
	}
	width := 0
	for _, op := range ops {
		width = max(width, len(op.Name))
	}
	for _, op := range ops {
		var req, opt []string
		for _, f := range op.Fields {
			if f.Required {
				req = append(req, f.Name)
			} else {
				opt = append(opt, f.Name)
			}
		}
		line := strings.Join(req, ", ")
		if len(opt) > 0 {
			if line != "" {
				line += " "
			}
			line += "[" + strings.Join(opt, ", ") + "]"
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, op.Name, line)
	}
	fmt.Fprintf(&b, "\n`%s help <op>` gives one operation's fields, types and rules.\n", tool)
	return b.String()
}

// OpHelp is the `help <op>` text.
func OpHelp(op OpDoc) string {
	var b strings.Builder
	b.WriteString(op.Name + "\n")
	if op.Summary != "" {
		b.WriteString("\n" + strings.TrimSpace(op.Summary) + "\n")
	}
	if len(op.Fields) > 0 {
		b.WriteString("\nfields:\n")
	}
	for _, f := range op.Fields {
		kind := f.Type
		if len(f.Enum) > 0 {
			kind = strings.Join(f.Enum, "|")
		}
		need := "optional"
		switch {
		case f.Required:
			need = "required"
		case f.Default != "":
			need = "default " + f.Default
		}
		fmt.Fprintf(&b, "  %-18s %s, %s", f.Name, kind, need)
		if f.Doc != "" {
			b.WriteString(". " + f.Doc)
		}
		b.WriteString("\n")
	}
	if len(op.Rules) > 0 {
		b.WriteString("\nrules:\n")
		for _, r := range op.Rules {
			b.WriteString("  " + r + "\n")
		}
	}
	return b.String()
}
