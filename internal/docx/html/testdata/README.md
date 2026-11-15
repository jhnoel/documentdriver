# HTML input golden set

`golden.json` records, for each input, what docxdriver 0.5.2 (commit
1ffb0d7) does on its HTML input path and how the Go port relates to it:
per aspect (`tree`: the parsed HTML; `frag`: `parse_fragment` items;
`doc`: document HTML split into sections and parsed) a class and, for `match`, a SHA-256 prefix of
docxdriver's result in the canonical JSON form of `diff_test.go`. The other
classes are the intended differences documented in `intendedClass`,
including `latex-equations`: docx accepts equations only as
`<equation display="inline|block">LaTeX</equation>`. The harness still
records `canonical_mathml`, which the Go side no longer has.

Inputs: every HTML-looking string literal in docxdriver's Rust sources and
tests, its JS tests, skills and docs (`oracle/extract.pl`), up to 300 bytes,
plus hand-written cases (`oracle/extra.pl`), plus one deterministic
mutation (truncation, deletion or duplication; seed 42) per tokenizer error
code. The full run (2,358 inputs including all mutations) is reproduced
with `oracle/run.sh`, a throwaway harness that compiles docxdriver's
`input.rs`, `parse.rs` and `mathml_omml.rs` by path, with the real
`latex2mathml` and MathML validator, and never writes into the docxdriver
checkout.
