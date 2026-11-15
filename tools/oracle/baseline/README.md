# docx parity baseline

Outputs of the pinned docxdriver 0.5.2 CLI (the parity oracle), recorded by
`go run ./tools/oracle record` and compared with the Go `docx` CLI by
`go run ./tools/oracle compare`.

## The oracle

docxdriver at commit `1ffb0d7` ("Bump workspace version to 0.5.2"), its
working tree clean, built without touching the docxdriver checkout except
its source files being read:

    cargo build --release --locked -p docxdriver-cli \
        --target-dir <this repo>/bin/oracle-target

run from the docxdriver checkout (`bin/` is gitignored). `oracle record`
uses `bin/oracle-target/release/docxdriver` unless `-oracle` or
`$DOCX_ORACLE` names another binary, and writes its `--version` line into
each manifest.

## Inputs

- `fixtures`: every `.docx` in the docxdriver repository's `test-docs/`,
  `crates/docxdriver-core/tests/fixtures/`,
  `crates/docxdriver-cli/tests/fixtures/` and the blank template
  `crates/docxdriver-core/assets/blank.docx` (7 files). The repository is
  `-docxdriver`, else `$DOCXDRIVER_REPO`, else `../docxdriver` beside the
  main checkout.
- `corpus-lo`: every `.docx` under docxdriver's `corpus/lo` (1,531
  LibreOffice import/export test files): `-corpus`, else `$DOCX_CORPUS`,
  else `<docxdriver>/corpus/lo`.
- `authored`: this repository's `testdata/docx` (see its README), small
  packages built to reach every reader.

## Command lines

Each input runs every case of `tools/oracle/cases.go` with the oracle's
default flags otherwise, from the input's own directory (docxdriver refuses
paths outside its working directory), with the file's base name:

| Case | Command |
| --- | --- |
| read-document | `docxdriver read <file>` |
| read-document-final | `docxdriver read <file> --view final` |
| read-document-original | `docxdriver read <file> --view original` |
| read-styles | `docxdriver read <file> --kind styles` |
| read-comments | `docxdriver read <file> --kind comments` |
| read-revisions | `docxdriver read <file> --kind revisions` |
| read-bookmarks | `docxdriver read <file> --kind bookmarks` |
| read-assets | `docxdriver read <file> --kind assets` |
| find | `docxdriver find <file> ""` |
| find-final | `docxdriver find <file> "" --view final` |
| find-original | `docxdriver find <file> "" --view original` |
| find-ignore-case | `docxdriver find <file> THE --ignore-case` |
| read-document-ui | `docxdriver read <file> --kind document_ui` |
| read-document-ui-final | `docxdriver read <file> --kind document_ui --view final` |
| read-document-ui-original | `docxdriver read <file> --kind document_ui --view original` |

The empty query matches every paragraph, so the find cases list every
paragraph entry. The read-document cases are the HTML projection: the agent
markup (kind document) and the UI profile (kind document_ui), in the three
views. `compare` runs the cases the Go CLI implements (`Ported` in
`cases.go`) unless `-cases` names others.

## Files

- `fixtures.tsv`, `authored.tsv`, `corpus-lo.tsv`: one line per input and case: path
  relative to the set's root, case, exit code, and the first 16 hex digits
  of the SHA-256 of stdout and of stderr (`-` when empty).
- `fixtures/<path>/<case>.stdout` and `authored/<path>/<case>.stdout` (and
  `.stderr` when not empty): the full outputs of the small sets.
- Full corpus outputs are written to the gitignored `results/oracle/oracle/`
  (`-out` changes the directory); `compare` writes the Go outputs to
  `results/oracle/docx/` and a per-run report to
  `results/oracle/compare-<set>.tsv`.

To re-record: `go run ./tools/oracle record` (all sets and cases), or
`-sets`, `-cases` and `-only` for a subset, which replaces only those
manifest lines.

## Outcomes

`compare` sorts every run into: identical (both succeed with identical
stdout, stderr and exit code), both-error-same (both fail identically),
differ (both succeed, outputs differ) and error-mismatch (one fails, or
both fail differently).

## Known differences

Where the oracle's output is wrong (wrong text or offsets, dropped
content, crashes, misleading errors, nondeterminism) the Go CLI gives the
correct output instead; where the oracle is right, parity is exact. Every
intended difference on a recorded input is listed in `known/read.tsv` (set,
file, case, reason), which `compare` reads: a listed run that differs
counts as "intended", anything unlisted still fails. The evidence:

1. **Oracle panic on a text leaf outside a run** (oracle bug, not copied).
   `segments.rs` `collect` calls `enclosing_run(..).expect("text element
   inside a run")`, so any `w:t` or `w:delText` that is not inside a `w:r`
   of its paragraph aborts the CLI. `read --kind revisions` on
   `testdata/docx/rich-bare-text.docx` prints
   `thread 'main' (<tid>) panicked at crates/docxdriver-core/src/segments.rs:80:49:
   text element inside a run` and dies on SIGABRT (recorded as exit -1;
   the thread id in its stderr changes per run). The Go port gives such a
   leaf no run and lists the revisions; `rich.docx`, the same package with
   the text in a run, is identical. No corpus file has such a leaf.
2. **Comment anchor offsets** (oracle bug, fixed). `read --kind comments`
   locators (`p<n>:<start>-<end>`) live in the paragraph's all-view segment
   space (`segments.rs`, also used by the revision locators), but
   `comments.rs` `walk_markers` counts the text of every
   `mc:AlternateContent` branch, the unsupported `mc:Choice` included, and
   of nested textbox paragraphs. `reading.rs` `comment_anchors` (the
   document read) walks correctly, so the two disagree. On
   `ooxmlexport/tdf154478.docx` and `tdf154481.docx` comment 8 is
   `p24:1-2739` in a paragraph whose all-view text has 19 characters; the
   Go CLI gives `p24:1-1`.
3. **Story targets** (oracle bug, fixed; no recorded input). `read --kind
   bookmarks` joins header, footer, note and comment relationship targets
   to the document's directory as written, so a target such as
   `./header1.xml` or `../word/header1.xml` names no part and its bookmarks
   are dropped; the Go CLI resolves targets like every other relationship
   (`resolve_rel_target`).
4. **Comment parts by relationship** (oracle bug, fixed; no recorded input).
   `comments.rs` reads `word/comments.xml`, `word/commentsExtended.xml`
   and `word/commentsIds.xml` at those fixed names. OPC finds parts through
   relationships: a package whose main part relates `notes/remarks.xml` as
   its comments lists no comments in the oracle, and an orphaned
   `word/comments.xml` that no relationship names is listed as if it were
   the document's. The Go CLI takes each part from the main document part's
   internal relationship of the `comments`, `commentsExtended` and
   `commentsIds` types (`internal/ooxml/ctypes`), with no fallback: the
   spec gives none. `TestCommentPartsByRelationship` covers renamed,
   orphaned and malformed parts. Every corpus/lo and fixture document
   relates its comment parts at the conventional names, so no recorded run
   changes. (`commentsExtensible` is not read by the listing.)
5. **Equations in paragraph entries** (intended surface change, as item 9).
   `find` and the document_ui paragraph entries list each paragraph's
   current equations as `{"equation", "latex", "display", "editable"}`,
   plus `"loss"` (the content the LaTeX leaves out, `docs/design.md`) when
   there is any: LaTeX from `math.OMMLToLaTeX` (item 9) where docxdriver
   gives `mathml`. find converts only the paragraphs it returns. With the
   real converter, the find runs on equation-bearing files (37 corpus
   files, `parity.docx`, both authored files) are identical once the
   `mathml` and `latex` (with `loss`) values are replaced by one token and
   the U+FFFC and line feeds of item 10 removed from the text (checked
   2026-11-13); they are listed in `known/read.tsv`.
6. **UTF-16 parts.** The tree reads UTF-8 only; the read model transcodes a
   part with a UTF-16 byte order mark first, as the oracle's parser reads
   UTF-16. No corpus/lo part is UTF-16 (checked 2026-11-13: 0 parts in
   1,531 files).
7. **CLI texts name the program `docx`**, not `docxdriver`; usage errors
   otherwise follow clap's wording (`cmd/docx/main_test.go` holds the
   captured cases). `--help` lists only the subcommands this binary has.
8. **Zip limits.** The Go reader applies `opc.DefaultLimits` (part size,
   total size, ratio, entry count); the oracle has none. No corpus file
   trips them. Duplicate part names (case-insensitively) are rejected by
   `opc`; the oracle reads them.
9. **Equations in the HTML projection** (intended surface change, planner
   decision 2026-11-13). The docx CLI has one equation format, LaTeX: the
   read-document cases render each equation as `<equation
   display="inline|block" data-docx-equation="N">LaTeX</equation>` (N its
   position among the paragraph's current equations, absent for an equation
   inside a deletion; `data-docx-loss` lists content the LaTeX leaves out),
   not docxdriver's `<math>` MathML. The converter is `math.OMMLToLaTeX`
   through `read.Options.Equation`. With it, replacing each `<math>…</math>`
   and `<equation>…</equation>` by one token (and, for document_ui, the
   paragraph entries' `mathml`/`latex` and the `html_start`/`html_end`
   offsets the shorter markup moves, and the U+FFFC and line feeds of item
   10), all 356 runs listed for the equation surface (items 5 and 9) are
   identical, apart from the 5 runs of `rich-bare-text.docx` that hit the
   oracle panic of item 1 (checked 2026-11-13). In the corpus one equation
   loses content (`m:r/w:commentReference`).
10. **Breaks and equations in text and offsets** (oracle bug B8, fixed;
   planner decision 2026-11-13). docxdriver counts `w:br`, `w:cr` and
   equations as no characters in read text, find and the segment offsets
   its edits select by, so a selection can span them unseen and a direct
   edit deletes them: on a paragraph `before`, display equation, `after`
   (`find` text "beforeafter"), `docxdriver replace-text --at 00000001
   --select eafte --with E --change-mode direct` writes `befor`, `E`, `r`,
   the equation gone; on `one`, `w:br`, `two`, `--select etw --with X`
   writes `on`, `X`, `o`, the break gone (checked 2026-11-13 with the
   pinned oracle). The Go read model (`model.LeafText`) counts a break as
   "\n" and an equation (`m:oMath` or `m:oMathPara`, whole) as one U+FFFC
   in paragraph text, segments and the document read's selections and
   comment anchors; the HTML still shows `<br>` and `<equation>`. The 1,414
   listed runs (find, the document reads, two revision listings) are those
   identical before this change and different after it, so the change is
   their only cause. `read --kind comments` locators count the same way;
   one corpus run changes (`wDateValueFormat.docx`, a comment over a break:
   `p356:0-1` where docxdriver gives `p356:0-0`).
11. **Tabs, hyphens, symbols and objects in text and offsets** (oracle bug
   B8b, fixed with the span engine, 2026-11-13). docxdriver's segments
   count only `w:t` and `w:delText`, so every other run leaf is zero
   width and a direct edit whose selection crosses it deletes it. On a
   paragraph `one`, X, `two` (`find` text "onetwo") with X each of
   `w:tab`, `w:ptab`, `w:noBreakHyphen` and `w:sym`, `docxdriver
   replace-text --at <id> --select etw --with X --change-mode direct` writes
   `on`, `X`, `o`: the tab, hyphen or symbol is gone (checked 2026-11-13
   with the pinned oracle). The Go text model has one taxonomy
   (`model.Classify`, table in `docs/design.md`): `w:tab` and `w:ptab`
   count as "\t" (docxdriver's own bookmark text already does),
   `w:noBreakHyphen` and `w:softHyphen` as U+2011 and U+00AD, and `w:sym`,
   `w:drawing`, `w:pict`, `w:object`, `w:contentPart`, note references and
   marks, separators, `w:pgNum` and the date blocks as one U+FFFC. The
   document read's `selections` also lists paragraphs holding the new
   leaves the HTML does not show as text (`w:cr`, `w:sym`, `w:object`, ...).
   The 5,021 listed runs (5,018 corpus, 1 fixture, 2 authored) are those
   identical before this change and different after it; with those
   characters removed and offsets masked every one matches its old output,
   except 114 document reads that gain `selections` entries and three files
   (`image-comment-at-char.docx`, `artistic_effects.docx`,
   `activex_textbox.docx`) where a comment or selection now covers an image
   (`select` "￼", where docxdriver gave an empty selection).
12. **Strict documents** (intended surface change, P4e 2026-11-13). The Go
   reader reads ISO/IEC 29500 Strict packages (the Strict namespaces of
   WordprocessingML and its relationship and DrawingML families) like
   Transitional ones (contract.md §10); docxdriver refuses them at open.
   The 7 Strict corpus files' runs (107) are listed in `known/read.tsv`.
13. **Lazy open** (intended, P4e 2026-11-13). Parts are read only when a
   command needs them, so a corrupt member nothing reads (a stored member
   whose CRC does not match, `tools/oracle/testdata/write/lazy.docx`) no
   longer fails the open (contract.md §10, degraded read); docxdriver
   reads every member up front. The write baseline's dirty family runs
   its positive plans on that fixture.
14. **Stored paraIds** (intended, P4c 901c503, advisor option b). A
   stored `w14:paraId` is the paragraph's address even when it repeats or
   is 80000000 or above: the second paragraph sharing an id is `id~2`
   (contract.md §6), and `idRepairsNeeded` counts neither. docxdriver
   rewrites duplicate and out-of-range ids to fresh provisional ones on
   every read and save, so addresses change between saves and the
   `w15:paraId`/`paraIdParent` references to them break. Only a value
   that is not eight hex digits stays provisional, with a note. 84 runs
   in 37 files are listed (read-document, read-document-ui,
   read-bookmarks); `parity.docx`'s find with query "x" is not a recorded
   case.
15. **Grid columns count `w:gridBefore`** (intended surface change, P5
   tables). docxdriver gives no grid column; the contract's read shows
   each cell's `grid_column`, and a row's `w:gridBefore` columns count
   in it, as they do in `table:N/column:N` addresses (the table
   operations' grid model): the first cell of a row that skips two
   columns is in grid column 3. The oracle projection
   (`read.FromOracle`) counts them the same way, so no recorded run
   differs and `known/read.tsv` lists none.
16. **Addresses of header, footer and note paragraphs** (intended surface
   change, P5 span). The text and bookmark operations address a
   paragraph of a header, footer, note or comment by its stored
   `w14:paraId` (`~n` for the n-th sharing it across those stories; a
   body paragraph with the same address wins), so the Go markup gives
   such a header, footer or note paragraph that `id` (no `data-ord`) and
   an equation in it a `data-ref` with that paragraph prefix; docxdriver's
   gives them none. `oracle compare` removes those addresses from the Go
   side before comparing (`read.WithoutStoryAddresses`: inside `<header>`,
   `<footer>` and `<aside data-notes>`), so no recorded run differs and
   `known/read.tsv` lists none; `TestDocumentMatchesOracle` does the same.
