package numfmt

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind classifies a number format by the data it renders.
type Kind int

// The number-format kinds, in the order a reader checks them.
const (
	General Kind = iota
	Number
	Text
	Date
	DateTime
	Time
	Duration
)

var kindNames = [...]string{
	"General", "Number", "Text", "Date", "DateTime", "Time", "Duration",
}

// String returns the name of the format kind, or "Kind(n)" if unknown.
func (k Kind) String() string {
	if k >= General && k <= Duration {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// builtins are the built-in number formats of ECMA-376 Part 1 §18.8.30
// (numFmt) for the ids 0-22, 37-40 and 45-49. The spec's table gives 14 as
// "mm-dd-yy", 22 as "m/d/yy h:mm" and 47 as "mmss.0"; Excel renders them
// by locale (en-US m/d/yyyy), and only their Kind matters here. Ids 5-8
// are locale currency formats the spec leaves to the application; the
// en-US forms are given.
var builtins = map[int]string{
	0:  "General",
	1:  "0",
	2:  "0.00",
	3:  "#,##0",
	4:  "#,##0.00",
	5:  "$#,##0_);($#,##0)",
	6:  "$#,##0_);[Red]($#,##0)",
	7:  "$#,##0.00_);($#,##0.00)",
	8:  "$#,##0.00_);[Red]($#,##0.00)",
	9:  "0%",
	10: "0.00%",
	11: "0.00E+00",
	12: "# ?/?",
	13: "# ??/??",
	14: "mm-dd-yy",
	15: "d-mmm-yy",
	16: "d-mmm",
	17: "mmm-yy",
	18: "h:mm AM/PM",
	19: "h:mm:ss AM/PM",
	20: "h:mm",
	21: "h:mm:ss",
	22: "m/d/yy h:mm",
	37: "#,##0_);(#,##0)",
	38: "#,##0_);[Red](#,##0)",
	39: "#,##0.00_);(#,##0.00)",
	40: "#,##0.00_);[Red](#,##0.00)",
	45: "mm:ss",
	46: "[h]:mm:ss",
	47: "mm:ss.0",
	48: "##0.0E+0",
	49: "@",
}

// BuiltIn returns the built-in number format with the given id, in its
// en-US form as ECMA-376 Part 1 §18.8.30 (numFmt) lists it. It reports
// false for an id the spec assigns no format to.
func BuiltIn(id int) (string, bool) {
	s, ok := builtins[id]
	return s, ok
}

// Classify reports the kind of data a number format renders. It follows
// the behaviour of Apache POI's DateUtil.isADateFormat
// (https://poi.apache.org/apidocs/dev/org/apache/poi/ss/usermodel/DateUtil.html),
// written independently, and reports a finer-grained kind than POI's
// boolean. Only the first section counts: sections after a ';' hold the
// negative, zero and text forms, which never change the kind.
func Classify(format string) Kind {
	sec := firstSection(format)
	toks, hasAt, elapsed := scanSection(sec)
	if elapsed {
		return Duration
	}
	if strings.TrimSpace(string(toks)) == "general" {
		return General
	}
	date, time := detect(toks)
	switch {
	case date && time:
		return DateTime
	case date:
		return Date
	case time:
		return Time
	case hasAt:
		return Text
	default:
		return Number
	}
}

// firstSection returns the part of a format before its first ';' that is
// outside a quoted literal or a bracket group and not backslash-escaped.
func firstSection(format string) string {
	inQuotes := false
	depth := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		switch {
		case inQuotes:
			if c == '"' {
				inQuotes = false
			}
		case c == '\\':
			i += runeLen(format[i+1:])
		case c == '"':
			inQuotes = true
		case c == '[':
			depth++
		case c == ']':
			if depth > 0 {
				depth--
			}
		case c == ';':
			if depth == 0 {
				return format[:i]
			}
		}
	}
	return format
}

// scanSection reduces a format section to its significant characters:
// quoted literals, backslash escapes, _x and *x padding and bracket
// groups are dropped. An elapsed-time bracket ([h], [hh], [m], [mm],
// [s], [ss], in any case) instead reports elapsed. Letters are lowercased;
// every other significant byte is kept, so '@' survives for the text check.
func scanSection(sec string) (toks []byte, hasAt, elapsed bool) {
	toks = make([]byte, 0, len(sec))
	inQuotes := false
scan:
	for i := 0; i < len(sec); i++ {
		c := sec[i]
		switch {
		case inQuotes:
			if c == '"' {
				inQuotes = false
			}
		case c == '"':
			inQuotes = true
		case c == '\\', c == '_', c == '*':
			i += runeLen(sec[i+1:])
		case c == '[':
			j := i + 1
			for j < len(sec) && sec[j] != ']' {
				j++
			}
			if j == len(sec) {
				break scan
			}
			if elapsedGroup(sec[i+1 : j]) {
				elapsed = true
			}
			i = j
		default:
			switch {
			case c == '@':
				hasAt = true
				toks = append(toks, c)
			case c >= 'A' && c <= 'Z':
				toks = append(toks, c-'A'+'a')
			default:
				toks = append(toks, c)
			}
		}
	}
	return toks, hasAt, elapsed
}

// runeLen returns the width in bytes of the first rune of s, and 0 for an
// empty string: how far an escape or padding mark skips.
func runeLen(s string) int {
	if s == "" {
		return 0
	}
	_, w := utf8.DecodeRuneInString(s)
	return w
}

// elapsedGroup reports whether a bracket group names an elapsed-time unit:
// [h], [hh], [m], [mm], [s] or [ss], in any case.
func elapsedGroup(g string) bool {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "h", "hh", "m", "mm", "s", "ss":
		return true
	}
	return false
}

// detect scans the significant characters for date and time tokens. y and
// d are dates; h and s are times; AM/PM and A/P are times; a run of one or
// two m is minutes when it sits directly after an h or directly before an
// s (ignoring separators), else it is a month (three or more are always
// month names).
func detect(toks []byte) (date, time bool) {
	s := string(toks)
	if strings.Contains(s, "am/pm") {
		time = true
		s = strings.ReplaceAll(s, "am/pm", "     ")
	}
	if strings.Contains(s, "a/p") {
		time = true
		s = strings.ReplaceAll(s, "a/p", "   ")
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 'y', 'd':
			date = true
		case 'h', 's':
			time = true
		case 'm':
			j := i
			for j < len(s) && s[j] == 'm' {
				j++
			}
			if j-i >= 3 || !minutes(s, i, j) {
				date = true
			} else {
				time = true
			}
			i = j - 1
		}
	}
	return date, time
}

// minutes reports whether the m run s[i:j] is a minute token: it is
// directly after an h or directly before an s, with only separators
// (non-letters) between.
func minutes(s string, i, j int) bool {
	for k := i - 1; k >= 0; k-- {
		if !isLetter(s[k]) {
			continue
		}
		return s[k] == 'h'
	}
	for k := j; k < len(s); k++ {
		if !isLetter(s[k]) {
			continue
		}
		return s[k] == 's'
	}
	return false
}

// isLetter reports whether byte c is an ASCII letter.
func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
