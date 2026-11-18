# The agent contract (version 1)

`pptx`, `docx`, `xlsx` and `vsdx` are four CLIs with one contract.
This page is the whole of what an agent may rely on; anything not
written here (whitespace, message wording, key order) may change.
Format-specific parts are marked **pptx**, **docx**, **xlsx** or
**vsdx**. `<tool>` is any of the binaries.

Status: signed off by the owner on 2026-11-13; the **xlsx** parts on
2026-11-14 (the owner's decisions and the design approver's rulings for
the owner, recorded in `docs/xlsx-contract-draft.md`). The **vsdx**
parts on 2026-11-17 (Fable for the owner; the amendments and rulings are
recorded in `docs/vsdx-contract-draft.md`). Where the code differs, this
page is the target; `docs/plan.md` says what is implemented.

## 1. Commands

```
<tool> read  <file> [flags]              facts about the document; never writes
<tool> find  <file> <query> [flags]      docx: paragraphs; xlsx: cells; vsdx: shapes and shape data containing text
<tool> apply <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
<tool> create <doc.html|-> [--dry-run] -o <out>     docx: a new document from HTML
<tool> create [<plan.json|->] [--dry-run] -o <out>  xlsx, vsdx: a new workbook or drawing, optionally with a plan applied
<tool> xml   <file> <address>            pptx, vsdx: source bytes of one object
<tool> render <file> ... -o <out.png>    pptx, vsdx: picture of a slide or page
<tool> help [<command>|ops|<op>|html|schema|values]   same text as --help
<tool> --version
```

**xlsx** has `read`, `find`, `apply`, `create`, `help` and `--version`;
there is no `xml` or `render`. Its help topics are `ops`, `<op>` and
`values` (the cell value forms of §7a); it has no `html` topic.

```
xlsx read  <file> [--range A] [--sheet sheet:Name] [--view cells|values|formulas] [--group] [--detail styles] [--kind workbook|names|tables|styles|comments] [--limit n]
xlsx find  <file> <query> [--ignore-case] [--sheet sheet:Name] [--view values|formulas] [--limit n]
xlsx apply <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full] [--recalc]
xlsx create [<plan.json|->] [--dry-run] [--recalc] -o <out>
```

**vsdx** has `read`, `find`, `xml`, `media`, `apply`, `create`, `render`,
`help` and `--version`. Its help topics are `ops`, `<op>` and `html`;
there is no `schema` or `values` topic.

```
vsdx read   <file> [--page pageN | --master masterN] [--detail sites|full] [--ids ids] [--region x,y,w,h] [--units pt|in]
vsdx find   <file> <query> [--ignore-case] [--page pageN] [--limit n]
vsdx xml    <file> <address>
vsdx media  <file> <address> -o <out>
vsdx apply  <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
vsdx create [<plan.json|->] [--dry-run] -o <out>
vsdx render <file> --page pageN -o <out.png> [--overlay ids,sites,glue] [--ids ids] [--scale n]
```

- Files: `.vsdx`, `.vsdm` (VBA copied through), `.vstx` and `.vstm`
  are read and edited; `.vssx` and `.vssm` (stencils) are read only:
  their masters, and `pages` is `[]`.
- `read` has no `--format svg`: there is no Visio geometry renderer.
  `render` rasterises one page through headless LibreOffice, with
  pptx render's flags. Its result is `{"saved", "px", "size",
  "scale", "hidden", "key"}`: the PNG written, its `[w, h]` in
  pixels, the page's `[w, h]` in points, the pixels per point, whether
  the page is hidden, and the numbered marks of `--overlay`
  (`[{"n", "id"}]`). LibreOffice draws the values the file caches, so
  the picture is for checking, not proof. `soffice` comes from
  `DD_SOFFICE` or `PATH` (with a PDF rasteriser: `pdftoppm`, `mutool`
  or `gs`); without them `render` is `operation_failed`, path `-o`,
  and nothing is written, as xlsx `--recalc` is at `--recalc`.
- `--page` and `--master` take an address as `read` lists it; any
  other value is `usage` at the flag, exit 2; a page address the
  drawing lacks is `not_found` at the flag.
- `media` writes the bytes of one image to the file named by `-o`
  (required) and prints `{"base","saved","image","type","bytes","sha256","px"}`.
  `<address>` is a picture (`page0/s5`) or a media part as `read`
  shows it in `image` (`/visio/media/image1.png`); anything else is
  `not_found`. It is the only way the tool gives out image bytes:
  `read` never inlines them. It writes only `-o` (§5).

- Flags are `--name value` or `--name=value`, anywhere after the command;
  `--` ends flags. Boolean flags take no value. There are no short flags
  except `-o` (`--out`) and `-h` (`--help`).
- Shared flags: `--pretty` (indent JSON), `--help`.
- A flag or command the tool does not know is a `usage` error; nothing is
  guessed.

## 2. Output and exit codes

| Exit | Meaning | stdout | stderr |
| --- | --- | --- | --- |
| 0 | done | the result | empty |
| 1 | refused or failed; nothing was written | empty | one errors object |
| 2 | the command line is wrong | empty | one errors object, code `usage` |

- stdout holds exactly one JSON document, UTF-8, ending in a newline.
  Default layout is compact; `--pretty` indents. Whitespace is not part
  of the contract.
- stderr holds the errors object and nothing else: no warnings, progress
  or logs, on success or failure. Things worth knowing that are not
  errors are in the result (`hazards`).
- The exceptions are outputs that are not JSON by request: `pptx read
  --format svg`, `pptx xml`, and `--help` text.
- The result object is the top-level value; there is no `outcome` wrapper.
  Success and failure are told apart by the exit code and the stream.

### Errors

```json
{"errors":[{"code":"invalid_operation","path":"ops[2].format.width","message":"width must be positive","hint":"…"}]}
```

`errors` always has at least one entry.

| Field | Meaning |
| --- | --- |
| `code` | one of the codes below; stable; switch on this |
| `path` | what is wrong: a plan path (`ops[2].at`, `author`), or a flag (`--view`). Omitted when nothing narrower than the whole input applies |
| `message` | one sentence for a reader; wording may change |
| `hint` | optional: the next step that usually fixes it |
| `span` | optional, plan text errors: `{"line","column","end_line","end_column"}`, 1-based, columns in characters |
| `matches` | `ambiguous` only: how many matches there were |
| `detail` | where the code says so: what would be introduced (`invalid_output`), what is wrong with the target (`unreadable_target`), what was left out (`operation_failed` for omitted content) |

Plan paths use `.` for fields and `[n]` for 0-based list indexes, rooted
at the plan object. Inside an HTML field, `@name` names an attribute and
`<name>` an element: `ops[1].html@data-foo`.

**How many errors.** Everything that can be checked without the document
(JSON, shape, every op's rules, HTML) is checked for the whole plan, and
all those errors come back together, in plan order. Once the document is
open the tool fails fast: the first op that does not apply is the only
error.

| Code | When | Needs the document | Exit |
| --- | --- | --- | --- |
| `usage` | unknown command or flag, missing argument, bad flag value | no | 2 |
| `io` | a file cannot be read or written | – | 1 |
| `invalid_json` | the plan is not JSON, or has a duplicate key | no | 1 |
| `invalid_plan` | the plan's shape is wrong: unknown or missing field, wrong type, unknown op, empty `ops` (path `ops`), unsupported `contract` | no | 1 |
| `invalid_operation` | an op's values break a rule (`path` names the field), including HTML the format cannot store | no | 1 |
| `invalid_document` | the container is unreadable (not a zip, no main part) or a safety limit trips (§10). Never for a defect in a part the command did not need | yes | 1 |
| `stale_base` | the plan's `base` is not this document's hash | yes | 1 |
| `not_found` | an address, alias or `select` text is not in the document | yes | 1 |
| `ambiguous` | `select` matches more than once and the op has neither `occurrence` nor `all`; `matches` gives the count | yes | 1 |
| `refused_in_mode` | docx: the op cannot be recorded as a tracked change; use `change_mode: direct` | yes | 1 |
| `unreadable_target` | the op's own target is too broken to edit safely (§10). The only refusal a pre-existing defect can cause | yes | 1 |
| `operation_failed` | any other reason an op does not apply to this document, including an op this version does not implement yet (`path` names the op) | yes | 1 |
| `invalid_output` | this plan would introduce a nonconformity; `path` names the op, `detail` what it would introduce (§10) | yes | 1 |
| `internal` | a bug in the tool | – | 1 |

For document errors `path` is `ops[n]`, or the field when one field is
at fault (`ops[n].at`, `ops[n].select`).

## 3. Plans

A plan is one JSON object: plan-wide settings, then ordered operations.

```json
{"ops":[{"op":"object.move","at":"slide3/s17","dx":10,"dy":0}]}
{"author":"Ada","change_mode":"track","ops":[{"op":"text.replace","at":"0A1B2C3D","select":"teh","with":"the"}]}
```

| Field | Tool | Required | Meaning |
| --- | --- | --- | --- |
| `ops` | both | yes | the operations, applied in order; at least one |
| `base` | both | no | hash of the document the plan was written for (§5) |
| `contract` | both | no | contract version the plan is written to (§11) |
| `units` | pptx | no | `pt` (default) or `emu` for lengths |
| `units` | vsdx | no | `pt` (default) or `in` for lengths |
| `change_mode` | docx | yes | `track` (record revisions) or `direct` |
| `author` | docx | when `change_mode` is `track` or the plan has a `comment.*` op | name on tracked changes and comments; not empty |
| (none) | xlsx | – | xlsx plans have only `ops`, `base` and `contract` |

Rules for both tools:

- Plan-wide settings are top-level keys; everything about one operation
  is inside it. Format-specific settings sit at the top level beside the
  shared ones, never in a nested block. There is no plan-level target:
  every op names what it acts on in `at` (pptx has no plan-level `slide`).
- Unknown keys are errors at every level. `null` is not a value: leave a
  field out (one exception: a cell of xlsx `range.set` `values`, §7a).
  Duplicate keys are errors. Numbers are plain JSON numbers.
- All or nothing: if any op fails, no file changes.
- A plan that changes nothing writes nothing.
- Values take the form `read` shows them in.
- Ops apply in order, each to the document as the ops before it left it.

### Operation names

`<noun>.<verb>`, lower case; a part with several words is snake_case.
The noun is the kind of thing `at` addresses. The same name means the
same thing in both tools.

Verbs, same meaning everywhere:

| Verb | Meaning |
| --- | --- |
| `add` | create a thing that has no position in a sequence (object, comment, bookmark) |
| `insert` | create a thing at a position in a sequence (`at` + `position`) |
| `delete` | remove the thing |
| `replace` | swap the addressed content for new content |
| `set` | replace the whole value of the thing; `set_<property>` sets that one property |
| `format` | change formatting properties, leaving content alone |
| `move`, `rename`, `duplicate`, `reorder`, `merge`, `split` | as named |
| `accept`, `reject` | docx: settle revisions |
| `clear` | empty the addressed content, keeping its formatting (xlsx `range.clear`) |

**pptx**: `object.add`, `object.set`, `object.move`, `object.delete`,
`object.duplicate`, `object.reorder`, `text.set`, `text.replace`,
`group.create`, `group.ungroup`, `group.fit`.

pptx text body insets (`text.insets` on `object.set` and `object.add`,
and in `read --detail full`) are in the plan's units and in the order
**[left, top, right, bottom] — not CSS order** (DrawingML `lIns`,
`tIns`, `rIns`, `bIns`). `read` always shows the array. A plan gives
either that array (all four) or the named form
`{"left", "top", "right", "bottom"}` with any non-empty subset; sides
left out keep the values shown, so `{"bottom": 4}` changes only the
bottom inset. Any other key, an empty object or another JSON kind is
`invalid_plan`. Writing back the array `read` shows is a no-op.

**docx**:

| Noun | Operations |
| --- | --- |
| `text` | `text.replace`, `text.revise`, `text.insert`, `text.format` |
| `paragraph` | `paragraph.insert`, `paragraph.replace`, `paragraph.delete`, `paragraph.format` |
| `list` | `list.set` |
| `equation` | `equation.replace`, `equation.delete` |
| `table` | `table.insert`, `table.delete`, `table.format` |
| `row`, `column`, `cell` | `row.insert`, `row.delete`, `row.format`, `column.insert`, `column.delete`, `cell.merge`, `cell.split`, `cell.format` |
| `section` | `section.set_margins`, `section.set_layout`, `section.set_boundary`, `section.set_first_page` |
| `document` | `document.set_even_odd_headers` |
| `header`, `footer` | `header.set`, `header.delete`, `footer.set`, `footer.delete` |
| `comment` | `comment.add`, `comment.reply`, `comment.set_status`, `comment.delete` |
| `bookmark` | `bookmark.add`, `bookmark.rename`, `bookmark.move`, `bookmark.delete` |
| `revision` | `revision.accept`, `revision.reject`, `revision.convert_to_formatting` |

**xlsx**:

| Noun | Operations |
| --- | --- |
| `cell` | `cell.set`, `cell.merge`, `cell.split` |
| `range` | `range.set`, `range.clear`, `range.format` |
| `text` | `text.replace` |
| `row`, `column` | `row.insert`, `row.delete`, `row.format`, `column.insert`, `column.delete`, `column.format` |
| `sheet` | `sheet.add`, `sheet.rename` |
| `name` | `name.add`, `name.set`, `name.delete` |

| Op | Fields |
| --- | --- |
| `cell.set` | `at` (a cell), `value` |
| `range.set` | `at` (a range, or its top-left cell), `values` (rows of values, all the same length; `null` leaves the cell empty, `""` writes an empty string) |
| `range.clear` | `at` (a range): empties content, keeps formatting |
| `text.replace` | `at` (a range or a sheet), `select`, `with`, `occurrence` or `all` |
| `row.insert`, `column.insert` | `at` (one row `Budget!5:5`, one column `Budget!C:C`), `position` (`before`, `after`), `count` (default 1) |
| `row.delete`, `column.delete` | `at` (rows or columns) |
| `row.format`, `column.format` | `at`, `height` (rows) or `width` (columns), `hidden` |
| `range.format` | `at`, `number_format`, `bold`, `italic`, `underline` (single, a boolean), `color` and `fill` (`RRGGBB`), `align` (`general`, `left`, `center`, `right`, `fill`, `justify`, `centerContinuous`, `distributed`), `valign` (`top`, `center`, `bottom`, `justify`, `distributed`), `wrap`; at least one |
| `cell.merge`, `cell.split` | `at` (the range; for `cell.split`, the merged range) |
| `sheet.add` | `name`, `as`; the sheet is appended |
| `sheet.rename` | `at` (a sheet), `name` |
| `name.add` | `name`, `refers_to` (`=` and a formula), `scope` (`workbook`, default, or `sheet:Budget`) |
| `name.set` | `at` (a name), `refers_to` |
| `name.delete` | `at` (a name) |

A structural op (`row.*`, `column.*` insert and delete, `sheet.rename`)
adjusts the references that point at what it moved, from a closed list
(`help <op>` gives it): cell formulas on every sheet, defined names,
merges, and the other references the help lists. A shift that would
cut a construct the tool cannot adjust (a pivot table, a multi-cell
array formula, a data table, a table crossed by a column shift, an
extension list reference at or after the shift, or a reference to the
sheet from another workbook) is refused with `operation_failed`, and
`detail` names the construct. Anything not on either list is never
touched.

**vsdx**:

| Noun | Operations |
| --- | --- |
| `object` | `object.add`, `object.set`, `object.move`, `object.delete`, `object.reorder` |
| `text` | `text.set`, `text.replace` |
| `prop` | `prop.add`, `prop.set`, `prop.delete` |
| `page` | `page.add`, `page.rename`, `page.delete`, `page.reorder`, `page.set` |

Layers are read only: `read` lists them and an object's `layers`;
no op writes them.

| Op | Fields |
| --- | --- |
| `object.add` (vsdx) | `at` (a page or group), `as`, `name`, `fill`, `line`, `html`, and one of: `master` (an address, `master2`, or the master's name as `read` shows it) with `box` or `pin` (`[x, y]`, the centre) for a 2-D master, or `from`/`to`/`from_glue`/`to_glue` for a 1-D master such as Dynamic connector; `geom` (`rect`, `ellipse`) with `box` or `pin`: a master-less shape with four connection points, 1 top, 2 right, 3 bottom, 4 left; `kind: "connector"` with `from`, `to`, `from_glue`, `to_glue`: a master-less straight connector, point glue only (whole-shape glue needs the Dynamic connector master); `kind: "picture"` with `image` (a file path, below) and `box` or `pin`, and optionally `crop` and `name`: a picture, with no `fill`, `line`, `html`, `shadow` or connector fields (`invalid_operation` at the field). `shadow` is accepted with `master` and `geom`. No `rot`, `flip_h`, `flip_v` (set them with object.set). `pin` is input only (`read` shows `box`): with `pin` and no `box`, a `geom` shape is 72 x 72 pt, and a master instance takes the master's size: it stores only its pin and inherits the rest, which Visio accepting is **Visio unconfirmed (VE1)**. `master` by name matches `name` or `name_u` as `read` shows them; an address (`master2`) is taken as an address first, and when several masters share a name the first in stored order is used |
| `object.set` (vsdx) | `at`, and at least one of `box`, `rot`, `flip_h`, `flip_v`, `name`, `fill` (`"none"` or `{"hex":"#RRGGBB"}`), `line` (`"none"` or `{w, color, dash}`; `color` is `{"hex":"#RRGGBB"}` as `read` shows it, or the bare string `"#RRGGBB"`; `dash` the LinePattern number, as a number or the string `read` shows; and on a connector `begin` and `end`, below), `shadow` (`"none"` or `{color, transparency, offset}`, below), and for a connector `from`, `to`, `from_glue`, `to_glue`; for a picture `image` and `crop`. No `html` (use text.set) |
| `object.move` (vsdx) | `at`, `dx`, `dy` (each defaults to 0) |
| `object.delete` (vsdx) | `at`. Connectors glued to the deleted shape lose that glue (their Connect rows and glue formulas are removed) and keep their end where it was; they appear in `changed` |
| `object.reorder` (vsdx) | `at`, `to` (`front`, `back`, or an object of the same parent with `position`) |
| `text.set` (vsdx) | `at` (a shape), `html`, `delete` (field addresses, §7) |
| `text.replace` (vsdx) | `at` (a shape or a page), `select`, `with`, `occurrence` or `all` |
| `prop.add` | `at` (a shape), `name` (the row name: ASCII letters, digits and `_`, not starting with a digit; anything else is `invalid_operation` at `name` before the document opens; rows a file already has with other names are read, set and deleted by their address), `type` (`string`, `number`, `boolean`, `date`, `duration`, `currency`, `fixed_list`, `variable_list`), `label`, `value`, `format`. The stored type codes and value forms for `boolean`, `date`, `duration` and `currency` are **Visio unconfirmed (VE9)** |
| `prop.set` | `at` (`page0/s5/prop:Cost`), `value` |
| `prop.delete` | `at` (a row) |
| `page.add` | `name`, `as`, `size`, `orientation`, `background` (`true`: the new page is a background page), `background_page` (a background page's address: the new page uses it), and `at` (another page) with `position` (`before`, `after`): the two go together; with neither, a foreground page is added after the last foreground page and a background page at the end. A new page is 8.5 x 11 in portrait unless `size` or `orientation` says otherwise |
| `page.rename` | `at`, `name` |
| `page.delete` | `at` |
| `page.reorder` | `at`, `to` (another page), `position` |
| `page.set` | `at`, and at least one of `orientation` (`portrait`, `landscape`), `size` (`[w, h]`), `background` (`true`, `false`), `background_page` (a background page's address, or `"none"`) |

**vsdx** rules:

- **Glue.** `from_glue`/`to_glue` is `"s12:3"` (connection point 3 of
  object 12), `"s12"` (the whole shape: Visio's dynamic glue, which
  needs only the target's pin, so any shape can be glued to), an
  alias with or without a point (`"$box:2"`), or `"none"`. Point glue
  works on every connector. Whole-shape glue works on a connector
  from a master that stores Visio's dynamic glue cells (Dynamic
  connector); on a master-less connector it is refused
  (`operation_failed` at the glue field, hint: add the connector from
  the Dynamic connector master), because no Visio-authored file
  shows those cells on a master-less shape. Whole-shape glue is
  written in the form Visio stores; that Visio re-walks it when a
  glued shape moves, and reroutes a dynamic connector whose ends
  changed, is **Visio unconfirmed (VE4, VE13)**.
  `from` with `from_glue` is accepted, as pptx: the end is written at
  `from` and glued; an end off its site is reported as
  `stale-connector` and `stale-values` (Visio moves it to the site
  when it recalculates).
- **Connectors** Visio drew are edited when their transform cells are
  constants, inherited, or Visio's standard 1-D forms (`GUARD(...)`,
  `SQRT`, `ATAN2` of the ends), and their ends are constants or glue. A
  free end is placed where asked and the connector's width, pin and
  stored geometry (a straight line) follow it; a dynamic connector's
  route is left for Visio to redraw (Visio unconfirmed, VE4).
- **object.move** moves a 2-D object, or a connector's free ends; a
  glued end keeps its glue and position. A connector glued at both
  ends is refused (set one end's glue to `none` first).
- **Moving a glue target** (object.move or box on a shape a connector
  is glued to) leaves the glued ends where they are: the result
  reports `stale-connector` and `stale-values`, and Visio moves the
  ends when it recalculates. Glue given without `from`/`to` places the
  end on the site. Glue equal to what `read` shows writes nothing when
  the end already stores Visio's glue formulas; an end glued only by a
  Connect row gets the formulas written and is placed on the site. To
  put a stale end back on its site, send `to` (or `from`) with the
  glue, or set the glue to `none` and then to the site again.
  vsdx does not move point-glued ends itself (§12).
- **Groups.** box or object.move on a group carries its children: a
  resize changes only the group's frame, and the children keep their
  stored local positions and sizes, which are measured from the
  group's lower-left corner, so their shown `y` changes. `changed`
  lists the group and then every descendant whose shown values
  changed.
- **Line ends.** `line.begin` and `line.end` are the arrowheads at a
  connector's `from` and `to` ends (Visio's Begin and End). Each is
  `"none"` or `{"arrow", "size"}`:
  - `arrow` is a name from the closed table, or the stored index as a
    string for an index the table does not name:

    | Name | Stored index | Grounding |
    | --- | --- | --- |
    | `none` | 0 | Visio-authored files |
    | `triangle` | 4 | drawn as a filled triangle by LibreOffice; stored by non-Visio writers only: **Visio unconfirmed (VE15)** |
    | `stealth` | 5 | stored by Visio; drawn as a notched filled arrow by LibreOffice: appearance **Visio unconfirmed (VE15)** |

    A plan may write the three names. An index string (`"13"`) is
    accepted only where it equals what `read` shows for that end (a
    no-op); any other index is `operation_failed` at the field, hint:
    the names. Names are added to the table (additive, §11) as each
    index is confirmed.
  - `size` is the stored size number, 0 to 6 (the corpus shows 0 to
    3; that Visio accepts 4 to 6 is **Visio unconfirmed, VE15**).
    Left out, the effective size is kept and nothing is written.
  - On input a bare name (`"end": "triangle"`) is shorthand for
    `{"arrow": "triangle"}`; `read` always shows the object.
  - A value equal to what `read` shows writes nothing; a different
    one is stored on the shape (`BeginArrow`, `EndArrow`,
    `BeginArrowSize`, `EndArrowSize`). The Formulas rule applies: an
    end whose arrow cell is bound to a formula vsdx does not evaluate
    (a master's `GUARD(0)`) is refused. A themed arrow may be
    overridden.
  - They apply to connectors only (any 1-D shape, with or without a
    master, the Dynamic connector included). On any other kind
    `begin` or `end` is `operation_failed` at the field: Visio draws
    line ends on open paths, and vsdx does not derive whether a 2-D
    shape's path is open.
  - Arrow cells may be written while `line` is `"none"`; nothing is
    drawn until the line has a pattern, and `read` shows no `begin` or
    `end` on a line that is `"none"`.
  - A connector added with `kind: "connector"` has no arrowheads
    unless the plan gives them.
- **Shadows.** `shadow` on object.add and object.set is `"none"` or
  `{"color", "transparency", "offset"}`: `color` as `line.color`
  (`{"hex":"#RRGGBB"}` or the bare string); `transparency` a number
  from 0 (opaque) to 100, in percent; `offset` `[dx, dy]` in points
  (or the plan's units), y down, so `[2, 2]` is a shadow below and to
  the right.
  - `"none"` stores `ShdwPattern` 0 on the object. An object value
    stores `ShdwPattern` 1, `ShdwForegnd`, `ShdwForegndTrans` (the
    percent as a fraction), `ShapeShdwType` 1 and
    `ShapeShdwOffsetX`/`ShapeShdwOffsetY` (inches, y up), the cells
    and values of the corpus's shadowed shapes. Only cells whose
    shown value differs are written.
  - On an object that shows no shadow, all three members are
    required: vsdx supplies no default colour or offset. A missing
    one is `operation_failed` at `shadow`, naming it (it depends on
    the document, so it is not a plan-shape error). On an object
    that shows one, members left out keep their value.
  - Blur, the oblique and scaled shadow types and the page's default
    shadow (`ShdwOffsetX`, `ShdwOffsetY`, `ShdwType` on the page
    sheet) are not written; `ShapeShdwShow` is never touched.
  - Applies to kinds `shape` and `group`. On a connector it is
    `operation_failed` at `shadow`; on a picture or kind `other` it is
    `operation_failed` at `at`, as fill and line are.
  - The Formulas rule applies (a master's `GUARD(0)` or
    `THEMEGUARD(…)` on `ShdwPattern` refuses); a themed value may be
    overridden.
  - That Visio shows a shadow for these cells alone, that
    `ShapeShdwType` 1 makes it use the shape's own offsets, and that
    `ShapeShdwShow` does not hide it, are **Visio unconfirmed
    (VE17)**.
- **Pictures.** `image` names a local image file: a path, absolute
  or relative to the working directory (never to the plan file, also
  when the plan is `-`), read as the operating system resolves it
  (symbolic links are followed), exactly as pptx `image`. It is the
  caller's path: confining it is the caller's job (§5). A value that
  starts with `/` and names a media part of the drawing as `read`
  shows it (`/visio/media/image1.png`) is that part; a value naming
  both a part and an existing file is refused (`invalid_operation`),
  as pptx.
  - The files a plan names are read and checked with the rest of the plan,
    before the document opens, and every fault comes back together with
    the plan's other faults, at `ops[n].image`: a file that is missing, unreadable or
    not a regular file is `io`; a file over 16 MiB (checked from its
    size, before it is read), a format other than PNG or JPEG (from
    the bytes' signature, never the name), a header that does not
    decode, or a pixel width or height over 16,384 is
    `invalid_operation`. A plan's files together may hold at most
    64 MiB. `--dry-run` reads and checks them too. (pptx reads its
    file when the op runs and has no size limit; the limits are new
    here.)
  - `object.add` `kind: "picture"` needs `image` and `box` or `pin`.
    With `pin` the picture is its pixel size at 96 pixels per inch
    (0.75 pt per pixel). `box` is taken as given: the aspect ratio is
    the plan's. The picture takes
    the drawing's "None" style sheet (LineStyle, FillStyle and TextStyle,
    as the Visio-authored samples store them), so it reads `fill` and
    `line` "none"; a drawing with no "None" sheet (one whose
    LinePattern and FillPattern are 0) gets LinePattern 0 and FillPattern
    0 on the picture itself. An added picture has no connection points.
  - `object.set` `image` replaces the picture's image and keeps its
    box, rotation, flips, crop and every other cell. An `image` equal
    to the part shown, or a file whose bytes equal that part's, writes
    nothing.
  - `crop` is `[l, t, r, b]`, the part of the image cut off at each
    side in percent of the image, as pptx: each from 0 to under 100,
    `l + r` and `t + b` under 100. It is stored in the picture's
    `ImgOffsetX`, `ImgOffsetY`, `ImgWidth` and `ImgHeight` cells as
    formulas of the corpus's form (`Width*k`, `ImgWidth*k`), so a
    later resize keeps the crop: `ImgWidth = Width*100/(100-l-r)`,
    `ImgOffsetX = ImgWidth*(-l/100)`, `ImgHeight =
    Height*100/(100-t-b)`, `ImgOffsetY = ImgHeight*(-b/100)`.
    `[0, 0, 0, 0]` is no crop (`Width*1`, `ImgWidth*0`). That Visio
    crops by these cells in this sense is **Visio unconfirmed
    (VE18)**. There is no separate scale: the box is the scale.
  - Media: bytes whose SHA-256 equals a part under `/visio/media/`
    with the same content type reuse that part; otherwise a new part
    `/visio/media/imageN.<ext>`, N one more than the largest `imageN`
    there or targeted by any relationship of the package (numbered
    across extensions). The page part gets one relationship of the
    image type to the part, reused by every picture of that image on
    the page, with a relative target (`../media/image3.png`). The
    content-type `Default` for the extension is added when the
    drawing has none (an `Override` only when a `Default` gives
    another type). The written form is the Visio-authored JPEG
    picture's (`ForeignData ForeignType='Bitmap' CompressionType='JPEG'`,
    media `imageN.jpeg`, `<Default Extension="jpeg"
    ContentType="image/jpeg"/>`); PNG takes the same form with
    `CompressionType='PNG'`, `imageN.png` and `image/png` (the owner's
    ruling of 2026-11-18 grounds the writes in MS-VSDX and that sample).
  - A replaced or deleted picture (the object, or any picture in a
    deleted group) releases its media: the page's relationship is
    removed when no shape of the page uses it, and the part when no
    relationship in the package targets it. `parts` lists them.
  - A picture that a master instance inherits (a picture with
    `MasterShape` inside an instance) is refused for `image` and
    `crop`. EMF, SVG, GIF
    and other formats are read (kind `picture`) and not written.
  - That Visio opens a drawing with a picture vsdx added is covered by
  §9's label; that Visio accepts the stored form is **Visio unconfirmed
  (VE18)**. Every picture write is checked against the MS-VSDX schema
  and imported and drawn by LibreOffice in the tests.
- **Background pages.** A page is a foreground page or a background
  page (`background`); a page of either kind may use one background
  page (`background_page`).
  - `page.set` `background_page` with a page address makes `at` use
    that page; `"none"` removes the use. `page.set` `background`
    `true` makes `at` a background page, `false` a foreground page.
    `page.add` takes both for the new page. A value equal to what
    `read` shows writes nothing.
  - When `background` changes the page's kind, the page moves in the
    stored order so that foreground pages stay before background
    pages: a page made a background goes to the end, a page made
    foreground goes after the last foreground page. `page.add` with
    `at` and `page.reorder` are refused when they would put a
    foreground page after a background page or a background page
    before a foreground page. A drawing that already mixes them is
    read and edited as it is. That Visio requires this order is
    **Visio unconfirmed (VE16)**; the rule is the cautious side.
  - Refused, `operation_failed` at the field: `background_page`
    naming a page that is not a background page (a foreground page);
    naming `at` itself; a chain of background pages that would come
    back to `at` (a cycle); `background: false` on a page another
    page uses as its background (set those to `"none"` first);
    `background: true` on the last foreground page;
    `background_page` naming a master or a page the drawing lacks
    (`not_found`). A plan may make a page a background and use it in
    one plan: ops apply in order.
  - In a plan, `page.set` with `background: true` and
    `background_page` on the same op is applied kind first.
  - `changed` lists each page whose summary entry changed; the pages
    that use a page are not changed when it is edited.
  - The stored form is the `Page` element's `Background` and
    `BackPage` attributes (the schema's; `BackPage` holds the
    background page's ID). That Visio reads them as vsdx writes them,
    and draws a background behind a page of a different size or
    orientation as it is, is **Visio unconfirmed (VE16)**: no
    Visio-authored sample is in the corpus.
- **Formulas.** A write to a cell whose formula vsdx does not
  evaluate is refused, `operation_failed`, `path` the field written
  (`ops[n].at` for object.move), `detail` each refused cell as
  `Width F=User.DefaultWidth`. Cells that inherit an evaluable
  formula from the master and depend on a written cell are
  stored with the master's formula reference (`F="Inh"`) and the new
  value, so a resized instance reads back the box asked for.
- **A value equal to what `read` shows writes nothing**, for object
  values, shape data, page values and text formats alike, whether the
  shown value is stored on the object or inherited (master, then style
  sheets): `read`'s output sent back is a no-op (one exception: glue on
  an end that has only a Connect row, see "Moving a glue target"). A different value is
  stored on the object, and its origin becomes `local`; that a local
  value with no formula overrides a formula the object inherits is
  **Visio unconfirmed (VE8)**. A text format
  equal to the inherited one is never stored, and a stored one equal to
  it is removed when the text is set (decision f of
  `docs/vsdx-design.md`). Pinning an inherited value locally (storing it
  unchanged so the object stops following its master) is deferred.
- **Value forms** are `read`'s: string, number and currency numbers,
  boolean, date `{"date":"YYYY-MM-DDThh:mm:ss"}` (anything else in it
  is `invalid_operation` at `value`), duration the stored text
  (Visio's elapsed-time number as written, unit not converted),
  `fixed_list` and `variable_list` the chosen item's text, with the
  choices in `format`, `;`-separated.
- **Aliases** extend: `$r/prop:Cost`, `$r/field:1`, `$p/s5` name what
  the address `$r` or `$p` got followed by that suffix, so a plan can
  add a shape and set its data atomically.
- **page.set orientation** is the page's shape (from its size): a
  different one swaps PageWidth and PageHeight, the same one leaves
  the size; the page sheet then stores `PrintPageOrientation` equal
  to it when the effective print orientation is absent or differs
  (never in a style sheet). Stored positions do not change, so every
  shown `y` moves by the change in height; `changed` lists the page
  and its top-level objects; an object now wholly off the page is an
  `off-page` hazard. That Visio keeps the stored positions when the
  orientation flips is **Visio unconfirmed (VE10)**. In page.add and
  page.set, a `size` that contradicts `orientation` is
  `invalid_operation` at `orientation`; a square size takes no
  `orientation`.
- **text.replace at a page** skips containers, lists and callouts (use
  `find` to see their matches); when its only matches are in skipped
  shapes it is `not_found` at `select`. A match that crosses a
  paragraph or line break, or touches a field, is refused.
- **Refused** (`operation_failed`, `path` the op or field, unless
  given): a formula-bound cell (above); a container, list or callout
  (`User.msvStructureType` `Container`, `List`, `Callout`) for every
  write, and a member of one (a `Relationships` cell with
  `DEPENDSON`) for box, rotation, flips, move and delete;
  geometry on a page with a drawing scale (box, rot, flips, ends,
  move, object.add, page.set size); a data-linked shape data
  row for prop.set and prop.delete; any write to a master, a master
  shape or a stencil; text, fill and shadow on a picture or other
    read-only kind (a picture takes box, rot, flips, name, move,
    reorder, delete, `image` and `crop`); glue to a cell other than a shape's pin or a
  connection point (`invalid_operation` at the glue field); deleting
  the last foreground page; deleting a page another page uses as its
  background; a duplicate page name (page.add, page.rename); a
  duplicate shape data row (prop.add); reorder relative to an object
  of another parent; a connector whose ends coincide; `from`/`to` or
  glue on a 2-D shape; move of a connector glued at both ends;
  whole-shape glue on a master-less connector; deleting a
  sub-shape a master instance inherits (a shape with `MasterShape`
  inside an instance), `ops[n].at`; `page.set` when PageWidth
    or PageHeight is bound to a formula; `begin` or `end` on an object
    that is not a connector, and an arrow index the table does not
    name; `shadow` on a connector; the background refusals above;
    `image` or `crop` on an object that is not a picture, and on a
    picture a master instance inherits; box, rot or flips on a
  connector; box or pin on a 1-D master, and from, to or glue on a
  2-D master (object.add); reorder relative to itself; line on a
  picture; a text.replace match that crosses a paragraph or line
  break or touches a field. The `refuses:` list of `help <op>` is
  the closed list for that op.

`text.insert` is new (an insertion point, §6); it needs its own laws and
a Word check before it ships. The other docx ops are docxdriver 0.5.2's
34, renamed. The table is the mapping `tools/oracle` and the op table
key on: each contract op, the docxdriver op it runs as, and the fields
that are fixed or moved.

| Contract op | docxdriver op | Fixed or moved fields |
| --- | --- | --- |
| `text.replace` | `replace_text` | `all` has no docxdriver form: one op per match |
| `text.revise` | `revise_text` | |
| `text.insert` | – | none (new) |
| `text.format` | `format_text` | |
| `paragraph.insert` | `insert_paragraph` | `html` ← `with` |
| `paragraph.replace` | `replace_paragraph` | `html` ← `with` |
| `paragraph.delete` | `delete_paragraphs` | |
| `paragraph.format` | `format_paragraph` | |
| `list.set` | `set_list` | |
| `equation.replace` | `replace_equation` | `at` `P/equation:n` ← `at` + `equation`; `latex` (docxdriver: `mathml`) |
| `equation.delete` | `delete_equation` | `at` `P/equation:n` ← `at` + `equation` |
| `table.insert` | `insert_table` | `cells` hold `html` |
| `table.delete` | `edit_table` | `action.kind: delete` |
| `table.format` | `format_table` | |
| `row.insert` | `edit_table` | `action.kind: insert_rows`; `at` `table:t/row:r` ← `at` + `action.row` |
| `row.delete` | `edit_table` | `action.kind: delete_rows`; `at` ← `at` + `action.row` |
| `row.format` | `format_row` | |
| `column.insert` | `edit_table` | `action.kind: insert_columns`; `at` `table:t/column:c` ← `at` + `action.column` |
| `column.delete` | `edit_table` | `action.kind: delete_columns`; `at` ← `at` + `action.column` |
| `cell.merge` | `edit_table` | `action.kind: merge_cells`; `at` `table:t/row:r/cell:c` ← `at` + `action.row`, `action.column` (a grid column: the cell is the one at it) |
| `cell.split` | `edit_table` | `action.kind: split_cell`; `at` ← `at` + `action.row`, `action.column` (a grid column) |
| `cell.format` | `format_cell` | |
| `section.set_margins` | `set_page_margins` | `at` `section:n` ← `section` |
| `section.set_layout` | `set_section_layout` | `at` ← `section` or `section_handle` |
| `section.set_boundary` | `set_section_boundary` | |
| `section.set_first_page` | `set_first_page_mode` | `at` ← `section` |
| `document.set_even_odd_headers` | `set_even_and_odd_headers` | |
| `header.set` | `set_header` | `at` ← `section`; `html` ← `with` |
| `header.delete` | `clear_header` | `at` ← `section` |
| `footer.set` | `set_footer` | `at` ← `section`; `html` ← `with` |
| `footer.delete` | `clear_footer` | `at` ← `section` |
| `comment.add` | `comment_add` | |
| `comment.reply` | `comment_reply` | `at` `comment:n` ← `comment_id` |
| `comment.set_status` | `comment_set_status` | `at` ← `comment_id` |
| `comment.delete` | `comment_delete` | `at` ← `comment_id` |
| `bookmark.add` | `bookmark_add` | |
| `bookmark.rename` | `bookmark_rename` | `at` `bookmark:Name` ← `name`; `name` ← `new_name` |
| `bookmark.move` | `bookmark_move` | `at` ← `name`; `to` ← `at` |
| `bookmark.delete` | `bookmark_delete` | `at` ← `name` |
| `revision.accept` | `revision_settle` | `action: accept`; `at` / `all` / `author` ← `target` |
| `revision.reject` | `revision_settle` | `action: reject`; `at` / `all` / `author` ← `target` |
| `revision.convert_to_formatting` | `convert_revisions_to_formatting` | `at` / `all` / `author` ← `target`, `at` |

The `delete` field of `replace` and `set` ops (§7) has no docxdriver
form: docxdriver deletes what the replacement leaves out.

`<tool> help ops` lists every op with its fields; `<tool> help <op>`
gives one op's fields, types, defaults and rules. Both are generated from
the op table the tool decodes and validates with, so they cannot drift.
**docx** `help schema` prints the plan JSON Schema (draft-07), generated
from the same table.

### Field names

- Keys are snake_case, in plans and in output. A key that mirrors an
  OOXML name is that name converted mechanically, so the OOXML name can
  be guessed back when reading `pptx xml`: put `_` before each upper-case
  letter that follows a lower-case letter or digit, and before the last
  letter of an upper-case run that is followed by a lower-case letter;
  then lower-case everything. `flipH` → `flip_h`, `txBox` → `tx_box`,
  `paraId` → `para_id`, `lnSpcReduction` → `ln_spc_reduction`,
  `forceAA` → `force_aa`.
- Enumerated *values* that are OOXML tokens are spelled as in OOXML
  (`roundRect`, `darkBlue`).
- A key that is the name of something stored in the document is data and
  stays as stored: a geometry guide (`adj1`), a colour modifier in a
  `mods` list (`lumMod`), a theme colour slot, a style id.
- Shared field vocabulary, same meaning in every op of both tools:

| Field | Meaning |
| --- | --- |
| `op` | the operation name |
| `at` | the address the op acts on (§6); a list where the op takes several |
| `as` | a name for what the op creates; later ops refer to it as `$name` |
| `position` | `before` or `after` `at`, for `insert` ops |
| `select` | exact text to find inside `at` (§6) |
| `occurrence` | 1-based: which match of `select` |
| `all` | `true`: every match (of `select`; every revision in revision ops). Not with `occurrence` |
| `with` | replacement text, plain (no markup) |
| `html` | content as HTML in the view's dialect (§7) |
| `text` | plain text of a new thing (a comment) |
| `to` | destination address, for `move` and `reorder` |
| `value` | xlsx: one cell value, in the form of §7a |
| `values` | xlsx: rows of cell values (a list of lists, top row first) |
| `image` | pptx, vsdx: the image of a picture: a local file path, or a media part as `read` shows it |
| `name` | the name being given (a new sheet, a renamed sheet, a defined name, a renamed bookmark) |
| `count` | how many things an `insert` op creates (rows, columns) |

- `text.replace` is `{at, select, with, occurrence | all}` in both tools.
- **docx** fields follow the vocabulary: the HTML of `paragraph.insert`,
  `paragraph.replace`, `header.set`, `footer.set` and table cells is
  `html` (docxdriver: `with`); section, header and footer ops take
  `at: "section:0A1B2C3D"` or `"section:2"` (docxdriver: `section_handle`, `section`); comment
  ops take `at: "comment:3"` (docxdriver: `comment_id`); bookmark ops
  take `at: "bookmark:Name"`, with `name` only for the name being given
  and `to` for the new anchor of `bookmark.move`; equation ops take
  `at: "0A1B2C3D/equation:2"` (docxdriver: `equation`); row, column and
  cell ops take the row, column or cell address (docxdriver: `row`,
  `column` numbers inside an `action`); revision ops take `at` or
  `all: true`, not both: `at` is one revision (`revision:7`) or a list
  of body paragraphs, meaning the revisions in them (their content,
  paragraph marks and paragraph, run and section property changes, a
  text box's included; `revision.accept` and `revision.reject` leave
  table, row and cell changes alone, `revision.convert_to_formatting`
  also converts the insertion or deletion of a row holding a listed
  paragraph); `all` is every revision of the body and of the headers
  and footers the sections use. An optional `author` narrows `all` or
  a paragraph list to that author's revisions (not one revision).
  Settling a paragraph mark's revision can join its paragraph with the
  next one (docxdriver: `target`; its `revision_settle` takes one id or
  all).
- **pptx** fields follow it too: `at` (was `target`, `targets`, and
  `parent` on `object.add`, whose `at` is the slide or group to add
  into), `as` (was `alias`), `select` (was `find`), `to` on
  `object.reorder` (`front`, `back`, or another object's address with
  `position`). Connector glue is `from_glue` and `to_glue` (was
  `start`, `end`), beside the end points `from` and `to`; `start` and
  `end` mean text offsets only, in both tools. (**vsdx** `line.begin`
  and `line.end` are keys of `line`, not op fields: Visio's names for
  a line's two ends, at `from` and `to`. pptx's `line.head` and
  `line.tail` are DrawingML's names for the same two ends.)
- Lengths are numbers in points unless the plan sets `units` (pptx).
  **xlsx** row heights are points; column widths are Excel's character
  widths, as `read` shows them.

## 4. Results

`read` and `find` print an object that starts with `base`, the hash of
the file read, and always has `notes`: a list of `{"at","reason"}`, one
for each thing the tool left out or could not read (a malformed part, an
object the reader could not model). `at` is the nearest address or the
part name. An empty list means the output is complete.

`apply` prints the edit result:

```json
{"base":"sha256:…","new_base":"sha256:…","dry_run":false,"saved":"out.pptx",
 "aliases":{"box":"slide3/s1001"},"changed":[…],"deleted":["slide3/s12"],
 "hazards":[{"kind":"dangling-glue","id":"slide3/s9","detail":"…","new":true}],
 "parts":["/ppt/slides/slide3.xml"]}
```

| Field | Meaning |
| --- | --- |
| `base` | hash of the input document |
| `new_base` | hash of the file written; use it as `base` of the next plan. Absent on a dry run; equal to `base` for a no-op |
| `dry_run` | true when nothing was written |
| `saved` | the file written; absent on a dry run or a no-op |
| `aliases` | each `as` name → the address it got |
| `changed` | every new or changed unit, read back from the document as the plan leaves it, one entry per unit under `read`'s key for its address, `id`. pptx: objects. docx: a paragraph as `{id, html}`; a table (`table:n`) whose structure or format changed, a section whose properties changed and a comment thread, each as `read` lists it. xlsx: each targeted range read back as `{id, cells}` (`id` the range, `Budget!A1:D3`; `cells` as `read --range` shows them); each sheet whose structure changed or whose references were adjusted as `{id, formulas_adjusted}` (`id` the sheet, `formulas_adjusted` the number of formulas rewritten); each defined name as `read --kind names` lists it. With `--changed full` (default) each is exactly as `read` shows it; with `--changed ids` each is its address only |
| `deleted` | addresses that no longer exist. xlsx: deleted rows and columns (`Budget!5:7`, `Budget!C:E`) and names |
| `hazards` | places where the document now contradicts itself; `new` marks those this plan introduced. The tool reports them and never fixes them |
| `parts` | package parts that changed |

Format fields (pptx `units`; docx `author`, `change_mode`) follow `base`
and echo the plan.

**docx** `create` prints `{"dry_run", "saved", "new_base"}`: the file
written and its hash, to use as the `base` of a plan on the new
document. With `--dry-run` it checks the HTML and writes nothing. Its
HTML is the dialect of §7, and what it cannot build is refused with
`invalid_operation` and a path into the HTML (`html<footnote>`,
`html@class`). It builds paragraphs, headings, inline content (formatted
runs, links, breaks, tabs), equations, page fields, lists (each `<ol>` or
`<ul>` a numbering definition of its own), tables (spans, nested tables,
and the style `read` writes on a table, row or cell; any other style is
refused at `html@style`), images (`<img>` whose `src` is a `data:` URL of
a PNG, JPEG or GIF), and sections with their headers and footers
(default, first page, even pages). Notes and comment markers are
refused; so is an image whose `src` is `read`'s asset address (put the
asset's `data_url` from `read --kind assets` in `src`) and a placeholder
`read` wrote for content HTML cannot express.

**xlsx** `create` prints `{"dry_run", "saved", "new_base"}` as docx
does. It makes a blank workbook with one sheet, `Sheet1`. Given a plan,
it applies the plan to that workbook first and also prints the plan's
`changed`, `deleted`, `aliases` and `hazards`; the plan must not have
`base`. With `--dry-run` it checks the plan and writes nothing.

**vsdx** `apply` prints the edit result; `units` follows `base` when
the plan sets it. `changed`: objects as `read` shows them, a
page as its summary entry, a shape data row as its `props` entry
(`{id, label, type, value}` and `format`, `unit`, `hidden`, `linked`
when read shows them). `deleted` includes field addresses a
`text.set` `delete` listed.

A page whose `background` or `background_page` changed is in
`changed` as its summary entry. `parts` includes media parts added
or removed and `/[content_types].xml` when a `Default` was added.

**vsdx** `create` prints `{"new_base", "dry_run", "saved"}`; with a
plan also its `aliases`, `changed`, `deleted`, `hazards` and
`parts`, and `units` when the plan sets it. The new drawing has
one page, `page0` "Page-1", 8.5 x 11 in portrait, and no masters, so
`object.add` with `master` is `not_found` in it; shapes come from
`geom` and connectors from `kind: "connector"`. The plan must not
have `base` (`invalid_plan`, path `base`). With `--dry-run` the plan
is checked and applied in memory and nothing is written.

A created drawing has no background page; a plan adds one with
`page.add` `background: true`. Pictures, line ends and shadows work
in a created drawing (none needs a master).

Hazard kinds:

- **pptx**: `stale-connector` (an end is glued but not on its site),
  `dangling-glue` (glue to a missing object), `group-frame` (a child
  outside its group's frame), `inset-overflow` (a shape the plan
  changed whose text insets leave no room for text: left + right ≥ the
  box width or top + bottom ≥ the box height, box in slide space;
  `detail` names the sides and sizes in pt and repeats that insets are
  [left, top, right, bottom], not CSS order; `new` is false when the
  shape had it before the plan). `inset-overflow` is checked only on
  shapes the plan changes, so `read` never reports it, and a shape whose
  box or any inset is not derived is not checked. The plan is applied:
  insets that fill the box are valid DrawingML, and the tool reports,
  it does not refuse.
- **docx**: the findings of the integrity check, which is the same
  check that guards a save (§10); there is no second detector. `kind` is
  the finding's class (`help hazards` lists them), `id` the part or the
  nearest address, `detail` the finding. Because a plan that would add a
  finding is refused (`invalid_output`), docx hazards are pre-existing
  ones on the parts the plan touched, and `new` is false. More kinds may
  be added later (§11).
- **xlsx**: `stale-values` (cached values may be out of date: the save
  asked Excel to recalculate, §9; `new` is true on the plan that first
  set the request), `ref-error` (a delete turned a reference into
  `#REF!`; `id` is the cell or name), and the findings of the integrity
  check on the parts the plan touched, which are pre-existing (`new`
  false), as in docx.
- **vsdx**: `stale-connector` and `dangling-glue` as pptx (vsdx
  never writes glue to a missing shape, and object.delete removes the
  glue to the deleted shape, so `dangling-glue` is only ever
  pre-existing); `off-page`
  (an object whose box lies wholly outside its page); `stale-values`
  (`id` `"document"`: the save left cached values that formulas would
  compute differently and asked Visio to recalculate on open, §9;
  `detail` names the cells of the first op that left them, at most
  ten, then "left stale"; `new` is true on the plan that first set the
  request).

Conventions for all output:

- Lists are always present (`[]`); a fact the tool cannot derive is
  omitted, never `null` and never a default.
- An agent must ignore keys it does not know.
- Every address a plan can use appears in `read` output in the form the
  plan takes.

### xlsx apply and create details

Approved by the design approver for the owner, 2026-11-14; the doc
comments of the types in `internal/xlsx/ops` and `internal/xlsx/apply`
are the precise rules.

- **`""` and `null` in plans (signed off by the owner, 2026-11-14).** In
  `range.set`, `null` leaves a cell empty and `""` writes an empty
  string, so a value `read` shows (`""` for an empty-string cell) writes
  back as itself. `cell.set` takes no `null` (use `range.clear`). The
  code keeps this as one switch point (`ops.EmptyFromNull`).
- **Codes:** `invalid_plan` for a wrong JSON type or shape;
  `invalid_operation` for a well-typed bad address or value, at the field
  (`ops[n].at`, `ops[n].value`, `ops[n].values[i][j]`): `$` refused, not
  sheet-qualified, the wrong address kind for the field, a malformed
  formula, a date that is not a calendar date, an unknown error literal,
  text past 32,767 characters, a formula past 8,192, a new spill formula,
  the spill operator `A1#`, a ragged or empty `values`.
- **Aliases:** only `sheet.add` takes `as`; `$name` is a sheet address
  and follows its sheet through renames. `aliases` gives the final
  address (`sheet:Memo` after a rename).
- **Inserts** take `position` and `count` (default 1); `at` is one row or
  one column.
- **`range.format`:** `underline` is a boolean; the `align`/`valign`
  enums stand; an area over 100,000 cells is `operation_failed` (use
  whole rows or columns; raising the cap later is not breaking).
- **Shared formulas:** writing over a master unshares its group (every
  member keeps its formula); a master whose `ref` does not parse is
  refused with `operation_failed`.
- **Run-time refusals** (path `ops[n].at` unless said): a write to a
  member or the anchor of a multi-cell array or spill, or to a table's
  header row, is `operation_failed`. `text.replace`: no match `not_found`
  (path `select`), several without `occurrence` or `all` `ambiguous`
  (`matches`), an `occurrence` past the matches `not_found` (path
  `occurrence`), a rich-text cell that would change `operation_failed`;
  only text values are searched. `sheet.add`/`sheet.rename` to a taken
  name and `name.add` of an existing name are `operation_failed` at
  `name`; `cell.merge` over a merge `operation_failed`; `cell.split` of a
  range that is not a merge `not_found`.
- **Structural lists** (`help <op>`): row and column insert and delete
  adjust cell formulas on every sheet, defined names, dimension, merges,
  cols, breaks, hyperlinks, conditional formatting and data validation,
  autoFilter, view selections and panes, and table ranges on row shifts;
  they refuse with `operation_failed`, `detail` naming the construct, a
  cut multi-cell array, spill or data table, a pivot table or pivot cache
  source on the sheet, a table crossed by a column shift or losing its
  header or last data row, x14 extension references, protected ranges,
  drawing anchors, chart ranges and comments at or after the point, an
  external link naming the sheet, a formula naming the sheet that does
  not lex, and a shift past the grid. `sheet.rename` adjusts cell
  formulas, defined names, conditional formatting and data validation
  formulas and hyperlink locations, and refuses a chart or a pivot cache
  that names the sheet, an external link naming it, and a formula naming
  it that does not lex.
- **`changed` units** have final addresses (an earlier op's range is
  reported where later inserts, deletes and renames left it; one a
  delete removed whole is left out): a range is
  `{"id":"Budget!A1:D3","cells":{…}}`, at most 10,000 cells with read's
  `truncated` when cut; a sheet `{"id":"sheet:Budget","formulas_adjusted":n}`;
  a name `{"id":"name:Tax", …}` with read `--kind names`' fields.
  `row.format` and `column.format` report no unit. A plan that changes
  nothing reports `changed: []`.
- **Hazards:** `stale-values` with `id` `"workbook"`; `ref-error` with `id`
  the cell or the name, `new` true; gate findings with `kind` the check's
  kind and `id` the part without its leading slash.
- **`apply --recalc` and `create --recalc`:** after the gate, LibreOffice
  headless computes the formula values on a private copy (a private
  profile with `OOXMLRecalcMode` 0) and only their `<v>` and `t` are
  spliced into the output, which the gate checks again. `soffice` comes
  from `DD_SOFFICE` or `PATH`; without it `--recalc` is
  `operation_failed`, path `--recalc`, and nothing is written. With
  `--dry-run` LibreOffice runs and nothing is written. `fullCalcOnLoad`
  is still set and `stale-values` still reported.
- **Newer functions:** a plan's function gets the stored prefix the
  corpus table or Microsoft's future-function list gives it (`XLOOKUP`
  is written `_xlfn.XLOOKUP`); a name on neither is written as given.
- **The gate** is scoped per sheet: the parts a plan changed (a changed
  table brings its sheet) plus the package, workbook, styles and shared
  strings count.

## 5. Dry run, staleness, files

- `--dry-run` does everything except write: same validation, same result
  apart from `dry_run: true` and no `saved` or `new_base`. A plan that
  passes a dry run and is applied to the same bytes succeeds with the
  same `changed`, `deleted`, `aliases` and `hazards`.
- `base` is `sha256:` and the hex SHA-256 of the file's bytes. When a plan
  has `base` and the file differs, the plan is refused with `stale_base`.
  It is a guard only: leaving it out means "the file as it is now". It
  never changes the output and never feeds id derivation.
- There is no preview key or confirm step. Preview is `--dry-run`; the
  guard is `base`.
- Paths: any path the caller gives, relative to the working directory or
  absolute. The plan is a file or `-` for stdin. The tool writes only the
  file named by `-o`, or the input in place (atomically) without `-o`.
  It never writes anywhere else and never follows a path taken from
  inside a document. Confining the tool to a directory is the caller's
  job (sandbox, working directory).
- **pptx, vsdx**: an op's `image` (and pptx `svg`) may name a file to
  read, by the same path rule. The tool reads it and never writes it;
  it is the only file besides the document and the plan that a
  command reads. `vsdx media` writes only its `-o`.
- Output is deterministic: the same document and the same ops give the
  same bytes, except fresh GUIDs and timestamps the format requires.

## 6. Addresses

An address is a string that names one thing in one document.

| Kind | Form | Tool | Stable across edits |
| --- | --- | --- | --- |
| slide | `slide3` (part name) | pptx | yes |
| object | `slide3/s17`; `slide3/s17~2` for the second object sharing id 17 | pptx | yes (`~n` is positional) |
| connection site | `s12:3`: object on the connector's own slide, site index; `none` | pptx | yes |
| paragraph | `0A1B2C3D` (eight upper-case hex digits); `0A1B2C3D~2` for the second paragraph sharing that id | docx | yes (`~n` is positional) |
| equation | `0A1B2C3D/equation:2` (1-based, in the paragraph) | docx | positional |
| opaque content | `0A1B2C3D/opaque:2` (1-based, in the paragraph): a placeholder (§7) | docx | positional |
| table, row, column, cell | `table:1`, `table:1/row:2`, `table:1/column:3`, `table:1/row:2/cell:1` (1-based). `cell:c` counts the row's cells (the `<td>` position `read` shows); `column:n` counts grid columns | docx | positional |
| section | `section:0A1B2C3D` (the id of the paragraph that ends it, as `read` shows it), `section:final` (the last section, which no paragraph ends); `section:2` (1-based) as a shorthand | docx | yes (`section:N` is positional) |
| comment, revision | `comment:3`, `revision:7` (the file's id) | docx | yes |
| bookmark | `bookmark:Name` | docx | yes |
| sheet | `sheet:Budget` (the sheet's name) | xlsx | until renamed |
| cell, range | `Budget!B2`, `Budget!A1:D10`; a name that needs quoting is quoted as Excel does, `'Q1 Budget'!A1`, `'Bob''s'!A1`. Always sheet-qualified; `$` is refused | xlsx | positional |
| rows, columns | `Budget!5:7`, `Budget!C:E` | xlsx | positional |
| name | `name:Tax_Rate` (workbook scope), `name:Budget!Local` (sheet scope) | xlsx | yes |
| table | `table:Sales`, `table:Sales/column:Amount` | xlsx | yes |
| opaque content | `sheet:Budget/opaque:2` (1-based, in the sheet): a chart, image, pivot table or other object the tool does not edit | xlsx | positional |
| page | `page0` (the page's stored ID); `page0~2` for the second page sharing it | vsdx | yes |
| master | `master2` (stored ID), read only | vsdx | yes |
| object | `page0/s5`, `master2/s5`; `page0/s5~2` for the second shape sharing id 5. Group children are addressed by their own id; `parent` names the group | vsdx | yes (`~n` is positional) |
| connection site | `s12:3` (point 3 of object 12 on the connector's page), `s12` (the whole shape); `none`. `read` also shows, read only, glue to another cell as `s12@Geometry1.X2` and glue to a shape the page lacks as `#12` | vsdx | yes |
| shape data row | `page0/s5/prop:Cost` (the row name) | vsdx | yes |
| layer | `page0/layer:Connector`, read only | vsdx | until renamed |
| field | `page0/s5/field:1` (1-based in the shape's text): a placeholder (§7) | vsdx | positional |
| media part | `/visio/media/image1.png` (the part name), as `read` shows it in a picture's `image`; taken by `image` and by `vsdx media` | vsdx | until the image is replaced |
| alias | `$name`, defined by an earlier op's `as` in the same plan. **vsdx**: followed by any suffix the named address takes (`$r/prop:Cost`, `$r:2`) | all | for the plan |

- Composite addresses nest with `/`; a kind and its number join with `:`.
- **Stable** addresses are ids stored in the file. Editing other things
  does not change them.
- **Positional** addresses are counted, so they are resolved against the
  document as it stands when that op runs: after `row.insert` before row
  2, the old row 2 is `row:3` for every later op. To refer to something
  an earlier op made, give it `as` and use `$name`; do not count.
  `read` after `apply` gives the new numbers.
- **Paragraph ids.** A paragraph's id is its `w14:paraId`. For a
  paragraph without one, the tool assigns an id that is a function of
  the file's bytes, so every read of the same file shows the same ids.
  Any `apply` that writes the file stores the assigned ids for the whole
  document (the one exception to §9), so ids stay the same across edits.
  `read` reports how many are not yet stored (`id_repairs_needed`).
  Duplicate ids already in the file are never rewritten: the first
  paragraph is `0A1B2C3D`, the second `0A1B2C3D~2`, as for pptx objects.
  A paragraph of a header, footer or note is addressed by its stored
  `w14:paraId` only (the read markup shows it as the paragraph's `id`;
  one without a stored id has none), counted with `~n` across those
  stories; a body paragraph with the same address wins. Such a
  paragraph is listed in `changed` with the markup of its header,
  footer or notes.
- **Cells and grid columns.** A table has a grid of columns; a cell
  that spans several (`colspan`) takes that many. `table:1/row:2/cell:3`
  is the third cell of the row, wherever it sits in the grid;
  `table:1/column:3` is the third grid column. `read` shows each cell's
  grid column (`grid_column`, counting the columns a row skips with
  `w:gridBefore`) and its spans, and `cell.merge` counts
  `row_span` and `column_span` in grid units.
- **Sections.** A section is named by its boundary paragraph, the one
  whose properties carry the section's `w:sectPr`: `section:0A1B2C3D`.
  The last section has none and is always `section:final`. A
  boundary paragraph sharing a duplicated id is `section:0A1B2C3D~2`,
  as for paragraphs. A value of eight hex digits (with its `~n`) is
  always a paragraph id; any other is a number
  (`section:2`, counted from 1 in body order, the body's implicit
  final section included). A boundary paragraph an earlier op joined
  into another is followed to the survivor.
- **Tables.** `table:` takes a number in docx (`table:1`, positional)
  and a name in xlsx (`table:Sales`, the table's stored name).
- **xlsx cells.** References are A1 only; there is no R1C1. A sheet
  name is quoted when Excel would quote it, with `''` for a quote
  inside it. `read --range` also takes a `name:` or `table:` address
  that resolves to one plain range; ops take A1 forms only. A row or
  column insert renumbers what follows it, so later ops in the plan see
  the new positions.
- Aliases live for one plan. The result's `aliases` gives the real
  address to use afterwards.

### Text selection

Text inside a paragraph is addressed by content, never by offsets.

- The text `select` matches is the paragraph's **final view**: the text
  as it reads with every tracked change accepted (insertions in,
  deletions out). An equation counts as one U+FFFC character and a line
  break as `\n`; a tab is `\t`. Matching is exact, case-sensitive, on
  Unicode scalar values, with no normalisation. `read` shows this text
  for each paragraph (`selectable_text`).
- **A span** is `at` + `select`, plus `occurrence` (1-based) or `all`
  where the op allows it. One match needs neither. Several matches with
  neither is `ambiguous`; no match is `not_found`.
- **A whole paragraph** is `at` with no `select`.
- **An insertion point** is `position: "before" | "after"` with a span
  (just before or after the selected text), or with `at` alone (the
  start or end of the paragraph). `text.insert` takes one.
- A selection may contain an equation or a break; the op then replaces
  or keeps it as the op's help says, and never drops it silently.
- **Anchors** (where a comment or bookmark sits) have one shape wherever
  they appear: `{"at","select","occurrence","start","end"}`. `start` and
  `end` (offsets in the final-view text) are output only, for
  orientation; no plan field takes an offset.

## 7. HTML

Text content is read and written as HTML: pptx and vsdx text bodies
and docx `markup`, all written in plans as `html`.

Input policy, both tools: **HTML5 syntax, strict meaning.**

- Parsed by one HTML5 tokenizer and tree builder (`internal/htmlx`). A
  tokenizer parse error refuses the plan, with the WHATWG error name and
  a `span`.
- Repairs that cannot change meaning are applied: optional end tags
  (`p`, `li`, `td`, `tr`, …), void elements, named character references,
  and `<x/>` for the empty elements `read` writes.
- **Derived attributes are ignored on input.** Attributes that `read`
  writes to describe the document (list markers, authors, revision ids,
  loss notes: marked read-only in `help html`) may be sent back changed,
  unchanged or not at all; they never affect the edit.
- **Unknown things are refused**, never dropped or guessed
  (`invalid_operation`, `path` naming the attribute or element): an
  element or attribute that is not in `help html`, a style property the
  format cannot store, misnested tags, repairs that would move or
  duplicate content, scripts, event handlers, stylesheets, external
  resources.
- **`read` never writes markup that `apply` refuses.** What `read`
  writes, `apply` accepts unchanged (round trip).
- **Placeholders.** Existing content the HTML cannot express (a drawing,
  a chart, a content control, a field the tool does not model) is written
  as an empty placeholder that carries its address, as equations do:
  `<opaque data-ref="0A1B2C3D/opaque:2" data-kind="drawing"></opaque>`
  and `<equation data-ref="0A1B2C3D/equation:1" …>`. Sent back, a
  placeholder keeps its original bytes exactly; it may be moved within
  the same `html`. A `data-ref` that names nothing in the content being
  replaced is `not_found`.
- **Leaving something out is refused, not a deletion.** The `html` of a
  `replace` or `set` op must account for every placeholder, equation and
  paragraph of the content it replaces: each is either sent back or
  listed in the op's `delete` field (a list of their addresses). An op
  whose `html` omits one that `delete` does not list is refused
  with `operation_failed`, `path` `ops[n].html`, and `detail` listing the
  addresses left out. What `delete` lists is deleted (in track mode, as
  a tracked deletion) and appears in the result's `deleted`.

**vsdx** text uses the pptx text grammar's subset that Visio stores
(`help html`): `<p>`, `<ul><li>` (`data-char`), `<b> <i> <u> <s>`,
`<br/>` (stored as U+2028, Visio's line break in a paragraph:
**Visio unconfirmed, VE7**); `<span style>` with `font-weight`, `font-style`,
`text-decoration` (with the `normal` and `none` forms), `font-size`
(at least 1pt, Visio's minimum; less is refused),
`color`, `font-family`, and `data-color` beside `color` in pptx's
canonical form (always `#RRGGBB`: Visio text has no theme colour
references; on input `data-color` must agree with `color`);
paragraph `style` with `text-align`, `margin-left`, `margin-right`,
`text-indent`, `margin-top`, `margin-bottom`, `line-height`. Fields
are placeholders, `<opaque data-ref="page0/s5/field:1"
data-kind="field" data-text="3"></opaque>` (`data-text`, the cached
display text, is derived), and follow the placeholder rules above:
`text.set` must send each back or list it in `delete`. As in
pptx, the HTML shows only the text's own formatting, never what it
inherits from style sheets or, under a shape's own text, from its
master; an "off" form (`font-weight:normal`) appears only where it
clears what the next sheet sets. When `text.replace` gives a shape
that showed its master's text text of its own, the master's
formatting still applies in Visio but is inherited, so the HTML no
longer shows it. A trailing line feed of the stored text adds no
empty paragraph. What the HTML does not show (tab runs,
sub/superscript position, case, small caps, double underline, letter
spacing, language, Asian fonts, force-justify) is kept from the run
or paragraph replaced. `<a href>`, `<ol>`, `data-level` and every
other element or attribute outside the subset are refused by name.
HTML is checked with the rest of the plan, before the document opens,
and every fault comes back together with the path forms above
(`ops[0].html<a>`, `ops[1].html@bogus`).

Attribute conventions, both tools:

- `id` is the element's address (§6).
- Standard HTML is used where HTML has the concept: `<b> <i> <u> <s>
  <sup> <sub> <a href> <br> <ol start type> <ins> <del>`, `style` for
  the listed CSS properties, `lang`, `hidden`.
- Everything else is `data-<name>`: lower case, words joined by `-`, no
  tool or format prefix (`data-revision`, not `data-docx-revision`). The
  JSON key `foo_bar` and the attribute `data-foo-bar` name the same fact.
- The same name means the same thing in both tools: `data-level` (0-based
  list or outline level), `data-color`, `data-highlight`, `data-kind`.
- Equations are `<equation display="inline|block">LaTeX</equation>`.

docx, where HTML is a whole paragraph (`paragraph.insert`, and
`paragraph.replace`, each `html` value or list item):

- One block element, `<p>` or `<h1>`..`<h6>`, in the form `read` shows
  it, is the paragraph: its storable attributes set the paragraph's
  properties (the heading tag's style, `data-style` or `class` for a
  `<p>`, the paragraph CSS in `style`); its derived attributes (`id`,
  list markers, revision marks, an equation's `data-ref` on insert) are
  ignored. A paragraph's markup from `read`, sent back, makes a
  paragraph that reads the same.
- Bare inline content is shorthand for the content alone: inserted, the
  paragraph takes the anchor's properties as Word's Enter does; replaced,
  it keeps the paragraph's properties.
- Two or more block elements in one value, or a block element inside
  inline content, are refused (`invalid_operation`, `path` the field):
  one paragraph per html value; use the list form for several.

docx `create`: the body is paragraphs, headings, lists and tables; a
top-level `<equation display="block">` is a paragraph of its own. An
inline equation, like any inline content, goes inside a `<p>` (a
top-level one is refused). `create` accepts the markup `read` writes for
a whole document, including `<section>` and read's derived attributes,
which are ignored.

## 7a. Cell values

**xlsx** has no HTML: a cell's content is a JSON value, in the same
form in `read` output and in plans (`help values`).

| Value | Form |
| --- | --- |
| number | a JSON number, in the shortest form that reads back to the same double. NaN and infinities are refused |
| string | a JSON string; up to 32,767 characters. A string that starts with `=` is written `{"text":"=literal"}` |
| boolean | `true`, `false` |
| error | `{"error":"#N/A"}` (one of Excel's error values) |
| date and time | `{"date":"2024-03-01"}`, `{"date":"2024-03-01T13:30:00"}`, `{"time":"13:30:00"}` |
| formula | in a plan, a string starting with `=` (`"=B2+C2"`), up to 8,192 characters. In `read`, see below |
| empty | in `range.set`, `null` leaves the cell empty; `""` is the empty string (owner decision, 2026-11-14) |

- **Dates** are derived from the cell's number format and the
  workbook's date system (1900 or 1904). A serial number outside the
  range a date can show stays a number. A date written into a cell with
  no date format gets the built-in date format 14.
- **Formulas in `read`.** The default view, `cells`, shows a formula
  cell as `{"f":"=B2+C2","v":2400}`, with `v` the value the file stored
  and left out when it stored none. `--view values` shows the stored
  value only, `--view formulas` the formula only. A cell that shares a
  formula with others shows its own formula, written out. Function
  prefixes the file stores (`_xlfn.`) are not shown, and are added
  when a plan writes the function.
- Existing dynamic-array spills are read. The anchor shows its formula
  normally; each non-anchor cell is `{"v":3,"spill":"Budget!A1"}`
  (the anchor address). Legacy CSE array-formula members are marked the
  same way; help distinguishes CSE arrays from dynamic spills. Which
  functions spill is determined by `formula.IsDynamicArray`. New spill
  formulas are refused by writes, and a shift that cuts a spill is
  refused.
- A malformed formula is refused with `invalid_operation`, path
  `ops[n].value` (or the `values` element).
- Rich text in a cell is read as its plain text; `text.replace` on a
  rich-text cell is refused with `operation_failed`.

## 8. Reading

- `read` is cheap to narrow: ask for less (`--slide`, `--ids`, `--region`,
  `--kind`, `--view`) instead of filtering afterwards.
- **pptx** `read <file>` lists slides (`{"base","slides":[…]}`);
  `--slide slideN` gives its objects; `--format json|svg`; `--detail
  sites|full`; `--units pt|emu`.
- **docx** `read <file>` gives the document (`markup` and the tables,
  sections, styles, equations, comments it refers to); `--kind
  document|comments|revisions|bookmarks|styles|assets`; `--view
  markup|final|original`. `find <file> <query> [--ignore-case] [--view]`
  gives `{"base","matches":[…]}`.
- **xlsx** `read <file>` gives the workbook summary:
  `{"base","sheets":[{"id","name","state","dimension","tables","opaque"}],"names","tables","notes"}`.
  `dimension` is the range the file stores; the used range is shown
  only when a sheet is read. `--sheet` or `--range` gives cells,
  keyed by address:
  `{"base","sheet","range","view","cells":{"A1":"Item","D2":{"f":"=B2+C2","v":2400}},"merges":[…],"notes":[]}`.
  `--view cells|values|formulas` (§7a); `--detail styles` adds
  `styles`, a list of `{"at", <format fields>}` regions;
  `--kind workbook|names|tables|styles|comments`.
- **xlsx** `read` returns at most `--limit` cells (default 10,000; `0`
  is no limit), cut at a row boundary. A cut read has
  `"truncated":{"next":"Budget!A501:D1000"}`, the range to read next,
  and a note.
- **xlsx** `read` has `"values_stale": true` when the workbook asks to
  be recalculated on open (§9): stored values may be out of date.
- **xlsx** `read` lists what it does not model as `opaque`
  (`{"id":"sheet:Budget/opaque:1","kind":"chart","range":"Budget!F2:M20"}`).
  Sheet kinds: `chart`, `image`, `shape`, `pivot_table`,
  `conditional_formatting`, `data_validation`, `sparkline`, `slicer`,
  `control`. Workbook kinds: `vba_project`, `external_link`,
  `connection`, `custom_xml`.
- **xlsx** `find <file> <query>` gives `{"base","matches":[…]}`: the
  cells whose text (`--view values`, default) or formula
  (`--view formulas`) contains the query.
- **vsdx** `read <file>` lists pages and masters:
  `{"base","pages":[{"id","name","size","orientation","print_orientation","background","background_page","objects"}],"masters":[{"id","name","name_u","objects"}],"notes"}`.
  `--page page0` (or `--master master2`, read only) gives the page's
  objects in paint order, groups before their children, with
  **effective** values (after master and style inheritance), lengths
  in points (`--units in`: inches) on a frame whose origin is the
  page's top-left corner, y down. A master is shown in the frame of
  its own page sheet; one with no stored PageHeight shows no `box`,
  `from` or `to`, with a note. The page also has `layers` (read
  only, `{id, name, index, visible, print}` and `lock` when set),
  `connections` (`{connector, from, to, from_glue, to_glue}`),
  `hazards` and `notes`. In the summary and in the page result,
  `background` is true on a page that is a background page (left out
  on a foreground page), and `background_page` is the address of the
  page this page uses as its background (left out when it uses none;
  a stored reference to a page the drawing lacks is shown as `#7`,
  read only, with a note). A page's objects never include its
  background's: read the background page itself.
- Object keys are pptx's where pptx has them (`id`, `kind`, `type`,
  `name`, `parent`, `hidden`, `box`, `rot`, `flip_h`, `flip_v`,
  `from`, `to`, `from_glue`, `to_glue`, `fill`, `line`, `html`,
  `sites` with `--detail sites`, `z`, `origins`, `omitted`, `xml` for
  kind `other` with `--detail full`); vsdx adds `name_u` (when it
  differs), `master`, `text` (plain), `props`, `layers`, `glued`
  (control-handle glue, read only) and, with `--detail full`, `cells`
  (the shape's own stored cells, `{"n","v","u","f"}`). Kinds: `shape`,
  `connector` (any 1-D shape), `group`, `picture`, `other`. `line`
  is `{w, color, dash}` with `dash` the LinePattern as stored, a
  string. A connector shows the `fill` it stores or inherits, as
  Visio stores it on 1-D shapes; Visio draws no fill on an open
  path. A `props` entry is `{id, label, type, value}` with
  `format`, `unit` (a number's length unit, value in inches),
  `hidden` and `linked` when they apply; a date that is not stored in
  date form shows no `value`, with an omission.
- `line` on a connector also has `begin` and `end`,
  `{"arrow","size"}` as §3, each shown only when its effective arrow
  is not `none`: the cell the shape stores, else its master's, else
  the line style's, as every `line` value, with the origin in
  `origins.line`. An end whose arrow is stored as `Themed` is left
  out, with `line.begin` or `line.end` in `omitted`; a size that is
  themed leaves `size` out of the entry the same way. Other kinds
  show no `begin` or `end`.
- `shadow` is shown on a shape or group whose effective
  `ShdwPattern` is not 0: `{"color","transparency","offset"}` in the
  plan's form, each member from the effective cell (shape, master,
  fill style), with the origin in `origins.shadow`. No `shadow` key
  means no shadow or one not derived: a themed `ShdwPattern` (most
  themed drawings) is left out with `shadow` in `omitted`, reason
  `themed value not derived`; a themed member is left out of the
  object with `shadow.color` (and so on) in `omitted`. `offset` is
  shown only when the effective `ShapeShdwType` is 1; otherwise it is
  omitted with the reason `page default shadow not derived` (the page
  sheet's `ShdwOffsetX`/`ShdwOffsetY` are not shown). A non-zero `ShapeShdwBlur`, oblique
  angle or scale is not shown and is named in `omitted`. `read` never
  shows `"shadow": "none"`.
- A picture (kind `picture`: a Foreign shape with an image
  relationship) has `image`, the media part (`/visio/media/image1.png`),
  `media` `{"type","bytes","sha256","px"}` (the content type from the
  package; the part's size in bytes; the SHA-256 of its bytes; `px`
  `[w, h]` from the header for PNG and JPEG, otherwise left out with
  `media.px` in `omitted`, as for EMF), and `crop` `[l, t, r, b]` in
  percent when the Img cells are in the forms §3 writes and not all
  zero (any other form is `crop` in `omitted`). `read` never contains
  image bytes; `vsdx media` extracts them. A picture whose
  relationship or part is missing shows no `image`, with a note.
- `origins` values: `local`, `inherited` (stored value, inherited
  formula), `master`, `style`. A value stored as `Themed` is left out,
  with the reason in `omitted`; palette-index colours are shown
  resolved (`#RRGGBB`).
- `orientation` is the page's shape from its size (absent when
  square); `print_orientation` the printer setting, absent when not
  stored; they may disagree. A page with a drawing scale has the note
  "drawing scale 1 in = 120 ft: lengths are stored drawing units"
  (with the page's own numbers and units), and its lengths are shown
  as stored (drawing units are not applied).
- `values_stale: true` on the summary and the page when the document
  asks Visio to recalculate on open.
- **vsdx** `find` gives `{"base","matches":[{"at","text"}],"notes"}`:
  each shape whose effective text, or shape data row whose value as
  read prints it, contains the query, pages in stored order;
  `--ignore-case` is simple Unicode case folding; at most `--limit`
  matches (default 10,000; 0 is no limit); a cut search has
  `"truncated":{"after":"<last at>"}` and a note.
- **vsdx** `xml <file> page0/s5` prints the shape's source bytes, its
  nested shapes included.
- The same thing has the same shape in every kind and view that shows it
  (a comment thread in `--kind document` and `--kind comments`).

### xlsx read and find details

Approved by the design approver for the owner, 2026-11-14. The doc
comments of the types in `internal/xlsx/model` and `internal/xlsx/read`
are the precise rules.

- **find matches**: `{"base","matches":[{"cell":"Budget!A1","value":…}],"notes"}`; `value` is the cell as read shows it in the searched view (`--view values`: the value form of §7a; `--view formulas`: the formula string `"=…"`). Rationale: the address and the value are directly actionable.
- **find numbers and other non-text values** are matched against their text as read prints them: numbers in the shortest round-trip JSON form (`1200`, `0.5`, `1e-7`), dates and times as their ISO text, booleans as `true`/`false`, errors as the literal (`#N/A`). Rationale: find never disagrees with read.
- **find limit (new)**: default 10,000 matches, `--limit 0` unlimited; a search with more matches than the limit has `"truncated":{"after":"Budget!C17"}` (the last match returned) and a note; exactly `--limit` matches is not a cut. Rationale: find has no `--range` to resume with, so it names where it stopped.
- **find scope**: every worksheet in tab order, hidden ones included; `--sheet` takes `sheet:Name`. The empty query matches every cell with a value (or formula). Rationale: as docx's find.
- **`--ignore-case`** uses simple Unicode case folding (one rune to one rune, no locale tailoring: `ß` does not match `ss`). Rationale: predictable and allocation-free.
- **read limit and truncation**: decision 6. `next` keeps the requested range's form: `Budget!A501:D1000` for an area, `Budget!501:1000` for whole rows; its last row is the smaller of the range's and the used range's. A first row larger than the limit is read whole. Rationale: a read always progresses and `next` can be passed back as `--range`.
- **summary entries**: `sheets:[{"id","name","state","dimension","tables"}]` with `state` one of `visible`, `hidden`, `very_hidden` (**new**: snake_case of the stored value), `dimension` sheet-qualified (`Budget!A1:D10`; **new**), `tables` the names of the sheet's tables; `names:[{"name","scope","refers_to","hidden"}]` (`hidden` true for definedName@hidden, left out otherwise) with `scope` `"workbook"` or `"sheet:Budget"` (left out, with a note, when localSheetId is invalid) and `refers_to` `"="` plus the display form; hidden and `_xlnm.` names are listed as stored (**new**); `tables:[{"name","sheet","ref","columns"}]` with `sheet` `"sheet:Budget"` and `ref` sheet-qualified, header and totals rows included. A1 has no `opaque` key (A2). Rationale: every value is an address or form the agent can type back.
- **notes** use contract §8's `{"at","reason"}`. **new**: per-cell notes (durations, serials, unreadable values) are aggregated per sheet and reason, `at` the first cell and the reason giving the count; at most 1,000 notes, then one note counting what was left out. Rationale: a sheet of a million duration cells must not print a million notes.
- **CSE arrays**: the anchor is a normal formula value plus `"array":"cse"`; members are `{"v":…,"spill":"…","array":"cse"}`; dynamic spill members omit `array`. A single-cell CSE formula is marked on its cell. **new**: data tables (`t="dataTable"`) mark anchor and members `"array":"data_table"`, the anchor shows its stored formula when the file has one (Excel stores none) and a note names its inputs; an array formula whose dynamic/CSE kind cannot be derived from `xl/metadata.xml` marks members with `spill` only, and a note says why (never guessed from the function name).
- **views of spills (new)**: `--view values` shows plain stored values for anchors and members (no marks); `--view formulas` shows a formula as the string `"=…"`, a CSE or data-table anchor as `{"f":"=…","array":"cse"}`, and members as `{"spill":"Budget!A1"}` (plus `array`). Rationale: values is the cached grid; formulas keeps the membership facts.
- **`range` and `used_range`**: `--sheet` without `--range` reads the sheet's used range, and `range` is that used range; `read --range sheet:Budget` is the same. The scanned used range (non-empty cells only) is `used_range`. Both are left out for a sheet with no non-empty cell. Rationale: the read range is always explicit.
- **hidden and merges (new detail)**: `hidden` lists hidden row runs then column runs that intersect the range, clipped to it; `merges` lists merged ranges that intersect the range, unclipped. A row of height zero is not hidden. Rationale: clipping keeps `hidden` about what was read; a merge is only meaningful whole.
- **`--group` (new)**: decision 4's opt-in, `--view formulas` only: each shared-formula group whose members inside the range fill a rectangle of two or more cells is listed once in `"groups":[{"at":"Budget!D2:D100","f":"=B2+C2"}]` (`f` written for the top-left cell) and its cells are left out of `cells`. Rationale: one entry per formula Excel filled.
- **`--sheet` input (new)**: only `sheet:Name` (`--sheet Budget` is refused with a hint), matching the address table; sheet, name, table and column names match ignoring case.
- **chartsheets, dialogsheets and macrosheets**: listed in the summary with a note; `read --sheet` of one is an empty result (`cells` `{}`) with a note, not an error.
- **an empty-string cell (new)** (`t="str"` with an empty value) reads as `""`.
- **safety limits and broken main parts**: a limit trip on any part, including one read lazily (a worksheet, the shared strings), a malformed workbook part and a binary `.xlsb` are all `invalid_document`.

- **Output bounds (added in A1 verification; approved by the design approver for the owner, 2026-11-14).** `--limit` counts cells, but one corpus file (apache-poi `poc-shared-strings.xlsx`: 12,000 cells referencing one 1 MB shared string) made a default read print about 10 GB. Read now does two things:
  - It leaves out, with one aggregated note, a stored text longer than Excel's 32,767 characters or a formula longer than 8,192: no file Excel wrote holds one.
  - It bounds one read's or find's output at 64 MiB. A read past it stops at a row boundary with the decision-6 `truncated.next` and a note naming the output limit; find stops with `truncated.after`. A single row whose cells alone pass it is refused with `operation_failed` and a hint to read fewer columns.

  No new keys; the 64 MiB bound is a fixed constant with no flag. docx and pptx read have no such bound; this is an xlsx rule.

## 9. What the tool promises about a document

- The output gives Office no new repair prompt: a file Office opened
  cleanly still opens cleanly. (A file that already needed repair is not
  made worse, and is not repaired.) **vsdx**: that Visio opens any vsdx
  output, including a created drawing, without a repair prompt is
  **Visio unconfirmed** until visio-smoke results are recorded in
  `docs/vsdx-design.md`. Verified: the structural integrity gate, vsdx
  read-back, and LibreOffice import. A vsdx save that adds or releases a picture changes
  `[Content_Types].xml`, the page's relationships and the media parts
  as §3 says; these are the op's own targets, not exceptions.
- Content no op targeted is unchanged, byte for byte. There are three
  exceptions:
  - a docx save writes the `w14:paraId` of every paragraph that lacked
    one (§6), so that addresses stay stable;
  - an xlsx save that changes a value, a formula or the structure of a
    sheet asks Excel to recalculate the workbook on open
    (`fullCalcOnLoad`), and removes the calculation chain
    (`calcChain.xml`) when a formula cell is added, removed or moved.
    The tool cannot recalculate: it never writes a value it computed,
    and the stored values of other cells are left as they were, so
    until Excel recalculates they may be out of date. The result
    reports the `stale-values` hazard and `read` shows `values_stale`.
  - a vsdx save that leaves cached values stale (a write that changes
    a cell other cells' formulas read, when vsdx does not evaluate
    them) sets the custom document property `RecalcDocument` true
    (creating `docProps/custom.xml` when absent) and marks each
    changed cell with `<?NewValue V?>`, so that Visio recalculates on
    open. That Visio does so, and opens a created
    `docProps/custom.xml` cleanly, is **Visio unconfirmed** until
    experiments VE2, VE3 and VE12 are recorded in
    `docs/vsdx-design.md`; this exception must not be signed off as
    confirmed before then. vsdx writes computed values only for the
    formulas its closed evaluator handles (decision c of
    `docs/vsdx-design.md`). The result reports `stale-values`.
- The tool derives facts and never decides: it reports hazards, and does
  not fix, normalise or tidy anything it was not asked to change.

## 10. Documents with defects

Real files have defects the tool did not make. The rule:

- **Checks only stop the tool from introducing nonconformities.** A
  defect the input already had never blocks `read`, an edit elsewhere,
  or the save, and existing invalid values can be overwritten.
  `invalid_output` means this plan would add a nonconformity; `path`
  names the op and `detail` what it would add.
- **An op refuses only when its own target is too broken to edit
  safely**, and says so at that op with `unreadable_target` (`path`
  `ops[n].at`, `detail` saying what is wrong). This is the only refusal
  a pre-existing defect can cause. An op never goes ahead and then fails
  at save time.
- **Degraded read.** Parts are parsed only when needed. A malformed part,
  or an object the reader cannot model, is left out of the output with a
  `notes` entry; everything else is read. Only ops that target that part
  or object fail. Building the result of `apply` follows the same rule:
  a neighbour the reader cannot model is a note, not a failed edit.
- **`invalid_document`** is only for a file that is not a zip, has no
  main part (no content types, or no document, presentation or workbook
  or Visio document part), or trips a safety limit (size, ratio, entry
  count, nesting depth). **xlsx**: also a binary workbook (`.xlsb`), a
  legacy `.xls` and an encrypted workbook. `.xlsm` and `.xltx` are read
  and edited; a VBA project is copied through unchanged. **vsdx**: also
  a legacy binary or XML Visio file (`.vsd`, `.vdx`, `.vsx`); a
  malformed page or master part is a note in the summary and
  `unreadable_target` (path `--page`) when read; a shape whose master is
  missing shows its own cells with a note. An `image` file that is not a readable PNG or JPEG is refused
  before the document opens (§3); an existing picture whose media part
  is missing or malformed is read with a note, can be moved, deleted
  and given a new `image`, and is `unreadable_target` only for `crop`
  and for `vsdx media`.
- **A read that asks for the damaged part itself** (`docx read --kind
  styles` on a file whose styles part is malformed) is refused with
  `unreadable_target`, path `--kind`: there is nothing else to show.
  `--kind document` on the same file succeeds, with a note.
- **Strict OOXML** (ISO/IEC 29500 Strict namespaces) is opened, read and
  edited like Transitional; new content is written in the namespaces the
  part already uses, and the file is never converted. **xlsx**: a Strict
  workbook's ISO 8601 date cells (`t="d"`) are read as dates.
- Unknown extensions, vendor markup and `mc:AlternateContent` are kept as
  they are and edited only when an op targets them.

## 11. Versioning

- The contract has one integer version, now `1`. `<tool> --version`
  prints `{"tool":"docx","version":"<release>","contract":1}` (`"tool"` is
  the binary: `pptx`, `docx`, `xlsx` or `vsdx`).
- A plan may say `"contract": 1`. A tool refuses a plan with a higher
  number than it implements (`invalid_plan`, path `contract`).
- Additive changes keep the version: new ops, new optional fields, new
  output keys, new error codes, new hazard kinds, new flags. Removing or
  renaming anything here, or changing what a valid plan does, raises it.

## 12. What is not in the contract

- docxdriver 0.5.2 plans, its `outcome` envelope, preview keys, the
  working-directory path rule and `document_ui`. `tools/oracle`
  translates plans and results for parity runs; the binaries do not.
- MCP, WASM and language bindings. Agents run the binaries.
- **vsdx** in v1 (deferred, not refused by name unless listed in §3):
  writing hyperlinks, comments (not read either), layers, group
  create/ungroup, object.duplicate, master import and editing
  masters; `read --format svg`; formula and ShapeSheet cell writes
  (there is no `cell.set`); arrow styles beyond `triangle` and
  `stealth`, and line ends on 2-D shapes; shadow blur, oblique and
  scaled shadows, the page's default shadow, and shadows on
  connectors and pictures; writing EMF, SVG, GIF or any image other
  than PNG and JPEG, picture brightness, contrast and other
  adjustments, and `data:` URLs for images; theme resolution for
  fill, line, line ends and shadow (a themed value is omitted, §8);
  pinning an inherited value locally; moving point-glued ends when
  their target moves.

## 13. Implementation notes (not for agents)

- One decoder in `internal/cli` reads plans for both tools: it carries
  the path of every value, detects duplicate keys, collects every
  document-independent error, and has no serde wording or number
  emulation.
- The pptx strict text parser is deleted only after the pptx text
  round-trip law passes on the corpus through the HTML5 parser, with
  tokenizer tests added; HTML errors carry the WHATWG code and a span,
  not Rust debug text.
- vsdx: `internal/vsdx` packages, `docs/vsdx-design.md`.
