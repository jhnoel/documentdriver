# vsdx contract amendments (draft for sign-off)

Status: **draft, revision 2 (2026-11-17), after Fable's review for the
owner: APPROVE-WITH-AMENDMENTS; Fable's rulings on the designer's eight
questions folded in the same day.** Not merged into `docs/contract.md`,
which stays the agent surface. Every promise below either holds in the
binary at 9a57372 or is marked with the chunk that delivers it
("(CC2)"); the chunks, their rows and the coverage matrix that maps
every op and refusal to a test are in the chunk plan (`chunks.md`,
"Contract-review chunks"). The merge happens after every marked chunk
has merged and `make ci` passes; its plan is the last section.

The amendments are additive: the contract version stays `1` (§11
allows a new tool's ops, fields, output keys, flags and hazard kinds).
Each entry names the section of `docs/contract.md` it changes and
gives the exact text to merge: **Replace** swaps the quoted text for
the new text, **Add after** inserts the new text after the quoted
anchor, **Add row** adds rows to a table. Rationale is in
`docs/vsdx-design.md` (decisions a to g, page orientation, create,
render); the rule of the draft is **vsdx is pptx's contract with
Visio's nouns**, and the record at the end lists each difference and
the format fact that forces it.

## Intro

**Replace**

> `pptx`, `docx` and `xlsx` are three CLIs with one contract. This page is
> the whole of what an agent may rely on; anything not written here
> (whitespace, message wording, key order) may change. Format-specific
> parts are marked **pptx**, **docx** or **xlsx**. `<tool>` is any of the
> binaries.

**with**

> `pptx`, `docx`, `xlsx` and `vsdx` are four CLIs with one contract.
> This page is the whole of what an agent may rely on; anything not
> written here (whitespace, message wording, key order) may change.
> Format-specific parts are marked **pptx**, **docx**, **xlsx** or
> **vsdx**. `<tool>` is any of the binaries.

**Add after** "recorded in `docs/xlsx-contract-draft.md`)."

> The **vsdx** parts on <date of sign-off> (Fable for the owner; the
> amendments and rulings are recorded in `docs/vsdx-contract-draft.md`).

## §1. Commands

**Add after** the xlsx command block (the anchor is the line
"`xlsx create [<plan.json|->] [--dry-run] [--recalc] -o <out>`" and its
closing fence)

> **vsdx** has `read`, `find`, `xml`, `apply`, `create`, `render`,
> `help` and `--version`. Its help topics are `ops`, `<op>` and `html`;
> there is no `schema` or `values` topic.
>
> ```
> vsdx read   <file> [--page pageN | --master masterN] [--detail sites|full] [--ids ids] [--region x,y,w,h] [--units pt|in]
> vsdx find   <file> <query> [--ignore-case] [--page pageN] [--limit n]
> vsdx xml    <file> <address>
> vsdx apply  <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
> vsdx create [<plan.json|->] [--dry-run] -o <out>
> vsdx render <file> --page pageN -o <out.png> [--overlay ids,sites,glue] [--ids ids] [--scale n]
> ```
>
> - Files: `.vsdx`, `.vsdm` (VBA copied through), `.vstx` and `.vstm`
>   are read and edited; `.vssx` and `.vssm` (stencils) are read only:
>   their masters, and `pages` is `[]`.
> - `read` has no `--format svg`: there is no Visio geometry renderer.
>   `render` rasterises one page through headless LibreOffice, with
>   pptx render's flags and result (`saved`, `px`, `size`, `scale`,
>   `key`); LibreOffice draws the values the file caches, so the
>   picture is for checking, not proof.
> - `--page` and `--master` take an address as `read` lists it; any
>   other value is `usage` at the flag, exit 2 (CC10); a page address
>   the drawing lacks is `not_found` at the flag.

## §3. Plans

**Add row** to the plan-field table

> | `units` | vsdx | no | `pt` (default) or `in` for lengths |

**Add after** the xlsx operation table (anchor: the row "| `name` |
`name.add`, `name.set`, `name.delete` |")

> **vsdx**:
>
> | Noun | Operations |
> | --- | --- |
> | `object` | `object.add`, `object.set`, `object.move`, `object.delete`, `object.reorder` |
> | `text` | `text.set`, `text.replace` |
> | `prop` | `prop.add`, `prop.set`, `prop.delete` |
> | `page` | `page.add`, `page.rename`, `page.delete`, `page.reorder`, `page.set` |
>
> Layers are read only: `read` lists them and an object's `layers`;
> no op writes them.

**Add after** the op field table's last pptx/xlsx row (the vsdx rows;
`help <op>` gives each op's fields with notes, its `rules:` and its
`refuses:`, CC7)

> | Op | Fields |
> | --- | --- |
> | `object.add` (vsdx) | `at` (a page or group), `as`, `name`, `fill`, `line`, `html`, and one of: `master` (an address, `master2`, or the master's name as `read` shows it) with `box` or `pin` (`[x, y]`, the centre) for a 2-D master, or `from`/`to`/`from_glue`/`to_glue` for a 1-D master such as Dynamic connector (CC3); `geom` (`rect`, `ellipse`) with `box` or `pin`: a master-less shape with four connection points, 1 top, 2 right, 3 bottom, 4 left; `kind: "connector"` with `from`, `to`, `from_glue`, `to_glue`: a master-less straight connector, point glue only (whole-shape glue needs the Dynamic connector master). No `rot`, `flip_h`, `flip_v` (set them with object.set) |
> | `object.set` (vsdx) | `at`, and at least one of `box`, `rot`, `flip_h`, `flip_v`, `name`, `fill` (`"none"` or `{"hex":"#RRGGBB"}`), `line` (`"none"` or `{w, color, dash}`; `dash` the LinePattern number, as a number or the string `read` shows), and for a connector `from`, `to`, `from_glue`, `to_glue`. No `html` (use text.set) |
> | `object.move` (vsdx) | `at`, `dx`, `dy` |
> | `object.delete` (vsdx) | `at` |
> | `object.reorder` (vsdx) | `at`, `to` (`front`, `back`, or an object of the same parent with `position`) |
> | `text.set` (vsdx) | `at` (a shape), `html`, `delete` (field addresses, §7) (CC11) |
> | `text.replace` (vsdx) | `at` (a shape or a page), `select`, `with`, `occurrence` or `all` |
> | `prop.add` | `at` (a shape), `name` (the row name: ASCII letters, digits and `_`, not starting with a digit; anything else is `invalid_operation` at `name` before the document opens, CC8; rows a file already has with other names are read, set and deleted by their address), `type` (`string`, `number`, `boolean`, `date`, `duration`, `currency`, `fixed_list`, `variable_list`), `label`, `value`, `format` |
> | `prop.set` | `at` (`page0/s5/prop:Cost`), `value` |
> | `prop.delete` | `at` (a row) |
> | `page.add` | `name`, `as`, `size`, `orientation`, and `at` (another page) with `position` (`before`, `after`): the two go together (CC13); neither appends. A new page is 8.5 x 11 in portrait unless `size` or `orientation` says otherwise |
> | `page.rename` | `at`, `name` |
> | `page.delete` | `at` |
> | `page.reorder` | `at`, `to` (another page), `position` |
> | `page.set` | `at`, `orientation` (`portrait`, `landscape`) and/or `size` (`[w, h]`) |
>
> **vsdx** rules:
>
> - **Glue.** `from_glue`/`to_glue` is `"s12:3"` (connection point 3 of
>   object 12), `"s12"` (the whole shape: Visio's dynamic glue, which
>   needs only the target's pin, so any shape can be glued to), an
>   alias with or without a point (`"$box:2"`), or `"none"`. Point glue
>   works on every connector. Whole-shape glue works on a connector
>   from a master that stores Visio's dynamic glue cells (Dynamic
>   connector, CC2, CC3); on a master-less connector it is refused
>   (`operation_failed` at the glue field, hint: add the connector from
>   the Dynamic connector master), because no Visio-authored file
>   shows those cells on a master-less shape. Whole-shape glue is
>   written in the form Visio stores; that Visio re-walks it when a
>   glued shape moves, and reroutes a dynamic connector whose ends
>   changed, is **Visio unconfirmed (VE4, VE13)**.
>   `from` with `from_glue` is accepted, as pptx: the end is written at
>   `from` and glued; an end off its site is reported as
>   `stale-connector` and `stale-values` (Visio moves it to the site
>   when it recalculates) (CC10).
> - **Connectors** Visio drew are edited when their transform cells are
>   constants, inherited, or Visio's standard 1-D forms (`GUARD(...)`,
>   `SQRT`, `ATAN2` of the ends), and their ends are constants or glue
>   (CC1, CC2). A free end is placed where asked and the connector's
>   width, pin and stored geometry (a straight line) follow it; a
>   dynamic connector's route is left for Visio to redraw (Visio
>   unconfirmed, VE4).
> - **object.move** moves a 2-D object, or a connector's free ends; a
>   glued end keeps its glue and position. A connector glued at both
>   ends is refused (set one end's glue to `none` first) (CC2).
> - **Formulas.** A write to a cell whose formula vsdx does not
>   evaluate is refused, `operation_failed`, `path` the field written
>   (`ops[n].at` for object.move), `detail` each refused cell as
>   `Width F=User.DefaultWidth` (CC10). Cells that inherit an
>   evaluable formula from the master and depend on a written cell are
>   stored with the master's formula reference (`F="Inh"`) and the new
>   value, so a resized instance reads back the box asked for (CC4).
> - **A value equal to what `read` shows writes nothing**, for object
>   values, shape data, page values and text formats alike, whether
>   the shown value is stored on the object or inherited (master, then
>   style sheets): `read`'s output sent back is a no-op (CC4, CC8,
>   CC11). A different value is stored on the object, and its origin
>   becomes `local`. A text format equal to the inherited one is never
>   stored, and a stored one equal to it is removed when the text is
>   set (decision f). Pinning an inherited value locally (storing it
>   unchanged so the object stops following its master) is deferred.
> - **Value forms** are `read`'s: string, number and currency numbers,
>   boolean, date `{"date":"YYYY-MM-DDThh:mm:ss"}` (anything else in it
>   is `invalid_operation` at `value`, CC8), duration the stored text
>   (Visio's elapsed-time number as written, unit not converted),
>   `fixed_list` and `variable_list` the chosen item's text, with the
>   choices in `format`, `;`-separated.
> - **Aliases** extend: `$r/prop:Cost`, `$r/field:1`, `$p/s5` name what
>   the address `$r` or `$p` got followed by that suffix, so a plan can
>   add a shape and set its data atomically (CC12).
> - **page.set orientation** is the page's shape (from its size): a
>   different one swaps PageWidth and PageHeight, the same one leaves
>   the size; the page sheet then stores `PrintPageOrientation` equal
>   to it when the effective print orientation is absent or differs
>   (never in a style sheet). Stored positions do not change, so every
>   shown `y` moves by the change in height; `changed` lists the page
>   and its top-level objects; an object now wholly off the page is an
>   `off-page` hazard.
> - **Refused** (`operation_failed`, `path` the op or field, unless
>   given): a formula-bound cell (above); a container, list or callout
>   (`User.msvStructureType` `Container`, `List`, `Callout`) for every
>   write, and a member of one (a `Relationships` cell with
>   `DEPENDSON`) for box, rotation, flips, move and delete (CC6);
>   geometry on a page with a drawing scale (box, rot, flips, ends,
>   move, object.add, page.set size) (CC6); a data-linked shape data
>   row for prop.set and prop.delete; any write to a master, a master
>   shape or a stencil; text and fill on a picture or other
>   read-only kind; glue to a cell other than a shape's pin or a
>   connection point (`invalid_operation` at the glue field); deleting
>   the last foreground page; deleting a page another page uses as its
>   background; a duplicate page name (page.add, page.rename); a
>   duplicate shape data row (prop.add); reorder relative to an object
>   of another parent; a connector whose ends coincide; `from`/`to` or
>   glue on a 2-D shape; move of a connector glued at both ends (CC2);
>   whole-shape glue on a master-less connector (CC2); deleting a
>   sub-shape a master instance inherits (a shape with `MasterShape`
>   inside an instance), `ops[n].at` (CC6); `page.set` when PageWidth
>   or PageHeight is bound to a formula.

## §4. Results

**Add after** the xlsx `create` paragraph (anchor: "it applies the plan
to that workbook first and also prints the plan's `changed`,
`deleted`, `aliases` and `hazards`; the plan must not have `base`. With
`--dry-run` it checks the plan and writes nothing.")

> **vsdx** `apply` prints the edit result; `units` follows `base` when
> the plan sets it (CC9). `changed`: objects as `read` shows them, a
> page as its summary entry, a shape data row as its `props` entry
> (`{id, label, type, value}` and `format`, `unit`, `hidden`, `linked`
> when read shows them). `deleted` includes field addresses a
> `text.set` `delete` listed (CC11).
>
> **vsdx** `create` prints `{"new_base", "dry_run", "saved"}`; with a
> plan also its `aliases`, `changed`, `deleted`, `hazards` and
> `parts`, and `units` when the plan sets it (CC9). The new drawing has
> one page, `page0` "Page-1", 8.5 x 11 in portrait, and no masters, so
> `object.add` with `master` is `not_found` in it; shapes come from
> `geom` and connectors from `kind: "connector"`. The plan must not
> have `base` (`invalid_plan`, path `base`). With `--dry-run` the plan
> is checked and applied in memory and nothing is written.

**Add after** the xlsx hazard bullet (anchor: "check on the parts the
plan touched, which are pre-existing (`new` false), as in docx.")

> - **vsdx**: `stale-connector` and `dangling-glue` as pptx; `off-page`
>   (an object whose box lies wholly outside its page); `stale-values`
>   (`id` `"document"`: the save left cached values that formulas would
>   compute differently and asked Visio to recalculate on open, §9;
>   `detail` names the cells of the first op that left them, at most
>   ten, then "left stale"; `new` is true on the plan that first set the
>   request).

## §6. Addresses

**Add row** (before the alias row)

> | page | `page0` (the page's stored ID); `page0~2` for the second page sharing it | vsdx | yes |
> | master | `master2` (stored ID), read only | vsdx | yes |
> | object | `page0/s5`, `master2/s5`; `page0/s5~2` for the second shape sharing id 5. Group children are addressed by their own id; `parent` names the group | vsdx | yes (`~n` is positional) |
> | connection site | `s12:3` (point 3 of object 12 on the connector's page), `s12` (the whole shape); `none` | vsdx | yes |
> | shape data row | `page0/s5/prop:Cost` (the row name) | vsdx | yes |
> | layer | `page0/layer:Connector`, read only | vsdx | until renamed |
> | field | `page0/s5/field:1` (1-based in the shape's text): a placeholder (§7) | vsdx | positional |

**Replace** the alias row

> | alias | `$name`, defined by an earlier op's `as` in the same plan | all | for the plan |

**with**

> | alias | `$name`, defined by an earlier op's `as` in the same plan. **vsdx**: followed by any suffix the named address takes (`$r/prop:Cost`, `$r:2`) (CC12) | all | for the plan |

## §7. HTML

**Replace**

> Text content is read and written as HTML: pptx text bodies and docx
> `markup`, both written in plans as `html`.

**with**

> Text content is read and written as HTML: pptx and vsdx text bodies
> and docx `markup`, all written in plans as `html`.

**Add after** the paragraph that ends "and appears in the result's
`deleted`." (the placeholder rules apply to vsdx fields, CC11)

> **vsdx** text uses the pptx text grammar's subset that Visio stores
> (`help html`): `<p>`, `<ul><li>` (`data-char`), `<b> <i> <u> <s>`,
> `<br/>`; `<span style>` with `font-weight`, `font-style`,
> `text-decoration` (with the `normal` and `none` forms), `font-size`
(at least 1pt, Visio's minimum; less is refused, CC1),
> `color`, `font-family`, and `data-color` beside `color` in pptx's
> canonical form (always `#RRGGBB`: Visio text has no theme colour
> references; on input `data-color` must agree with `color`);
> paragraph `style` with `text-align`, `margin-left`, `margin-right`,
> `text-indent`, `margin-top`, `margin-bottom`, `line-height`. Fields
> are placeholders, `<opaque data-ref="page0/s5/field:1"
> data-kind="field" data-text="3"></opaque>` (`data-text`, the cached
> display text, is derived), and follow the placeholder rules above:
> `text.set` must send each back or list it in `delete` (CC11). As in
> pptx, the HTML shows only the text's own formatting, never what it
> inherits from style sheets or, under a shape's own text, from its
> master; an "off" form (`font-weight:normal`) appears only where it
> clears what the next sheet sets. When `text.replace` gives a shape
> that showed its master's text text of its own, the master's
> formatting still applies in Visio but is inherited, so the HTML no
> longer shows it. A trailing line feed of the stored text adds no
> empty paragraph. What the HTML does not show (tab runs,
> sub/superscript position, case, small caps, double underline, letter
> spacing, language, Asian fonts, force-justify) is kept from the run
> or paragraph replaced. `<a href>`, `<ol>`, `data-level` and every
> other element or attribute outside the subset are refused by name.
> HTML is checked with the rest of the plan, before the document opens,
> and every fault comes back together with the path forms above
> (`ops[0].html<a>`, `ops[1].html@bogus`) (CC5).

## §8. Reading

**Add after** the xlsx reading bullet (anchor: the bullet that starts
"**xlsx** `read <file>`")

> - **vsdx** `read <file>` lists pages and masters:
>   `{"base","pages":[{"id","name","size","orientation","print_orientation","background","background_page","objects"}],"masters":[{"id","name","name_u","objects"}],"notes"}`.
>   `--page page0` (or `--master master2`, read only) gives the page's
>   objects in paint order, groups before their children, with
>   **effective** values (after master and style inheritance), lengths
>   in points (`--units in`: inches) on a frame whose origin is the
>   page's top-left corner, y down. A master is shown in the frame of
>   its own page sheet; one with no stored PageHeight shows no `box`,
>   `from` or `to`, with a note. The page also has `layers` (read
>   only), `connections` (`{connector, from, to, from_glue, to_glue}`),
>   `hazards` and `notes`.
> - Object keys are pptx's where pptx has them (`id`, `kind`, `type`,
>   `name`, `parent`, `hidden`, `box`, `rot`, `flip_h`, `flip_v`,
>   `from`, `to`, `from_glue`, `to_glue`, `fill`, `line`, `html`,
>   `sites` with `--detail sites`, `z`, `origins`, `omitted`, `xml` for
>   kind `other` with `--detail full`); vsdx adds `name_u` (when it
>   differs), `master`, `text` (plain), `props`, `layers`, `glued`
>   (control-handle glue, read only) and, with `--detail full`, `cells`
>   (the shape's own stored cells, `{"n","v","u","f"}`). Kinds: `shape`,
>   `connector` (any 1-D shape), `group`, `picture`, `other`. `line`
>   is `{w, color, dash}` with `dash` the LinePattern as stored, a
>   string. A `props` entry is `{id, label, type, value}` with
>   `format`, `unit` (a number's length unit, value in inches),
>   `hidden` and `linked` when they apply; a date that is not stored in
>   date form shows no `value`, with an omission (CC8).
> - `origins` values: `local`, `inherited` (stored value, inherited
>   formula), `master`, `style`. A value stored as `Themed` is left out,
>   with the reason in `omitted`; palette-index colours are shown
>   resolved (`#RRGGBB`).
> - `orientation` is the page's shape from its size (absent when
>   square); `print_orientation` the printer setting, absent when not
>   stored; they may disagree. A page with a drawing scale has the note
>   "drawing scale 1 in = 120 ft: lengths are stored drawing units" (V13; its numbers and units as stored)
>   (CC6), and its lengths are shown as stored (drawing units are not
>   applied).
> - `values_stale: true` on the summary and the page when the document
>   asks Visio to recalculate on open.
> - **vsdx** `find` gives `{"base","matches":[{"at","text"}],"notes"}`:
>   each shape whose effective text, or shape data row whose value as
>   read prints it, contains the query, pages in stored order;
>   `--ignore-case` is simple Unicode case folding; at most `--limit`
>   matches (default 10,000; 0 is no limit); a cut search has
>   `"truncated":{"after":"<last at>"}` and a note.
> - **vsdx** `xml <file> page0/s5` prints the shape's source bytes, its
>   nested shapes included.

## §9. What the tool promises about a document

**Add after** the xlsx exception (anchor: "reports the `stale-values`
hazard and `read` shows `values_stale`.")

>   - a vsdx save that leaves cached values stale (a write that changes
>     a cell other cells' formulas read, when vsdx does not evaluate
>     them) sets the custom document property `RecalcDocument` true
>     (creating `docProps/custom.xml` when absent) and marks each
>     changed cell with `<?NewValue V?>`, so that Visio recalculates on
>     open. That Visio does so, and opens a created
>     `docProps/custom.xml` cleanly, is **Visio unconfirmed** until
>     experiments VE2, VE3 and VE12 are recorded in
>     `docs/vsdx-design.md`; this exception must not be signed off as
>     confirmed before then. vsdx writes computed values only for the
>     formulas its closed evaluator handles (decision c of the design).
>     The result reports `stale-values`.

## §10. Documents with defects

**Replace**

> workbook part), or trips a safety limit (size, ratio, entry count,
> nesting depth). **xlsx**: also a binary workbook (`.xlsb`), a legacy
> `.xls` and an encrypted workbook.

**with**

> workbook or Visio document part), or trips a safety limit (size,
> ratio, entry count, nesting depth). **xlsx**: also a binary workbook
> (`.xlsb`), a legacy `.xls` and an encrypted workbook. **vsdx**: also
> a legacy binary or XML Visio file (`.vsd`, `.vdx`, `.vsx`); a
> malformed page or master part is a note in the summary and
> `unreadable_target` (path `--page`) when read; a shape whose master
> is missing shows its own cells with a note.

## §11. Versioning

**Replace**

> the binary: `pptx`, `docx` or `xlsx`).

**with**

> the binary: `pptx`, `docx`, `xlsx` or `vsdx`).

## §12. What is not in the contract

**Add after** "MCP, WASM and language bindings. Agents run the binaries."

> - **vsdx** in v1 (deferred, not refused by name unless listed in §3):
>   writing hyperlinks, comments (not read either), layers, assigning a
>   page's background page, group create/ungroup, object.duplicate,
>   master import and editing masters; `read --format svg`.

## §13. Implementation notes (not for agents)

**Add after** the last bullet

> - vsdx: `internal/vsdx` packages, `docs/vsdx-design.md`.

## Record: vsdx differs from pptx because (not merged; for the reviewer)

| Difference | Forced by |
| --- | --- |
| `--page pageN` where pptx has `--slide slideN`; `page0` is the stored page ID | Visio page parts are renumbered on save; IDs are stable |
| `--master masterN` (read only) | Visio masters are drawings instances inherit from |
| no `--format svg` | no Visio geometry renderer in v1 |
| `create` takes a plan, not HTML | a drawing is not a flow of paragraphs (as xlsx) |
| `object.add` `geom` and `kind: "connector"` make master-less shapes and connectors; with `master` it instantiates a master (2-D or 1-D) | a new drawing has no masters; pptx has no masters to instantiate |
| whole-shape glue only on connectors from the Dynamic connector master | every Visio-authored whole-shape glue in the corpus is on such an instance; the cells it needs are not cited on a master-less shape |
| `--units pt|in` (pptx `pt|emu`) | Visio's stored unit is the inch |
| y down from the page's top-left (Visio stores y up) | pptx's frame for agents |
| `origins` values `local`, `inherited`, `master`, `style` | Visio's inheritance chain |
| `cells` under `--detail full` | ShapeSheet cells are Visio's property model |
| `props`, `prop.*` ops | Visio Shape Data |
| `connections` on the page; glue `s12` (whole shape) besides `s12:3` | Visio stores glue as page Connect rows and has dynamic glue |
| a glued connector end does not move with object.move | Visio's glue formula owns the end |
| `orientation`, `print_orientation`, `page.set orientation` | Visio pages have their own size and a printer orientation |
| "off" run forms only where they clear an inherited setting | Visio stores Style on most rows |
| `glued` (control-handle glue) | Visio Connect rows from control cells |
| `off-page` hazard | Visio pages resize independently of their shapes |
| `page.*` ops | Visio pages are independent drawings |
| `layers`, `layer:` addresses (read only) | Visio layers |
| `stale-values`, `RecalcDocument` | Visio caches every computed value |
| formula-bound cells refused | ShapeSheet formulas |
| field placeholders `<opaque data-kind="field">` | Visio text fields |
| no `<a href>`, `<ol>`, `data-level` | Visio hyperlinks are shape-level; no list levels |
| not a difference: `find` | the contract's docx/xlsx command, useful on multi-page drawings |

## Merge plan

Fable confirmed (2026-11-17) that this draft's section order matches
its section map; steps 1 to 4 stand.

1. When every CC chunk has merged and `make ci` passes on wt/vsdx-int,
   the coverage matrix has no row without a passing test, and the
   "(CCn)" marks are removed from this draft.
2. One commit on the merge branch applies the entries in the order of
   the sections above to `docs/contract.md` (each Replace's quoted text
   must be found exactly once, or the merge stops), sets the status
   line, and changes this file's status to "merged on <date>", keeping
   it as the record.
3. `docs/plan.md` gets the vsdx line; `help` text is compared with §3's
   op table and refusal list (CC7's test).
4. Fable reviews the merged diff before main moves.
