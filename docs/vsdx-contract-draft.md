# vsdx contract amendments (draft for owner sign-off)

Status: **draft, 2026-11-17.** Not merged into `docs/contract.md`, which
stays the agent surface until the owner signs this. The amendments are
additive (contract section 11: new tool, ops, fields, output keys,
hazard kinds and flags keep version `1`). Rationale for each choice is
in `docs/vsdx-design.md` (decisions a to g, page orientation). Section
numbers below are `docs/contract.md`'s; each says what to merge.

The rule for this draft: **vsdx is pptx's contract with Visio's nouns.**
Where pptx has a form, vsdx uses it; section 13 lists every difference
and the format fact that forces it.

## Intro

`pptx`, `docx`, `xlsx` and `vsdx` are four CLIs with one contract;
format-specific parts are marked **vsdx** as for the others.

## 1. Commands

```
vsdx read  <file> [--page pageN | --master masterN] [--detail sites|full] [--ids ids] [--region x,y,w,h] [--units pt|in]
vsdx find  <file> <query> [--ignore-case] [--page pageN] [--limit n]
vsdx xml   <file> <address>
vsdx apply <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
vsdx create [<plan.json|->] [--dry-run] -o <out>
vsdx render <file> --page pageN -o <out.png> [--overlay ids,sites,glue] [--ids ids] [--scale n]
vsdx help  [<command>|ops|<op>|html]
vsdx --version
```

- No `read --format svg` in v1 (pptx draws its SVG from its own
  geometry model; vsdx has no Visio geometry renderer yet).
- `render` rasterises one page through headless LibreOffice, as `pptx
  render` does a slide, with the same flags and result
  (`saved`, `px`, `size`, `scale`, `key`); LibreOffice draws the values
  the file caches, so the picture is for checking, not proof.
- `create` writes a new drawing (section 4) and applies the plan to it
  when one is given, as `xlsx create`.
- Files: `.vsdx`, `.vsdm` (VBA copied through), `.vstx`, `.vstm` are
  read and edited; `.vssx`, `.vssm` (stencils) are read only (their
  masters; `pages` is `[]`). Legacy `.vsd`, `.vdx`, `.vsx` and anything
  without a Visio document part are `invalid_document` (section 10).

## 3. Plans (P2)

Plan-wide fields: `ops`, `base`, `contract`, and `units` (`pt`, the
default, or `in`) as pptx's `units`.

**vsdx** operations:

| Noun | Operations |
| --- | --- |
| `object` | `object.add`, `object.set`, `object.move`, `object.delete`, `object.reorder` |
| `text` | `text.set`, `text.replace` |
| `prop` | `prop.set`, `prop.add`, `prop.delete` |
| `page` | `page.add`, `page.rename`, `page.delete`, `page.reorder`, `page.set` |

| Op | Fields |
| --- | --- |
| `object.add` | `at` (a page or group), `as`, `box` or `pin` (`[x, y]`, the shape's centre), `html`, `name`, `fill`, `line`, and one of: `master` (`master2`, or the master's name as `read` shows it); `geom` (`rect`, `ellipse`: a master-less shape with four connection points, 1 top, 2 right, 3 bottom, 4 left); `kind: "connector"` (a master-less straight connector) with `from`, `to`, `from_glue`, `to_glue` (point glue only: `"s3:2"`, `"$a:2"`; whole-shape glue is refused on a master-less connector) |
| `object.set` | `at`, and at least one of `box`, `rot`, `flip_h`, `flip_v`, `fill` (`"none"` or `{"hex":"#RRGGBB"}`), `line` (`"none"` or `{w, color, dash}`, `dash` the stored LinePattern number), `name`, `from`, `to`, `from_glue`, `to_glue` (`"s12"`, `"s12:3"`, `"$name"`, `"$name:3"`, `"none"`) |
| `object.move` | `at`, `dx`, `dy` (as pptx) |
| `object.delete` | `at`; its Connect rows go with it, and connectors glued to it become free |
| `object.reorder` | `at`, `to` (`front`, `back`, or an object with `position`) (as pptx) |
| `text.set` | `at` (a shape), `html` (as pptx) |
| `text.replace` | `at` (a shape or a page), `select`, `with`, `occurrence` or `all` (as pptx) |
| `prop.set` | `at` (`page0/s5/prop:Cost`), `value` (the form `read` shows: string, number, boolean, `{"date":"2024-03-01T00:00:00"}`) |
| `prop.add` | `at` (a shape), `name` (the row name), `label`, `type` (`string`, `number`, `boolean`, `date`, `duration`, `currency`, `fixed_list`, `variable_list`), `value`, `format` |
| `prop.delete` | `at` (a row); a row the master defines is hidden with `Del` (the instance stops inheriting it) |
| `page.add` | `name`, `as`, `position` and `at` (another page; default: appended), `size`, `orientation` (a new page is 8.5 x 11 in portrait unless given) |
| `page.rename` | `at`, `name` |
| `page.delete` | `at` |
| `page.reorder` | `at`, `to` (another page), `position` |
| `page.set` | `at`, `orientation` (`portrait`, `landscape`) and/or `size` (`[w, h]`) |

Rules specific to vsdx:

- A write to a cell whose formula is not a constant, `Inh`, `No Formula`,
  `THEMEVAL()` or `THEMEGUARD(...)` is refused, `operation_failed`,
  `detail` naming the cell and formula (a group child's
  `Sheet.10!Width*0.25`, a `GUARD(...)`, a glued end's `PAR(PNT(...))`).
- `page.set orientation`: `orientation` is
  the page's shape (from its size). A different orientation swaps
  PageWidth and PageHeight; the same one leaves the size. Then the
  page sheet stores `PrintPageOrientation` equal to the orientation
  when the effective print orientation is absent or differs (never in a
  style sheet). No shape's stored position changes, so every shown `y`
  moves by the change in height; `changed` lists the page and its
  top-level objects; an object now wholly off the page is an `off-page`
  hazard.
- The closed refusal list (`help <op>` gives each op's): containers,
  lists and callouts; data-linked shape data; masters and stencils;
  text and fill on pictures and other read-only kinds; glue to a cell
  other than a shape or a connection point; deleting a page used as
  another's background; geometry on a scaled page.

## 4. Results

`read` and `find` start with `base` and have `notes`, as every tool.

**vsdx** `apply` prints the edit result of section 4. `changed`:
objects as `read` shows them (as pptx), a page as its summary entry, a
shape data row as `{id, label, type, value}`.

**vsdx** `create` prints `{"dry_run","saved","new_base"}`, and with a
plan also its `aliases`, `changed`, `deleted` and `hazards`, as xlsx
create. The new drawing has one page, `page0` "Page-1", 8.5 x 11 in
portrait, and no masters, so `object.add` with `master` is `not_found`
in it; shapes come from `geom` and connectors from `kind: "connector"`.
The plan must not have `base` (`invalid_plan`, path `base`). With
`--dry-run` the plan is checked and applied in memory and nothing is
written.

Hazard kinds: pptx's
`stale-connector` and `dangling-glue`, `off-page` (new: an object whose
box lies wholly outside the page after `page.set`), and xlsx's
`stale-values` (`id` `"document"`): the save left cached values that formulas would compute
differently, and asked Visio to recalculate on open (section 9);
`detail` names up to ten cells.

## 6. Addresses

| Kind | Form | Tool | Stable across edits |
| --- | --- | --- | --- |
| page | `page0` (the page's stored ID) | vsdx | yes |
| master | `master2` (stored ID) | vsdx | yes |
| object | `page0/s5`, `master2/s5`; `page0/s5~2` for the second shape sharing id 5 | vsdx | yes (`~n` is positional) |
| connection site | `s12:3` (connection point 3 of object 12 on the connector's page), `s12` (the whole shape); `none` | vsdx | yes |
| shape data row | `page0/s5/prop:Cost` | vsdx | yes |
| layer | `page0/layer:Connector` | vsdx | until renamed |
| field | `page0/s5/field:1` (1-based in the shape's text): a placeholder | vsdx | positional |

Group children are addressed by their own id (ids are unique per page
across nesting); `parent` names the group.

## 7. HTML

**vsdx** text is read and written in the pptx text grammar (`help
html`), a subset of it: `<p>`, `<ul><li>` (`data-char`), `<b> <i> <u>
<s>`, `<br/>`, `<span style>` with `font-weight`, `font-style`,
`text-decoration` (the `normal`/`none` forms), `font-size`, `color`,
`font-family`, and `data-color` beside `color` in pptx's canonical form
(`<span style="color:#FF0000" data-color="#FF0000">`: the colour as
written, always an sRGB `#RRGGBB` in Visio; an indexed colour is written
resolved); on input `data-color` takes only `#RRGGBB` and must agree
with `color` when both are given (pptx's scheme forms such as
`accent1 lumMod=75000` are refused: Visio text has no theme colour
references); paragraph `style` with `text-align`, `margin-left`,
`margin-right`, `text-indent`, `margin-top`, `margin-bottom`,
`line-height`. Fields are placeholders:
`<opaque data-ref="page0/s5/field:1" data-kind="field" data-text="3"></opaque>`
(`data-text`, the cached display text, is derived). As in pptx, the
HTML shows only the text's own formatting, never what it inherits from
style sheets or (under a shape's own text) from its master; vsdx shows
an "off" form (`font-weight:normal`) only when it clears what the next
sheet sets. A trailing line feed of the stored text adds no empty
paragraph. What the HTML does
not show (tab runs, sub/superscript position, case, small caps, double
underline, letter spacing, language, Asian fonts, force-justify) is kept
from the run or paragraph replaced. A grammar element vsdx cannot store
(`<a href>`, `<ol>`, `data-level`) is refused by name.

## 8. Reading

- **vsdx** `read <file>` lists pages and masters:

```json
{"base":"sha256:…",
 "pages":[{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"portrait","background_page":"page3","objects":12},
          {"id":"page3","name":"Background-1","background":true,"size":[612,792],"objects":2}],
 "masters":[{"id":"master2","name":"Process","name_u":"Process","objects":1}],
 "notes":[]}
```

- `--page page0` (or `--master master2`, read only) gives the objects
  in paint order, groups before their children, with **effective**
  values (after master and style inheritance), lengths in points on a
  page whose origin is the top-left corner, y down:

  A master is shown in the frame of its own page sheet (the PageWidth
  and PageHeight of its `Master` entry's PageSheet in masters.xml, as
  Visio's master editing window shows it): origin at that sheet's
  top-left corner, y down, so a master shape drawn on its 1 x 0.75 in
  sheet has y between 0 and 54 pt. A master whose page sheet stores no
  PageHeight shows no `box`, `from` or `to`, with a note.

```json
{"base":"sha256:…","page":"page0","name":"Page-1","units":"pt","size":[612,792],"orientation":"portrait","print_orientation":"portrait",
 "layers":[{"id":"page0/layer:Flowchart","name":"Flowchart","index":0,"visible":true,"print":true}],
 "objects":[
  {"id":"page0/s1","kind":"shape","name":"Process","master":"master2","box":[108,117,72,54],"fill":{"hex":"#FFFFFF"},"line":{"w":0.75,"color":{"hex":"#000000"}},"html":"<p>Process</p>","text":"Process",
   "props":[{"id":"page0/s1/prop:Cost","label":"Cost","type":"currency","value":12.5}],"layers":["page0/layer:Flowchart"]},
  {"id":"page0/s3","kind":"connector","master":"master4","from":[180,144],"to":[324,144],"from_glue":"s1","to_glue":"s2:2"}],
 "connections":[{"connector":"page0/s3","from":"page0/s1","to":"page0/s2","from_glue":"s1","to_glue":"s2:2"}],
 "hazards":[],"notes":[]}
```

- Object keys as pptx where pptx has them (`id`, `kind`, `type`, `name`,
  `parent`, `hidden`, `box`, `rot`, `flip_h`, `flip_v`, `from`, `to`,
  `from_glue`, `to_glue`, `fill`, `line`, `html`, `sites` with
  `--detail sites`, `z`, `origins`, `omitted`, `xml` for kind `other`
  with `--detail full`); vsdx adds `name_u` (when it differs from
  `name`), `master`, `text` (plain), `props`, `layers`, and with
  `--detail full` `cells` (the shape's own stored cells:
  `{"n":"PinX","v":"2","u":"IN","f":"…"}`, names as the ShapeSheet
  spells them). Kinds: `shape`, `connector` (any 1-D shape), `group`,
  `picture`, `other`.
- `origins` values: `local`, `inherited` (stored value, inherited
  formula), `master`, `style`.
- `orientation` is the page's shape from its size (absent when square);
  `print_orientation` the printer orientation (PrintPageOrientation
  through the page sheet's styles), absent when not stored; they may
  disagree.
- A value stored as `Themed` (theme-derived, no cached value) is left
  out, with the reason under `--detail full` `omitted`. Colours stored
  as palette indexes are shown resolved (`#RRGGBB`).
- `glued` lists the Connect rows of an object's non-end cells
  (`[{"cell":"Controls.Row_1","to":"s2"}]`), read only.
- A duplicated page ID is addressed `page0~2`, as objects.
- `values_stale: true` on the summary and the page when the document asks
  Visio to recalculate on open.
- **vsdx** `find` gives `{"base","matches":[{"at","text"}],"notes"}`:
  each shape whose effective text, or shape data row whose value (as
  read prints it), contains the query, pages in stored order;
  `--ignore-case` is simple Unicode case folding (as xlsx); at most
  `--limit` matches (default 10,000; 0 no limit), a cut search has
  `"truncated":{"after":"<last at>"}` and a note.
- **vsdx** `xml <file> page0/s5` prints the shape's source bytes, its
  nested shapes included (as pptx `xml`).

## 9. What the tool promises

Add the exception: **vsdx** a save that leaves cached values stale
(section 3's rules and decision c of the design) sets the custom
document property `RecalcDocument` true (creating `docProps/custom.xml`
when absent), marks each changed cell with `<?NewValue V?>`, and
reports `stale-values`. The tool never writes a value it computed except
the dependents its closed evaluator handles (owner decision 3, approved).

## 10. Documents with defects

**vsdx**: `invalid_document` also for a package with no Visio document
relationship, a legacy binary or flat-XML Visio file, and a malformed
document part. A malformed page or master part is a note in the summary
and `unreadable_target` (path `--page`) when read; a shape whose master
is missing shows its own cells with a note.

## 12. Implementation notes

`internal/vsdx` packages; `docs/vsdx-design.md`.

## 13. vsdx differs from pptx because

| Difference | Forced by |
| --- | --- |
| `--page pageN` where pptx has `--slide slideN`; `page0` is the stored page ID, not a part name | Visio pages carry stable IDs; Visio renumbers page parts on save |
| `--master masterN` (read only) | Visio masters are first-class drawings that instances inherit from; PowerPoint layouts are not read as objects |
| no `--format svg` | no Visio geometry renderer in v1 |
| `create` takes a plan, not HTML | a drawing is not a flow of paragraphs (as xlsx); pptx has no create |
| `object.add` `geom` and `kind: "connector"` are master-less, point glue only | a new drawing has no masters; whole-shape glue needs Visio's dynamic connector master |
| `--units pt|in` (pptx `pt|emu`) | Visio's stored unit is the inch |
| y down from the page's top-left (Visio stores y up from the bottom-left); shown y moves when page height changes | pptx's frame kept for agents; Visio's own frame would make the same `box` mean different places in the two tools |
| `rot` from Visio's radians counter-clockwise about the pin | stored form |
| `origins` values `local`, `inherited`, `master`, `style` (pptx: `slide`, `layout`, `master`, `theme`, ...) | Visio's inheritance chain |
| `cells` under `--detail full` | ShapeSheet cells are Visio's property model |
| `props` are shape data rows; `prop.*` ops | Visio Shape Data (`Prop.Name`); pptx has none |
| `connections` graph on the page | Visio stores glue as page-level Connect rows |
| glue `s12` (whole-shape) besides `s12:3`; `s12@Cell` read only | Visio dynamic glue has no site index |
| `orientation`, `print_orientation` in read and `page.set orientation` | Visio pages have their own size and a printer orientation; a deck has one slide size |
| "off" run forms only when they clear an inherited setting | Visio stores Style on most rows; pptx's literal rule would mark nearly every run |
| `glued` (control-handle glue) | Visio Connect rows from control cells |
| `off-page` hazard | Visio pages resize independently of their shapes |
| `page.*` ops | Visio pages are independent drawings; pptx has no slide ops in v1 |
| `layers`, `layer:` addresses | Visio layers |
| `stale-values` hazard | Visio caches every computed value and does not recalculate on open |
| formula-bound cells refused | ShapeSheet formulas (pptx has none) |
| `object.add` takes `master` (pptx: `kind` and geometry) | Visio shapes are master instances; master-less shapes come in P3 |
| field placeholders `<opaque data-kind="field">` | Visio text fields with cached values |
| no `<a href>`, `<ol>`, `data-level` in HTML | Visio text has no hyperlink runs (hyperlinks are shape-level) or list levels |
| no `group.*`, `object.duplicate` in v1 | scope (P3) |

Additions that are not forced differences (pptx could have them too):
`find` (the contract's docx/xlsx command, useful on multi-page
drawings).
