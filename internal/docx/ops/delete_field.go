package ops

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The delete field of paragraph.replace, header.set and footer.set
// (docs/contract.md, section 7): the html of an op that replaces content
// accounts for every equation and placeholder of that content, sending
// it back by its data-ref or listing its address in delete. What delete
// lists is deleted.

// paragraphContentRef matches the address of an equation or placeholder
// in a paragraph: 0A1B2C3D/equation:N, 0A1B2C3D/opaque:N (~n allowed).
var paragraphContentRef = regexp.MustCompile(`^[0-9A-Fa-f]{8}(~[1-9][0-9]*)?/(equation|opaque):[1-9][0-9]{0,8}$`)

// storyContentRef matches the address of an equation or placeholder in a
// header or footer, counted across it: equation:N, opaque:N.
var storyContentRef = regexp.MustCompile(`^(equation|opaque):[1-9][0-9]{0,8}$`)

// deleteRule checks delete's entries without the document: each an
// address of the right form (in a paragraph, or across a story), none
// twice.
func deleteRule[T any](paragraph bool, get func(*T) []string, example string) Rule {
	form := "equation:N or opaque:N (header and footer content is counted across the story)"
	re := storyContentRef
	if paragraph {
		form, re = "0A1B2C3D/equation:N or 0A1B2C3D/opaque:N", paragraphContentRef
	}
	return ruleX("delete lists addresses of equations and placeholders ("+form+"), each once",
		func(t *T) (string, string, bool) {
			seen := map[string]bool{}
			for i, a := range get(t) {
				path := "delete[" + strconv.Itoa(i) + "]"
				if !re.MatchString(a) {
					return "delete[" + strconv.Itoa(i) + "] " + strconv.Quote(a) + " is not an address of an equation or placeholder: write " + form, path, true
				}
				k := canonicalContentRef(a)
				if seen[k] {
					return "delete lists " + a + " twice", path, true
				}
				seen[k] = true
			}
			return "", "", false
		}, example)
}

// canonicalContentRef upper-cases the paragraph part of a content
// address, as read writes it.
func canonicalContentRef(a string) string {
	i := strings.LastIndexByte(a, '/')
	if i < 0 {
		return a
	}
	return strings.ToUpper(a[:i]) + a[i:]
}

// accountFor checks that html accounts for the content it replaces:
// have are the addresses of the replaced content's equations and
// placeholders, sent those the html carries, del the op's delete field.
// A delete entry naming nothing there is not_found at its index; one
// the html also sends back is invalid_operation; content neither sent
// nor listed is operation_failed at the html field, listing what is
// left out.
func accountFor(have, sent, del []string, htmlField string) error {
	in := map[string]bool{}
	for _, h := range have {
		in[h] = true
	}
	back := map[string]bool{}
	for _, s := range sent {
		back[s] = true
	}
	listed := map[string]bool{}
	for i, d := range del {
		d = canonicalContentRef(d)
		field := "delete[" + strconv.Itoa(i) + "]"
		if !in[d] {
			return reject("not_found", field, "delete lists %s, which names no equation or placeholder of the content being replaced", d)
		}
		if back[d] {
			return reject("invalid_operation", field, "%s is both sent back in html and listed in delete: keep it or delete it", d)
		}
		listed[d] = true
	}
	var missing []string
	for _, h := range have {
		if !back[h] && !listed[h] {
			missing = append(missing, h)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		r := &Rejection{Code: "operation_failed", Field: htmlField, Matches: missing,
			Msg: "html leaves out " + strings.Join(missing, ", ") + ": send each back as read shows it (its data-ref), or list it in delete to delete it"}
		return r
	}
	return nil
}
