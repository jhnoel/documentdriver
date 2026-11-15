package coverage

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Status is a coverage status. Read statuses say what the views show of
// a path, write statuses what plans can do to it; an item has one of
// each, and its overall status is the strongest (Overall).
type Status string

// Statuses.
const (
	ReadModelled      Status = "read-modelled"       // projected in the JSON and SVG views
	WriteCodec        Status = "write-codec"         // settable through a plan
	RoundTripLaw      Status = "round-trip-law"      // settable, and written back as shown it is a no-op
	PreservedRaw      Status = "preserved-raw"       // shown as raw XML, or carried untouched by the codec that rewrites its element
	OmittedWithReason Status = "omitted-with-reason" // the read model records an omission
	NonGoal           Status = "non-goal"            // excluded by the design
	Unknown           Status = "unknown"             // none of the above: the gap list
)

// Dim is the dimension a claim speaks for.
type Dim string

// Dimensions.
const (
	Read  Dim = "read"
	Write Dim = "write"
)

// Claim declares what one reader or codec honours: the paths (patterns,
// below) and, for an enumerated attribute, the values. Every claim except
// non-goals is checked against behaviour by this package's tests: a read
// claim by changing the path in a probe deck and comparing the views, a
// write claim by a plan that sets it, a carry claim by a plan that
// rewrites the element around it.
//
// Patterns are universe paths with "*" for any run of characters within
// one step, "**" for any number of whole steps (zero included) and
// "{a,b}" for alternatives. Within a dimension the first matching claim
// decides.
type Claim struct {
	By     string
	Dim    Dim
	Status Status
	Paths  []string
	// Except are patterns the claim does not cover although Paths match.
	Except []string
	// Values, for an enumerated attribute, lists the values the claim
	// covers; nil covers every value.
	Values []string
	// Facet names a per-value capability instead of a status: Has says
	// which values have it, and the claim does not set the paths' status.
	// Has reads the code's own table, so the facet follows the code.
	Facet string
	Has   func(v string) bool
	Note  string

	once sync.Once
	res  []*regexp.Regexp
	exc  []*regexp.Regexp
}

// Matches reports whether path matches one of the claim's patterns.
func (c *Claim) Matches(path string) bool {
	c.once.Do(func() {
		for _, p := range c.Paths {
			c.res = append(c.res, compilePattern(p))
		}
		for _, p := range c.Except {
			c.exc = append(c.exc, compilePattern(p))
		}
	})
	for _, re := range c.exc {
		if re.MatchString(path) {
			return false
		}
	}
	for _, re := range c.res {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

// Covers reports whether the claim covers value v (all values when it
// lists none).
func (c *Claim) Covers(v string) bool {
	if c.Values == nil {
		return true
	}
	return slices.Contains(c.Values, v)
}

// compilePattern compiles a coverage path pattern into a regular expression.
func compilePattern(p string) *regexp.Regexp {
	if p == "**" {
		return regexp.MustCompile(`^.*$`)
	}
	var b strings.Builder
	b.WriteString("^")
	sep := false
	for i, s := range splitSteps(p) {
		if s == "**" {
			// Zero or more whole steps.
			if i == 0 {
				b.WriteString(`(?:[^/]+/)*`)
			} else {
				b.WriteString(`(?:/[^/]+)*`)
			}
			continue
		}
		if sep {
			b.WriteString("/")
		}
		b.WriteString(stepRegexp(s))
		sep = true
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// splitSteps splits a pattern at the slashes outside braces.
func splitSteps(p string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '/':
			if depth == 0 {
				out = append(out, p[start:i])
				start = i + 1
			}
		}
	}
	return append(out, p[start:])
}

// stepRegexp converts one pattern step into a regular expression.
func stepRegexp(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		switch s[0] {
		case '*':
			b.WriteString(`[^/]*`)
			s = s[1:]
		case '{':
			end := strings.IndexByte(s, '}')
			alts := strings.Split(s[1:end], ",")
			for i, a := range alts {
				alts[i] = stepRegexp(a)
			}
			b.WriteString("(?:" + strings.Join(alts, "|") + ")")
			s = s[end+1:]
		case '/':
			// Inside an alternative that spans steps ("a:buChar/@char").
			b.WriteString("/")
			s = s[1:]
		default:
			b.WriteString(regexp.QuoteMeta(s[:1]))
			s = s[1:]
		}
	}
	return b.String()
}

// Decide returns the first claim of dimension d (status claims, not
// facets) matching path, or nil.
func Decide(d Dim, path string) *Claim {
	for _, c := range Claims {
		if c.Dim == d && c.Facet == "" && c.Matches(path) {
			return c
		}
	}
	return nil
}

// Facets returns the facet claims matching path.
func Facets(path string) []*Claim {
	var out []*Claim
	for _, c := range Claims {
		if c.Facet != "" && c.Matches(path) {
			out = append(out, c)
		}
	}
	return out
}
