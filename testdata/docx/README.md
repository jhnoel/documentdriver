# docx fixtures

Authored for documentdriver; no third-party content. Both are built by
`richDoc` in `internal/docx/read/fuzz_test.go` (stored zip members, fixed
times, so the bytes are reproducible) and kept equal to it by
`TestAuthoredFixtures`; `DOCX_UPDATE_FIXTURES=1 go test ./internal/docx/read
-run TestAuthoredFixtures` rewrites them.

- `rich.docx`: numbering with a level override and restart, a style with
  numbering, comments with a resolved thread and a reply, tracked
  insertions, deletions, a paragraph-mark deletion, a property change and a
  row insertion, bookmarks in the body and a header, a header with a
  tracked change, two image parts with the same bytes, an equation and an
  mc:AlternateContent.
- `rich-bare-text.docx`: the same with one `w:t` directly in its paragraph,
  outside any run. docxdriver 0.5.2 panics listing its revisions (see
  `tools/oracle/baseline/README.md`).

The parity oracle runs them as its `authored` input set.
