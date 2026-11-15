package ops

import (
	"fmt"
	"reflect"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// Plan is a decoded xlsx plan: ops, and the shared base (contract is
// checked by the envelope and not kept). xlsx plans have no format
// settings (docs/xlsx-contract-draft.md, §3).
type Plan struct {
	// Base is the plan's base hash ("" when it has none).
	Base string
	Ops  []Op
}

// Op is one decoded operation.
type Op struct {
	Name string
	Spec Spec
}

// Def returns the operation's table entry.
func (o Op) Def() *OpDef {
	if d, ok := opIndex().Lookup(o.Name); ok {
		return d
	}
	return &OpDef{Name: o.Name}
}

// ParsePlan decodes and checks a plan's text: the envelope (cli.ParsePlan:
// JSON, duplicate keys, ops, base, contract), every op's fields (the
// shared decoder: unknown and missing fields, types, null, and the
// address and value forms) and then every op's rules. All the
// document-independent faults come back together, in plan order, as
// cli.Errors; nil errors means the plan is valid.
func ParsePlan(text []byte) (*Plan, cli.Errors) {
	env, d := cli.ParsePlan(text)
	if env == nil {
		return nil, d.Errs()
	}
	p := &Plan{Base: env.Base}
	for i, v := range env.Ops {
		name := env.Names[i]
		if name == "" {
			continue // the envelope reported it
		}
		path := cli.Index("ops", i)
		def, ok := Lookup(name)
		if !ok {
			d.UnknownOp(i, v, "xlsx")
			continue
		}
		spec := def.New()
		n := len(d.Errs())
		d.Struct(v, path, spec, "op")
		if len(d.Errs()) > n {
			continue
		}
		if msg, field, bad := def.check(spec); bad {
			at, val := path, v
			if field != "" {
				at, val = cli.Join(path, field), v.Get(field)
			}
			d.Add(cli.Errorf(cli.CodeInvalidOperation, at, "%s", msg), val)
			continue
		}
		p.Ops = append(p.Ops, Op{Name: name, Spec: spec})
	}
	if errs := d.Errs(); len(errs) > 0 {
		return nil, errs
	}
	return p, nil
}

// NewPlan builds a plan from Go values, for tests and create: each spec
// is a pointer to an op struct of the table. The rules are not checked
// (a test of a rule decodes JSON); a spec that is not in the table
// panics, as a test's own mistake.
func NewPlan(specs ...Spec) *Plan {
	p := &Plan{}
	for _, s := range specs {
		d, ok := OpOf(s)
		if !ok {
			panic(fmt.Sprintf("ops.NewPlan: %s is not an operation", reflect.TypeOf(s)))
		}
		p.Ops = append(p.Ops, Op{Name: d.Name, Spec: s})
	}
	return p
}
