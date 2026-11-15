// Package read is the docx read surface: the read and find commands,
// ported from docxdriver 0.5.2 (api.rs typed_read and typed_find,
// commands/readonly.rs, and the listing halves of commands/comments.rs,
// revisions.rs and bookmarks.rs, html/assets.rs). The content is
// docxdriver's; the shape follows the agent contract (docs/contract.md):
// no outcome wrapper, base and notes on every result, snake_case keys,
// addresses in the form a plan takes them, one comment thread shape, and
// no nulls. tools/oracle maps a result back to docxdriver's shape for
// parity runs. Reading never changes a document.
package read

import (
	"errors"
	"runtime"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// Options are what the read side takes from outside the package.
type Options struct {
	// Equation converts an Office Math element (m:oMath or m:oMathPara) to
	// LaTeX, the docx CLI's one equation format (math.OMMLToLaTeX):
	// paragraph entries list their equations with it (equations[].latex,
	// and equations[].loss for content the LaTeX leaves out), and the HTML
	// projection renders each as <equation display="inline|block">LaTeX
	// </equation> (with data-loss likewise). When it is nil, reading
	// or finding in a document with equations fails with ErrNoEquation.
	Equation view.EquationFunc
	// Checkpoint, when set, is called where reading holds the most (the
	// parsed document; the result beside it), for the memory budget
	// (laws.WithinMemory).
	Checkpoint func(string)
}

// mark calls the checkpoint, if any.
func (o Options) mark(name string) {
	if o.Checkpoint != nil {
		o.Checkpoint(name)
	}
}

// ErrNoEquation is returned when a document has equations and Options has
// no OMML to LaTeX converter.
var ErrNoEquation = errors.New("read: the document has equations and no OMML to LaTeX converter is configured")

// Result is what a read or find command prints (docs/contract.md,
// section 4): the hash of the file read, the result's own fields, and
// the notes.
type Result struct {
	// Base is "sha256:<hex>" of the input bytes.
	Base string
	// Value holds the result's fields.
	Value Object
	// Notes are the damaged parts the command read around: each
	// disabled only its own facet (model.Note). Empty when the result
	// is complete.
	Notes []model.Note
}

// Object returns the result as its top-level object: base, the fields
// (without nulls: a fact that is not derived is left out) and notes.
func (r Result) Object() Object {
	out := Object{}
	for k, v := range r.Value {
		if v = withoutNulls(v); v != nil {
			out[k] = v
		}
	}
	out["base"] = r.Base
	notes := make([]any, len(r.Notes))
	for i, n := range r.Notes {
		notes[i] = Object{"at": n.At, "reason": n.Reason}
	}
	out["notes"] = notes
	return out
}

// withoutNulls returns v with every null member of an object removed, at
// any depth. Lists keep their length: a null element stays.
func withoutNulls(v any) any {
	switch x := v.(type) {
	case Object:
		out := make(Object, len(x))
		for k, e := range x {
			if e = withoutNulls(e); e != nil {
				out[k] = e
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withoutNulls(e)
		}
		return out
	case []Object:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withoutNulls(e)
		}
		return out
	}
	return v
}

// EncodeResult writes the result as JSON, base first and the other keys
// sorted; compact, or indented when pretty.
func EncodeResult(r Result, pretty bool) (string, error) {
	o := r.Object()
	delete(o, "base")
	rest, err := Encode(o, pretty)
	if err != nil {
		return "", err
	}
	head, err := Encode(Object{"base": r.Base}, pretty)
	if err != nil {
		return "", err
	}
	// Both are objects; join them at the brace.
	if pretty {
		return strings.TrimSuffix(head, "\n}") + "," + strings.TrimPrefix(rest, "{"), nil
	}
	return strings.TrimSuffix(head, "}") + "," + strings.TrimPrefix(rest, "{"), nil
}

// SourceHash is "sha256:<hex>" of b: a document's base.
func SourceHash(b []byte) string { return cli.Hash(b) }

// unreadable is the error for a read kind whose own part cannot be
// read: the one read a damaged part fails (every other kind reads around
// it and says so in notes).
func unreadable(detail string) error {
	return &cli.Error{Code: cli.CodeUnreadableTarget, Path: "--kind", Message: "the part this kind reads cannot be read", Detail: detail}
}

// invalid is the error for a file that cannot be opened as a Word
// document at all.
func invalid(msg string) error {
	return &cli.Error{Code: cli.CodeInvalidDocument, Message: msg}
}

// parse opens the document; a failure is the rejection message.
func parse(b []byte) (*model.Document, string) {
	d, err := model.OpenDocument(b)
	if err != nil {
		return nil, err.Error()
	}
	return d, ""
}

// Kinds are the read kinds, in help order.
var Kinds = []string{"document", "comments", "revisions", "bookmarks", "styles", "assets"}

// Read runs the read command: kind is the read kind ("document" when the
// caller gave none) and viewName the --view value ("" for none), which
// only the document kind (the HTML projection) uses. A file that cannot
// be opened is an invalid_document error; a part that cannot be read is
// a note, and the rest is read (docs/contract.md, section 10).
func Read(input []byte, kind, viewName string, opt Options) (Result, error) {
	res := Result{Base: SourceHash(input)}
	st, msg := parse(input)
	if st == nil {
		return Result{}, invalid(msg)
	}
	opt.mark("read parsed")
	var value Object
	switch kind {
	case "document":
		v, err := readDocument(st, viewName, opt)
		if err != nil {
			return Result{}, err
		}
		value = v
	case "assets":
		value = Object{"kind": "assets", "assets": assetManifest(st.Pkg, true)}
	case "styles":
		v, reject := listStyles(st.Pkg)
		if reject != "" {
			return Result{}, unreadable(reject)
		}
		value = listing(v, "styles")
	case "comments":
		threads, reject := commentThreads(st)
		if reject != "" {
			return Result{}, unreadable(reject)
		}
		value = listing(Object{"comments": threads}, "comments")
	case "revisions":
		value = listing(listRevisions(st.Pkg, st.XML), "revisions")
	case "bookmarks":
		value = listing(listBookmarks(st), "bookmarks")
	default:
		return Result{}, cli.Errorf(cli.CodeUsage, "--kind", "unknown read kind %q: %s", kind, strings.Join(Kinds, ", "))
	}
	res.Value = value
	res.Notes = st.Pkg.Notes()
	opt.mark("read done")
	runtime.KeepAlive(st)
	return res, nil
}

// listing adds the kind to a listing result.
func listing(v Object, kind string) Object {
	v["kind"] = kind
	return v
}

// Find runs the find command: the paragraph entries whose text in view
// contains query. view is "markup", "final" or "original".
func Find(input []byte, query string, ignoreCase bool, view string, opt Options) (Result, error) {
	res := Result{Base: SourceHash(input)}
	st, msg := parse(input)
	if st == nil {
		return Result{}, invalid(msg)
	}
	opt.mark("find parsed")
	v := model.All
	switch view {
	case "final":
		v = model.Current
	case "original":
		v = model.Baseline
	}
	needle := query
	if ignoreCase {
		needle = rustLower(query)
	}
	entries, err := paragraphEntries(st, v, opt, func(text string) bool {
		if ignoreCase {
			text = rustLower(text)
		}
		return containsString(text, needle)
	})
	if err != nil {
		return Result{}, err
	}
	matches := make([]any, len(entries))
	for i, e := range entries {
		matches[i] = e
	}
	res.Value = Object{"matches": matches}
	res.Notes = st.Pkg.Notes()
	opt.mark("find done")
	runtime.KeepAlive(st)
	return res, nil
}
