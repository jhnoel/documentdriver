package plan

import (
	"bytes"
	"encoding/json"
)

// Hazard is a place where the document, after a plan, contradicts
// itself, as the format's read model reports it. Kinds are the format's
// (pptx: stale-connector, dangling-glue, group-frame, inset-overflow). New is set when
// the input did not have it (same kind and id).
type Hazard struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
	New    bool   `json:"new"`
}

// MarkNew returns after with New set on each hazard whose kind and id
// are not in before. It never fixes a hazard; it only says which ones the
// plan introduced.
func MarkNew(before, after []Hazard) []Hazard {
	had := map[string]bool{}
	for _, h := range before {
		had[h.Kind+" "+h.ID] = true
	}
	out := make([]Hazard, 0, len(after))
	for _, h := range after {
		h.New = !had[h.Kind+" "+h.ID]
		out = append(out, h)
	}
	return out
}

// Result is the engine side of a plan's edit result: what every format
// returns. A format adds its own leading fields (pptx: the slide and the
// units) through JSON's head.
type Result struct {
	// Base is the hash of the input document ("sha256:<hex>"); NewBase
	// the hash of the file written ("" on a dry run). The format's CLI
	// fills them in; "" leaves the key out.
	Base, NewBase string
	DryRun        bool
	// Saved is the file written ("" for a dry run).
	Saved string
	// Aliases maps each alias (without $) to the id it names.
	Aliases map[string]string
	// Changed holds the new and changed units, each as the format's JSON
	// view shows it. Deleted lists removed units' input ids.
	Changed []json.RawMessage
	Deleted []string
	Hazards []Hazard
	// Parts lists the package parts the plan changed.
	Parts []string
}

// NewResult returns an empty result: every list empty, not nil, so that
// the JSON shows [] and {}.
func NewResult() Result {
	return Result{Aliases: map[string]string{}, Changed: []json.RawMessage{}, Deleted: []string{}, Hazards: []Hazard{}, Parts: []string{}}
}

// Field is one leading key of a result's JSON.
type Field struct {
	Key   string
	Value any
}

// JSON writes the result with one key per line, head first, and each
// changed unit and hazard on its own line.
func (r *Result) JSON(head ...Field) ([]byte, error) {
	var b bytes.Buffer
	var err error
	enc := func(v any) []byte {
		var x bytes.Buffer
		e := json.NewEncoder(&x)
		e.SetEscapeHTML(false)
		if eerr := e.Encode(v); eerr != nil && err == nil {
			err = eerr
		}
		return bytes.TrimRight(x.Bytes(), "\n")
	}
	list := func(items [][]byte) {
		if len(items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[")
		for i, it := range items {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n  ")
			b.Write(it)
		}
		b.WriteString("\n ]")
	}
	b.WriteString("{")
	if r.Base != "" {
		head = append([]Field{{Key: "base", Value: r.Base}}, head...)
	}
	if r.NewBase != "" {
		head = append(head, Field{Key: "new_base", Value: r.NewBase})
	}
	for _, f := range head {
		b.WriteString("\n ")
		b.Write(enc(f.Key))
		b.WriteString(": ")
		b.Write(enc(f.Value))
		b.WriteString(",")
	}
	b.WriteString("\n \"dry_run\": ")
	b.Write(enc(r.DryRun))
	if r.Saved != "" {
		b.WriteString(",\n \"saved\": ")
		b.Write(enc(r.Saved))
	}
	b.WriteString(",\n \"aliases\": ")
	b.Write(enc(r.Aliases))
	b.WriteString(",\n \"changed\": ")
	var ch [][]byte
	for _, c := range r.Changed {
		ch = append(ch, c)
	}
	list(ch)
	b.WriteString(",\n \"deleted\": ")
	b.Write(enc(r.Deleted))
	b.WriteString(",\n \"hazards\": ")
	var hz [][]byte
	for _, h := range r.Hazards {
		hz = append(hz, enc(h))
	}
	list(hz)
	b.WriteString(",\n \"parts\": ")
	b.Write(enc(r.Parts))
	b.WriteString("\n}\n")
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
