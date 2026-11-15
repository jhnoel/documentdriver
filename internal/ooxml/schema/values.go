package schema

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ErrSchema is returned for a value outside the schema type of the
// attribute it would be written to.
var ErrSchema = errors.New("value outside the schema")

// CheckAttr checks v against the schema type of attribute attr of complex
// type ctype. An attribute the tables do not hold is a programming error
// and fails too, so that a codec never writes an attribute unchecked.
func CheckAttr(ctype, attr, v string) error {
	t, ok := AttrType(ctype, attr)
	if !ok {
		return fmt.Errorf("schema: %s@%s is not tabulated", ctype, attr)
	}
	return CheckValue(t, v)
}

// CheckValue checks v against simple type typ (or an xsd: built-in). The
// error wraps ErrSchema and says what the type allows: a few of its
// values, or its bounds.
func CheckValue(typ, v string) error {
	if why := check(typ, v); why != "" {
		return fmt.Errorf("%w: %q is not %s (%s)", ErrSchema, v, typ, why)
	}
	return nil
}

// check returns why v is not of type typ, "" when it is.
func check(typ, v string) string {
	if strings.HasPrefix(typ, "xsd:") {
		return checkBuiltin(typ, v, 0)
	}
	st, ok := SimpleType(typ)
	if !ok {
		return "type not tabulated"
	}
	if st.List != "" {
		for _, item := range strings.Fields(v) {
			if why := check(st.List, item); why != "" {
				return fmt.Sprintf("item %q: %s", item, why)
			}
		}
		return ""
	}
	if len(st.Union) > 0 {
		var whys []string
		for _, m := range st.Union {
			why := check(m, v)
			if why == "" {
				return ""
			}
			whys = append(whys, why)
		}
		return strings.Join(whys, "; or ")
	}
	if why := checkBuiltin(st.Base, v, st.Length); why != "" {
		return why
	}
	if len(st.Enum) > 0 {
		if slices.Contains(st.Enum, v) {
			return ""
		}
		return "want one of " + ListSome(st.Enum)
	}
	if st.HasMin || st.HasMax {
		n, _ := strconv.ParseInt(v, 10, 64) // checkBuiltin accepted an integer
		if st.HasMin && n < st.Min || st.HasMax && n > st.Max {
			return "want " + bounds(st)
		}
	}
	for _, p := range st.Patterns {
		if !pattern(p).MatchString(v) {
			return "want the pattern " + p
		}
	}
	n := utf8.RuneCountInString(v)
	switch {
	case st.MinLen > 0 && n < st.MinLen, st.MaxLen > 0 && n > st.MaxLen:
		return fmt.Sprintf("want %d to %d characters", st.MinLen, st.MaxLen)
	case st.Length > 0 && st.Base != "xsd:hexBinary" && n != st.Length:
		return fmt.Sprintf("want %d characters", st.Length)
	}
	return ""
}

// bounds describes the integer bounds of a simple type.
func bounds(st Simple) string {
	switch {
	case st.HasMin && st.HasMax:
		return fmt.Sprintf("an integer from %d to %d", st.Min, st.Max)
	case st.HasMin:
		return fmt.Sprintf("an integer of at least %d", st.Min)
	}
	return fmt.Sprintf("an integer of at most %d", st.Max)
}

// ListSome lists up to eight values, then how many there are.
func ListSome(vals []string) string {
	if len(vals) <= 8 {
		return strings.Join(vals, ", ")
	}
	return strings.Join(vals[:6], ", ") + fmt.Sprintf(", ... (%d values)", len(vals))
}

// intRanges are the bounds of the XSD integer built-ins.
var intRanges = map[string][2]int64{
	"xsd:byte": {math.MinInt8, math.MaxInt8}, "xsd:short": {math.MinInt16, math.MaxInt16},
	"xsd:int": {math.MinInt32, math.MaxInt32}, "xsd:long": {math.MinInt64, math.MaxInt64},
	"xsd:integer":      {math.MinInt64, math.MaxInt64},
	"xsd:unsignedByte": {0, math.MaxUint8}, "xsd:unsignedShort": {0, math.MaxUint16},
	"xsd:unsignedInt": {0, math.MaxUint32}, "xsd:unsignedLong": {0, math.MaxInt64},
	"xsd:nonNegativeInteger": {0, math.MaxInt64}, "xsd:positiveInteger": {1, math.MaxInt64},
}

// checkBuiltin checks the lexical form of an XSD built-in; types without
// a restricted form (strings, tokens) accept anything. length is the
// hexBinary length facet in octets.
func checkBuiltin(typ, v string, length int) string {
	if r, ok := intRanges[typ]; ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < r[0] || n > r[1] {
			return fmt.Sprintf("want an integer from %d to %d", r[0], r[1])
		}
		return ""
	}
	switch typ {
	case "xsd:boolean":
		if v != "true" && v != "false" && v != "1" && v != "0" {
			return "want true, false, 1 or 0"
		}
	case "xsd:double", "xsd:float", "xsd:decimal":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "want a number"
		}
	case "xsd:hexBinary":
		switch {
		case length > 0 && (len(v) != 2*length || strings.Trim(v, "0123456789abcdefABCDEF") != ""):
			return fmt.Sprintf("want %d hex digits", 2*length)
		case len(v)%2 != 0 || strings.Trim(v, "0123456789abcdefABCDEF") != "":
			return "want an even number of hex digits"
		}
	}
	return ""
}

var (
	patternMu sync.Mutex
	patterns  = map[string]*regexp.Regexp{}
)

// pattern compiles an XSD pattern (implicitly anchored). The generator
// checked that every tabulated pattern compiles.
func pattern(p string) *regexp.Regexp {
	patternMu.Lock()
	defer patternMu.Unlock()
	re, ok := patterns[p]
	if !ok {
		re = regexp.MustCompile("^(?:" + p + ")$")
		patterns[p] = re
	}
	return re
}
