# docx integrity checks

`internal/docx/integrity` ports the WML integrity checks of docxdriver 0.5.2
(`crates/docxdriver-core/src/integrity/{mod,schema,tree}.rs` at 1ffb0d7).
`Check` reports the same findings, part and message, as docxdriver's
`integrity::check`; `Gate` is the commit rule (an edit may not introduce an
error kind its input lacked). `go run ./tools/docxintegrity FILE...` prints
findings in the format of docxdriver's `examples/integrity.rs`.

## Located findings and the edit gate

Every finding is also kept as an `Instance`, located at the elements of
its part it is about, by their position in document order: the element
with the bad attribute or count, the misplaced child, the dangling
reference, every element sharing a duplicate id, the starts or ends left
unpaired, the table, row or cell. Faults of a part as a whole (it does
not parse, has no content type) and of the package have no elements.
Byte spans are left for later (P5).

`GateEdit(before, after, ws, log)` is the commit rule of a plan, with
faults attributed to what the plan did. The plan's workspace (the edit
core's `*edit.Workspace`) gives the correspondence: an element parsed
from the input keeps its byte span there through edits and moves, a
created one has none. A finding is refused only when its element was
created or changed, or is an unchanged element whose reference or pairing
the edit broke; the element did not have that message, as often, in the
input; and, for a created element, it is no copy of an input element
that had it. Copies are known exactly: the edit core records which
element each new half of `SplitAt` or `SplitText` and each `Copy` (with
its descendants) was made from (`Workspace.SourceOf`; a format layer that
rebuilds an element from another's bytes declares it with `RecordCopy`),
and the gate follows that chain to an input element. An element parsed
or placed fresh is never a copy, even when its bytes equal a faulty
input element's. Moved content keeps its identity and so its findings. Unlocated
faults, and parts the working tree does not account for, are compared by
count per part and message, as `Gate` does. `OpLog.Record`, called after
each operation, names the operation that first declared or created the
element (else the part); `GateFailure.Op` is the earliest, and
`GateFailure.Located` lists each fault with its element ("new w:jc in
paragraph 1A2B3C4D"). `Gate` (by count) and `GateCreated` (create: no
fault at all) are kept.

## Parity

The Go checks keep docxdriver's findings where docxdriver is right and
fix it where it is wrong; every difference is listed below and in
`intendedDifferences` (`differences_test.go`), and the parity tests accept
a difference only when a listed one accounts for every finding involved.

Measured 2026-11-13 on docxdriver's `test-docs`, its test fixtures under
`crates/` and `corpus/lo` (1,538 documents): 1,536 identical, 2 differ, each
by a listed difference (a template main part the ctypes table does not
list, and one named attribute error). `testdata/rules.golden` holds docxdriver's findings on 22 packages
written to break the rules the corpus never breaks (145 finding kinds): 21
identical, one differs by two listed differences (an impossible date, the
xml:space type's name).
`xml_test.go` records docxdriver's parser verdicts on 120 XML inputs; 96 are
kept and the 24 in `parseFixes` corrected.

To compare again, build docxdriver's own example outside its tree and run
it from its checkout, then point the test at the output:

```sh
# in the docxdriver checkout (nothing is written there)
cargo build --locked --release -p docxdriver-core --example integrity \
  --target-dir <this repo>/bin/rust-target
find test-docs crates -name '*.docx' -not -path '*/target/*' | sort > /tmp/f
find corpus/lo -name '*.docx' | sort >> /tmp/f
tr '\n' '\0' < /tmp/f | xargs -0 <this repo>/bin/rust-target/release/examples/integrity > oracle.tsv
# here
INTEGRITY_ORACLE_TSV=oracle.tsv go test ./internal/docx/integrity -run TestOracleParity -v
```

`DOCXDRIVER_REPO` (default `../docxdriver` beside the main checkout) and
`DOCX_CORPUS` (default its `corpus/lo`) locate the inputs; `TestCorpus`
runs on them whenever they exist.

## Intended differences

docxdriver wrong, fixed here:

- **Non-well-formed XML accepted.** quick-xml accepted, and docxdriver
  passed, parts XML 1.0 or Namespaces in XML forbid: `<` in an attribute
  value (XML 1.0 [10] AttValue), attributes without white space between
  them ([40] STag), an XML declaration not at the very start, repeated, or
  without `version` or with version 2.0 ([23] XMLDecl, 2.8), a processing
  instruction whose target is not a Name ([16] PI), a prefix other than
  `xml` bound to the xml namespace or any prefix bound to the xmlns
  namespace (Namespaces in XML 1.0, 3), and a CDATA section outside the
  root element ([1] document). Each is now an `xml:` finding naming the
  fault. Evidence: `parseFixes`.
- **Attribute errors named.** docxdriver kept the text before the first
  colon of quick-xml's message, so every attribute syntax error read
  "not well-formed: position " (from "position 197: duplicated
  attribute..."). They now read "duplicate attribute", "malformed
  attribute" or "invalid element name". Corpus: `tdf165348_broken_package`.
- **NCName.** docxdriver tested Rust's alphabetic and alphanumeric classes
  for relationship Ids and xsd:NCName/ID/IDREF values; they refuse valid
  names with combining marks and digits from other scripts in the wrong
  places and admit some invalid ones. The XML Name productions are used
  instead; element and attribute names are left to the tokenizer, which
  applies them already.
- **xsd:double and xsd:float** accepted Rust's `inf`, `infinity` and `nan`
  in any case and sign; XSD 1.0 allows `INF`, `-INF` and `NaN` only.
- **xsd:dateTime** was checked by pattern only, so 2023-02-29 or 04-31 were
  valid; days are now checked against the month (proleptic Gregorian, as
  XSD). Rules case `schema`.
- **Zip.** An entry neither stored nor deflated read as "entry cannot be
  read" because the zip crate, built with deflate only, refused it before
  docxdriver's own "neither stored nor deflated" check; that check now
  reports. An entry whose length differs from its declared size is "entry
  cannot be read" (docxdriver checked the CRC only).

Not docxdriver's fault:

- **Expected content types come from `internal/ooxml/ctypes`**, not
  docxdriver's hand-kept list, for the relationship types docxdriver
  checked. The table keeps types two Office-authored files use (ctypesgen's
  rule, lowered from three so that it holds commentsExtensible, which Word
  writes in two corpus files). It lacks the Word template and
  macro-enabled main types, which no two Office-authored files use, and
  lists the PresentationML main and comments types too. Corpus effect:
  `n779642.docx`, the one file with a Word template main part, gets a
  finding docxdriver did not report. Not lowered to one file: a rewritten
  test file that names Word (`tdf165642_glossaryFootnote.docx`) would then
  make `application/xml` an expected type of settings, footnotes, endnotes
  and glossary parts and hide three correct findings. No Office-authored
  file uses altChunk, so `aFChunk` stays spelled as the relationships
  namespace plus its last segment.
- **xml:space's type** is anonymous in xml.xsd; docxdriver's generator
  named it `anonymous1`, so a bad value read "is not a valid anonymous1".
  The tables name it by its attribute: "is not a valid xml:space".
- **Limits.** Entries are read within `opc.Limits`; an entry over a limit
  is "package: entry exceeds a safety limit", too many entries "package:
  exceeds a safety limit (too many entries)", nesting deeper than
  `tree.MaxDepth` "not well-formed: elements nested more than 512 deep".
  docxdriver had no limits, and allocated each entry's declared size up
  front.
- **Parser, and what this check is not.** Parts are read with
  `internal/ooxml/tree`, the engine's own tokenizer; docxdriver read them
  with a second parser (quick-xml) so that a fault the engine's parser and
  writer share could not hide. These checks are therefore not independent
  of the engine's parser: a part the tokenizer misreads in a way it also
  writes would pass. The independent check is `make conformance`, which
  validates outputs with lxml against the ECMA-376 schemas and with the
  Open XML SDK. A well-formed character reference longer than the
  tokenizer reads is rewritten and parsed again. With several faults in
  one part the first reported may differ (namespace faults are found
  after syntax faults).
- **No round-trip check.** docxdriver refused an edit when a rewritten part
  did not survive its XML reader and writer. The Go engine's lossless tree,
  copied untouched spans and `plan.WriteSet` verification make it
  unnecessary (docs/design.md).
- **Scoped gate.** `Gate` checks only the changed parts and their partners
  (relationships, the main document, and all WordprocessingML parts when
  any of them changed); docxdriver checked every part of both packages.
  The answer is the same (`TestGateMatchesFullCheck`): it falls back to
  whole packages when names or content types differ, and to the input's
  full schema findings when a new schema finding appears.

Other docxdriver bugs, without effect on findings: `percent_decode` slices
a `&str` at byte offsets, so a target such as `%aé` panics.

## Schema tables

The content model comes from `internal/ooxml/schema`: `ContentModel`,
`RootType` and `SimpleType`, generated by `gen_schema.go` from the same
pinned zip as the slot tables (plus the W3C xml.xsd for xml:space, which
the zip lacks), with the algorithm of docxdriver's
`scripts/generate-wml-model.py`. Generated from the zip, it equals
docxdriver's `wml-model.json` (generated from `tools/conformance/
ooxml-schemas`) on every type, child, rank, count, required flag,
wildcard, attribute, root and simple type, except the name of xml:space's
type (above). Simple types are checked through the tables' resolved form
(built-in, most derived enumeration, every step's patterns, bounds,
lengths), which is equivalent to docxdriver's step-by-step walk for every
type the model uses.

## Not ported yet

The tests of docxdriver's `tests/integrity.rs` that commit edits through
the plan engine (11) wait for the docx operations (P5); their integrity
assertions on inputs are ported, and `TestEngineDependentGateTests` names
each with what to assert.
