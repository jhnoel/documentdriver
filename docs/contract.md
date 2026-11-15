# The agent contract (version 1)

`pptx`, `docx` and `xlsx` are three CLIs with one contract. This page is
the whole of what an agent may rely on; anything not written here
(whitespace, message wording, key order) may change. Format-specific
parts are marked **pptx**, **docx** or **xlsx**. `<tool>` is any of the
binaries.

Status: signed off by the owner on 2026-11-13; the **xlsx** parts on
2026-11-14 (the owner's decisions and the design approver's rulings for
the owner, recorded in `docs/xlsx-contract-draft.md`). Where the code
differs, this page is the target; `docs/plan.md` says what is
implemented.

## 1. Commands

```
<tool> read  <file> [flags]              facts about the document; never writes
<tool> find  <file> <query> [flags]      docx: paragraphs containing text; xlsx: cells containing text
<tool> apply <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
<tool> create <doc.html|-> [--dry-run] -o <out>     docx: a new document from HTML
<tool> create [<plan.json|->] [--dry-run] -o <out>  xlsx: a new workbook, optionally with a plan applied
<tool> xml   <file> <address>            pptx: source bytes of one object
<tool> render <file> ... -o <out.png>    pptx: picture of a slide
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
  `end` mean text offsets only, in both tools.
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

Hazard kinds:

- **pptx**: `stale-connector` (an end is glued but not on its site),
  `dangling-glue` (glue to a missing object), `group-frame` (a child
  outside its group's frame).
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
| alias | `$name`, defined by an earlier op's `as` in the same plan | all | for the plan |

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

Text content is read and written as HTML: pptx text bodies and docx
`markup`, both written in plans as `html`.

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
  made worse, and is not repaired.)
- Content no op targeted is unchanged, byte for byte. There are two
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
  main part (no content types, or no document, presentation or
  workbook part), or trips a safety limit (size, ratio, entry count,
  nesting depth). **xlsx**: also a binary workbook (`.xlsb`), a legacy
  `.xls` and an encrypted workbook. `.xlsm` and `.xltx` are read and
  edited; a VBA project is copied through unchanged.
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
  the binary: `pptx`, `docx` or `xlsx`).
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

## 13. Implementation notes (not for agents)

- One decoder in `internal/cli` reads plans for both tools: it carries
  the path of every value, detects duplicate keys, collects every
  document-independent error, and has no serde wording or number
  emulation.
- The pptx strict text parser is deleted only after the pptx text
  round-trip law passes on the corpus through the HTML5 parser, with
  tokenizer tests added; HTML errors carry the WHATWG code and a span,
  not Rust debug text.
