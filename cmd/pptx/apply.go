package main

import (
	"io"
	"os"
	"reflect"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/apply"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
)

var applyCommand = &cli.Command{
	Name:    "apply",
	Args:    []string{"deck.pptx", "plan.json|-"},
	JSON:    true,
	Summary: "apply a plan of operations, all or nothing",
	Flags: []cli.Flag{
		{Name: "dry-run", Help: "do everything except write"},
		cli.OutFlag(),
		{Name: "changed", Value: "ids|full", Enum: []string{"ids", "full"}, Help: "show changed objects as addresses only, or in full (default)"},
		{Name: "seed", Value: "n", Hidden: true},
		{Name: "clock", Value: "rfc3339", Hidden: true},
	},
	Help: `Applies a plan (a JSON file, or - for stdin) to the deck and prints the
edit result: base and new_base (the deck's hash before and after), the
addresses given to aliases, every changed object as pptx read shows it,
deleted addresses, the hazards on the slides after the plan (new marks
those the plan introduced) and the parts touched. Without -o the deck is
replaced in place, atomically; --dry-run prints the result and writes
nothing. A plan that fails changes no file.

plan: {"ops": [{"op": "object.move", "at": "slide3/s17", "dx": 10, "dy": 0}]}
optional plan fields: base (the hash pptx read shows: the plan is refused
if the deck has changed), units (pt, the default, or emu).

` + "`pptx help ops`" + ` lists the operations; ` + "`pptx help <op>`" + ` gives one.`,
}

// opsIntro is the text above the generated operation list.
const opsIntro = `Operations of a pptx plan. Required fields first, optional in brackets.
at is an address as pptx read shows it: slide3/s17, slide3/s17~2, or $name
for what an earlier operation's as created. Values take the form pptx read
shows them in; lengths are points unless the plan sets units.`

// opsNotes is the text below it: what the field lists cannot say.
const opsNotes = `
Fields of object.add and object.set, as pptx read shows them:
  box [x, y, w, h], rot (degrees), flip_h, flip_v; geom (a preset) with adj,
  or path: the SVG subset M L H V C Q Z (absolute or relative; no arcs),
  relative to the box, or a list of {d, fill, stroke}; custom (--detail
  full) sets the sites (cxn) and text rectangle (rect).
  fill and line: "none", a colour {"scheme": "accent1"} or {"hex":
  "#FF0000"}, line {w, color, dash, cap, head, tail}.
  Connectors: from, to (points), first (h or v), from_glue and to_glue
  ("s12:3": object on the same slide and site index; "$name:1"; "none").
  Pictures: image (a PNG, JPEG or GIF file, or a part as shown), svg (an
  SVG file with image as its fallback, or none), crop, fit, fill_rect,
  locks.
  descr, title, hidden, tx_box (shapes), and text: the body properties
  pptx read --detail full shows (anchor, wrap, insets, autofit
  none|shape|shrink, vert, num_col, ...).
html is the text body in the HTML pptx read shows (<p>, <ul>/<ol> with
<li>, <b>, <i>, <u>, <s>, <a href>, <br>, <span style>, lang and data-*
attributes). Anything outside that grammar is refused, never dropped; what
the HTML does not show (east Asian and symbol fonts, kerning, proofing
state) is kept from the paragraph or run it replaces.
`

// opDocs returns documentation for every implemented pptx plan operation.
func opDocs() []cli.OpDoc {
	var out []cli.OpDoc
	for _, d := range ops.Ops {
		if d.Pending {
			continue
		}
		out = append(out, cli.OpDoc{Name: d.Name, Summary: d.Summary,
			Fields: cli.FieldDocs(reflect.TypeOf(d.New()).Elem(), nil)})
	}
	return out
}

// opsHelp returns the full pptx operations help text with field notes.
func opsHelp() string { return cli.OpsHelp("pptx", opsIntro, opDocs()) + opsNotes }

// opHelp returns the help text for one named operation and whether it exists.
func opHelp(name string) (string, bool) { return cli.FindOpHelp(opDocs(), name) }

// runApply applies the parsed plan to the deck and writes its JSON result.
func runApply(p *cli.Parsed, stdout io.Writer) error {
	opt := apply.Options{ChangedIDs: p.Get("changed") == "ids"}
	opt.DryRun = p.Has("dry-run")
	opt.Out = p.Get("out")
	// Hidden: deterministic mode for evals and the parity oracle.
	src, err := cli.SeedClock(p)
	if err != nil {
		return err
	}
	opt.IDs = src
	text, err := cli.ReadPlan(p.Args[1], os.Stdin)
	if err != nil {
		return err
	}
	res, err := apply.Run(p.Args[0], text, opt)
	if err != nil {
		return err
	}
	b, err := res.JSON()
	if err != nil {
		return err
	}
	return cli.WriteJSON(stdout, b, p.Has("pretty"))
}

// htmlHelp is `pptx help html`.
func htmlHelp() string {
	return `HTML in pptx results and plans (docs/contract.md, section 7).

pptx read writes an object's text body as HTML in html; a plan writes it
in html (text.set, object.add, object.set). The input is HTML5: end tags
HTML makes optional may be left out, <br> needs no slash, attribute values
need no quotes, named character references work. Anything a text body
cannot store is refused, never dropped: an element or attribute not listed
here, an end tag with nothing to close, an element left open, a comment.
The error's path names the attribute or element (ops[1].html@class).

Elements:
  <p>                      a paragraph; bare text or inline elements alone
                           are one paragraph
  <ul>, <ol>, <li>         bulleted and numbered paragraphs (levels are
                           data-level, not nested lists)
  <b> <i> <u> <s>          bold, italic, underline, strike
  <a href>                 a hyperlink
  <span style lang>        run formatting
  <br>                     a line break

Attributes: the ones pptx read writes on these elements (style with the
properties it shows, lang, dir, href and data-* such as data-level, the
0-based level of a paragraph). Send them back as read; an attribute or
style property the grammar does not have is refused by name.

A value the file stores that the HTML does not show (east Asian and symbol
fonts, kerning, proofing state) is kept from the paragraph or run the
html replaces.
`
}
