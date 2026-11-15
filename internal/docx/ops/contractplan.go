package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// ParsePlan decodes, validates and normalises a plan in the agent
// contract's envelope (docs/contract.md, section 3), through the shared
// plan decoder (internal/cli): ops is required and not empty; base and
// contract are optional; change_mode is required; author is required
// when the plan tracks changes or has a comment operation. Unknown and
// duplicate keys and null are errors.
//
// Every fault that can be found without the document is returned
// together: the envelope's, then per operation its first decoding fault
// (invalid_plan) or its first broken rule (invalid_operation), each with
// the path of the value at fault.
//
// A plan without a base has an empty Base: the caller fills in the
// document's hash, which is what "the file as it is" means.
func ParsePlan(text []byte) (*Plan, cli.Errors) {
	env, d := cli.ParsePlan(text, "author", "change_mode")
	if env == nil {
		return nil, d.Errs()
	}
	p := &Plan{Base: SourceHash(env.Base)}
	if m := env.Settings["change_mode"]; m == nil {
		d.Fail(cli.CodeInvalidPlan, "change_mode", env.Root, "missing field \"change_mode\" (track or direct)")
	} else {
		var mode string
		d.Value(m, "change_mode", &mode)
		switch ChangeMode(mode) {
		case ModeTrack, ModeDirect:
			p.ChangeMode = ChangeMode(mode)
		default:
			if m.Kind == cli.String {
				d.Fail(cli.CodeInvalidPlan, "change_mode", m, "change_mode is track or direct, not %q", mode)
			}
		}
	}
	author := env.Settings["author"]
	if author != nil {
		d.Value(author, "author", &p.Author)
		if author.Kind == cli.String && p.Author == "" {
			d.Fail(cli.CodeInvalidPlan, "author", author, "author must not be empty")
		}
	}
	comments := false
	for i, op := range env.Ops {
		name := env.Names[i]
		if name == "" {
			continue // the envelope reported it
		}
		def, ok := Lookup(name)
		if !ok {
			d.UnknownOp(i, op, "docx")
			continue
		}
		comments = comments || strings.HasPrefix(name, "comment.")
		at := cli.Index("ops", i)
		spec := def.New()
		if !decodeSpec(d, op, at, spec) {
			continue
		}
		if msg, path, bad := def.check(spec); bad {
			d.Fail(cli.CodeInvalidOperation, cli.Join(at, path), op, "%s", msg)
			continue
		}
		if def.Normalize != nil {
			def.Normalize(spec)
		}
		p.Ops = append(p.Ops, Op{Name: name, Spec: spec})
	}
	if author == nil && (p.ChangeMode == ModeTrack || comments) {
		d.Fail(cli.CodeInvalidPlan, "author", env.Root, "missing field \"author\": a plan that tracks changes or comments names who made them")
	}
	if errs := d.Errs(); len(errs) > 0 {
		return nil, errs
	}
	return p, nil
}
