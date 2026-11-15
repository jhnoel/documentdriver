package ops

import (
	"reflect"
	"strings"
)

// Aliases. sheet.add's `as` names the sheet it adds; a later op's
// SheetAddress may be "$name". Aliases live for one plan, and the
// result's aliases give each name's address ("sheet:New").

// ResolveAliases replaces every "$name" SheetAddress in spec (a field of
// the op struct) with the sheet it names in aliases (name to
// "sheet:Name"). An alias no earlier op defined is not_found at the
// field.
func ResolveAliases(spec Spec, aliases map[string]string) error {
	rv := reflect.ValueOf(spec).Elem()
	rt := rv.Type()
	for i := range rt.NumField() {
		f := rv.Field(i)
		a, ok := f.Addr().Interface().(*SheetAddress)
		if !ok || a.Alias == "" {
			continue
		}
		field, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		to, ok := aliases[a.Alias]
		if !ok {
			return Refuse("not_found", field, "no earlier operation defined the alias $%s", a.Alias)
		}
		a.Name, a.Alias = strings.TrimPrefix(to, "sheet:"), ""
	}
	return nil
}

// AliasOf returns the `as` name a spec defines, "" for none: the As
// field of an op struct (sheet.add).
func AliasOf(spec Spec) string {
	rv := reflect.ValueOf(spec).Elem()
	if f := rv.FieldByName("As"); f.IsValid() && f.Kind() == reflect.Pointer && !f.IsNil() {
		return f.Elem().String()
	}
	return ""
}
