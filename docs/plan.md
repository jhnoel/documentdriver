# documentdriver: monorepo plan

One Go module, standard library only, that ships two CLIs: `docx` (a port of
docxdriver 0.5.2) and `pptx` (pptxdriver, moved in). Both share one OOXML engine.
MCP, WASM, Python and Monty bindings are out of scope; agents call the binaries.

## Why

- Supply chain: docxdriver ships 236 crates.io crates across its targets
  (core alone pulls 50 on the host, MCP 140). The Go module would have none.
- pptxdriver already has the hard parts in stdlib Go: a lossless XML tree
  (`internal/ooxml/tree`), a lazy, copy-through, atomic OPC package
  (`internal/opc`), a schema-ordered insert table generated from ECMA-376, and
  an architecture test that enforces them.
- Both tools have the same contract (`docs/contract.md`): ordered JSON
  plans, all-or-nothing, `--dry-run`, an edit result with hazards, and
  untouched content left byte-for-byte alone.

## Decisions

| Question | Decision | Reason |
| --- | --- | --- |
| Starting point | This repo (started with the plan), with pptxdriver merged in with its full history | pptxdriver already builds, passes `make check`, and holds the shared engine; merging keeps `git log --follow` and blame on its files |
| docxdriver history | Not imported. The Rust repo stays read-only as the reference and parity oracle | None of the Rust code carries over; tooling is copied with its source SHA noted |
| Module | `github.com/jhnoel/documentdriver`, one `go.mod`, no `require` lines | Stdlib only, enforced by `make norequire` |
| `golang.org/x/image` | Removed: the render overlay draws its labels with an embedded 7x13 bitmap font | It is used only for `basicfont.Face7x13` in `render/overlay.go` |
| Binaries | Two: `docx` and `pptx` | Narrow tools suit agents; each has its own read model and ops |
| Agent contract (2026-11-13, decision 1) | One contract for `pptx` and `docx`, `docs/contract.md`, designed before the docx operations are written; pptx moved to it. Greenfield: neither the pptxdriver surface nor the docxdriver 0.5.2 surface is a constraint, and the binaries take only the contract's plan grammar. This replaces the earlier decision to keep the plan schemas separate and the 0.5.2 plan JSON unchanged | Nothing has shipped and no one depends on either CLI. One vocabulary (`at`, `as`, `select`, `with`, `html`), one envelope, one errors object and one address model are cheaper for an agent than two, and the 34 operations had not been written yet. `tools/oracle` translates plans and results for parity runs (the mapping table is in the contract) |
| Parity with docxdriver (2026-11-13, decision 2) | Narrowed to three things: what the output document means; the content of read and find results; and whether a plan is accepted or refused, with the error code and field path. Exact clap, serde, stderr and Rust-Debug wording is not a goal. The oracle's surface warts are bugs, fixed here and listed: mixed camelCase and snake_case keys, comment anchors shaped differently in `--kind document` and `--kind comments`, the working-directory path rule, the `outcome` wrapper, preview keys | Wording parity cost a clap emulation and a serde-compatible parser and bought nothing an agent sees. `encoding/json` is allowed; only `encoding/xml` is banned outside `eval/` |
| Documents with defects (2026-11-13) | Checks only stop the tool from introducing nonconformities. A defect the input already had never blocks a read, an edit elsewhere or the save; a part or object that cannot be read is a note; an operation refuses only when its own target is unreadable, at that operation (`unreadable_target`), never at save time. Strict OOXML is read and edited in its own namespaces | Real files have latent defects the tool did not make; refusing them makes the tool useless on exactly the documents agents are given (`docs/contract.md`, section 10) |
| HTML input (2026-11-13) | HTML5 syntax, strict meaning, through one front end (`internal/htmlx`, `ParseHTML` with `Strict`): a tokenizer error refuses; only repairs that cannot change meaning are applied (optional end tags, void elements, character references); anything a format cannot store is refused, never dropped. Both tools use it. For docx this refuses 137 of the 624 recorded docxdriver inputs that docxdriver accepted, or refused for another reason (stray end tags, unclosed elements, comments): the golden set records them as the difference class `strict-input` | Agents write HTML5, not XML; silent repair hides their mistakes. One policy for both tools replaces a strict XML-like grammar (pptx) and a lenient HTML5 one (docx) |
| Canonical TOML plan | Replaced with canonical JSON | Only the Pi formatter and tests use TOML; removing it removes the dependency |
| Per-op docx subcommands (`replace-text`, …) | Not ported at first | The skills drive `apply` with plans, not the per-op commands; add later from the op table if needed |
| JSON Schema for plans | A small reflect-based generator under `tools/`, output committed, drift test in `make check` | Replaces `schemars` |
| Equation format | LaTeX only (the Markdown/MathJax dialect), converted to and from OMML directly with a lossless round-trip law; MathML is dropped. Equations are `<equation display="inline\|block">` in HTML and `latex` in `replace_equation` | One format, not two. docxdriver's own benchmark: LaTeX used 27% fewer tokens than MathML on the same equation task with equal success, and equation reads were 3.4x smaller. Agents know LaTeX from Markdown. Breaks the 0.5.2 `mathml` plan field, recorded as an intended difference |
| Conformance tooling | Copied as is (Python + lxml, .NET Open XML validator, Word smoke) | Dev-only; never shipped |
| Python access | The `documentdriver` wheel: the binaries plus a pure-Python runner (`run`, `EngineError`, `binary`), shipped in `py3-none-<platform>` wheels (RELEASING.md) | No cgo; one wheel per platform covers every Python |
| Portability guard | `make wasip1` builds `GOOS=wasip1` | Keeps a WASM build possible without committing to it |

## Layout

```
documentdriver/
  go.mod                        no require lines
  Makefile                      check = norequire + vet + test + gofmt; ci adds lint, vuln, race, repro, wasip1
  cmd/pptx/                     read, apply, xml, render (and the hidden debug)
  cmd/docx/                     read, find, apply, create
  internal/cli/                 the agent contract's shared code: command grammar, errors object,
                                exit codes, plan envelope and decoder, generated help
  internal/opc/                 zip package: lazy reads, copy-through, atomic save, limits
  internal/ooxml/tree/          lossless XML tree
  internal/ooxml/edit/          edit core: handles, schema-placed inserts, codecs, units
  internal/ooxml/schema/        generated child order and values: wml, pml, dml, shared
  internal/ooxml/ctypes/        content and relationship types taken from Office-authored files
  internal/plan/                Tx, write set, verifier, edit result, hazards, dry run, atomic save
  internal/ids/                 the one source of fresh ids and the clock
  internal/htmlx/               the HTML front end of both tools
  internal/laws/                the format-neutral law harness (tests only)
  internal/archtest/            one rule set for both formats
  internal/testdeck/            pptx fixture decks and plan helpers for tests
  internal/pptx/                model, derive, show, view, ops (edit, prop, text), apply, render,
                                laws, coverage
  internal/docx/
    model/                      package, paragraphs, segments, numbering, styles, paragraph ids
    read/                       read and find results
    view/                       the HTML projection of a document
    html/                       HTML input: parse and lower into items
    math/                       OMML <-> LaTeX
    settle/                     accept or reject revisions on a private copy (final and original views)
    ops/                        the plan grammar: op table, decoding, validation, canonical form
    integrity/                  WML checks and the save gate
  testdata/{decks,docx,soffice}/
  eval/                         pptxbench: corpus, tasks, graders, arms, the Pi extension, coverage report
  tools/
    conformance/                ooxml-conformance.py, ooxml_integrity.py, ooxml-schemas/
    openxml-validator/          .NET Open XML SDK validator
    oracle/                     runs docxdriver 0.5.2 and the Go docx side by side
    pptoracle/                  PowerPoint as the oracle for "opens without repair"
    schemagen/                  docx plan JSON Schema and operation reference
    ctypesgen/, coverage/, corpusdir/, docxintegrity/
  corpus/                       gitignored
  docs/                         contract.md (the agent contract), plan.md, design.md (engine and pptx,
                                with every recorded decision), docx-plan.md and its schema,
                                docx-integrity.md
  AGENTS.md                     rules for contributors and agents
```

Architecture rules carried over and extended to docx:

- No `encoding/xml` outside `eval/`; OOXML goes through `internal/ooxml/tree`.
- Tree mutation only in `opc` and the edit layer; inserts only through
  schema-placed helpers that read the generated child order.
- Writes go through `internal/plan` transactions, which verify the declared
  write set at commit.
- Never serialise a read model back into OOXML.
- `internal/pptx` may be imported only from inside `internal/pptx`, from
  `cmd/pptx`, and from the dev-only `eval/` and `tools/`; likewise for
  `internal/docx` with `cmd/docx`. The shared engine never depends on a
  format.

## Phases

Every phase ends green on `make check`, the conformance gate, and (on macOS)
the Word or PowerPoint smoke test.

### P0. Freeze the oracle

- Tag docxdriver `v0.5.2-oracle`; build the release CLI.
- Record the baseline: conformance results for every source × operation ×
  mode, read and HTML projection output for fixtures and `corpus/lo`.
- Decide the clock and id seeds the parity runs use (revision dates,
  `w14:paraId`, durable ids) so both engines produce comparable output.
  Decided for the Go engine: `pptx apply --seed N --clock RFC3339` (seed
  default 0, clock default 2000-01-01T00:00:00Z) feeds `internal/ids.Fixed`;
  the oracle runs pass the same pair. Ids derive from SHA-256 of seed and
  call order. docx ids are content-derived instead (see Architecture
  refinements), so the docx side takes only the clock.

Exit: baseline files committed under `tools/oracle/baseline/`.

### P1. Bootstrap the repo

- Merge pptxdriver's `main` into this repo with
  `git merge --allow-unrelated-histories`, keeping its files where they are
  in that commit. Its `docs/design.md` and this plan do not collide.
- In separate commits after the merge, so history follows each file: rename
  the module path to `github.com/jhnoel/documentdriver`, then move the files.
- Move pptx packages under `internal/pptx/` and `cmd/pptx`; keep `opc` and
  `ooxml/tree` at the top of `internal/`.
- Replace `x/image` in the render overlay; delete the `require`.
- Merge AGENTS.md; keep the `.worktrees/` and `wt/<task>` workflow.
- Local gate: `make ci` (plain git hooks under `.githooks`, installed by
  `make hooks`): `make check`, lint, vuln, race, repro, `GOOS=wasip1` build, a check that
  `go.mod` has no `require`.

Exit: pptx tests green and one fixed pptxbench bundle scores the same as
before the move.

### P2. Shared engine

- Extend the schema generator to `wml.xsd` and emit one table per vocabulary.
- Lift the transaction, write set, result and hazard types from
  `internal/pptx/ops` into `internal/plan`; pptx keeps working on top.
- Move content and relationship types into `internal/ooxml/ctypes`, filled
  from Word- and PowerPoint-authored files; `ooxml_integrity.py`'s
  `EXPECTED_CONTENT_TYPES` checks against the same table.
- Bring in the conformance gate and run it over pptx test decks (the Open
  XML SDK validator covers PresentationML too).

Exit: the gate is green on pptx testdata, and pptx outputs are unchanged.

### P3. docx read side

Port in this order: package and `document.rs`, `segments.rs`, `read`/`find`,
then the HTML projection (`render`, `parse`, `build`, `ctx`, `styles`,
`surface`, `compact`, `chrome`, `input`, `assets`) and the equation
converters (OMML to and from LaTeX; MathML is dropped, see Decisions).
That is roughly 12k lines of Rust.

Exit: read, find and HTML content is identical to the oracle on fixtures and
`corpus/lo`, apart from the listed differences. Reading is deterministic, so
the bar is exact match of content; key spelling, the envelope and attribute
names follow `docs/contract.md`, and `tools/oracle` maps between the two.

### Architecture refinements (2026-11-13)

**Never inherit the oracle's bugs.** Where docxdriver is wrong, the Go side
does the right thing and the difference is listed with its evidence in a
known-differences file that the comparator reads; anything unlisted still
fails. Where docxdriver is right, parity is exact.

Parity is judged at public boundaries only, and (decision 2) on three
things: the content of read and find results; whether a plan is accepted
or refused, with its code and field path; and what the output document
means (read through the oracle's own reader). Wording, the envelope and
exit codes follow `docs/contract.md`, not docxdriver. Internal structure is
free. That lets the Go side share machinery the Rust side repeats:

- **One read model, three views.** `internal/docx/model` is read once; read,
  find and the HTML projection are views over it, as on the pptx side.
- **No private HTML dialect.** Rust lowers HTML into a private atom string
  and re-parses it; Go lowers straight into the parsed item types.
- **One HTML front end** (`internal/htmlx`, `ParseHTML`) for pptx text and
  docx input: HTML5 syntax, with a strict mode that applies only the
  repairs that cannot change meaning (see Decisions). Both tools parse in
  strict mode; each format keeps its own lowering.
- **A shared edit core and law harness** lifted from the pptx edit layer, so
  docx operations reuse handles, schema-placed inserts, write-set
  declaration, codecs and the laws.
- **Operations as compositions of six primitives**: resolve a handle,
  select a span over segments, isolate it, insert/delete/re-property it,
  author the change, settle. Ported once, not per op (the design study
  estimates about 12-14k Go lines against 18-19k for a straight port).
- **Track changes as one layer.** A `Reviser` with Track and Direct modes;
  Direct is Track followed by accepting the recorded revisions, except for
  the ops whose class says otherwise (below). Settle and direct mode share
  the accept code.
- **One op table** drives decoding, validation, canonical JSON, the plan
  JSON Schema, the op reference docs, negative-case generation for the
  oracle, and each op's change-mode law.
- **Integrity at save time covers semantics only.** The round-trip,
  container and XML-level checks are subsumed by the lossless tree,
  copy-through and write-set verification; the gate evaluates changed parts
  and their cross-referenced partners.

Facts the design study found that change earlier assumptions:

- docx ids are content-derived, not seeded: inserted paraIds come from
  `sha256(source) | sha256(canonical_json(plan)) | op index | slot`. The
  docx side needs only the clock from `internal/ids`, and canonical JSON
  must match serde byte for byte.
- The oracle CLI reads plans as TOML; `tools/oracle` renders JSON plans to
  TOML for it. The Go CLI takes JSON only.
- Committed plan output carries `bytes` and `canonical_toml`, which the
  comparator drops as intended differences.
- Every plan report embeds post-edit HTML, so write-side CLI parity rests on
  the P3 HTML projection.
- New run properties are placed in the schema's declaration order (Word's
  order). Existing order is never changed or flagged.
- LaTeX cannot carry an equation's run formatting (`w:rPr`, `m:ctrlPr`,
  grow, justification: 99 of 138 corpus equations have some). So the
  edit side never re-converts an equation it was not asked to change: an
  `<equation>` in replacement HTML that carries an existing equation's
  `data-docx-equation` identity and unchanged canonical LaTeX keeps its
  original OMML bytes, and only new or changed equations are converted.
  A raw-OMML escape inside LaTeX is not added.
  Run formatting (fonts, size, kerning, colour, language, `m:ctrlPr`) is
  not part of the LaTeX surface and is not reported as a loss on read;
  only content losses are (`data-docx-loss`, `equations[].loss`). A
  replaced equation's new runs inherit the replaced equation's most common
  `w:rPr` (`math.RunProperties`, `math.LaTeXToOMMLWith`).
- docxdriver counts equations and `w:br`/`w:cr` as zero characters in read,
  find and selection, so a direct `replace_text` whose span crosses one
  silently deletes the equation or break (track mode keeps it, so the modes
  disagree). The Go text model counts each equation as U+FFFC and each break
  as `\n`, the same in read, find and selection, and the span engine treats
  them as objects a selection may contain but never silently drops. This is
  an intended difference in read and find output.

### P4. docx write foundations

- **P4a engine additions:** docx units for `plan.Verify` (body, paragraph,
  table, row, cell, section, header/footer roots); write-set kinds for
  namespace declarations on the root and repaired paraIds; recording of
  created nodes; ctypes entries for comment and header parts.
- **P4b plan contract for all 42 contract operations at once** (the
  contract's names and fields, `docs/contract.md` section 3; the 34
  docxdriver operations they run as are a record, `ops.OracleOps`): op
  table, decoding through the shared decoder (`internal/cli`), validation
  (each rule names the field that is actually wrong; Rust guesses it from
  the message text, which is a bug), canonical JSON, schema generation,
  fold, aliases, report, `create`. No preview key reaches an agent: `apply`
  runs once, and a refusal writes nothing.
- **P4c revision core:** `Reviser`, settle, and `revision_settle` as the
  first op.
- **P4d write baseline:** generate and record oracle results for every
  operation in track and direct mode before porting any: positive plans
  from the oracle's read output, targeted negative plans, one plan per
  validation rule (`tools/oracle/baseline/write`).
- The WML integrity checks (`integrity/*`).

Exit (met 2026-11-13, P4 close-out):

- invalid-plan parity for all 42 operations, ported or not: accept or
  refuse, the code and the path (never wording), on the negative, rule
  and schema kinds of the write baseline, every difference listed with
  its reason (`tools/oracle/baseline/known/write-plan.tsv`: 0 unexplained
  of 2,486 runs) and the parse-level ones in `ops.Differences`;
- schema drift clean (`TestCommittedOutputs`);
- a plan whose operations change nothing writes nothing, bytes and mtime
  (`TestNoOpPlanWritesNothing`; this replaces the earlier "empty plan"
  criterion, which the contract refuses before it runs);
- `create`'s output passes `GateCreated`, the Open XML SDK validator and
  reads back (`TestCreateExitCriteria`, `make conformance`);
- every unported operation refuses at apply with `operation_failed`
  (`TestUnportedOpsRefuse`), never `internal`;
- docx outputs in `make conformance` (each ported op in both modes, and
  `create`) and cost linear in the document (`make scaling`).

### Agent contract (2026-11-13): what is in

`docs/contract.md` is the target for both tools. Status:

- **Shared code (`internal/cli`), in:** the command grammar, the errors
  object and exit codes, JSON output with `--pretty`, the plan envelope
  (`ops`, `base`, `contract` and the format's settings), the path-carrying
  decoder that refuses duplicate keys, unknown fields and null and reports
  every fault together, `cli.Snake`, and `help ops` / `help <op>` generated
  from an op table.
- **pptx, in:** the envelope without a plan-level slide; addresses
  `slide3/s17` in plans, read and results; the shared field vocabulary;
  snake_case keys; `text.replace` with `occurrence` and `all`; `base`,
  `new_base`, `--changed ids|full`, `notes`; the error codes with field
  paths; HTML input through the strict HTML5 front end; the rule for
  documents with defects (unreadable layout, master or theme is a note; a
  rotation or flip stored wrong can be overwritten; `unreadable_target`).
- **pptx, not yet:** nothing known.
- **docx, in:** the CLI on `internal/cli` (`read`, `find`, `apply` with
  `--dry-run`, `-o`, `--changed`, and `create`; any path; no
  `document_ui`); read and
  find results in the contract's shape (base, notes, snake_case keys, no
  nulls, addresses, one comment thread shape), with `read.FromOracle` for
  parity runs; the 42 operations in one table (`ops.Ops`), one run file
  each, with the oracle mapping and `ops.FromOracle`; the contract's
  envelope (`ops.ParsePlan`: base optional, author when tracking or
  commenting, every fault together); strict HTML input; HTML attributes
  without the `data-docx-` prefix, `id` as a table, row or cell's address,
  `data-ref` as an equation's, and unknown attributes refused by name;
  `help ops`, `help <op>`, `help html`.
- **docx, not yet:**
  - the operations themselves, apart from `paragraph.format`,
    `paragraph.insert`, `paragraph.delete`, `comment.add`,
    `revision.accept` and `revision.reject` (P5); the other 36 refuse
    with `operation_failed`;
  - `revision.accept` / `revision.reject` over paragraphs or one author,
    `text.replace` with `all`, the `delete` field and `<opaque>`
    placeholders (the view has nothing to emit them for yet);
- **docx, done in the P4 close-out:** fields decode through the shared
  decoder (the serde emulation is gone; `ops.ParsePlanJSON` reads
  docxdriver's envelope with the same decoder, for the parity tests), and
  `apply` runs once (no preview key).
- **vsdx, in (2026-11-17):** the contract's vsdx parts, merged into
  `docs/contract.md` from `docs/vsdx-contract-draft.md` (kept as the
  record): `read`, `find`, `xml`, `apply`, `create`, `render`, `help`;
  the 15 operations of §3 (`object.*`, `text.*`, `prop.*`, `page.*`);
  `help` text compared with §3's op table and refusals (CC7's test).
  Design: `docs/vsdx-design.md`.
- **vsdx, not yet:** a Visio check that every vsdx output, including a
  created drawing, opens without a repair prompt (visio-smoke); the
  Visio experiments VE1 to VE14 (`docs/vsdx-design.md`, none run in Visio:
  the contract labels each claim that rests on one "Visio
  unconfirmed"); the v1 deferrals of §12.
- **vsdx, in (2026-11-18):** line ends, shadows, background page
  assignment, picture reads (`image`, `media`, `crop`), `vsdx media`,
  and picture writes (owner ruling: schema- and sample-grounded,
  VE15 to VE18 open).

### P5. docx operations, by family

Ported in P4: `paragraph.format`, `paragraph.insert`, `paragraph.delete`,
`comment.add`, `revision.accept`, `revision.reject`. The other 36 run in
parallel, one agent per family, each in its own worktree:

| Agent | Count | Operations |
| --- | --- | --- |
| property | 8 | `section.set_margins`, `section.set_layout`, `section.set_first_page`, `document.set_even_odd_headers`, `revision.convert_to_formatting`, `comment.reply`, `comment.set_status`, `comment.delete` |
| tables | 11 | `table.insert`, `table.delete`, `table.format`, `row.format`, `cell.format`, `row.insert`, `row.delete`, `column.insert`, `column.delete`, `cell.merge`, `cell.split` |
| span | 8 | `text.replace`, `text.revise`, `text.insert`, `text.format`, `bookmark.add`, `bookmark.rename`, `bookmark.move`, `bookmark.delete` |
| block | 9 | `paragraph.replace`, `section.set_boundary`, `equation.replace`, `equation.delete`, `header.set`, `footer.set`, `header.delete`, `footer.delete`, `list.set` |

Shared packages are frozen for the op agents and owned by the integration
owner: `internal/docx/apply`, `internal/docx/integrity`,
`internal/ooxml/edit`, `internal/ooxml/tree`, `internal/opc` and the
revision settle step (`internal/docx/revise/settle.go`). A change an op
agent needs in a frozen package goes to the integration owner as a
request (through the coordinator), who lands it in a small commit of its
own that is merged to main for every agent at once.

Each op agent changes its operations' run files (`run_<name>.go`,
removing `pending`), helpers in files of its own in `internal/docx/ops`,
its tests, its rows of `tools/oracle/baseline/known/write-ops.tsv`, and
the shared code it owns:

| Owner | Code |
| --- | --- |
| block | `internal/docx/build` (except `build/table.go`), and the paragraph HTML (`parseParagraphHTML`) for `paragraph.replace` |
| tables | `internal/docx/build/table.go` |
| span | insertion and deletion authoring in `internal/docx/revise` |
| property | `internal/docx/revise/props.go` and the comments files of `internal/docx/edit` |
| integration owner | the frozen packages above, including `revise/settle.go` |

The HTML front end is shared like the frozen packages.

Rules for every operation: it reaches the document only through its
`Env` and the workspace; it never re-opens or re-reads the document; and
it never holds a document-wide derived copy (a second view, index or
projection of the whole document).

An operation is done when:

1. its `Run` is in `run_<name>.go` and `pending` is gone; `help <op>`
   no longer says "not implemented";
2. its laws are a `laws.Suite` entry (identity write, read after write,
   untargeted bytes unchanged, atomicity under injected failure,
   sequencing) and its change-mode law holds (direct equals accept-all of
   tracked; reject-all equals the input, per its class), on the fixtures
   and on `DOCX_CORPUS`;
3. `go run ./tools/oracle write compare -ops <oracle name>` shows no
   `differ` and no `not-implemented` (`-require-implemented`): every
   remaining difference is fixed, or listed in `write-ops.tsv` with its
   reason; oracle bugs are fixed, never inherited, and listed with
   evidence;
4. documents with defects: a pre-existing fault never blocks it or the
   save; it refuses only its own unreadable target, before any change
   (`unreadable_target`), and the dirty kind passes;
5. `make scaling` passes (add a `laws.Scales` case if the op is a new
   family's first) and `make conformance` shows no new error kind for
   its outputs in both modes;
6. its outputs pass the Word smoke test (`make word-smoke`, run by the
   owner);
7. the pre-commit hook, `go test` of `internal/docx/...` and
   `cmd/docx`, and `make lint` are clean.

Parity method (`tools/oracle`): run each source x plan through both engines
in track and direct mode. Read the Go output with the oracle's reader and
compare the projections (markup, final and original views, comments,
revisions, bookmarks, styles), the same projections after accept-all and
reject-all, the report, and the newly introduced integrity findings. A
canonical XML diff of touched parts (namespaces and attribute order
normalised, adjacent equal runs merged, empty `pPr`/`rPr` dropped, fresh ids
relabelled) is for triage only. Untouched parts must be byte-identical to
the source. Every remaining difference is fixed or written down as an
intended change in the known-differences file the comparator reads.

Exit per family: every operation done as above.

### P6. Agent surface

- docx-pi: switch the extension from the WASM runtime to `execFile` on
  `docx`, as `eval/pi/pptx-extension.mjs` does; update the skills.
- Extend pptxbench with docx tasks; run the Rust-WASM and Go-CLI arms on the
  same tasks and compare.

Exit: the Go arm is no worse than the Rust arm on task success.

### P7. Release and cut over

- Release script (stdlib Go): `CGO_ENABLED=0 -trimpath` builds for
  darwin, linux and windows on amd64 and arm64, plus `SHA256SUMS`. In:
  `make release` (`tools/release`, RELEASING.md) for linux amd64/arm64
  and darwin arm64, with a `documentdriver` PyPI wheel per platform;
  windows and darwin/amd64 are not built yet.
- Tag `documentdriver v0.1.0`.
- Deprecate the docxdriver crates, npm and PyPI packages with a pointer to
  the new repo, and archive the docxdriver repo.

## Size and order of effort

| Area | Rust lines | Notes |
| --- | --- | --- |
| Operations (`commands/`) | ~12.0k | `typed.rs` alone is 3.4k |
| HTML and math (`html/`) | ~9.0k | Read side, parity is exact |
| API, plans (`api.rs`) | ~3.2k | Shrinks without `schemars`/TOML |
| Integrity | ~2.5k | Partly shared with pptx |
| Model (`document`, `segments`, `media`, …) | ~2.9k | |
| Tests | 262 Rust tests | Port alongside each module |

P1–P2 are days of work, P3–P4 one to two weeks each, and P5 is the bulk at
several weeks. The oracle and the conformance gate make it mostly
translation, not redesign.

## Risks

- **Determinism.** Random paraIds and wall-clock revision dates break parity.
  Both engines need an injected clock and id seed from P0.
- **Intended output changes.** The Go engine copies untouched spans where
  Rust re-serialised them. Byte-comparing fixtures will need new expected
  outputs; the parity tool must compare touched parts only.
- **Word is the only real oracle**, and only on macOS. Keep the smoke test
  in every wave's exit, not just at release.
- **Losing users of the bindings.** Anyone on the npm, PyPI or crates.io
  packages or an MCP client must move to the CLI. Announce at P6, not P7.
