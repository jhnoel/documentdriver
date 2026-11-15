package cli

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Decoder decodes parsed plan values into Go values, collecting every
// error with the path and span of the value it is about, so that a plan
// is refused with all its document-independent faults at once.
type Decoder struct {
	text []byte
	errs Errors
}

// NewDecoder returns a decoder for values parsed from text.
func NewDecoder(text []byte) *Decoder { return &Decoder{text: text} }

// Errs returns the errors so far, in the order found.
func (d *Decoder) Errs() Errors { return d.errs }

// Err returns the errors as an error, or nil when there are none.
func (d *Decoder) Err() error {
	if len(d.errs) == 0 {
		return nil
	}
	return d.errs
}

// Fail records an error about v (nil: no span) at path.
func (d *Decoder) Fail(code, path string, v *Value, format string, args ...any) {
	e := Errorf(code, path, format, args...)
	if v != nil {
		e.Span = SpanOf(d.text, v.Off, v.End)
	}
	d.errs = append(d.errs, e)
}

// Add records e, giving it v's span when it has none.
func (d *Decoder) Add(e *Error, v *Value) {
	if e.Span == nil && v != nil {
		e.Span = SpanOf(d.text, v.Off, v.End)
	}
	d.errs = append(d.errs, e)
}

// PlanDecoder is implemented by plan types with their own wire form.
// DecodePlan reports faults through d and leaves the receiver as far
// decoded as it got.
type PlanDecoder interface {
	DecodePlan(v *Value, path string, d *Decoder)
}

// Optional is implemented by a field type that tells absent from zero
// (a format's Opt[T]): Slot returns a pointer to decode the value into
// and MarkSet records that the field was written.
type Optional interface {
	Slot() any
	MarkSet()
}

// Join appends a field name to a path: an empty name returns path and
// an empty path returns name.
func Join(path, name string) string {
	if name == "" {
		return path
	}
	if path == "" {
		return name
	}
	return path + "." + name
}

// Index appends a list index to a path.
func Index(path string, i int) string { return path + "[" + strconv.Itoa(i) + "]" }

// Field describes one plan field of a struct.
type Field struct {
	// Name is the wire name; Index the field's index path in the struct.
	Name  string
	Index []int
	// Required fields must be written: every field that is not a pointer,
	// an Optional, or tagged `plan:"optional"`.
	Required bool
	// Needed marks a field (`plan:"needed"`) that the decoder takes as
	// optional but a rule of the operation requires: the reference lists
	// it as required, and the rule's error names it.
	Needed bool
	// Tag is the struct tag, for the doc, default and enum texts.
	Tag reflect.StructTag
	// Type is the field's type.
	Type reflect.Type
}

var optionalType = reflect.TypeFor[Optional]()

// Fields returns a struct type's plan fields in declaration order, with
// embedded structs flattened. A field without a json tag is not a plan
// field.
func Fields(t reflect.Type) []Field {
	var out []Field
	for i := range t.NumField() {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("json")
		if f.Anonymous && !ok && f.Type.Kind() == reflect.Struct {
			for _, sub := range Fields(f.Type) {
				sub.Index = append([]int{i}, sub.Index...)
				out = append(out, sub)
			}
			continue
		}
		if !ok || tag == "-" || !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		_, hasDefault := f.Tag.Lookup("default")
		optional := f.Type.Kind() == reflect.Pointer || reflect.PointerTo(f.Type).Implements(optionalType) ||
			f.Tag.Get("plan") == "optional" || hasDefault
		out = append(out, Field{Name: name, Index: []int{i}, Required: !optional, Needed: f.Tag.Get("plan") == "needed", Tag: f.Tag, Type: f.Type})
	}
	return out
}

// Struct decodes object v into the struct dst points to. skip names
// members that are not fields (an op's "op"). Unknown members and missing
// required fields are invalid_plan errors.
func (d *Decoder) Struct(v *Value, path string, dst any, skip ...string) {
	d.structValue(v, path, reflect.ValueOf(dst).Elem(), skip)
}

// structValue decodes object v into rv, recording invalid_plan errors for bad members.
func (d *Decoder) structValue(v *Value, path string, rv reflect.Value, skip []string) {
	if v.Kind != Object {
		d.Fail(CodeInvalidPlan, path, v, "expected an object, found %s", v.Kind)
		return
	}
	fields := Fields(rv.Type())
	seen := make([]bool, len(fields))
members:
	for _, m := range v.Members {
		for _, s := range skip {
			if m.Key == s {
				continue members
			}
		}
		k := -1
		for i, f := range fields {
			if f.Name == m.Key {
				k = i
				break
			}
		}
		if k < 0 {
			e := Errorf(CodeInvalidPlan, Join(path, m.Key), "unknown field %q", m.Key)
			e.Hint = expected(fields)
			e.Span = SpanOf(d.text, m.KeyOff, m.KeyOff+len(m.Key)+2)
			d.errs = append(d.errs, e)
			continue
		}
		seen[k] = true
		d.value(m.Value, Join(path, m.Key), rv.FieldByIndex(fields[k].Index))
	}
	for i, f := range fields {
		if !seen[i] && f.Required {
			d.Fail(CodeInvalidPlan, Join(path, f.Name), v, "missing field %q", f.Name)
		}
	}
}

// expected lists the field names valid in an object, or says it takes none.
func expected(fields []Field) string {
	if len(fields) == 0 {
		return "this object takes no fields"
	}
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return "fields: " + strings.Join(names, ", ")
}

// Value decodes v into the value dst points to.
func (d *Decoder) Value(v *Value, path string, dst any) {
	d.value(v, path, reflect.ValueOf(dst).Elem())
}

var (
	rawType         = reflect.TypeFor[json.RawMessage]()
	unmarshalerType = reflect.TypeFor[json.Unmarshaler]()
)

// value decodes v into rv, handling custom decoders, raw JSON and plain kinds.
func (d *Decoder) value(v *Value, path string, rv reflect.Value) {
	if v.Kind == Null {
		d.Fail(CodeInvalidPlan, path, v, "null is not a value; leave the field out")
		return
	}
	if rv.CanAddr() {
		switch x := rv.Addr().Interface().(type) {
		case PlanDecoder:
			x.DecodePlan(v, path, d)
			return
		case Optional:
			n := len(d.errs)
			d.value(v, path, reflect.ValueOf(x.Slot()).Elem())
			if len(d.errs) == n {
				x.MarkSet()
			}
			return
		}
		if rv.Type() != rawType && rv.Addr().Type().Implements(unmarshalerType) {
			if err := rv.Addr().Interface().(json.Unmarshaler).UnmarshalJSON(d.text[v.Off:v.End]); err != nil {
				d.Fail(CodeInvalidPlan, path, v, "%v", err)
			}
			return
		}
	}
	if rv.Type() == rawType {
		rv.SetBytes(append([]byte(nil), d.text[v.Off:v.End]...))
		return
	}
	switch rv.Kind() {
	case reflect.Pointer:
		p := reflect.New(rv.Type().Elem())
		d.value(v, path, p.Elem())
		rv.Set(p)
	case reflect.String:
		if d.want(v, path, String) {
			rv.SetString(v.Str)
		}
	case reflect.Bool:
		if d.want(v, path, Bool) {
			rv.SetBool(v.Bool)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if d.want(v, path, Number) {
			n, err := strconv.ParseInt(v.Num, 10, rv.Type().Bits())
			if err != nil {
				d.Fail(CodeInvalidPlan, path, v, "expected a whole number that fits %d bits, found %s", rv.Type().Bits(), v.Num)
				return
			}
			rv.SetInt(n)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if d.want(v, path, Number) {
			n, err := strconv.ParseUint(v.Num, 10, rv.Type().Bits())
			if err != nil {
				d.Fail(CodeInvalidPlan, path, v, "expected a whole number from 0 that fits %d bits, found %s", rv.Type().Bits(), v.Num)
				return
			}
			rv.SetUint(n)
		}
	case reflect.Float32, reflect.Float64:
		if d.want(v, path, Number) {
			f, err := strconv.ParseFloat(v.Num, 64)
			if err != nil || math.IsInf(f, 0) {
				d.Fail(CodeInvalidPlan, path, v, "the number %s is out of range", v.Num)
				return
			}
			rv.SetFloat(f)
		}
	case reflect.Slice:
		if d.want(v, path, Array) {
			s := reflect.MakeSlice(rv.Type(), len(v.Items), len(v.Items))
			for i, it := range v.Items {
				d.value(it, Index(path, i), s.Index(i))
			}
			rv.Set(s)
		}
	case reflect.Array:
		if d.want(v, path, Array) {
			if len(v.Items) != rv.Len() {
				d.Fail(CodeInvalidPlan, path, v, "expected a list of %d, found %d", rv.Len(), len(v.Items))
				return
			}
			for i, it := range v.Items {
				d.value(it, Index(path, i), rv.Index(i))
			}
		}
	case reflect.Map:
		if d.want(v, path, Object) {
			m := reflect.MakeMapWithSize(rv.Type(), len(v.Members))
			for _, mem := range v.Members {
				e := reflect.New(rv.Type().Elem()).Elem()
				d.value(mem.Value, Join(path, mem.Key), e)
				m.SetMapIndex(reflect.ValueOf(mem.Key), e)
			}
			rv.Set(m)
		}
	case reflect.Struct:
		d.structValue(v, path, rv, nil)
	case reflect.Interface:
		if rv.NumMethod() == 0 {
			if g, ok := d.generic(v, path); ok {
				rv.Set(reflect.ValueOf(g))
			}
			return
		}
		fallthrough
	default:
		d.Fail(CodeInternal, path, v, "no plan decoding for %s", rv.Type())
	}
}

// want reports whether v has kind k, recording an invalid_plan error otherwise.
func (d *Decoder) want(v *Value, path string, k Kind) bool {
	if v.Kind == k {
		return true
	}
	d.Fail(CodeInvalidPlan, path, v, "expected %s, found %s", k, v.Kind)
	return false
}

// generic is v as plain Go values (map[string]any, []any, float64,
// string, bool), for fields of type any.
func (d *Decoder) generic(v *Value, path string) (any, bool) {
	switch v.Kind {
	case Bool:
		return v.Bool, true
	case String:
		return v.Str, true
	case Number:
		f, err := strconv.ParseFloat(v.Num, 64)
		if err != nil || math.IsInf(f, 0) {
			d.Fail(CodeInvalidPlan, path, v, "the number %s is out of range", v.Num)
			return nil, false
		}
		return f, true
	case Array:
		out := make([]any, 0, len(v.Items))
		for i, it := range v.Items {
			g, ok := d.generic(it, Index(path, i))
			if !ok {
				return nil, false
			}
			out = append(out, g)
		}
		return out, true
	case Object:
		out := make(map[string]any, len(v.Members))
		for _, m := range v.Members {
			g, ok := d.generic(m.Value, Join(path, m.Key))
			if !ok {
				return nil, false
			}
			out[m.Key] = g
		}
		return out, true
	}
	d.Fail(CodeInvalidPlan, path, v, "null is not a value; leave the field out")
	return nil, false
}
