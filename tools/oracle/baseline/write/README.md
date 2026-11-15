# docx write baseline

What docxdriver 0.5.2 (the parity oracle, `../README.md`) does with every
write operation, recorded before any operation is ported, for the P5
waves to compare the Go `docx apply` against.

## Commands

    go run ./tools/oracle write fixtures   # rebuild testdata/write/*.docx (byte-reproducible)
    go run ./tools/oracle write sites      # scan docxdriver's commands/ into sites.tsv
    go run ./tools/oracle write gen        # pick sources, write plans/ and sources.tsv
    go run ./tools/oracle write record     # run every plan through docxdriver: results/, coverage.tsv
    go run ./tools/oracle write compare    # run the plans through the Go CLI and compare

`make oracle-write-gen`, `make oracle-write-record` and `make
oracle-write-compare` wrap them; `OPS=format_paragraph,set_page_margins`
narrows record and compare to those operations (docxdriver op names), as a
P5 wave does. Further filters: `-kinds positive,dirty`, `-plan <substring
of id or source>`, `-modes track`. A partial record replaces only the
selected results. `compare` builds `./cmd/docx` unless `-docx` names a
binary, writes `results/oracle/write-compare.tsv` and, for every
differing case, both sides' outputs (indented) under
`results/oracle/write-compare/<case>/<mode>/{oracle,go}/`; the record
keeps no output texts, so triage runs the oracle again on that case.
Cases the Go CLI cannot run yet (no `apply`, or an operation that is not
implemented) count as `not-implemented`, which passes unless
`-require-implemented`.

Record and compare need the pinned oracle CLI (`bin/oracle-target`, or
`-oracle`/`$DOCX_ORACLE`), the docxdriver checkout and its `corpus/lo`
(`-docxdriver`, `-corpus`, as for the read baseline). Compare reads every
output, the Go CLI's included, with the oracle's reader. A full record
runs about 5,600 cases in roughly 7 minutes on 10 cores.

## Plans

`plans/<op>.jsonl` holds one case per line, filed under the docxdriver
operation it exercises (`text.insert`, which docxdriver lacks, has its
own file):

- `id`: `<op>/<kind>/<set>:<source>/<what it covers>`, or
  `<op>/rule/<n>` for the op table's rule n. Keyed by op and coverage,
  never by plan bytes or field paths, so ids survive regeneration.
- `ops` in the agent contract's form (`docs/contract.md`, version 1).
  `translate.go` turns them into docxdriver's plan (its §3 mapping table:
  addresses for sections, comments, bookmarks, equations, rows, columns
  and cells; `html` for `with`; `text.replace` with `all` runs as a
  `revise_text` edit with `all`; the `delete` field is dropped, since
  docxdriver deletes what the html leaves out; LaTeX equations become the
  paired MathML in `equationPairs`), and `toml.go` renders that as the
  TOML docxdriver's CLI reads. The generators write docxdriver shapes and
  translate them up; `TestTranslateRoundTrip` checks the round trip.
- `equivalent`: the contract ops the oracle runs instead, for what
  docxdriver cannot say. For `text.insert`, a `text.replace` of the span
  by itself plus the new text; the case is a law: in direct mode every
  projection must equal the equivalent's, in track mode the accept-all
  projections. For revision.accept and revision.reject by author or by
  paragraph (`settle by ...` cases), settling all when every revision of
  the source is that author's, else the selected listed revisions one id
  at a time, last first, rows last; every projection must be equal.
- `base` (`stale`), `plan` (whole plan text, `$BASE` and `$MODE`
  substituted) and `author` for the cases that need them.

Every case runs in `track` and `direct` mode. Kinds:

| Kind | What |
| --- | --- |
| positive | valid plans aimed at real targets from the oracle's read of each source: every op's main variants and option combinations, and the mode-sensitive cases on the write fixtures |
| dirty | the same generic plans on sources whose input already has integrity or schema findings (see below) |
| negative | valid plans that must be refused when applied: missing paragraphs, tables, rows, cells, sections, comments, bookmarks, revisions and equations, undefined or reused aliases, `select` not found or ambiguous, out-of-range occurrences, conflicting operations, track-mode refusals, a stale base, bad HTML |
| rule | one plan per validation rule with an example in the op table (`internal/docx/ops`, 149 of 151 rules; two are unreachable after decoding) |
| schema | each op's field shape broken from the op table (each required field missing, an unknown field, a wrong type) and the plan-level shapes |

Sources (`sources.tsv`: set, file, SHA-256, features): the authored
write fixtures `tools/oracle/testdata/write` (built by
`writefixtures.go`: `mixed.docx` with foreign authors' revisions, the
plan author's own pending insertions, property changes, comment ranges
over runs and across paragraphs with a reply, bookmarks, inline and
display equations, a line break, lists, a table with merges and tracked
rows, paragraphs without paraIds; `sections.docx` with three sections and
default, first and even headers and footers; `dirty.docx` with
pre-existing defects), `testdata/docx/rich.docx`, docxdriver's fixtures,
two corpus files per feature (merged cells, tracked rows, sections,
first/even stories, equations, comments and replies, bookmarks, two
revision authors, lists, missing paraIds, tables), the eight smallest
dirty corpus files, and, for the `revision_settle` sweep (P4c's live
differential, folded in), every small corpus file with revisions, all and
first id, accept and reject.

## Results

`results/<op>.jsonl`, one line per case and mode, holds only what is
compared and two tags:

| Field | Compared | Meaning |
| --- | --- | --- |
| `outcome` | yes | `accepted` or `refused` |
| `code`, `path` | yes | the refusal, as contract error code and plan path (`upCode`, `upPath` translate docxdriver's diagnostic; document refusals arrive as `operation_rejected` and the message says which code) |
| `proj` | yes | per projection, `exit digest` of the oracle's read of the output, relabelled (below): `doc`, `doc-final`, `doc-original`, `comments`, `revisions`, `bookmarks`, `styles`, and `accept-` and `reject-` `doc`, `comments`, `bookmarks` after the oracle's `revision-settle --target all` |
| `integrity` | yes | WML integrity findings the output introduced (`internal/docx/integrity`, docxdriver's checks) |
| `dropped` | dirty kind | input findings the output no longer has |
| `parts` | one way | parts the output changed; the Go output may change no part the oracle left alone |
| `class` | selects equivalences | `AcceptAuthored` (op class Exact), `OwnPath` (KeepsAdditions, EmptiesStory, ComplexField), `Same` (Untracked), `DirectOnly`, `TrackGuard` (track mode refuses the case) |
| `sites` | no | the docxdriver refusal sites whose message template matches (`sites.tsv`, `coverage.tsv`) |

Not compared, by the owner's decision (2026-11-13): message texts, CLI
wording, serde wording, report layout, exit codes, stderr, the preview
key and `canonical_toml`, the output's bytes. Each engine is reached
through an adapter (`oracleApplier`, `goApplier` in `writerun.go`); only
`goApplier` knows the Go CLI's surface (contract.md §1-2: `apply FILE
PLAN -o OUT`, errors on stderr; the clock from the hidden `--clock`).

Ids: revision, comment and bookmark ids in a projection are allocated in
the writing engine's order, an artefact. `relabel.go` renames them by
first appearance, per id space, on both sides before digesting.

## The dirty family

Checks may only stop the tool from introducing nonconformities; a
pre-existing one never blocks an edit or a save (contract.md §10). For
kind `dirty` compare asserts that the Go run saves, introduces no
finding, and drops none of the input's (each defect is left alone).
Where docxdriver refused such a file over a pre-existing defect
(`invalid_output` from its integrity gate, an unreadable source), that is
a docxdriver bug: the case counts as intended when the Go side meets the
rule. `tools/word-smoke` judges Word repair prompts relative to the
input the same way.

## Known differences

`../known/`, one file per case family (README there).

## Oracle bugs

`../known/write-oracle-bugs.tsv` lists what recording found wrong in docxdriver, with
evidence. The Go side fixes them; they are listed as intended
differences, never encoded as expected Go behaviour.
