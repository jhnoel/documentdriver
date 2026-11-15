package integrity

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/opc"
)

// Introduced returns the error kinds of which after has more instances
// than before: a new kind, or a new instance of a kind the input already
// had in that part (finding messages carry no positions or counts, so
// equal instances are equal). A schema finding is known when before had
// it in any part: the bundled schemas predate markup Word writes
// (w:hint="cs"), and an edit that carries such existing formatting into
// another part, as a new comment does, has not damaged anything.
// Pre-existing faults never count: only what the edit added.
func Introduced(before, after *Findings) []Finding {
	knownSchema := map[string]bool{}
	for _, k := range before.Kinds() {
		if strings.HasPrefix(k.Message, "schema: ") {
			knownSchema[k.Message] = true
		}
	}
	var out []Finding
	for _, k := range after.Kinds() {
		if after.Count(k) > before.Count(k) && !knownSchema[k.Message] {
			out = append(out, k)
		}
	}
	return out
}

// GateFailure is a commit-gate refusal: what the edit would have broken.
type GateFailure struct {
	// Introduced are the error kinds the edit would introduce.
	Introduced []Finding
	// Located are the faults one by one, with the element each is at and
	// the operation that introduced it (GateEdit only).
	Located []Introduction
}

// Op returns the index of the earliest operation that introduced one of
// the faults, or -1 when no operation can be named (Gate, or faults
// outside anything an operation declared).
func (g *GateFailure) Op() int {
	op := -1
	for _, i := range g.Located {
		if i.Op >= 0 && (op < 0 || i.Op < op) {
			op = i.Op
		}
	}
	return op
}

// Error implements error with the summary.
func (g *GateFailure) Error() string { return g.Summary() }

// Summary describes the refusal in one line, at most twelve faults.
func (g *GateFailure) Summary() string {
	var lines []string
	if len(g.Located) > 0 {
		for _, i := range g.Located {
			lines = append(lines, i.String())
		}
	} else {
		for _, k := range g.Introduced {
			lines = append(lines, Introduction{Finding: k}.String())
		}
	}
	const shown = 12
	if more := len(lines) - shown; more > 0 {
		lines = append(lines[:shown], fmt.Sprintf("and %d more", more))
	}
	return "the edit would damage the document, so nothing was written; the source is unchanged. " + strings.Join(lines, "; ")
}

// Gate is the commit rule: output may not have an error kind input lacks.
// It returns nil or a *GateFailure.
//
// It checks only what an edit can have changed: the parts whose bytes
// differ between the packages and their partners (see check), in both
// packages, which gives the same answer as comparing Check of both
// (TestGateMatchesFullCheck). When the content types, the set of part
// names or a duplicated name differs, or a new schema finding needs the
// rest of the input's schema findings (Introduced), it checks the whole
// packages. docxdriver also refused an edit when a rewritten part did not
// round-trip through its XML writer; the engine's lossless tree, copied
// untouched spans and the plan's write-set verification make that check
// unnecessary here (docs/design.md).
func Gate(input, output []byte) error {
	return GateWithLimits(input, output, opc.DefaultLimits())
}

// GateWithLimits is Gate with explicit reading limits.
func GateWithLimits(input, output []byte, l opc.Limits) error {
	l = withDefaults(l)
	in, out := unzip(input, l), unzip(output, l)
	var introduced []Finding
	if scope, ok := changedScope(in, out); ok {
		introduced = Introduced(check(in, scope), check(out, scope))
		if slices.ContainsFunc(introduced, func(f Finding) bool { return strings.HasPrefix(f.Message, "schema: ") }) {
			known := map[string]bool{}
			for _, k := range check(in, nil).Kinds() {
				known[k.Message] = true
			}
			introduced = slices.DeleteFunc(introduced, func(f Finding) bool {
				return strings.HasPrefix(f.Message, "schema: ") && known[f.Message]
			})
		}
	} else {
		introduced = Introduced(check(in, nil), check(out, nil))
	}
	if len(introduced) == 0 {
		return nil
	}
	return &GateFailure{Introduced: introduced}
}

// GateCreated is the rule for a package built from nothing (create): it
// must have no fault at all. It returns nil or a *GateFailure.
func GateCreated(output []byte) error { return GateCreatedAt(output, nil) }

// GateCreatedAt is GateCreated that calls mark (when not nil) once the
// check is done, while its view is held: the memory budget's checkpoint.
func GateCreatedAt(output []byte, mark func(string)) error {
	f := Check(output)
	if mark != nil {
		mark("create checked")
	}
	if f.Empty() {
		return nil
	}
	return &GateFailure{Introduced: f.Kinds()}
}

// changedScope returns the parts whose bytes differ between the
// packages, and false when only a whole-package check is sound: either is
// not a readable zip, the names or the content types differ, or a name
// occurs twice.
func changedScope(input, output *archive) (map[string]bool, bool) {
	read := func(a *archive) (map[string]string, []string, bool) {
		if !a.ok || len(a.faults) > 0 {
			return nil, nil, false
		}
		m := make(map[string]string, len(a.entries))
		names := make([]string, 0, len(a.entries))
		for _, e := range a.entries {
			if _, dup := m[e.name]; dup {
				return nil, nil, false
			}
			m[e.name] = e.data
			names = append(names, e.name)
		}
		slices.Sort(names)
		return m, names, true
	}
	before, bn, ok1 := read(input)
	after, an, ok2 := read(output)
	if !ok1 || !ok2 || !slices.Equal(bn, an) || before[contentTypesName] != after[contentTypesName] {
		return nil, false
	}
	scope := map[string]bool{}
	for name, data := range before {
		if after[name] != data {
			scope[name] = true
		}
	}
	return scope, true
}
