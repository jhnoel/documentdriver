package cli

import (
	"io"
	"os"
	"strings"
)

// Envelope is a plan's shared part: the fields both tools have. The
// format decodes its own top-level fields and the operations from the
// values kept here.
type Envelope struct {
	// Base is the plan's base hash, "" when it has none.
	Base string
	// Ops are the operation objects, each with a string "op" member.
	Ops []*Value
	// Names are the operations' names, by index.
	Names []string
	// Settings are the format's top-level fields that the plan wrote.
	Settings map[string]*Value
	// Root is the plan object.
	Root *Value
}

// ReadPlan reads the plan text from a file, or from stdin when name is
// "-".
func ReadPlan(name string, stdin io.Reader) ([]byte, error) {
	if name == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, Errorf(CodeIO, "", "read the plan from stdin: %v", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(name) //nolint:gosec // G304: the plan is the file the caller named
	if err != nil {
		return nil, Errorf(CodeIO, "", "read the plan: %v", err)
	}
	return b, nil
}

// ParsePlan parses a plan's text and checks its envelope: one object,
// only the shared fields (ops, base, contract) and the format's settings
// at the top level, a non-empty ops list of objects that each name an
// op. It returns the envelope and the decoder the format goes on
// decoding with; envelope faults are already in the decoder. The
// envelope is nil when the text is not a JSON object.
func ParsePlan(text []byte, settings ...string) (*Envelope, *Decoder) {
	d := NewDecoder(text)
	root, perr := ParseJSON(text)
	if perr != nil {
		d.errs = append(d.errs, perr)
		return nil, d
	}
	if root.Kind != Object {
		d.Fail(CodeInvalidPlan, "", root, "a plan is a JSON object, found %s", root.Kind)
		return nil, d
	}
	env := &Envelope{Root: root, Settings: map[string]*Value{}}
	var ops *Value
members:
	for _, m := range root.Members {
		switch m.Key {
		case "ops":
			ops = m.Value
			continue
		case "base":
			if d.want(m.Value, "base", String) {
				if !validBase(m.Value.Str) {
					d.Fail(CodeInvalidPlan, "base", m.Value, "base is sha256: and 64 hex digits, as read shows it")
				} else {
					env.Base = m.Value.Str
				}
			}
			continue
		case "contract":
			var n int
			before := len(d.errs)
			d.Value(m.Value, "contract", &n)
			if len(d.errs) == before && (n < 1 || n > Contract) {
				d.Fail(CodeInvalidPlan, "contract", m.Value, "this tool implements contract %d, the plan asks for %d", Contract, n)
			}
			continue
		}
		for _, s := range settings {
			if m.Key == s {
				env.Settings[s] = m.Value
				continue members
			}
		}
		e := Errorf(CodeInvalidPlan, m.Key, "unknown field %q", m.Key)
		e.Hint = "plan fields: ops, base, contract" + prefixed(settings)
		e.Span = SpanOf(text, m.KeyOff, m.KeyOff+len(m.Key)+2)
		d.errs = append(d.errs, e)
	}
	switch {
	case ops == nil:
		d.Fail(CodeInvalidPlan, "ops", root, "missing field \"ops\"")
	case ops.Kind != Array:
		d.Fail(CodeInvalidPlan, "ops", ops, "expected a list, found %s", ops.Kind)
	case len(ops.Items) == 0:
		d.Fail(CodeInvalidPlan, "ops", ops, "a plan needs at least one operation")
	}
	if ops != nil && ops.Kind == Array {
		for i, op := range ops.Items {
			path := Index("ops", i)
			name := ""
			switch {
			case op.Kind != Object:
				d.Fail(CodeInvalidPlan, path, op, "an operation is an object, found %s", op.Kind)
			case op.Get("op") == nil:
				d.Fail(CodeInvalidPlan, Join(path, "op"), op, "missing field \"op\"")
			case op.Get("op").Kind != String:
				d.Fail(CodeInvalidPlan, Join(path, "op"), op.Get("op"), "expected a string, found %s", op.Get("op").Kind)
			default:
				name = op.Get("op").Str
			}
			env.Ops = append(env.Ops, op)
			env.Names = append(env.Names, name)
		}
	}
	return env, d
}

// prefixed joins names with commas, prefixed by one, or returns empty when none.
func prefixed(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return ", " + strings.Join(names, ", ")
}

// validBase reports whether s is a sha256: hash with 64 lowercase hex digits.
func validBase(s string) bool {
	hex, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(hex) != 64 {
		return false
	}
	for i := range len(hex) {
		c := hex[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// UnknownOp records the error for an operation name the format does not
// have.
func (d *Decoder) UnknownOp(i int, op *Value, tool string) {
	v := op.Get("op")
	e := Errorf(CodeInvalidPlan, Join(Index("ops", i), "op"), "unknown operation %q", v.Str)
	e.Hint = "run `" + tool + " help ops` for the list"
	d.Add(e, v)
}

// CheckBase is the staleness guard: nil when the plan has no base or it
// is the hash of data.
func CheckBase(base string, data []byte) *Error {
	if base == "" || base == Hash(data) {
		return nil
	}
	return Errorf(CodeStaleBase, "base", "the document is not the one the plan was written for").
		WithHint("read the document again and rewrite the plan against it")
}
