package main

import (
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/apply"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/ops"
	"github.com/jhnoel/documentdriver/internal/docx/read"
	"github.com/jhnoel/documentdriver/internal/docx/view"
)

var applyCommand = &cli.Command{
	Name:    "apply",
	Args:    []string{"file.docx", "plan.json|-"},
	JSON:    true,
	Summary: "apply a plan of operations, all or nothing",
	Flags: []cli.Flag{
		{Name: "dry-run", Help: "do everything except write"},
		cli.OutFlag(),
		{Name: "changed", Value: "ids|full", Enum: []string{"ids", "full"}, Help: "show changed units as addresses only, or as read shows them (default)"},
		{Name: "clock", Value: "rfc3339", Hidden: true},
	},
	Help: `Applies a plan (a JSON file, or - for stdin) to the document and prints
the edit result: base and new_base (the file's hash before and after), the
addresses given to aliases, every changed unit as docx read shows it (a
paragraph as its id and html, a table, a section, a comment thread),
deleted addresses, hazards (the findings of the integrity check on the
parts the plan touched) and the parts changed. Without -o the document is
replaced in place, atomically; --dry-run prints the result and writes
nothing. A plan that fails changes no file.

plan: {"author": "Ada", "change_mode": "track",
       "ops": [{"op": "text.replace", "at": "0A1B2C3D", "select": "teh", "with": "the"}]}
change_mode is track (record revisions) or direct. author is required when
the plan tracks changes or adds a comment. base is optional: the hash docx
read shows; the plan is refused if the document has changed.

` + "`docx help ops`" + ` lists the operations; ` + "`docx help <op>`" + ` gives one;
` + "`docx help schema`" + ` prints the plan's JSON Schema.`,
}

const opsIntro = `Operations of a docx plan. Required fields first, optional in brackets.
at is an address as docx read shows it: a paragraph (0A1B2C3D), table:1,
table:1/row:2, table:1/column:3, table:1/row:2/cell:1, section:2,
comment:3, revision:7, bookmark:Name, 0A1B2C3D/equation:2, or $name for
what an earlier operation's as created. select is exact text of the
paragraph's final view; html is content in the HTML docx read shows; with
is plain text. Lengths are points.`

// opDocs returns documentation for every docx plan operation.
func opDocs() []cli.OpDoc {
	out := make([]cli.OpDoc, 0, len(ops.Ops))
	for i := range ops.Ops {
		d := &ops.Ops[i]
		doc := cli.OpDoc{Name: d.Name, Summary: d.Description(),
			Fields: cli.FieldDocs(reflect.TypeOf(d.New()).Elem(), typeName)}
		for _, r := range d.Rules {
			doc.Rules = append(doc.Rules, r.Msg)
		}
		if _, pending := d.New().(interface{ PendingOp() }); pending {
			doc.Summary += "\n\nNot implemented yet: a plan with this operation is refused when it runs (operation_failed at the operation), and nothing is written."
		}
		out = append(out, doc)
	}
	return out
}

// typeName names a field's type for the reference: the plan's own types
// by what they hold.
func typeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Name() {
	case "Points", "SignedPoints":
		return "points"
	case "ParagraphAddress", "InsertAnchor", "SectionAddress", "CommentAddress", "BookmarkAddress", "EquationAddress":
		return "address"
	case "RevisionScope":
		return "address or list of addresses"
	case "ParagraphContent":
		return "html, or a list of html"
	case "InsertPosition":
		return "before|after"
	case "ChromeKind":
		return "default|first|even"
	case "CommentStatus":
		return "open|resolved"
	case "HighlightColor", "RevisionAuthor":
		return "string"
	}
	if t.Kind() == reflect.Slice {
		return "list of " + typeName(t.Elem())
	}
	return cli.TypeName(t)
}

// opsHelp returns the full operations help text.
func opsHelp() string { return cli.OpsHelp("docx", opsIntro, opDocs()) }

// opHelp returns the help text for one named operation and whether it exists.
func opHelp(name string) (string, bool) { return cli.FindOpHelp(opDocs(), name) }

// htmlHelp is `docx help html`.
func htmlHelp() string {
	return `HTML in docx results and plans (docs/contract.md, section 7).

docx read writes the document as HTML in markup; a plan writes content in
html. The input is HTML5: end tags HTML makes optional may be left out,
<br> needs no slash, attribute values need no quotes, named character
references work. Anything the document cannot store is refused, never
dropped: an unknown element, an end tag with nothing to close, an element
left open, a comment, a style on an element that has no formatting.

Elements a plan may write:
  <p>, <h1>..<h6>            a paragraph (h1..h6 by style)
  <ul>, <ol start type>, <li> lists
  <b> <strong> <i> <em> <u> <s> <strike> <sup> <sub> <mark>, <span style>
  <a href>                   a hyperlink
  <br>                       a line break
  <table> <tr> <td> <th>     a table (no formatting attributes: use
                             table.format, row.format, cell.format)
  <equation display="inline|block">LaTeX</equation>
  <image src="data:<mime>;base64,..."/>
  <header> <footer> <section/> <page-number/> <page-count/>
                             only where an operation takes a whole document

A paragraph's html (paragraph.insert, each value or list item) is one
<p> or <h1>..<h6> as read shows it, whose style and paragraph CSS become
the paragraph's (id and the other data- attributes ignored), or bare
inline content, the content alone. Two paragraphs in one value are
refused: one paragraph per html value; use the list form for several.
In create, a top-level <equation display="block"> is a paragraph of its
own; an inline equation goes inside a <p>.

Placeholders: content the HTML cannot edit (a drawing, a chart, an
object, a symbol, a note reference, a field, a content control, a ruby,
alternate content, an unknown element) carries its address in data-ref,
0A1B2C3D/opaque:N (opaque:N in a header or footer, counted across it):
on its rendering where read has one (<img>, <sup data-note>, <span
data-field>, a <span data-kind> around a content control's text), else
as an empty <opaque data-ref data-kind></opaque>. Sent back, any element
with that data-ref is that content, kept byte for byte and moved where
the html puts it; its text and other attributes are read's and cannot
be edited there. An <opaque> without data-ref, or one naming nothing in
the content being replaced, is refused. A replace or set op whose html
leaves out a placeholder or equation of the content it replaces is
refused unless its delete field lists the address.

Attributes: id is an element's address (a paragraph's id, table:1,
table:1/row:2, table:1/row:2/cell:1); data-ref on an equation is its
address (0A1B2C3D/equation:1; equation:N in a header or footer). The other data- attributes read writes
(data-marker, data-author, data-revision, data-level, data-style, ...)
describe the document: send them back as read shows them, or leave them
out; they are not instructions. An attribute the input does not know is
refused by name, including the data-docx- names of docxdriver.
`
}

// applyResult is the edit result as the contract shapes it (section 4).
type applyResult struct {
	Base       string            `json:"base"`
	Author     string            `json:"author,omitempty"`
	ChangeMode string            `json:"change_mode"`
	NewBase    string            `json:"new_base,omitempty"`
	DryRun     bool              `json:"dry_run"`
	Saved      string            `json:"saved,omitempty"`
	Aliases    map[string]string `json:"aliases"`
	Changed    []any             `json:"changed"`
	Deleted    []string          `json:"deleted"`
	Hazards    []hazard          `json:"hazards"`
	Parts      []string          `json:"parts"`
}

type hazard struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
	New    bool   `json:"new"`
}

// contractCodes maps the pipeline's own diagnostic codes onto the
// contract's; a code that is already the contract's passes through.
var contractCodes = map[string]string{
	"source_mismatch":         cli.CodeStaleBase,
	"integrity_regression":    cli.CodeInvalidOutput,
	"source_invalid":          cli.CodeInvalidDocument,
	"paragraph_id_allocation": cli.CodeInvalidDocument,
	"paragraph_id_repair":     cli.CodeInvalidDocument,
	"invalid_now":             cli.CodeUsage,
	"preview_key_mismatch":    cli.CodeInternal,
}

var contractSet = map[string]bool{
	cli.CodeNotFound: true, cli.CodeAmbiguous: true, cli.CodeRefusedInMode: true, cli.CodeUnreadableTarget: true,
	cli.CodeOperationFailed: true, cli.CodeInvalidOutput: true, cli.CodeInvalidDocument: true, cli.CodeStaleBase: true,
	cli.CodeInvalidOperation: true, cli.CodeInvalidPlan: true, cli.CodeUsage: true, cli.CodeInternal: true,
}

// refusal turns a rejected run's diagnostic into the contract's error.
func refusal(d *apply.Diagnostic) error {
	code, ok := contractCodes[d.Code]
	if !ok {
		code = d.Code
	}
	if !contractSet[code] {
		code = cli.CodeOperationFailed
	}
	e := &cli.Error{Code: code, Path: d.Path, Message: d.Message}
	if code == cli.CodeUsage {
		e.Path = "--clock"
	}
	return e
}

// runApply applies the parsed plan to the document and writes its JSON result.
func runApply(p *cli.Parsed, stdout io.Writer, ropt read.Options) error {
	text, err := cli.ReadPlan(p.Args[1], os.Stdin)
	if err != nil {
		return err
	}
	plan, errs := ops.ParsePlan(text)
	if errs != nil {
		return errs
	}
	path := p.Args[0]
	input, err := readFile(path)
	if err != nil {
		return err
	}
	if e := cli.CheckBase(string(plan.Base), input); e != nil {
		return e
	}
	base := cli.Hash(input)
	plan.Base = ops.SourceHash(base) // no base means the file as it is
	var unitsBefore read.Object
	// A dry run stamps the real clock (apply.PreviewClock).
	opt := apply.Options{Out: p.Get("out"), Now: p.Get("clock"), PreviewClock: p.Has("dry-run"), View: view.Options{Equation: ropt.Equation},
		Input: func(d *model.Document) { unitsBefore = read.Units(d) }}
	out := applyResult{Base: base, Author: plan.Author, ChangeMode: string(plan.ChangeMode), DryRun: p.Has("dry-run")}
	if !out.DryRun {
		// One run commits: a refusal, at an operation or at the gate,
		// writes nothing, so no separate review is needed.
		opt.Commit = plan.PreviewKey()
	}
	res, err := apply.Run(path, plan, opt)
	if err != nil {
		return err
	}
	if res.Outcome == apply.Rejected {
		return refusal(res.Diagnostic)
	}
	if !out.DryRun {
		if len(res.Parts) == 0 {
			out.NewBase = base // a plan that changes nothing writes nothing
		} else {
			out.Saved = path
			if opt.Out != "" {
				out.Saved = opt.Out
			}
			written, err := readFile(out.Saved)
			if err != nil {
				return err
			}
			out.NewBase = cli.Hash(written)
		}
	}
	out.Aliases, out.Parts = res.Aliases, res.Parts
	if out.Aliases == nil {
		out.Aliases = map[string]string{}
	}
	if out.Parts == nil {
		out.Parts = []string{}
	}
	out.Changed, out.Deleted = changedUnits(unitsBefore, res, p.Get("changed") == "ids")
	out.Hazards = make([]hazard, 0, len(res.Hazards))
	for _, h := range res.Hazards {
		out.Hazards = append(out.Hazards, hazard{Kind: h.Kind, ID: strings.TrimPrefix(h.ID, "/"), Detail: h.Detail, New: h.New})
	}
	b, err := cli.Marshal(out)
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}
