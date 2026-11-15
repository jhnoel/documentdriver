package ops

import (
	"errors"
	"sync"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// ErrNotImplemented is what an operation's Run returns until it is
// ported.
var ErrNotImplemented = errors.New("docx: operation not implemented")

// Spec is one decoded operation: a pointer to an op struct, whose fields
// are the operation's plan fields (the json tag is the contract's field
// name) in the order the reference lists them.
type Spec interface {
	// Run applies the operation.
	Run(env *Env) error
}

// OpDef is one operation of the agent contract (docs/contract.md,
// section 3): its noun.verb name, its fields (the struct New returns),
// its validation rules, how a tracked edit records it and how its direct
// mode is made, and the docxdriver 0.5.2 operation it runs as with what
// the contract fixes or moves. Several operations may run as one
// docxdriver operation (the row, column and cell edits are its
// edit_table; revision.accept and revision.reject its revision_settle).
type OpDef struct {
	// Name is the contract name ("text.replace").
	Name string
	// Oracle is the docxdriver operation it runs as ("replace_text"), ""
	// for one docxdriver does not have (text.insert).
	Oracle string
	// Fixed are the docxdriver fields the operation fixes, as dotted path
	// and JSON value ("action.kind": `"insert_rows"`).
	Fixed map[string]string
	// Moved says which contract fields carry which docxdriver fields, as
	// the contract's mapping table words it.
	Moved string
	// New returns a zero Spec to decode into.
	New func() Spec
	// Rules are checked in order after decoding; the first that is Bad
	// rejects the operation with invalid_operation.
	Rules []Rule
	// Normalize rewrites a valid Spec into its canonical form (nil: none).
	Normalize func(Spec)
	// Track is the operation's change-mode class.
	Track Track
	// Direct is how the operation's direct mode relates to its track
	// mode.
	Direct Direct
	// TrackGuards and DirectGuards are the refusals made in one change
	// mode only, as their messages' gist (refused_in_mode when the mode
	// refuses the operation outright); the change-mode law expects
	// refusal parity per mode, not across modes.
	TrackGuards, DirectGuards []string
}

var textGuard = []string{"a selection overlapping a tracked change or a comment range"}

// refusedInTrack is the guard of operations track mode refuses: Word
// records no revision for them.
var refusedInTrack = []string{"refused_in_mode: Word records no revision for it"}

// Direct is how an operation's direct mode is made, as table data for the
// Reviser and the change-mode law.
type Direct uint8

const (
	// AcceptAuthored means direct mode is track mode followed by accepting
	// exactly the revisions the operation authored (the Reviser's
	// ledger); the change-mode law holds on inputs both modes accept.
	AcceptAuthored Direct = iota
	// OwnPath means direct mode has its own code path (Word's tracked form of
	// the change differs from the change).
	OwnPath
	// Same means the operation writes no revision; both modes are one path.
	Same
	// OnlyDirect means track mode refuses the operation, so there is no
	// tracked form to accept.
	OnlyDirect
)

// String returns the class name.
func (d Direct) String() string {
	switch d {
	case AcceptAuthored:
		return "AcceptAuthored"
	case OwnPath:
		return "OwnPath"
	case Same:
		return "Same"
	case OnlyDirect:
		return "OnlyDirect"
	}
	return "Direct(?)"
}

// Rule is one validation check of an operation's values, made without
// the document (ported from docxdriver's normalize_op, in the contract's
// field names).
type Rule struct {
	// Msg is the message. A rule with Explain has several or formatted
	// messages; Msg then documents them.
	Msg string
	// Path is the offending field, relative to the operation ("" for the
	// operation itself, "format.padding.top" for a nested field).
	Path string
	// Bad reports whether the operation breaks the rule.
	Bad func(Spec) bool
	// Explain, when set, returns the message and path for a Spec that is
	// Bad, for rules whose message or path depends on the values.
	Explain func(Spec) (msg, path string)
	// Example is an operation (JSON, in the contract's grammar) that
	// breaks this rule and no earlier one; "" for a rule decoding makes
	// unreachable.
	Example string
}

// Track is how an operation behaves in track mode, as docxdriver's
// change-mode tests (tests/change_mode.rs) classify it.
type Track uint8

const (
	// Exact records revisions only: accept-all gives the direct output,
	// reject-all the input.
	Exact Track = iota
	// KeepsAdditions is Exact, but reject-all keeps an empty story the op
	// added (a new header part, whose reference Word cannot track).
	KeepsAdditions
	// EmptiesStory leaves an empty story on accept-all where the direct
	// edit removes its reference.
	EmptiesStory
	// ComplexField is Exact, but a simple field is written as the complex
	// field runs a tracked change holds.
	ComplexField
	// DirectOnly is refused in track mode: Word records no revision for it.
	DirectOnly
	// Untracked is applied directly in both modes: Word does not track it
	// (comments, bookmarks) or it settles revisions instead of writing any.
	Untracked
)

// String returns the class name.
func (t Track) String() string {
	switch t {
	case Exact:
		return "Exact"
	case KeepsAdditions:
		return "KeepsAdditions"
	case EmptiesStory:
		return "EmptiesStory"
	case ComplexField:
		return "ComplexField"
	case DirectOnly:
		return "DirectOnly"
	case Untracked:
		return "Untracked"
	}
	return "Track(?)"
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

// OpOf returns the table entry of a Spec value, by its type: each
// operation has its own Spec type.
func OpOf(s Spec) (*OpDef, bool) {
	return opIndex().Of(s)
}

// rule builds a Rule over one op struct type.
func rule[T any](msg, path string, bad func(*T) bool, example string) Rule {
	return Rule{Msg: msg, Path: path, Bad: func(s Spec) bool { return bad(any(s).(*T)) }, Example: example}
}

// ruleX builds a Rule whose message or path depends on the values:
// explain returns them and whether the rule is broken.
func ruleX[T any](doc string, explain func(*T) (msg, path string, bad bool), example string) Rule {
	return Rule{
		Msg: doc,
		Bad: func(s Spec) bool {
			_, _, bad := explain(any(s).(*T))
			return bad
		},
		Explain: func(s Spec) (string, string) {
			msg, path, _ := explain(any(s).(*T))
			return msg, path
		},
		Example: example,
	}
}

// check runs def's rules over s and returns the first broken rule's
// message and path.
func (d *OpDef) check(s Spec) (msg, path string, bad bool) {
	for _, r := range d.Rules {
		if !r.Bad(s) {
			continue
		}
		if r.Explain != nil {
			msg, path = r.Explain(s)
			return msg, path, true
		}
		return r.Msg, r.Path, true
	}
	return "", "", false
}
