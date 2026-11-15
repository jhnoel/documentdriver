package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// A refusal site is a place in docxdriver's commands/ that refuses an
// edit or fails it: a message template in an error construct
// (Outcome::error, Outcome::blocked, Err(..), ok_or.., and the helpers
// that wrap them). `oracle write sites` scans the pinned source into
// baseline/write/sites.tsv; record tags every rejected case with the
// sites whose template matches its message, and writes coverage.tsv.
type site struct {
	ID       string // file:line of the literal, e.g. typed.rs:76
	Template string // the literal as written, Rust escapes kept
	Fn       string // the enclosing Rust function
	re       *regexp.Regexp
	lits     int // literal characters, to prefer the most specific match
}

// siteMarkers start an error construct on a line.
var siteMarkers = regexp.MustCompile(`Outcome::(?:error|blocked)|\bErr\(|\.ok_or(?:_else)?\(|\bblocked_at\(|\bfail\(|\bmissing_[a-z_]+\(|\brefuse[a-z_]*\(|\breject[a-z_]*\(`)

var rustString = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// scanSites reads the command sources of the docxdriver checkout.
func scanSites(docxdriver string) ([]site, error) {
	dir := filepath.Join(docxdriver, "crates", "docxdriver-core", "src", "commands")
	files, err := filepath.Glob(filepath.Join(dir, "*.rs"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	var out []site
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(b), "\n")
		seen := map[int]bool{}
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !siteMarkers.MatchString(line) {
				continue
			}
			// The message is the first long enough literal on the marker
			// line or the few lines a wrapped call continues on.
			for j := i; j < len(lines) && j <= i+4; j++ {
				if j > i && siteMarkers.MatchString(lines[j]) {
					break
				}
				m := rustString.FindStringSubmatch(lines[j])
				if m == nil || len(literalText(m[1])) < 8 || seen[j] {
					if j > i && strings.Contains(lines[j], ";") {
						break
					}
					continue
				}
				seen[j] = true
				s := site{ID: fmt.Sprintf("%s:%d", filepath.Base(f), j+1), Template: m[1], Fn: enclosingFn(lines, j)}
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

// literalText is a template without its placeholders and escapes.
func literalText(t string) string {
	t = regexp.MustCompile(`\{[^{}]*\}`).ReplaceAllString(t, "")
	return unescapeRust(t)
}

// unescapeRust decodes Rust string escapes in s.
func unescapeRust(s string) string {
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\t`, "\t", `{{`, "{", `}}`, "}").Replace(s)
}

// compile prepares a site's matcher: placeholders match anything.
func (s *site) compile() {
	parts := regexp.MustCompile(`\{[^{}]*\}`).Split(s.Template, -1)
	quoted := make([]string, len(parts))
	n := 0
	for i, p := range parts {
		u := unescapeRust(p)
		n += len(u)
		quoted[i] = regexp.QuoteMeta(u)
	}
	s.re = regexp.MustCompile(strings.Join(quoted, "(?s:.*?)"))
	s.lits = n
}

// sitesPath is the committed site list.
func (c *config) sitesPath() string { return filepath.Join(c.writeDir(), "sites.tsv") }

// saveSites writes sites as a TSV file with a header comment at path.
func saveSites(path string, sites []site) error {
	var b strings.Builder
	b.WriteString("# docxdriver 1ffb0d7 crates/docxdriver-core/src/commands refusal sites: file:line, enclosing function, message template (Rust literal)\n")
	for _, s := range sites {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", s.ID, s.Fn, s.Template)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// loadSites reads refusal sites from the TSV file at path.
func loadSites(path string) ([]site, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []site
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		s := site{ID: f[0], Fn: f[1], Template: f[2]}
		s.compile()
		out = append(out, s)
	}
	return out, sc.Err()
}

// matchSites returns the most specific sites whose template occurs in msg.
func matchSites(sites []site, msg string) []string {
	best := 0
	var out []string
	for i := range sites {
		s := &sites[i]
		if s.lits < best || !s.re.MatchString(msg) {
			continue
		}
		if s.lits > best {
			best, out = s.lits, nil
		}
		out = append(out, s.ID)
	}
	return out
}

var rustFn = regexp.MustCompile(`\bfn ([a-z_0-9]+)`)

// enclosingFn is the last Rust function declared at or above line i.
func enclosingFn(lines []string, i int) string {
	for ; i >= 0; i-- {
		if m := rustFn.FindStringSubmatch(lines[i]); m != nil {
			return m[1]
		}
	}
	return "-"
}

// siteStatus classifies a site no recorded case reaches: why a plan
// cannot reach it, or "open" when one could and no case does yet.
func siteStatus(s site) (status, reason string) {
	t := literalText(s.Template)
	has := func(subs ...string) bool {
		for _, x := range subs {
			if strings.Contains(t, x) {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(s.ID, "readonly.rs:") || strings.HasPrefix(s.ID, "reading.rs:"):
		return "unreachable", "read-side command (read, find, inventory); apply never calls it"
	case slices.Contains([]string{"parse_char_format", "parse_para_format", "opt_bool", "optional_string", "optional_twips",
		"format_text", "format_paragraph", "insert_paragraph", "delete_paragraphs", "comment_add"}, s.Fn):
		return "dead", "parser of the JSON-value command surface; commands/mod.rs dispatches only the *_typed functions"
	case slices.Contains([]string{"validate_table", "validate_cell", "validate_row", "dimension_valid", "width_valid",
		"borders_valid", "clear_valid", "validate", "validate_new_name", "number", "address"}, s.Fn),
		has("must be a valid w14:paraId", "must be valid w14:paraId", "at must not be empty", "occurrence requires select",
			"section must be >= 1", "setPageMargins requires", "superscript and subscript", "invalid section_handle",
			"name must name an existing bookmark", "edit_table requires a table handle"):
		return "shadowed", "plan decoding or validate_plan (api.rs) refuses the same input first; the rule and schema cases record it there"
	case !strings.Contains(t, " ") || strings.HasPrefix(t, "replaced paragraph") || strings.HasPrefix(t, "comment ") && strings.Contains(t, "anchored"):
		return "not-a-refusal", "a label or success summary the scanner picked up, not a refusal message"
	case has("exhausted", "limit exceeded", "overflow", "internal:"):
		return "unreachable", "needs id spaces or table sizes beyond plan limits, or is an internal assertion"
	case has("malformed", "no root", "missing relationships root", "missing numbering root", "missing types root",
		"has no body", "no w:body", "no final section", "no block container", "has no row", "has no table", "has no grid",
		"missing r:id", "does not resolve to a package part", "has no target", "external numbering", "grid and row widths disagree",
		"conflicting grid span", "inconsistent vertical merge", "legacy hMerge", "empty story part"):
		return "open", "reachable only with a malformed or unusual package; needs a dirty-family fixture"
	}
	return "open", "reachable; no recorded case reaches it yet"
}
