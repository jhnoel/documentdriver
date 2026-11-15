package ops

import (
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Plan is a decoded, validated and normalised docx plan.
type Plan struct {
	// Base is the hash of the document the plan was written against.
	Base SourceHash `json:"base"`
	// Author is the name revisions and comments are recorded under.
	Author string `json:"author"`
	// ChangeMode is track or direct.
	ChangeMode ChangeMode `json:"change_mode"`
	// Ops are the operations, in order.
	Ops []Op `json:"ops"`
}

// Op is one operation of a plan.
type Op struct {
	// Name is the op tag; Spec is its fields.
	Name string
	Spec Spec
}

// Def returns the operation's table entry.
func (o Op) Def() *OpDef {
	d, ok := Lookup(o.Name)
	if !ok {
		return &OpDef{Name: o.Name}
	}
	return d
}

// DecodePlan implements cli.PlanDecoder: the op member names the
// operation, the others are its fields.
func (o *Op) DecodePlan(v *cli.Value, path string, d *cli.Decoder) {
	if v.Kind != cli.Object {
		bad(d, v, path, "an operation is an object, found %s", v.Kind)
		return
	}
	t := v.Get("op")
	if t == nil {
		bad(d, v, cli.Join(path, "op"), "missing field \"op\"")
		return
	}
	name, ok := planString(d, t, cli.Join(path, "op"))
	if !ok {
		return
	}
	def, ok := Lookup(name)
	if !ok {
		bad(d, t, cli.Join(path, "op"), "unknown operation %q (docx help ops lists them)", name)
		return
	}
	spec := def.New()
	decodeSpec(d, v, path, spec)
	*o = Op{Name: name, Spec: spec}
}

// encodeJSON returns the operation with its op name as an object.
func (o Op) encodeJSON() *value {
	out := encodeStruct(reflect.ValueOf(o.Spec).Elem())
	out.m = append(out.m, member{key: "op", val: &value{kind: kString, s: o.Name}})
	return out
}

// Diagnostic is why a plan was rejected, as docxdriver reports it.
type Diagnostic struct {
	// Code is invalid_json, invalid_plan, empty_plan, invalid_author or
	// invalid_operation.
	Code string `json:"code"`
	// Message says what is wrong.
	Message string `json:"message"`
	// Path names the offending value ("ops[2].format.width"), if any.
	Path string `json:"path,omitempty"`
	// Span is where in the plan text, if known.
	Span *Span `json:"span,omitempty"`
}

// Error returns the message.
func (d *Diagnostic) Error() string { return d.Message }

// Span is a range of the plan text: 1-based lines and columns.
type Span struct {
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndLine   int `json:"end_line"`
	EndColumn int `json:"end_column"`
}

// ParsePlanJSON decodes, validates and normalises a plan in
// docxdriver's envelope (base, author and change_mode required), as
// docxdriver 0.5.2's parse_plan_json does: decoding faults first
// (syntax, unknown or missing fields, wrong types, malformed values),
// through the shared decoder, then an empty plan, an empty author, and
// the first operation that breaks a rule. The diagnostic is the first
// fault. It serves the parity tests and the tools that drive docxdriver
// plans; the CLI reads plans with ParsePlan.
func ParsePlanJSON(text []byte) (*Plan, *Diagnostic) {
	root, perr := cli.ParseJSON(text)
	if perr != nil {
		dg := &Diagnostic{Code: "invalid_json", Message: perr.Message}
		if s := perr.Span; s != nil {
			dg.Span = &Span{Line: s.Line, Column: s.Column, EndLine: s.EndLine, EndColumn: s.EndColumn}
		}
		return nil, dg
	}
	d := cli.NewDecoder(text)
	var p Plan
	d.Struct(root, "", &p)
	if errs := d.Errs(); len(errs) > 0 {
		return nil, &Diagnostic{Code: "invalid_plan", Message: errs[0].Message, Path: errs[0].Path, Span: fieldSpan(string(text), errs[0].Path)}
	}
	if dg := validate(&p); dg != nil {
		dg.Span = fieldSpan(string(text), dg.Path)
		return nil, dg
	}
	return &p, nil
}

// validate is validate_plan: empty plan, empty author, then each
// operation's rules; it normalises the operations it accepts.
func validate(p *Plan) *Diagnostic {
	if len(p.Ops) == 0 {
		return &Diagnostic{Code: "empty_plan", Message: "plan must contain at least one operation", Path: "ops"}
	}
	if p.Author == "" {
		return &Diagnostic{Code: "invalid_author", Message: "author must not be empty", Path: "author"}
	}
	for i, op := range p.Ops {
		d := op.Def()
		at := "ops[" + strconv.Itoa(i) + "]"
		if msg, path, bad := d.check(op.Spec); bad {
			return &Diagnostic{Code: "invalid_operation", Message: msg, Path: cli.Join(at, path)}
		}
		if d.Normalize != nil {
			d.Normalize(op.Spec)
		}
	}
	return nil
}

// fieldSpan is docxdriver's diag_span: the field's name in its
// operation's object, else the last path segment's first occurrence in
// the text, else the first character. Index suffixes ("cells[2]") are
// searched without the index.
func fieldSpan(text, path string) *Span {
	if path == "" {
		return nil
	}
	key := path
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		key = key[i+1:]
	}
	// An index suffix is this package's own ("cells[2]"); the oracle's
	// paths ("ops[0]") are searched as they are.
	if k := strings.IndexByte(key, '['); k > 0 && strings.Contains(path, ".") {
		key = key[:k]
	}
	if s := opFieldSpan(text, path, key); s != nil {
		return s
	}
	if i := strings.Index(text, key); i >= 0 {
		return makeSpan(text, i, i+len(key))
	}
	if text == "" {
		return nil
	}
	_, size := utf8.DecodeRuneInString(text)
	return makeSpan(text, 0, min(len(text), max(1, size)))
}

// opFieldSpan is exact_field_span's JSON half: find key inside the
// index-th object of the ops array.
func opFieldSpan(text, path, key string) *Span {
	rest, ok := strings.CutPrefix(path, "ops[")
	if !ok {
		return nil
	}
	index, field, ok := strings.Cut(rest, "]")
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(index)
	if err != nil || n < 0 || !strings.HasPrefix(field, ".") || key == "" {
		return nil
	}
	arrayStart := strings.Index(text, `"ops"`)
	if arrayStart < 0 {
		return nil
	}
	depth := 0
	objStart := -1
	var objects [][2]int
	for off, ch := range text[arrayStart:] {
		switch {
		case ch == '[':
			depth++
		case ch == '{' && depth > 0:
			if depth == 1 {
				objStart = arrayStart + off
			}
			depth++
		case ch == '}' && depth > 1:
			depth--
			if depth == 1 && objStart >= 0 {
				objects = append(objects, [2]int{objStart, arrayStart + off + 1})
				objStart = -1
			}
		case ch == ']' && depth > 0:
			depth--
		}
	}
	if n >= len(objects) {
		return nil
	}
	start, end := objects[n][0], objects[n][1]
	local := strings.Index(text[start:end], key)
	if local < 0 {
		return nil
	}
	return makeSpan(text, start+local, start+local+len(key))
}

// makeSpan is make_span: 1-based lines and character columns.
func makeSpan(text string, start, end int) *Span {
	lc := func(at int) (int, int) {
		prefix := text[:min(at, len(text))]
		line := strings.Count(prefix, "\n") + 1
		last := prefix[strings.LastIndexByte(prefix, '\n')+1:]
		return line, utf8.RuneCountInString(last) + 1
	}
	l, c := lc(start)
	el, ec := lc(end)
	return &Span{Line: l, Column: c, EndLine: el, EndColumn: ec}
}
