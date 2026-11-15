package ops

import "strconv"

// NewPlan builds a plan from Go values instead of JSON text: the
// operations are Spec values (&ReplaceText{At: "0A1B2C3D", Select: "a",
// With: "b"}), each named by its type's entry in the operation table. The
// plan is checked and normalised exactly as a decoded one is (an empty
// plan, an empty author, then each operation's rules), so what NewPlan
// returns is what ParsePlanJSON would return for the same plan written
// out. It is how tests and tools make plans; Plan.CanonicalJSON gives the
// text when one is needed.
//
// A Spec type that is not in the table is a programming error and
// panics.
func NewPlan(base SourceHash, author string, mode ChangeMode, specs ...Spec) (*Plan, *Diagnostic) {
	p := &Plan{Base: base, Author: author, ChangeMode: mode}
	for i, s := range specs {
		d, ok := OpOf(s)
		if !ok {
			panic("ops.NewPlan: operation " + strconv.Itoa(i) + " is not a Spec of the operation table")
		}
		p.Ops = append(p.Ops, Op{Name: d.Name, Spec: s})
	}
	if d := validate(p); d != nil {
		return nil, d
	}
	return p, nil
}

// Ptr returns a pointer to v, for the optional fields of a Spec.
func Ptr[T any](v T) *T { return &v }
