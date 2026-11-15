package main

import (
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/ops"
)

// The op classes a recorded case is tagged with: which equivalence
// between the change modes the comparison asserts for it.
const (
	// classAcceptAuthored (op class Exact): the direct output equals the
	// tracked output with every revision accepted, and rejecting every
	// revision of the tracked output gives the input back.
	classAcceptAuthored = "AcceptAuthored"
	// classOwnPath (KeepsAdditions, EmptiesStory, ComplexField): track
	// and direct take their own paths; each mode is compared with the
	// oracle's same mode only, and the class's known deviation from
	// AcceptAuthored (an empty story kept or left, complex fields) holds.
	classOwnPath = "OwnPath"
	// classSame (Untracked): both modes write the same output.
	classSame = "Same"
	// classDirectOnly: a DirectOnly op in direct mode.
	classDirectOnly = "DirectOnly"
	// classTrackGuard: track mode refuses this case (a DirectOnly op, or
	// an edit_table action Word does not track); the refusal itself is
	// what is compared.
	classTrackGuard = "TrackGuard"
)

// opClass tags a plan in a mode by its op (the first operation's class
// for a plan of several).
func opClass(p *WritePlan, mode string) string {
	if p.Op == opTextInsert {
		return classAcceptAuthored // an insertion: the same equivalences as Exact
	}
	d, ok := ops.OracleLookup(p.Op)
	if !ok {
		return ""
	}
	if mode == "track" && (d.Track == ops.DirectOnly || trackRefusedAction(p, d) || hasDirectOnly(p)) {
		return classTrackGuard
	}
	switch d.Track {
	case ops.Exact:
		return classAcceptAuthored
	case ops.KeepsAdditions, ops.EmptiesStory, ops.ComplexField:
		return classOwnPath
	case ops.Untracked:
		return classSame
	case ops.DirectOnly:
		return classDirectOnly
	}
	return ""
}

// trackRefusedAction reports whether one of the plan's edit_table
// operations uses an action track mode refuses.
func trackRefusedAction(p *WritePlan, d *ops.OracleOp) bool {
	if len(d.TrackRefused) == 0 {
		return false
	}
	v, err := oracleOps(p)
	if err != nil || v.kind != 'a' {
		return false
	}
	for _, op := range v.items {
		if name := op.get("op"); name == nil || name.s != d.Name {
			continue
		}
		if k := op.get("action").get("kind"); k != nil && slices.Contains(d.TrackRefused, k.s) {
			return true
		}
	}
	return false
}

// hasDirectOnly reports whether any of the plan's operations is
// DirectOnly (track mode refuses the whole plan then).
func hasDirectOnly(p *WritePlan) bool {
	v, err := oracleOps(p)
	if err != nil || v.kind != 'a' {
		return false
	}
	for _, op := range v.items {
		if name := op.get("op"); name != nil {
			if d, ok := ops.OracleLookup(name.s); ok && d.Track == ops.DirectOnly {
				return true
			}
		}
	}
	return false
}

// opTextInsert is the contract op docxdriver has no counterpart for: its
// cases are Go-only laws (WritePlan.Equivalent).
const opTextInsert = "text.insert"

// opNames lists the case keys in order: the op table's operations
// (docxdriver's names), then text.insert.
func opNames() []string {
	out := make([]string, 0, len(ops.OracleOps())+1)
	for _, d := range ops.OracleOps() {
		out = append(out, d.Name)
	}
	return append(out, opTextInsert)
}

// oracleOps is the plan's ops in docxdriver's form.
func oracleOps(p *WritePlan) (*jv, error) {
	list := p.Ops
	if len(p.Equivalent) > 0 {
		list = p.Equivalent
	}
	down, err := downTranslate(`{"ops":` + string(list) + `}`)
	if err != nil {
		return nil, err
	}
	v, err := parseJV([]byte(down))
	if err != nil {
		return nil, err
	}
	return v.get("ops"), nil
}
