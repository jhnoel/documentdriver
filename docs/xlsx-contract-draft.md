# xlsx contract amendments (draft for owner sign-off)

Status: **merged into `docs/contract.md` on 2026-11-14 (P2 verification), which is now the agent surface.** This file stays as the record of the amendments as drafted, the owner's decisions and the design approver's rulings, with their rationales. The merge kept `help schema` in §1's block (the draft predates it), added `xlsx apply` and `create` with `--recalc` to the xlsx command lines, noted `range.set`'s `null` as the one exception to §3's "`null` is not a value", and put the approved shapes under §4 ("xlsx apply and create details") and §8 ("xlsx read and find details").

The amendments are additive: the contract version stays `1` (§11 allows
new ops, fields, output keys, flags and hazard kinds without a bump).
Each entry below names the section of `docs/contract.md` it changes and
gives the exact text to merge: **Replace** swaps the quoted text for the
new text, **Add after** inserts the new text after the quoted anchor,
**Add row** adds rows to a table.

The source is the xlsx design (`docs/design.md`, section "xlsx"). Excel
behaviours the amendments rely on (the recalculation flag, inline
strings, the calcChain) are unverified until experiments E1 to E8 are
recorded there; the §9 exception in particular must not be signed off
before E1 and E2 are.

## Intro

**Replace**

> `pptx` and `docx` are two CLIs with one contract. This page is the whole
> of what an agent may rely on; anything not written here (whitespace,
> message wording, key order) may change. Format-specific parts are marked
> **pptx** or **docx**. `<tool>` is either binary.

**with**

> `pptx`, `docx` and `xlsx` are three CLIs with one contract. This page is
> the whole of what an agent may rely on; anything not written here
> (whitespace, message wording, key order) may change. Format-specific
> parts are marked **pptx**, **docx** or **xlsx**. `<tool>` is any of the
> binaries.

## §1. Commands

**Replace** the command block with

```
<tool> read  <file> [flags]              facts about the document; never writes
<tool> find  <file> <query> [flags]      docx: paragraphs containing text; xlsx: cells containing text
<tool> apply <file> <plan.json|-> [--dry-run] [-o <out>] [--changed ids|full]
<tool> create <doc.html|-> [--dry-run] -o <out>     docx: a new document from HTML
<tool> create [<plan.json|->] [--dry-run] -o <out>  xlsx: a new workbook, optionally with a plan applied
<tool> xml   <file> <address>            pptx: source bytes of one object
<tool> render <file> ... -o <out.png>    pptx: picture of a slide
<tool> help [<command>|ops|<op>|html|values]   same text as --help
<tool> --version
```

**Add after** the block

> **xlsx** has `read`, `find`, `apply`, `create`, `help` and `--version`;
> there is no `xml` or `render`. Its help topics are `ops`, `<op>` and
> `values` (the cell value forms of §7a); it has no `html` topic.
>
> ```
> xlsx read  <file> [--range A] [--sheet sheet:Name] [--view cells|values|formulas] [--group] [--detail styles] [--kind workbook|names|tables|styles|comments] [--limit n]
> xlsx find  <file> <query> [--ignore-case] [--sheet sheet:Name] [--view values|formulas] [--limit n]
> ```

## §3. Plans

**Add row** to the plan field table (no new plan-wide field; this row
only states that xlsx has none of the format settings):

| Field | Tool | Required | Meaning |
| --- | --- | --- | --- |
| (none) | xlsx | – | xlsx plans have only `ops`, `base` and `contract` |

**Add row** to the verb table

| Verb | Meaning |
| --- | --- |
| `clear` | empty the addressed content, keeping its formatting (xlsx `range.clear`) |

**Add after** the docx operation table (before "`text.insert` is new")

> **xlsx**:
>
> | Noun | Operations |
> | --- | --- |
> | `cell` | `cell.set`, `cell.merge`, `cell.split` |
> | `range` | `range.set`, `range.clear`, `range.format` |
> | `text` | `text.replace` |
> | `row`, `column` | `row.insert`, `row.delete`, `row.format`, `column.insert`, `column.delete`, `column.format` |
> | `sheet` | `sheet.add`, `sheet.rename` |
> | `name` | `name.add`, `name.set`, `name.delete` |
>
> | Op | Fields |
> | --- | --- |
> | `cell.set` | `at` (a cell), `value` |
> | `range.set` | `at` (a range, or its top-left cell), `values` (rows of values, all the same length; `null` leaves the cell empty, `""` writes an empty string) |
> | `range.clear` | `at` (a range): empties content, keeps formatting |
> | `text.replace` | `at` (a range or a sheet), `select`, `with`, `occurrence` or `all` |
> | `row.insert`, `column.insert` | `at` (one row `Budget!5:5`, one column `Budget!C:C`), `position` (`before`, `after`), `count` (default 1) |
> | `row.delete`, `column.delete` | `at` (rows or columns) |
> | `row.format`, `column.format` | `at`, `height` (rows) or `width` (columns), `hidden` |
> | `range.format` | `at`, `number_format`, `bold`, `italic`, `underline` (single, a boolean), `color` and `fill` (`RRGGBB`), `align` (`general`, `left`, `center`, `right`, `fill`, `justify`, `centerContinuous`, `distributed`), `valign` (`top`, `center`, `bottom`, `justify`, `distributed`), `wrap`; at least one |
> | `cell.merge`, `cell.split` | `at` (the range; for `cell.split`, the merged range) |
> | `sheet.add` | `name`, `as`; the sheet is appended |
> | `sheet.rename` | `at` (a sheet), `name` |
> | `name.add` | `name`, `refers_to` (`=` and a formula), `scope` (`workbook`, default, or `sheet:Budget`) |
> | `name.set` | `at` (a name), `refers_to` |
> | `name.delete` | `at` (a name) |
>
> A structural op (`row.*`, `column.*` insert and delete, `sheet.rename`)
> adjusts the references that point at what it moved, from a closed list
> (`help <op>` gives it): cell formulas on every sheet, defined names,
> merges, and the other references the help lists. A shift that would
> cut a construct the tool cannot adjust (a pivot table, a multi-cell
> array formula, a data table, a table crossed by a column shift, an
> extension list reference at or after the shift, or a reference to the
> sheet from another workbook) is refused with `operation_failed`, and
> `detail` names the construct. Anything not on either list is never
> touched.

**Add rows** to the shared field vocabulary

| Field | Meaning |
| --- | --- |
| `value` | xlsx: one cell value, in the form of §7a |
| `values` | xlsx: rows of cell values (a list of lists, top row first) |
| `name` | the name being given (a new sheet, a renamed sheet, a defined name, a renamed bookmark) |
| `count` | how many things an `insert` op creates (rows, columns) |

**Replace**

> - Lengths are numbers in points unless the plan sets `units` (pptx).

**with**

> - Lengths are numbers in points unless the plan sets `units` (pptx).
>   **xlsx** row heights are points; column widths are Excel's character
>   widths, as `read` shows them.

## §4. Results

**Replace**, in the `changed` row of the result table,

> docx: a paragraph as `{id, html}`; a table (`table:n`) whose structure or format changed, a section whose properties changed and a comment thread, each as `read` lists it.

**with**

> docx: a paragraph as `{id, html}`; a table (`table:n`) whose structure or format changed, a section whose properties changed and a comment thread, each as `read` lists it. xlsx: each targeted range read back as `{id, cells}` (`id` the range, `Budget!A1:D3`; `cells` as `read --range` shows them); each sheet whose structure changed or whose references were adjusted as `{id, formulas_adjusted}` (`id` the sheet, `formulas_adjusted` the number of formulas rewritten); each defined name as `read --kind names` lists it.

**Replace**, in the `deleted` row,

> | `deleted` | addresses that no longer exist |

**with**

> | `deleted` | addresses that no longer exist. xlsx: deleted rows and columns (`Budget!5:7`, `Budget!C:E`) and names |

**Add after** the docx `create` paragraph

> **xlsx** `create` prints `{"dry_run", "saved", "new_base"}` as docx
> does. It makes a blank workbook with one sheet, `Sheet1`. Given a plan,
> it applies the plan to that workbook first and also prints the plan's
> `changed`, `deleted`, `aliases` and `hazards`; the plan must not have
> `base`. With `--dry-run` it checks the plan and writes nothing.

**Add after** the docx hazard kinds

> - **xlsx**: `stale-values` (cached values may be out of date: the save
>   asked Excel to recalculate, §9; `new` is true on the plan that first
>   set the request), `ref-error` (a delete turned a reference into
>   `#REF!`; `id` is the cell or name), and the findings of the integrity
>   check on the parts the plan touched, which are pre-existing (`new`
>   false), as in docx.

## §6. Addresses

**Add rows** to the address table

| Kind | Form | Tool | Stable across edits |
| --- | --- | --- | --- |
| sheet | `sheet:Budget` (the sheet's name) | xlsx | until renamed |
| cell, range | `Budget!B2`, `Budget!A1:D10`; a name that needs quoting is quoted as Excel does, `'Q1 Budget'!A1`, `'Bob''s'!A1`. Always sheet-qualified; `$` is refused | xlsx | positional |
| rows, columns | `Budget!5:7`, `Budget!C:E` | xlsx | positional |
| name | `name:Tax_Rate` (workbook scope), `name:Budget!Local` (sheet scope) | xlsx | yes |
| table | `table:Sales`, `table:Sales/column:Amount` | xlsx | yes |
| opaque content | `sheet:Budget/opaque:2` (1-based, in the sheet): a chart, image, pivot table or other object the tool does not edit | xlsx | positional |

**Replace**, in the alias row,

> | alias | `$name`, defined by an earlier op's `as` in the same plan | both | for the plan |

**with**

> | alias | `$name`, defined by an earlier op's `as` in the same plan | all | for the plan |

**Add after** the "Sections" bullet

> - **Tables.** `table:` takes a number in docx (`table:1`, positional)
>   and a name in xlsx (`table:Sales`, the table's stored name).
> - **xlsx cells.** References are A1 only; there is no R1C1. A sheet
>   name is quoted when Excel would quote it, with `''` for a quote
>   inside it. `read --range` also takes a `name:` or `table:` address
>   that resolves to one plain range; ops take A1 forms only. A row or
>   column insert renumbers what follows it, so later ops in the plan see
>   the new positions.

## §7a. Cell values (new section, after §7)

**Add** a new section after §7:

> ## 7a. Cell values
>
> **xlsx** has no HTML: a cell's content is a JSON value, in the same
> form in `read` output and in plans (`help values`).
>
> | Value | Form |
> | --- | --- |
> | number | a JSON number, in the shortest form that reads back to the same double. NaN and infinities are refused |
> | string | a JSON string; up to 32,767 characters. A string that starts with `=` is written `{"text":"=literal"}` |
> | boolean | `true`, `false` |
> | error | `{"error":"#N/A"}` (one of Excel's error values) |
> | date and time | `{"date":"2024-03-01"}`, `{"date":"2024-03-01T13:30:00"}`, `{"time":"13:30:00"}` |
> | formula | in a plan, a string starting with `=` (`"=B2+C2"`), up to 8,192 characters. In `read`, see below |
> | empty | in `range.set`, `null` leaves the cell empty; `""` is the empty string (owner decision, 2026-11-14) |
>
> - **Dates** are derived from the cell's number format and the
>   workbook's date system (1900 or 1904). A serial number outside the
>   range a date can show stays a number. A date written into a cell with
>   no date format gets the built-in date format 14.
> - **Formulas in `read`.** The default view, `cells`, shows a formula
>   cell as `{"f":"=B2+C2","v":2400}`, with `v` the value the file stored
>   and left out when it stored none. `--view values` shows the stored
>   value only, `--view formulas` the formula only. A cell that shares a
>   formula with others shows its own formula, written out. Function
>   prefixes the file stores (`_xlfn.`) are not shown, and are added
>   when a plan writes the function.
> - Existing dynamic-array spills are read. The anchor shows its formula
>   normally; each non-anchor cell is `{"v":3,"spill":"Budget!A1"}`
>   (the anchor address). Legacy CSE array-formula members are marked the
>   same way; help distinguishes CSE arrays from dynamic spills. Which
>   functions spill is determined by `formula.IsDynamicArray`. New spill
>   formulas are refused by writes, and a shift that cuts a spill is
>   refused.
> - A malformed formula is refused with `invalid_operation`, path
>   `ops[n].value` (or the `values` element).
> - Rich text in a cell is read as its plain text; `text.replace` on a
>   rich-text cell is refused with `operation_failed`.

## §8. Reading

**Add after** the docx bullet

> - **xlsx** `read <file>` gives the workbook summary:
>   `{"base","sheets":[{"id","name","state","dimension","tables","opaque"}],"names","tables","notes"}`.
>   `dimension` is the range the file stores; the used range is shown
>   only when a sheet is read. `--sheet` or `--range` gives cells,
>   keyed by address:
>   `{"base","sheet","range","view","cells":{"A1":"Item","D2":{"f":"=B2+C2","v":2400}},"merges":[…],"notes":[]}`.
>   `--view cells|values|formulas` (§7a); `--detail styles` adds
>   `styles`, a list of `{"at", <format fields>}` regions;
>   `--kind workbook|names|tables|styles|comments`.
> - **xlsx** `read` returns at most `--limit` cells (default 10,000; `0`
>   is no limit), cut at a row boundary. A cut read has
>   `"truncated":{"next":"Budget!A501:D1000"}`, the range to read next,
>   and a note.
> - **xlsx** `read` has `"values_stale": true` when the workbook asks to
>   be recalculated on open (§9): stored values may be out of date.
> - **xlsx** `read` lists what it does not model as `opaque`
>   (`{"id":"sheet:Budget/opaque:1","kind":"chart","range":"Budget!F2:M20"}`).
>   Sheet kinds: `chart`, `image`, `shape`, `pivot_table`,
>   `conditional_formatting`, `data_validation`, `sparkline`, `slicer`,
>   `control`. Workbook kinds: `vba_project`, `external_link`,
>   `connection`, `custom_xml`.
> - **xlsx** `find <file> <query>` gives `{"base","matches":[…]}`: the
>   cells whose text (`--view values`, default) or formula
>   (`--view formulas`) contains the query.

## §9. What the tool promises about a document

**Replace**

> - Content no op targeted is unchanged, byte for byte. The one exception:
>   a docx save writes the `w14:paraId` of every paragraph that lacked one
>   (§6), so that addresses stay stable.

**with**

> - Content no op targeted is unchanged, byte for byte. There are two
>   exceptions:
>   - a docx save writes the `w14:paraId` of every paragraph that lacked
>     one (§6), so that addresses stay stable;
>   - an xlsx save that changes a value, a formula or the structure of a
>     sheet asks Excel to recalculate the workbook on open
>     (`fullCalcOnLoad`), and removes the calculation chain
>     (`calcChain.xml`) when a formula cell is added, removed or moved.
>     The tool cannot recalculate: it never writes a value it computed,
>     and the stored values of other cells are left as they were, so
>     until Excel recalculates they may be out of date. The result
>     reports the `stale-values` hazard and `read` shows `values_stale`.

## §10. Documents with defects

**Replace**

> - **`invalid_document`** is only for a file that is not a zip, has no
>   main part (no content types, or no document or presentation part), or
>   trips a safety limit (size, ratio, entry count, nesting depth).

**with**

> - **`invalid_document`** is only for a file that is not a zip, has no
>   main part (no content types, or no document, presentation or
>   workbook part), or trips a safety limit (size, ratio, entry count,
>   nesting depth). **xlsx**: also a binary workbook (`.xlsb`), a legacy
>   `.xls` and an encrypted workbook. `.xlsm` and `.xltx` are read and
>   edited; a VBA project is copied through unchanged.

**Replace**

> - **Strict OOXML** (ISO/IEC 29500 Strict namespaces) is opened, read and
>   edited like Transitional; new content is written in the namespaces the
>   part already uses, and the file is never converted.

**with**

> - **Strict OOXML** (ISO/IEC 29500 Strict namespaces) is opened, read and
>   edited like Transitional; new content is written in the namespaces the
>   part already uses, and the file is never converted. **xlsx**: a Strict
>   workbook's ISO 8601 date cells (`t="d"`) are read as dates.

## §11. Versioning

**Replace**

> prints `{"tool":"docx","version":"<release>","contract":1}`.

**with**

> prints `{"tool":"docx","version":"<release>","contract":1}` (`"tool"` is
> the binary: `pptx`, `docx` or `xlsx`).

## Owner decisions (signed off 2026-11-14)

These decisions are binding.

1. `read`'s grid is a JSON map keyed by cell address (`"B2": 1200`).
2. Default view is `cells`; formulas show as `{"f":"=…","v":…}`, omitting `v` when no cached value exists. `values` shows cached values only; `formulas` formulas only.
3. Display formulas in Excel form (`A1#`, `[@Col]`, `FILTER(...)`): strip `_xlfn.`, `_xlfn._xlws.`, `_xlpm.` and related prefixes. Never show stored form. Materialise shared-formula children with `formula.Translate`; use `internal/xlsx/formula` only.
4. Repeated formulas list per cell by default. Grouping is opt-in only in `--view formulas` (document `--group` in help).
5. Hidden rows and columns are reported in `hidden`, beside `merges`, e.g. `["Budget!5:7","Budget!C:C"]`.
6. `--limit` defaults to 10,000 non-empty cells, cuts at a row boundary, and reports `"truncated":{"next":"Budget!A501:D1000"}` plus a note; `--limit 0` is unlimited.
7. Recalculation is a write concern. Read sets `"values_stale":true` when the workbook has `fullCalcOnLoad`; it never computes values.
8. Existing dynamic-array spills are read; each non-anchor cell is `{"v":3,"spill":"Budget!A1"}`. The anchor shows its formula normally. Legacy CSE array formulas are also read and their non-anchor cells marked the same way (help distinguishes them). Spill functions are determined by `formula.IsDynamicArray`. New spill formulas are refused; shifts cutting spills are refused.
9. Dates are `{"date":"2024-03-01"}`, `{"date":"…T13:30:00"}`, or `{"time":"13:30:00"}`, derived from number format and date1904. Durations such as `[h]:mm` and 1900-system serials below 61 stay plain numbers plus a note. The format picks the form; when the stored serial has a part the form leaves out (a time of day under a date format, whole days under a time format) a note says so (approver ruling, 2026-11-14).
10. Addresses use `sheet:Name`; cells and ranges always include a sheet and Excel quoting (`'Q1 Budget'!A1`, with `''` escaping). `$` is refused; no R1C1. Rows use `Budget!5:7`, columns `Budget!C:E`. `name:` and `table:` are accepted by `read --range` only when resolving to one plain range.
11. New text is written inline, sst untouched; its count may go stale. `E3` requires Excel.
12. Structural operations refuse with `operation_failed` naming a construct from a closed list.
13. `cell.set` on a table header is refused in v1.
14. The new verb `clear` is accepted; `create` takes a plan.
15. Safety limits are `opc.DefaultLimits`; corpus limit trips are reported.

## Approved shapes (design approver for the owner, 2026-11-14)

A1 implements these. They were proposed by the A1 design, and the design
approver approved every one as listed, for the owner, on 2026-11-14.
Each has a one-line rationale. Items marked **new** were added by the A1
design (internal/xlsx/model and internal/xlsx/read hold the types; their
doc comments are the precise rules).

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

## Decided by the owner (P2)

- **`""` and `null` in plans (signed off by the owner, 2026-11-14).** In
  `range.set`, `null` leaves a cell empty and `""` writes an empty
  string, so a value `read` shows (`""` for an empty-string cell) writes
  back as itself. `cell.set` takes no `null` (use `range.clear`). The
  code keeps this as one switch point (`ops.EmptyFromNull`).

## P2 write-side shapes (approved, design approver for the owner, 2026-11-14)

The P2 design (internal/xlsx/ops, internal/xlsx/apply) adds these
agent-visible shapes; the design approver approved them for the owner
with the amendments folded in here. The doc comments of the types are
the precise rules.

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
