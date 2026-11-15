package apply

import (
	"reflect"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// Aliases (contract.md section 6). An op's `as` names what it created;
// later ops address it as `$name`. A list paragraph.insert names a group:
// `$name` for the whole group where an op takes several paragraphs (or
// as an insertion anchor: before its first, after its last), and
// `$name_N` for its Nth member (api.rs fold_typed and
// resolve_typed_aliases). Aliases live for one plan; the result maps each
// to the address it got.
//
// Before an operation runs, the paragraph addresses in its fields that
// are aliases are replaced by the ids they name, on a copy of the
// operation (the plan itself is never changed). Table aliases are left
// for the table operations, which resolve them through the registry.

var (
	paragraphAddress = reflect.TypeFor[ops.ParagraphAddress]()
	paragraphList    = reflect.TypeFor[[]ops.ParagraphAddress]()
	insertAnchor     = reflect.TypeFor[ops.InsertAnchor]()
	insertPosition   = reflect.TypeFor[ops.InsertPosition]()
	paragraphContent = reflect.TypeFor[ops.ParagraphContent]()
)

// jsonName is a struct field's contract name.
func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name
}

// resolveAliases returns a copy of spec whose paragraph addresses that
// are aliases name the paragraphs' ids. An alias no earlier op defined is
// not_found; a group where one paragraph is meant is invalid_operation.
func resolveAliases(reg *docxedit.Registry, spec ops.Spec) (ops.Spec, error) {
	v := reflect.ValueOf(spec)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return spec, nil
	}
	cp := reflect.New(v.Elem().Type())
	cp.Elem().Set(v.Elem())
	s := cp.Elem()
	t := s.Type()
	before := false
	for i := range t.NumField() {
		if t.Field(i).Type == insertPosition {
			before = s.Field(i).String() == "before"
		}
	}
	for i := range t.NumField() {
		f, field := t.Field(i), s.Field(i)
		if !field.CanSet() {
			continue
		}
		name := jsonName(f)
		switch f.Type {
		case paragraphAddress:
			id, err := single(reg, field.String(), name)
			if err != nil {
				return nil, err
			}
			field.SetString(id)
		case insertAnchor:
			id, err := anchor(reg, field.String(), name, before)
			if err != nil {
				return nil, err
			}
			field.SetString(id)
		case paragraphList:
			var out []ops.ParagraphAddress
			for j := range field.Len() {
				a := field.Index(j).String()
				if g, ok := groupOf(reg, a); ok {
					for _, id := range g {
						out = append(out, ops.ParagraphAddress(id))
					}
					continue
				}
				id, err := single(reg, a, name+"["+strconv.Itoa(j)+"]")
				if err != nil {
					return nil, err
				}
				out = append(out, ops.ParagraphAddress(id))
			}
			if out != nil {
				field.Set(reflect.ValueOf(out))
			}
		}
	}
	return cp.Interface().(ops.Spec), nil
}

// groupOf returns the members of the group alias a names.
func groupOf(reg *docxedit.Registry, a string) ([]string, bool) {
	name, ok := strings.CutPrefix(a, "$")
	if !ok {
		return nil, false
	}
	return reg.Group(name)
}

// single resolves a one-paragraph address: an id stays as it is.
func single(reg *docxedit.Registry, a, field string) (string, error) {
	name, ok := strings.CutPrefix(a, "$")
	if !ok {
		return a, nil
	}
	if g, ok := reg.Group(name); ok {
		return "", &ops.Rejection{Code: "invalid_operation", Field: field,
			Msg: "$" + name + " names " + strconv.Itoa(len(g)) + " paragraphs; address one as $" + name + "_N"}
	}
	id, err := reg.Alias(name)
	if err != nil {
		return "", &ops.Rejection{Code: "not_found", Field: field, Msg: "unknown plan-local alias $" + name}
	}
	if strings.HasPrefix(id, "$") {
		// A table alias: the table operations resolve it.
		return a, nil
	}
	return id, nil
}

// anchor resolves an insertion anchor: a group stands for its first
// member when inserting before it, its last when inserting after.
func anchor(reg *docxedit.Registry, a, field string, before bool) (string, error) {
	if g, ok := groupOf(reg, a); ok && len(g) > 0 {
		if before {
			return g[0], nil
		}
		return g[len(g)-1], nil
	}
	return single(reg, a, field)
}

// asName returns the op's `as` name, or "".
func asName(spec ops.Spec) string {
	v := reflect.ValueOf(spec)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return ""
	}
	s := v.Elem()
	for i := range s.NumField() {
		f := s.Type().Field(i)
		if jsonName(f) != "as" || f.Type != reflect.TypeFor[*string]() || s.Field(i).IsNil() {
			continue
		}
		return s.Field(i).Elem().String()
	}
	return ""
}

// isGroup reports whether the op creates a group (a list html).
func isGroup(spec ops.Spec) bool {
	v := reflect.ValueOf(spec)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return false
	}
	s := v.Elem()
	for i := range s.NumField() {
		if s.Type().Field(i).Type == paragraphContent {
			return s.Field(i).Interface().(ops.ParagraphContent).List
		}
	}
	return false
}

// duplicate returns a rejection error reporting a duplicate alias definition.
func duplicate(msg string) error {
	return &ops.Rejection{Code: "invalid_operation", Field: "as", Msg: msg}
}

// claimAlias refuses an op whose `as` is already defined, before it runs.
func claimAlias(reg *docxedit.Registry, spec ops.Spec) error {
	if name := asName(spec); name != "" && reg.Taken(name) {
		return duplicate("alias " + strconv.Quote(name) + " is already defined")
	}
	return nil
}

// defineAliases binds the op's `as` to what it created: one paragraph, or
// a group and its members name_1..name_N. An op that binds its own
// aliases (the table operations, through the registry) reports nothing
// created.
func defineAliases(reg *docxedit.Registry, spec ops.Spec, created []string) error {
	name := asName(spec)
	if name == "" || len(created) == 0 {
		return nil
	}
	if !isGroup(spec) {
		if err := reg.DefineAlias(name, created[0]); err != nil {
			return duplicate(err.Error())
		}
		return nil
	}
	for i := range created {
		member := name + "_" + strconv.Itoa(i+1)
		if reg.Taken(member) {
			return duplicate("paragraph alias " + strconv.Quote(member) + " of alias " + strconv.Quote(name) + " is already defined")
		}
	}
	if err := reg.DefineGroup(name, created); err != nil {
		return duplicate(err.Error())
	}
	for i, id := range created {
		if err := reg.DefineAlias(name+"_"+strconv.Itoa(i+1), id); err != nil {
			return duplicate(err.Error())
		}
	}
	return nil
}

// aliasAddresses maps each alias to the address it names at the end of
// the plan: a paragraph's id, or a table's current position. Group names
// map to nothing (their members are listed).
func aliasAddresses(reg *docxedit.Registry) map[string]string {
	out := map[string]string{}
	aliases, _ := reg.Aliases()
	for name, v := range aliases {
		if strings.HasPrefix(v, "$") {
			if _, addr, err := reg.Table(v); err == nil {
				out[name] = addr
			}
			continue
		}
		out[name] = v
	}
	return out
}
