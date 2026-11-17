package ops

import (
	"time"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// init registers the shape data rules with the rule extension point
// (rules.go): prop.add row names and date values, checked before the
// document opens. Chunk CC8.
func init() {
	extraRules = append(extraRules, checkPropRules)
}

// checkPropRules reports the first shape data rule s breaks: a
// prop.add name that is not [A-Za-z_][A-Za-z0-9_]*, or a date value
// whose string is not YYYY-MM-DDThh:mm:ss. It returns false for a
// spec it does not apply to. Chunk CC8.
func checkPropRules(s Spec) (string, string, bool) {
	switch v := s.(type) {
	case *PropAddSpec:
		if v.Name != "" && !cli.ValidAlias(v.Name) {
			return "name must be ASCII letters, digits and _, not starting with a digit", "name", true
		}
		if v.Type == "date" && v.Value != nil {
			if ds, ok := propDateString(v.Value); ok && !validPropDateText(ds) {
				return "date value must be YYYY-MM-DDThh:mm:ss", "value", true
			}
		}
	case *PropSetSpec:
		if v.Value != nil {
			if ds, ok := propDateString(v.Value); ok && !validPropDateText(ds) {
				return "date value must be YYYY-MM-DDThh:mm:ss", "value", true
			}
		}
	}
	return "", "", false
}

// propDateString returns the date string of a plan date value
// ({"date": "..."} as read shows it) and whether the value is one.
// A date value whose string is not a string comes back empty with
// true, so the caller refuses it. Chunk CC8.
func propDateString(value any) (string, bool) {
	switch v := value.(type) {
	case map[string]any:
		s, ok := v["date"]
		if !ok {
			return "", false
		}
		str, ok := s.(string)
		if !ok {
			return "", true
		}
		return str, true
	case map[string]string:
		s, ok := v["date"]
		if !ok {
			return "", false
		}
		return s, true
	}
	return "", false
}

// validPropDateText reports whether s is YYYY-MM-DDThh:mm:ss with a
// valid calendar value and nothing else (no zone, no fraction).
// Chunk CC8.
func validPropDateText(s string) bool {
	if len(s) != 19 {
		return false
	}
	_, err := time.Parse("2006-01-02T15:04:05", s)
	return err == nil
}
