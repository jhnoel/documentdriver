package cli

import (
	"slices"
	"strings"
)

// DecodeEnum sets *dst to v when v is a string equal to one of variants,
// reporting true. Otherwise it records an invalid_plan fault at path
// ("expected a string, found %s" for a non-string, or "%q is not one of
// %s" with the variants joined by ", " for a string outside the set) and
// leaves *dst unchanged, reporting false. It does not allocate.
func DecodeEnum[T ~string](d *Decoder, v *Value, path string, variants []string, dst *T) bool {
	if v.Kind != String {
		d.Fail(CodeInvalidPlan, path, v, "expected a string, found %s", v.Kind)
		return false
	}
	if !slices.Contains(variants, v.Str) {
		d.Fail(CodeInvalidPlan, path, v, "%q is not one of %s", v.Str, strings.Join(variants, ", "))
		return false
	}
	*dst = T(v.Str)
	return true
}
