// Package ops is the vsdx plan grammar: the op table, the specs (whose
// struct tags are the plan fields), decoding through internal/cli and
// the document-independent rules. Running an op is apply's (with the
// edit layer); nothing here touches a package. Specs follow the contract
// draft (docs/vsdx-contract-draft.md, section 3).
package ops

import (
	"github.com/jhnoel/documentdriver/internal/cli"
)

// Spec is one decoded operation.
type Spec any

// Plan is a decoded vsdx plan: base, units ("pt" or "in"; "" is pt) and
// the operations in order.
type Plan struct {
	Base  string
	Units string
	Ops   []Spec
}

// ParsePlan decodes and checks a plan's text: the envelope
// (cli.ParsePlan with the setting "units"), every op's fields through
// the shared decoder and then every op's rules; all document-independent
// faults come back together in plan order. Chunk O0.
func ParsePlan(text []byte) (*Plan, cli.Errors) {
	env, d := cli.ParsePlan(text, "units")
	if env == nil {
		return nil, d.Errs()
	}
	p := &Plan{Base: env.Base}
	if u := env.Settings["units"]; u != nil {
		d.Value(u, "units", &p.Units)
		if u.Kind == cli.String && p.Units != "pt" && p.Units != "in" {
			d.Fail(cli.CodeInvalidPlan, "units", u, "units is pt or in, not %q", p.Units)
		}
	}
	for i, v := range env.Ops {
		name := env.Names[i]
		if name == "" {
			continue
		}
		path := cli.Index("ops", i)
		def, ok := Lookup(name)
		if !ok {
			d.UnknownOp(i, v, "vsdx")
			continue
		}
		spec := def.New()
		n := len(d.Errs())
		d.Value(v, path, spec)
		if len(d.Errs()) > n {
			continue
		}
		if msg, field, bad := checkSpec(spec); bad {
			at, val := path, v
			if field != "" {
				at = cli.Join(path, field)
				val = v.Get(field)
				if val == nil {
					val = v
				}
			}
			d.Add(cli.Errorf(cli.CodeInvalidOperation, at, "%s", msg), val)
			continue
		}
		p.Ops = append(p.Ops, spec)
	}
	if errs := d.Errs(); len(errs) > 0 {
		return nil, errs
	}
	return p, nil
}
