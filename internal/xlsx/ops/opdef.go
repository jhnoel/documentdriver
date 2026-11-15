package ops

import (
	"sync"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Spec is one decoded operation: a pointer to the operation's struct,
// whose fields are its plan fields (the json tag is the contract's
// field name).
type Spec interface {
	// Run applies the operation to env's working copy. It returns nil, a
	// *Rejection, or another error (a bug: internal).
	Run(env *Env) error
}

// OpDef is one operation of the table: its noun.verb name, its fields
// (the struct New returns), its validation rules and its help.
type OpDef struct {
	// Name is the contract name ("cell.set").
	Name string
	// Summary is the first paragraph of `xlsx help <op>`.
	Summary string
	// New returns a zero Spec to decode into.
	New func() Spec
	// Rules are checked in order after decoding; the first broken one
	// refuses the operation with invalid_operation.
	Rules []Rule
	// Adjusts lists, for a structural op, the references it adjusts
	// (the closed "rewritten" list) and Refuses the constructs it
	// refuses (decision 12); `help <op>` prints both.
	Adjusts, Refuses []string
}

// Rule is one validation of an operation's values made without the
// document.
type Rule struct {
	// Msg is the message (and the rule's line in help).
	Msg string
	// Path is the offending field relative to the operation ("" for the
	// operation itself).
	Path string
	// Bad reports whether the operation breaks the rule.
	Bad func(Spec) bool
	// Example is an operation (JSON, in the contract's grammar) that
	// breaks this rule and no earlier one (TestRuleExamples).
	Example string
}

// rule builds a Rule over one op struct type.
func rule[T any](msg, path string, bad func(*T) bool, example string) Rule {
	return Rule{Msg: msg, Path: path, Bad: func(s Spec) bool { return bad(any(s).(*T)) }, Example: example}
}

// opIndex is the operation table indexed by name and by Spec type,
// built once on first use.
var opIndex = sync.OnceValue(func() *cli.OpIndex[OpDef] {
	return cli.NewOpIndex(Ops, func(d *OpDef) string { return d.Name }, func(d *OpDef) any { return d.New() })
})

// Lookup returns the operation named name.
func Lookup(name string) (*OpDef, bool) {
	return opIndex().Lookup(name)
}

// OpOf returns the table entry of a Spec value, by its type.
func OpOf(s Spec) (*OpDef, bool) {
	return opIndex().Of(s)
}

// check runs d's rules over s: the first broken rule's message and path.
func (d *OpDef) check(s Spec) (msg, path string, bad bool) {
	for _, r := range d.Rules {
		if r.Bad(s) {
			return r.Msg, r.Path, true
		}
	}
	return "", "", false
}
