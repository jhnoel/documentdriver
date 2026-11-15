package integrity

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
)

// maxTypeDepth bounds the walk through simple type derivations; a chain
// deeper than this is accepted, as docxdriver did.
const maxTypeDepth = 16

// valid reports whether value is in the lexical space of simple type
// name. The tables resolve a type through its restriction chain (the
// built-in it ends in, the most derived enumeration, every step's
// pattern, the bounds and lengths); checking those is checking each step,
// as docxdriver did.
func (m *contentModel) valid(name, value string, depth int) bool {
	if depth > maxTypeDepth {
		return true
	}
	if b, ok := strings.CutPrefix(name, "xsd:"); ok {
		return builtinValid(b, value)
	}
	st, ok := schema.SimpleType(name)
	if !ok {
		return true
	}
	if len(st.Union) > 0 {
		return slices.ContainsFunc(st.Union, func(u string) bool { return m.valid(u, value, depth+1) })
	}
	builtin := strings.TrimPrefix(st.Base, "xsd:")
	if builtin != "string" && builtin != "anySimpleType" {
		value = strings.TrimSpace(value)
	}
	if !builtinValid(builtin, value) {
		return false
	}
	if len(st.Enum) > 0 && !slices.Contains(st.Enum, value) {
		return false
	}
	for _, p := range st.Patterns {
		if re := m.pattern(p); re != nil && !re.MatchString(value) {
			return false
		}
	}
	length := utf8.RuneCountInString(value)
	if builtin == "hexBinary" {
		length = len(value) / 2
	}
	if st.Length > 0 && length != st.Length || st.MinLen > 0 && length < st.MinLen || st.MaxLen > 0 && length > st.MaxLen {
		return false
	}
	if v, ok := finiteNumber(value); ok {
		if st.HasMin && v < float64(st.Min) || st.HasMax && v > float64(st.Max) {
			return false
		}
	}
	return true
}

// rustFloat parses s as Rust's f64::from_str does: an optional sign, then
// inf, infinity or nan in any case, or decimal digits with an optional
// fraction and exponent. An out-of-range value is infinite, not an error.
func rustFloat(s string) (float64, bool) {
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 {
		return 0, false
	}
	sign := 1
	if strings.HasPrefix(s, "-") {
		sign = -1
	}
	switch strings.ToLower(body) {
	case "inf", "infinity":
		return math.Inf(sign), true
	case "nan":
		return math.NaN(), true
	}
	i, digits := 0, 0
	for i < len(body) && isDigit(body[i]) {
		i, digits = i+1, digits+1
	}
	if i < len(body) && body[i] == '.' {
		i++
		for i < len(body) && isDigit(body[i]) {
			i, digits = i+1, digits+1
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(body) && (body[i] == 'e' || body[i] == 'E') {
		i++
		if i < len(body) && (body[i] == '+' || body[i] == '-') {
			i++
		}
		exp := i
		for i < len(body) && isDigit(body[i]) {
			i++
		}
		if i == exp {
			return 0, false
		}
	}
	if i != len(body) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return v, true
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// finiteNumber parses a finite number.
func finiteNumber(s string) (float64, bool) {
	v, ok := rustFloat(s)
	return v, ok && !math.IsInf(v, 0) && !math.IsNaN(v)
}

var (
	minI128 = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
	maxI128 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
)

// integerIn reports whether value is an integer in [lo, hi]; nil bounds
// stand for the 128-bit limits docxdriver parsed with. An integer outside
// 128 bits is valid only for an unbounded type.
func integerIn(value string, lo, hi *big.Int) bool {
	digits := value
	if digits != "" && (digits[0] == '+' || digits[0] == '-') {
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if !isDigit(digits[i]) {
			return false
		}
	}
	v, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return false
	}
	if v.Cmp(minI128) < 0 || v.Cmp(maxI128) > 0 {
		return lo == nil && hi == nil
	}
	return (lo == nil || v.Cmp(lo) >= 0) && (hi == nil || v.Cmp(hi) <= 0)
}

var integerRanges = map[string][2]*big.Int{
	"nonNegativeInteger": {big.NewInt(0), maxI128},
	"positiveInteger":    {big.NewInt(1), maxI128},
	"long":               {big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)},
	"int":                {big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)},
	"short":              {big.NewInt(math.MinInt16), big.NewInt(math.MaxInt16)},
	"byte":               {big.NewInt(math.MinInt8), big.NewInt(math.MaxInt8)},
	"unsignedLong":       {big.NewInt(0), new(big.Int).SetUint64(math.MaxUint64)},
	"unsignedInt":        {big.NewInt(0), big.NewInt(math.MaxUint32)},
	"unsignedShort":      {big.NewInt(0), big.NewInt(math.MaxUint16)},
	"unsignedByte":       {big.NewInt(0), big.NewInt(math.MaxUint8)},
}

// builtinValid checks the lexical form of an XSD built-in type.
func builtinValid(builtin, raw string) bool {
	switch builtin {
	case "string", "anySimpleType", "normalizedString", "anyURI":
		return true
	}
	value := strings.TrimSpace(raw)
	if r, ok := integerRanges[builtin]; ok {
		return integerIn(value, r[0], r[1])
	}
	switch builtin {
	case "token":
		return true
	case "boolean":
		return value == "true" || value == "false" || value == "1" || value == "0"
	case "integer":
		return integerIn(value, nil, nil)
	case "decimal":
		body := value
		if body != "" && (body[0] == '+' || body[0] == '-') {
			body = body[1:]
		}
		if body == "" || body == "." || strings.Count(body, ".") > 1 {
			return false
		}
		for i := 0; i < len(body); i++ {
			if !isDigit(body[i]) && body[i] != '.' {
				return false
			}
		}
		return true
	case "double", "float":
		// XSD 1.0 spells the specials INF, -INF and NaN only; docxdriver
		// also accepted Rust's inf, infinity and nan in any case and sign.
		if value == "INF" || value == "-INF" || value == "NaN" {
			return true
		}
		_, ok := rustFloat(value)
		return ok && !strings.ContainsAny(value, "iInN")
	case "hexBinary":
		return len(value)%2 == 0 && isHexString(value)
	case "base64Binary":
		for i := 0; i < len(value); i++ {
			c := value[i]
			if !(isDigit(c) || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || strings.IndexByte("+/= \n\r\t", c) >= 0) {
				return false
			}
		}
		return true
	case "NMTOKEN":
		return true
	case "NCName", "ID", "IDREF":
		return isNCName(value)
	case "dateTime":
		return isDateTime(value)
	}
	return true
}

// isHexString reports whether s consists only of hexadecimal digits.
func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isDigit(c) && !('a' <= c && c <= 'f') && !('A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// dateTimeRE is xsd:dateTime: -?YYYY-MM-DDThh:mm:ss(.s+)?(Z|(+|-)hh:mm)?.
var dateTimeRE = regexp.MustCompile(`^-?\d{4,}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)?$`)

// isDateTime reports whether v is an xsd:dateTime. docxdriver checked the
// pattern only, so it accepted days a month does not have (2024-02-30).
func isDateTime(v string) bool {
	ok := dateTimeRE.MatchString(v) ||
		strings.HasSuffix(v, "T24:00:00") && dateTimeRE.MatchString(strings.ReplaceAll(v, "T24:", "T00:"))
	if !ok {
		return false
	}
	date, _, _ := strings.Cut(strings.TrimPrefix(v, "-"), "T")
	parts := strings.Split(date, "-")
	year, err1 := strconv.ParseInt(parts[0], 10, 64)
	month, err2 := strconv.Atoi(parts[1])
	day, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return true // a year beyond int64: the pattern is all docxdriver had
	}
	if strings.HasPrefix(v, "-") {
		year = -year
	}
	return day <= daysIn(year, month)
}

// daysIn returns the days of a month of the proleptic Gregorian calendar
// XSD uses (year 0 is 1 BCE, a leap year).
func daysIn(year int64, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}
