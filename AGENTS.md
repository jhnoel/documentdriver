# Agent notes

documentdriver is one Go module, standard library only, that ships four CLIs
on one OOXML engine: `pptx` (moved in from pptxdriver), `docx` (a port of
docxdriver 0.5.2; see `docs/plan.md`), `xlsx` and `vsdx` (Visio; design in
`docs/vsdx-design.md`, agent surface in `docs/contract.md`; the merged draft is kept as the record in `docs/vsdx-contract-draft.md`). Agents run these
binaries on documents they did not write, so every input is untrusted.

Read before changing anything:

- `docs/contract.md`: the agent contract both CLIs follow (commands, plans,
  results, errors, addresses, HTML). It is signed off by the owner; a change
  to what an agent sees starts there.
- `docs/plan.md`: the monorepo plan and phases.
- `docs/design.md`: the engine and the pptx side, including every recorded
  decision. Decisions there override general advice here.

Rules below are marked **[enforced]** when `make check`, `make ci` or a git hook fails on a
violation, and **[review]** when they are required but not yet automated.
A [review] rule is still a rule; when you automate one, change its mark.

## Workflow

- Work in your own git worktree under `.worktrees/` and commit on a
  `wt/<task>` branch. Never commit in the main checkout; the maintainer
  works there.
- There is no hosted CI: the gate is local. Install the committed hooks
  once per clone with `make hooks` (it sets `core.hooksPath` to
  `.githooks`) and never bypass them with `--no-verify`. The pre-commit
  hook runs gofmt, `go vet`, `make norequire` and `internal/archtest`; the
  pre-push hook requires a `make ci` pass for every pushed commit. A pass
  on a clean tree is recorded by tree hash and Go version in the common
  git directory (shared by worktrees, so a pass on a `wt/` branch covers
  the fast-forwarded main), and the hook pushes recorded commits at once;
  for the clean HEAD with no record it runs `make ci` itself. Run
  `make ci` before `git push`: git connects to the remote before the
  hook runs, and the remote drops the connection during a long one. `fuzz`, `make conformance` and
  `make powerpoint-smoke` are manual only; the smoke target force-quits
  PowerPoint, so run it only after the maintainer agrees. `make conformance`
  is the OOXML conformance gate (ECMA-376 schemas, package integrity, Open
  XML SDK validator) over the pptx fixture decks, the plans applied to
  them, and `eval/plans` on their corpus decks, and over docx outputs: a
  sample of the write baseline's plans for each ported operation in
  track and direct mode (`DOCX_CORPUS` and `DOCXDRIVER_REPO` add the
  corpus and fixture sources) and `docx create` on
  `tools/conformance/docx/*.html`; it is slow, needs `uv` and
  `dotnet`, and fails on error kinds an output has that its input deck did
  not (errors already in an input are listed, never fixed in the checker).
  Run it when a change can alter what an operation writes.
- Run `make check` before every commit **[enforced: `.githooks/pre-commit`
  covers the fast subset]**:
  `go vet`, all tests including `internal/archtest`, gofmt, and
  `make norequire`.
- Run `make ci` before merging to main **[enforced: `.githooks/pre-push`]**. It
  runs norequire, vet, test, fmt, lint, vuln, race, repro and wasip1 (and
  fuzz once that target exists) and lists every step that failed. The
  steps:
  - `make lint`: golangci-lint v2 with `.golangci.yml`. It needs a local
    install: `make tools` installs the pinned version into the gitignored
    `bin/`.
  - `make vuln`: govulncheck (pinned, installed by `make tools`).
  - `make race`: `go test -race ./...`.
  - `make repro`: builds `cmd/pptx` twice from `git archive HEAD` in clean
    directories and fails if the binaries differ; it checks committed
    work only.
- `make corpus` is the full corpus sweep: every test with `PPTX_CORPUS`
  (default the repository's `corpus/`) and `DOCX_CORPUS` (default
  `corpus/lo` of `DOCXDRIVER_REPO`, the docxdriver checkout beside the
  main checkout) set. It is manual, not part of `make ci`. Corpus suites
  run their per-file work through `internal/corpustest` **[review]**: one
  subtest per file (named by its corpus-relative path, so `-run
  'TestLawsCorpus/repos/...'` reruns one file), `CORPUS_PARALLEL` files at
  once per suite (default GOMAXPROCS; 1 is serial), each file with its own
  `t.TempDir` and its own counts, summed in file order after `Run`
  returns, never through shared variables. The limit is per test binary:
  suites that call `t.Parallel` (the docx apply and pptx law suites) run
  side by side in the same slots, largest files first, so one slow file
  does not idle the rest. `CORPUS_P` is `go test -p`
  (default 2). A suite holding large documents caps itself with
  `corpustest.MaxParallel`. A fault hook a parallel suite installs is
  scoped to its own file (`opc.SetFaultHookFor`).
- `make corpus SHARD=i/n` (`CORPUS_SHARD=i/n`, 1 <= i <= n) runs shard i
  of n of every corpus suite: the files whose FNV-1a hash of their
  corpus-relative name is i-1 mod n. The shards are disjoint and together
  are the full set, and choosing them needs nothing but the name, so
  another machine runs its shard with no syncing: copy the committed tree
  (`git archive HEAD | ssh host 'mkdir -p dd && tar -x -C dd'`), give it
  the corpora at any path, and run
  `PPTX_CORPUS=<pptx corpus> DOCX_CORPUS=<docx corpus> make corpus SHARD=2/4`
  there (without make: `CORPUS_SHARD=2/4 PPTX_CORPUS=... DOCX_CORPUS=...
  CGO_ENABLED=0 go test -count=1 -timeout 3h -p 2 ./...`). Each shard passes or fails on its own; totals a suite logs
  are the shard's, and assertions over the whole corpus (at least one
  file exercised) are skipped when `corpustest.Sharded()`.
- Paths are repo-relative. Find the root with `internal/repo`; never write
  an absolute home, temp or user path into a tracked file **[enforced:
  `TestNoAbsoluteHomePaths`]**. Overrides come from flags or environment
  variables (`PPTX_CORPUS`, `DOCX_CORPUS`, `PPTORACLE_WORKDIR`,
  `PDRAW_REPO`).
- Credentials come from the environment or the caller, never from code,
  files in the repo, or the keychain.
- Data stays out of git: `corpus/`, `eval/bundles/` and `results/` are
  gitignored. Commit only small fixtures you authored, with provenance.
- Long waits: never write `pgrep -f` loops (they match themselves and never
  exit). Use exact process names (`pgrep -x`) with a deadline.
- Python is for throwaway analysis only; run it with `uv run`. Anything
  reused is Go. The one exception is the dev-only conformance gate in
  `tools/conformance` (lxml via inline script metadata, `uv run --script`);
  it never ships. The shipped exception is the wheel's import package
  `packaging/python/documentdriver`: standard library only, Python 3.8
  and later, a runner over the contract's streams and exit codes, never
  a second copy of its commands or operations; `make release-smoke`
  tests it.

## Dependencies and builds

- Standard library only. `go.mod` has no `require` lines **[enforced:
  `make norequire`]**. Dev tools that are modules (golangci-lint,
  govulncheck) are pinned by version in the Makefile and installed
  with `go install pkg@version` into `bin/` (`make tools`), never as `tool`
  lines or requires in the main `go.mod`.
- No `unsafe` and no cgo (`import "C"`) anywhere, tests included
  **[enforced: `TestNoUnsafeNoCgo` in `internal/archtest`]**. Builds and
  tests use `CGO_ENABLED=0` **[enforced: the Makefile exports it, so `make`
  inherits it]**. The one exception is `make race`, which sets `CGO_ENABLED=1` because the race detector needs cgo on macOS
  and Linux; it compiles no C of our own.
- Release builds use `CGO_ENABLED=0 -trimpath`; a release is reproducible
  (two builds of the same commit give identical hashes) **[enforced:
  `make repro`]**.
- Keep a `GOOS=wasip1` build possible **[enforced: `make wasip1`]**;
  only `eval/cmd/pptxbench` is excluded.

## Style

- Follow the Google Go Style Guide (normative), Effective Go and Go Code
  Review Comments; use the Uber Go Style Guide where those are silent.
- gofmt **[enforced]**; run `go fix` (the Go 1.26 modernizers) when you
  touch a package and commit the result separately.
- Static analysis: `go vet` **[enforced]**. golangci-lint v2 with govet
  (all analyzers), staticcheck, errcheck, gosec, revive, ineffassign and
  unused **[enforced: `make lint`]**. Fix findings; do not
  add blanket exclusions. An exclusion names the linter, the rule and the
  reason: in `.golangci.yml` scoped by path, or inline as
  `//nolint:<linter> // <rule>: <reason>`.
- Never discard an error with `_`. Wrap with context (`fmt.Errorf("...:
  %w", err)`); compare with `errors.Is`/`errors.As`.
- Doc comments on every exported identifier, and on every function and
  method, exported or not: a sentence starting with its name that says
  what it does. A helper's doc comment is how the next agent finds it
  (the semantic function search embeds signature plus doc comment)
  **[enforced: `TestEveryFunctionDocumented` in `internal/archtest`;
  `docExemptDirs` lists the package directories still being backfilled,
  only shrinks, and a fully documented directory must be removed from
  it]**. Small files, one concern each.
- No global mutable state outside `main`. No `init` side effects beyond
  registering tables.
- No panics on input. A panic in a reader or operation is a bug, even on a
  malformed document.

## Untrusted input

Every `.docx`/`.pptx`, plan and HTML fragment comes from somewhere you do
not control. Code that reads them must fail with an error, never crash,
hang or exhaust memory.

- Zip: bound what you decompress. Enforce limits on bytes actually read
  (not declared sizes, which are attacker data): per part, in total, the
  compression ratio, the number of entries, and entries sharing compressed
  data. `opc.Open` applies `opc.DefaultLimits` (256 MiB per part, 1 GiB
  in total, 1000:1 above 1 MiB, 20,000 entries); pass an `opc.Limits` to
  `OpenWithLimits` to change them, never an environment variable
  **[enforced: `TestLimit*` in `internal/opc`; its `TestCorpus` fails if
  a corpus deck trips a limit]**.
- XML: the tokenizer caps nesting at `tree.MaxDepth` (512; the corpus
  peaks at 25) and the HTML text grammar at 128. A recursive walk over a
  tree (group transforms, inheritance chains, coverage counting, text and
  view builders) must be iterative or depth-limited: either it walks only
  trees the tokenizer built, or it carries its own depth or cycle limit
  **[enforced for the cap: `TestMaxDepth`, `TestParseDepth`, and the
  deepest-slide tests `TestDeepGroups` (view, apply) and
  `TestCountDeepGroups`; review for new walkers]**.
- Never expand XML entities beyond the five predefined ones; never fetch
  external resources named in a document. (HTML input decodes the HTML5
  named character references, from the standard library's table, as
  docxdriver does.)
- Part names, relationship targets and paths from a document never reach
  the filesystem; the tool writes only to paths its caller gave it.
- Fuzz every parser of untrusted input with native Go fuzzing: the XML
  tokenizer (`FuzzRoundTrip`, `FuzzSetAttr`, and `FuzzDifferential`
  against the frozen `treeoracle`), OPC open (`FuzzOpen`), plan
  JSON (`FuzzParseJSON` in `internal/cli`, `FuzzPlan`), the HTML text grammar (`FuzzParse`), the shared
  HTML front end (`FuzzParseHTML`), the docx HTML input (`FuzzParseFragment`), the SVG path
  parser (`FuzzParsePath`), read on mutated decks (`FuzzRead`,
  `FuzzReadEdits`), docx read (`FuzzRead` in `internal/docx/read`), and the docx equation converters (`FuzzOMMLToLaTeX`,
  `FuzzLaTeXToOMML`), the xlsx command line (`FuzzCommandLine` in
  `cmd/xlsx`), the xlsx integrity check (`FuzzCheck`, `FuzzCheckParts`
  in `internal/xlsx/integrity`), splices (`FuzzPatch` in
  `internal/ooxml/splice`), and xlsx formulas (`FuzzLex`, `FuzzShift`, `FuzzDisplay` in
  `internal/xlsx/formula`), number formats (`FuzzClassify` in
  `internal/xlsx/numfmt`), the xlsx sheet scanner (`FuzzScan`, `FuzzSST`
  in `internal/xlsx/sheet`) and xlsx read and find (`FuzzRead`,
  `FuzzReadParts`, `FuzzFind` in `internal/xlsx/read`), and the xlsx
  writes: plans and values (`FuzzParsePlan`, `FuzzDecodeValue` in
  `internal/xlsx/ops`), formula storing (`FuzzStore`), the edit lexer
  (`FuzzLexNode`), structural shifts (`FuzzSheetData`) and apply
  (`FuzzApply`), the vsdx number and address parsers (`FuzzNumber`,
  `FuzzParseAddress` in `internal/vsdx/vsd`), the vsdx section index
  (`FuzzIndex` in `internal/vsdx/sheet`), the vsdx text layout (`FuzzText` in
  `internal/vsdx/text`), vsdx read and find (`FuzzRead`, `FuzzFind` in
  `internal/vsdx/read`), vsdx plans (`FuzzParsePlan` in `internal/vsdx/ops`),
  the vsdx command line (`FuzzCommandLine` in `cmd/vsdx`), and the
  embedded font reader (`FuzzParse` in `internal/render/ttf`). Each keeps a seed corpus in its package's
  `testdata/fuzz`, which `make check` runs as tests **[enforced for the
  seeds; `make fuzz` (FUZZTIME, default 10s per target) fuzzes them, to
  run on a schedule in CI; add a new parser's target to `FUZZ_TARGETS`
  (review)]**. Commit every crasher as a seed corpus entry with its fix.
- Run tests with `-race` when touching anything concurrent, and before
  merging engine changes **[enforced: `make race`]**.
- `govulncheck ./...` must be clean **[enforced: `make vuln`]**. With no dependencies it reports toolchain and standard-library
  issues, so keep the pinned Go version current.

## Performance and memory

Measure before and after; do not optimise by intuition.

- Cost is linear in the document. On an input of four times the units,
  an operation may allocate at most 5.5 times the bytes and allocations,
  and its wall time grows linearly too (ratio at most 8): bytes and
  allocations are deterministic, so they are enforced in every run,
  while time ratios give their verdict only in `make scaling`
  (`LAWS_TIME_VERDICT=1`, `-p 1`: packages one at a time), because package-level
  parallel load moves time ratios on linear work past the bound, and
  are only logged in `make check`. Peak live heap
  and allocation stay within the docx budgets (`TestMemoryBudget`)
  **[enforced: `make scaling`, a `make ci` step, `TestScaling` in
  `internal/docx/apply`, `internal/docx/create` and `internal/pptx/apply` through
  `internal/laws.Scales`]**. Run `make scaling` in your quick checks
  whenever you touch a parser, the tree, OPC, the edit core, the
  integrity checks or an operation; add a `laws.Scales` case for a new
  command or operation family. The budget and the measured numbers are
  in `docs/design.md` (Scaling and memory budget): a one-paragraph
  `docx apply` costs at most three times `docx read` of the document,
  and peak memory stays the order of one parsed tree.
- Never build a list of a node's children, scan every relationship, or
  re-read a part inside a per-element or per-operation loop; walk the
  tree, keep a map, or reuse the read you have.
  **[enforced: `TestNoSiblingSlices` for the sibling-search form]**

- Benchmarks use `B.Loop` (Go 1.24+), report allocations, and are compared
  with `benchstat` over at least 10 runs. A change to the tokenizer, tree,
  OPC or an operation's hot path includes before/after numbers in its
  commit message or design note.
- Profile with `go test -cpuprofile/-memprofile` and `go tool pprof`; check
  heap escapes on hot paths with `go build -gcflags=-m`.
- Read lazily and copy through: parse only the parts an operation needs;
  untouched zip members are copied compressed (`zip.Writer.Copy`) and
  untouched XML is copied as original bytes.
- Watch peak memory on large documents (the Go vs Rust profile measured
  edit at about 32x the size of `document.xml`). Do not hold more than one
  copy of a part's bytes when the tree already references them.
- Profile-guided optimisation: once a representative CPU profile exists,
  commit it as `cmd/<tool>/default.pgo` and refresh it when hot paths
  change.
- Do not tune the GC (`GOGC`, `GOMEMLIMIT`) in library code. The one
  setting is in each CLI's `main`: GOGC 50 unless the environment sets
  `GOGC` (docx: about 15% less peak RSS for about 4% more time; pptx:
  about 16% less RSS on read, time within noise; measured in
  `docs/design.md`). The tools run on servers and beside many parallel
  agents, so memory outranks a few percent of time. A change to the
  setting comes with new measurements.

## Architecture rules (both formats)

All **[enforced: `internal/archtest`]** unless marked.

- No `encoding/xml` outside `eval/`; OOXML goes through
  `internal/ooxml/tree`.
- Tree mutation only in `internal/opc` and the edit core
  (`internal/ooxml/edit`); a format's edit layer (`internal/pptx/ops/edit`,
  and the docx one to come) mutates only through the core's `Handle` and
  unit placement. Inserts only in the core's `place.go`, `unit.go` and `structure.go`,
  through schema-placed helpers that read the generated child order
  (`Ensure`, `AppendEach`, `AppendExt`, `ReplaceChildren`, `ReplaceRun`,
  `SlotEnd`) or next to an existing sibling (`Before`, `After`,
  `JustAfter`), and the structural verbs inside a handle's own content
  (`SplitAt`, `SplitText`, `Wrap`, `Unwrap`, `Rename`, `SetText`).
  The revision core (`internal/docx/revise`) mutates only through region
  handles (`Range`, `Story`, `Part`), including the private, never-saved
  copies the HTML projection settles (`edit.Private`).
- Writes go through `internal/plan` transactions, which verify the
  declared write set at commit: only `internal/opc` and `internal/plan`
  save a package; parts are added, deleted or replaced only by
  operations inside a transaction (`internal/pptx/ops`). A format keeps
  its operations, plan schema, units and hazard kinds on top
  (`internal/pptx/ops.Tx` wraps a `plan.Tx`).
- Never serialise a read model back into OOXML.
- `internal/pptx` may be imported only from inside `internal/pptx`, from
  `cmd/pptx`, and from the dev-only `eval/` and `tools/`; likewise for
  `internal/docx` with `cmd/docx`, `internal/xlsx` with `cmd/xlsx` and `internal/vsdx` with `cmd/vsdx`. The shared engine (`internal/opc`,
  `internal/ooxml/...` including the edit core, `internal/plan`,
  `internal/ids`, `internal/ascii`, `internal/geom`, `internal/casefold`, the HTML front end `internal/htmlx`, and the test-only law
  harness `internal/laws`) never depends on a format, not even through
  another package. `internal/laws` is imported only by tests. The shared
  agent surface `internal/cli` (command grammar, errors object, exit
  codes, plan envelope and decoder, generated help) is part of the shared
  engine and imports no format.
- Content and relationship types come from Office-authored files, never
  from memory: they are constants of `internal/ooxml/ctypes`, generated by
  `tools/ctypesgen` from `provenance.json` (which names the source file of
  every entry), and non-test code outside that package and its generator
  never spells one as a string literal **[enforced:
  `TestContentTypesFromTable` in `internal/archtest`; table drift:
  `TestTableDrift` in `tools/ctypesgen`]**. Add or refresh entries with
  `ctypesgen -extract` over the corpora (see its doc comment), then
  `go generate ./internal/ooxml/ctypes`.
- No second XML lexer/codec: tokens/entities/escapers only via `xmlscan`
  (allow xmlscan/tree/splice Writer/htmlx/formula; grandfathered
  shrink-only) **[enforced: `TestOneXMLLexer`]**.
- JSON only via `cli`: no `encoding/json.Marshal`/`MarshalIndent`/
  `NewEncoder` outside `internal/cli`,`cmd/` (grandfathered shrink-only)
  **[enforced: `TestJSONThroughCLI`]**.
- No exact clone (≥6 body lines, locals renamed by position) across files
  except listed pairs/treeoracle/xlsxtest `colName` (shrink-only)
  **[enforced: `TestNoExactClones`]**.
- Processes only via `soffice`: no `os/exec.Command`/`CommandContext`/
  `LookPath` outside `internal/soffice`,`tools/`,`eval/` **[enforced:
  `TestProcessesThroughSoffice`]**.
- Zips only via `opc`: no `archive/zip.NewWriter` outside `internal/opc`,
  `tools/`,`eval/` (grandfathered docx template/testdeck) **[enforced:
  `TestZipThroughOPC`]**.

## Shared helpers

One capability, one helper — reach for the helper, never a second copy of
it.

| Capability | Use | Never write |
|---|---|---|
| Understand an XML part | `tree.Scan` / `tree.ScanString` + `tree.NSStack` | a visitor-side namespace stack |
| Find byte spans in scanned XML | `xmlscan.Next`, `Attrs`, `FindAttr`, `ElementEnd` | a tag lexer, a quote loop, `strings.Index("</x>")` |
| Attribute value span in a Scan visitor | `tree.Node.AttrSpan` | re-scan of the start tag |
| Change attributes of a tag in bytes | `xmlscan.EditTag` | a tag renderer |
| Sub-node byte edits of a small element | `xmlscan.Apply` | a patch sorter |
| Whole-node edits of a large part | `splice.Patch` + `splice.Writer` | anything else |
| Decode / escape XML | `xmlscan.DecodeText/DecodeAttr/EscapeText/EscapeAttr` (`EscapeAttrMinimal` keeps `>`) | a Replacer, an entity switch |
| XML names | `xmlscan.ValidQName`, `IsNameStart` | a name table |
| `_xHHHH_` | `sml.UnescapeX`, `sml.EscapeX` | a hex4 |
| SpreadsheetML namespaces, main child | `sml.NS*`, `sml.IsMain`, `sml.MainChild` | a namespace literal |
| Workbook sheets, names, tables | `sml.SheetEntries`, `ResolveSheet`, `DefinedNames`, `ParseTable` | a reader of workbook.xml or a table part |
| Index and count attributes | `sml.ParseIndex`, `sml.IsTrue` | atoi, parseNum |
| A1 cell / area / range | `formula.ParseCell`, `ParseArea`, `ParseRange`, `Range.Normalized`, `Bounds` | parseArea |
| Column letters | `formula.ColumnName`, `AppendColumnName`, `ColumnIndex` | `'A' + n%26` |
| Sheet quoting, prefixes | `formula.QuoteSheet`, `formula.SheetPrefix`, `NeedsQuote` | quoteSheet |
| Unit ids | `model.SheetID`, `NameID`, `Cell`, `Area`, `ParseRef` | `"sheet:"+`, splitting on `!` |
| cols / mergeCells / CF-DV formulas | `sheet.ColRuns`, `sheet.Merges`, `sheet.FormulaTexts` | a walker |
| Part name keys | `opc.PartKey` | `strings.ToLower("/"+name)` |
| New zip bytes | `opc.BuildZip` | `zip.NewWriter` |
| JSON out | `cli.Marshal`, `cli.WriteJSON`, `cli.NonNil` | `json.Marshal` |
| Op tables | `cli.OpIndex`, `cli.ValidAlias` | Lookup/OpOf loops |
| Enum decode, op help | `cli.DecodeEnum`, `cli.FindOpHelp` | planEnum, a hand-written op help |
| LibreOffice | `soffice.Find`, `soffice.Run` | exec.Command, LookPath |
| Office apps (tools) | `tools/internal/officeapp` | pgrep/pkill/open/osascript in a tool |
| Ids and clock | `ids.Source` | rand, time.Now |
| Element sibling | tree sibling links, `model.NextElement`, `model.PrevElement` | a list of a node's children in a loop |
| ASCII case and classes | `ascii.Lower`, `ascii.EqualFold`, `ascii.IsAlpha` and friends | asciiLower, isASCII |
| Case-insensitive find | `casefold.Simple` (simple Unicode folding) | a fold table, `strings.ToLower` on both sides |
| Points, affine maps, box/rotation/flips of a placed object, view number form | `geom.Pt`, `geom.Affine`, `geom.Box`, `geom.Decompose`, `geom.FormatNum` | a matrix type, a decompose, a two-decimal formatter |
| docx body blocks | `model.FindSectPr`, `model.AssetURL`, `model.InsidePPr` | a sectPr, asset or pPr walk in a reader |
| Map keys in sorted order | `slices.Sorted(maps.Keys(m))` | a range over a map plus a sort |
| Content and relationship types | `ctypes` constants | a type string |

Before writing a helper, search the repository for an existing one; if
the one you need is unexported or in a package you may not edit, do not
copy it: stop that piece and report `NEEDS-EXPORT: <package>.<name>: <why>`.

## Contract and correctness

- The agent surface is `docs/contract.md`, and both binaries reach it
  through `internal/cli`: a command is a `cli.Command`, a failure is a
  `*cli.Error` or `cli.Errors` with one of the contract's codes and the
  path of what is wrong, and a plan is decoded by the `cli` decoder
  (duplicate keys, unknown fields and null are errors; every fault found
  without the document is reported together). Do not print to stderr, add
  a flag syntax or decode a plan another way **[review]**. `help ops` and
  `help <op>` are generated from the format's op table; never hand-write
  an operation list.
- Keys are snake_case, converted from OOXML names by `cli.Snake` only;
  addresses are shown in the form a plan takes them.
- Documents with defects (`docs/contract.md`, section 10): checks only stop
  the tool from introducing a nonconformity. A defect the input already had
  never blocks a read, an edit elsewhere or the save; a part or object that
  cannot be read is left out with a note; an operation refuses only when its
  own target is unreadable, at that operation (`unreadable_target`), never at
  save time. Tests for this are in `internal/pptx/apply/defects_test.go`;
  add a case when you add a reader or a codec.

- Derive facts, never decide. If a fact cannot be derived faithfully, omit
  it and say why; never substitute a default. Readers return an error or an
  absent value, never zero.
- The four-outcome contract holds on every operation: Office opens the
  output without a repair prompt; untargeted content is unchanged; plans
  apply completely or not at all; a no-op save writes nothing. Do not
  normalise, validate or repair content no operation touched.
- Every new property or operation lands with its laws (identity write is a
  no-op, read after write shows the value, untargeted bytes unchanged,
  atomicity under injected failure, sequencing) on fixtures, and on the
  corpus behind `PPTX_CORPUS`. Write them as a table entry of the
  format-neutral harness `internal/laws` (`laws.Suite`); add a check
  outside it only for what it does not model.
- Values written to OOXML are validated against tables generated from the
  pinned ECMA-376 schemas (`internal/ooxml/schema`, one table per
  vocabulary: pml, dml, wml, sml, shared); never hand-write enumerations or
  child orders.
- Output must be deterministic given the same input and plan, except where
  the format requires fresh ids (e.g. `a16:creationId`) or a clock reading;
  those come from one injectable `ids.Source` (`internal/ids`: `GUID()`,
  `Now()`), carried by the transaction (`ops.WithIDs`, `apply.Options.IDs`).
  Production default is crypto-random GUIDs and the real clock; `ids.Fixed`
  takes a seed and a clock. The hidden flags `pptx apply --seed N --clock
  RFC3339` select it (either one; defaults seed 0, clock 2000-01-01T00:00:00Z)
  for the bench and the docx parity oracle; flags, not env vars, and not
  part of the agent contract. Operations
  never call `math/rand`, `crypto/rand` or `time.Now`, and map iteration that
  reaches output is in sorted key order **[enforced: `TestDeterminismSources`
  in `internal/archtest` forbids the imports and `time.Now` outside
  `internal/ids` (justified exceptions listed in the test);
  `TestDeterministicBenchPlans` in `internal/pptx/apply` applies every
  `eval/plans` plan twice per seed, byte-identical, and across seeds only
  GUIDs differ (needs `PPTX_CORPUS`)]**.

## docx operations

- The operation table is `internal/docx/ops.Ops`: the contract's 42
  operations, one entry each, with the docxdriver 0.5.2 operation it runs
  as (`Oracle`, `Fixed`, `Moved`). There is one table; docxdriver's side
  (`OracleOps`) is a record of the pinned oracle, not a second one.
- An operation lives in its own file, `run_<name>.go` (`text.replace` is
  `run_text_replace.go`): its Spec struct, its table entry and its `Run`.
  Porting an operation means removing `pending` from its struct and
  writing `Run` there. Do not edit `table.go` or another operation's run
  file; shared helpers go in a file of their own **[enforced for the
  layout: `TestOpTable`]**.
- Field names are the contract's (`at`, `as`, `select`, `with`, `html`,
  `to`); addresses are the types in `addresses.go`. A rule's `Path` and
  `Example` are in the contract's grammar **[enforced: `TestRuleExamples`]**.
- Tests build plans from Go values with `ops.NewPlan` (and `ops.Ptr` for
  optional fields), not from JSON text; write JSON only to test the
  decoder.
- Parity with docxdriver's plan parsing is accept or refuse and the code,
  through `ops.FromOracle` **[enforced: `TestOracle`; every difference is
  listed in `ops.Differences`]**.
- Write parity is the full write baseline: `make oracle-write-gate` (the
  unnarrowed `oracle write compare -require-implemented`; needs the oracle
  CLI, docxdriver and its corpus, so it is manual) must report 0 differ
  before a change that can alter what an operation writes merges, and a
  claim of "0 differ" means that run **[review]**. `make
  oracle-write-compare OPS=...` is for a wave's triage only; a narrowed
  compare says so in its output, and its counts never stand for the
  gate. Every intended difference is a row in
  `tools/oracle/baseline/known/write-*.tsv` with its reason;
  `write-paraid.tsv` is regenerated by `oracle write gen`, so hand rows
  go in `write-ops.tsv`.

## pptx specifics

- Writes go through `internal/pptx/ops`: operations get `edit.Handle`s from
  the workspace resolver (which declares their write set), properties go
  through the codecs in `internal/pptx/ops/prop` (the core's `Codec` and
  `Set`), and new elements only through `Handle.Ensure` or the placed
  inserts: the core's (`AppendEach`, `ReplaceChildren`, ...) and the
  object-level wrappers in `edit/schema.go` (`InsertObject`, `MoveObject`,
  `DuplicateObject`, `GroupObjects`, `Ungroup`, `ReplaceParagraphs`).
  Code that orders children reads the generated schema table
  (`internal/ooxml/schema`) through `edit.ChildOrder`; do not keep a copy.
- `internal/pptx/ops` and everything it imports never depend on
  `internal/pptx/model` (the read model) **[enforced]**.
- The coverage report (`eval/coverage`) is regenerated with
  `go run ./tools/coverage` whenever a reader or codec changes
  **[enforced: staleness test]**. Do not claim coverage a test does not
  prove.

## xlsx specifics

`xlsx` `read`, `find`, `apply` and `create` work (P1 Track A1 and P2):
every operation of `docs/contract.md` (its xlsx parts, merged from
`docs/xlsx-contract-draft.md`, which stays as the record of the
decisions) runs through `internal/xlsx/apply` with the scoped integrity
gate. The design is `docs/design.md` (xlsx, and its "Write (P2)").

- Layering: operations (`internal/xlsx/ops`, one `run_<name>.go` each)
  are thin glue over pure kernels (strings and values in, strings, stats
  and errors out; no I/O, no `*edit.Sheet`) and reach the workbook only
  through the xlsx edit layer (`internal/xlsx/edit`), the only xlsx
  package that splices a part (archtest's `patcherPkgs`). Structural reference adjustment is
  `internal/xlsx/shift`, from a closed list (`help <op>`).
- Values: `range.set` `null` leaves a cell empty and `""` writes an
  empty string (owner decision); one switch point, `ops.EmptyFromNull`.
- Recalculation: the tool never computes a value. A save that changes a
  value, a formula or a sheet's structure sets `fullCalcOnLoad` and
  reports `stale-values`; the calcChain goes when a formula cell is
  added, removed or moved. `--recalc` asks LibreOffice headless
  (`DD_SOFFICE` or `PATH`, run through `internal/soffice`, which kills
  its process group) for the values and splices only `<v>` and `t`.
  Tests that need it skip without it; never open Excel for them.
- Streaming rule: a worksheet's `sheetData` and `sharedStrings.xml` are
  never parsed into a tree (no `Part.Tree()`, `tree.Parse` or
  `tree.ParseString` on them). Read them with `tree.Scan` and write them
  as splices; small parts (workbook, styles, tables, drawings) use the
  tree and the edit core. Non-test xlsx code may call `Part.Tree` or
  the tree parsers only in the files listed in archtest's `xlsxTreeFiles`;
  add a file there when it parses a small part **[enforced: archtest]**.
- Splices (`internal/ooxml/splice`): an edit to a large part is a
  `splice.Patch` of ordered, non-overlapping whole-element span
  replacements over the part's original string, saved with
  `Part.SetPatch` and declared with `WriteSet.DeclareSpliced`; Verify
  refuses any change outside the declared spans. Only the packages on archtest's
  `patcherPkgs` call `SetPatch` outside tests **[enforced: archtest]**.
- Xlsx vocabulary only via `sml`/`model`/`formula`: no namespace literal
  outside `sml`, no `"sheet:"`/`"name:"+` outside `model`, no `%26`
  outside formula column home (grandfathered shrink-only) **[enforced:
  `TestXlsxVocabulary`]**.
- `make excel-smoke SOURCE=in.xlsx OUT="out.xlsx ..."`
  (`tools/excel-smoke`) force-quits Excel: the maintainer approves every
  run, and agents never launch it themselves.
- The corpus is `XLSX_CORPUS` (gitignored; `make corpus` passes it
  through), fetched by `eval/corpus/fetch-xlsx.sh` at pinned commits.
  Excel behaviours the design relies on are unverified until experiments
  E1 to E13 (`testdata/xlsx/experiments`) are recorded in
  `docs/design.md`.

## Office oracles

- PowerPoint and Word are the only real check for "opens without repair".
  `tools/pptoracle` force-quits PowerPoint: never run it while the
  maintainer may have PowerPoint open; ask first, and check with
  `pgrep -x "Microsoft PowerPoint"` before each launch.
- PowerPoint is sandboxed: it opens and saves only in its granted work
  folder (`corpus/pptoracle`), as flat files, never subfolders.
- `make word-smoke SOURCE=in.docx OUT="out.docx ..."` (`tools/word-smoke`)
  opens a source and its outputs in Word, flat copies in the same folder,
  and fails an output that prompts for repair or fails to open where its
  source did not. It force-quits Word: the maintainer approves every run,
  and agents never launch it themselves.
- Never launch other GUI apps from automation.

## Delegated work

The general method (designer, implementers, dispatcher, reviewers; chunk
rules; gates) is in the maintainer's global agent instructions. What is
specific to this repository:

- Working files for a phase live in a gitignored `.worktrees/<phase>-work/`:
  `chunks.md` (the chunk plan and every expected-value row), `briefs/`,
  `logs/` (full output of every gate run), `pi-sessions/`, `state/done.txt`
  and the queue and gate scripts (`job.sh`, `queue.sh`, `gate.sh`,
  `merge.sh`). Chunks run in `.worktrees/<phase>-<id>` on
  `wt/<phase>-<id>`; they merge into an integration branch
  `wt/<phase>-int`, and the designer works on `wt/<phase>-design`.
- Chunk ids are unique across a phase, including fix chunks: never reuse
  an id a branch or worktree already has.
- Implementers run through the `pi` CLI, at most 5 at once, each in its own
  tmux window, with stdin closed (`</dev/null`). The maintainer chooses the
  model.
- Briefs tell implementers to search for existing helpers with `gcs "need"`
  (semantic Go function search) before writing one; reports use
  `NEEDS-EXPORT` (see Shared helpers) and `NEEDS-DESIGN` (a row that
  cannot hold, or a line needed in a file the chunk does not own).
- Extension points follow the docx and xlsx op layout: one `run_<name>.go`
  per operation; whether an operation is implemented is derived from that
  file, never from a flag in `table.go`; tests iterate the table. Shared
  test fixtures come from the format's test helper package (`vsdxtest`,
  `xlsxtest`).
- Scaling rows use `internal/laws`: `Scales` for bytes and allocations
  (absolute floor below which a ratio is not judged) and `TimeScales` for
  time (one fixed sample, no verdict under `-race`). Never hand-write a
  timing loop. Memory budgets checkpoint inside the call, and process
  peak RSS is gated in `make scaling` for the read paths.
- Fly Sprites (dd-ci) are not valid for time verdicts: fresh-heap page
  faulting there skews `TimeScales`. Run `make scaling` and a
  time-bearing `make ci` locally on an idle Mac. Sprites are fine for
  bytes and allocations, peak RSS on Linux, `-race` and corpus runs.
- Corpora are fetched by `eval/corpus/fetch-*.sh` at pinned commits into
  the gitignored `corpus/` (`PPTX_CORPUS`, `DOCX_CORPUS`, `XLSX_CORPUS`,
  `VSDX_CORPUS`, each the directory that holds `repos/`, not `repos/`
  itself). A corpus test skips only when its variable is unset;
  with it set, a named file that is missing fails the test. Third-party
  schemas whose licence forbids redistribution (MS-VSDX) are fetched by
  checksum into an ignored cache (`make vsdx-schema`); only data derived
  from them is committed.
- Office desktop apps are the only proof that output opens; where none is
  available (Visio), say so in the test's doc comment and the report.
  LibreOffice import is a render check, not a validity check.

## Evaluation integrity

- Trial agents get only the tool surface under test (the Pi extension in
  `eval/pi`, no shell or file tools). They must never be able to read task
  specs, rubrics, graders, other trials or the repo.
- A trial that touched hidden files is a harness error, not a result.
  Never tune a task or grader to a model's answer; fix a task only when the
  task itself is wrong, and record why in its `notes`.
