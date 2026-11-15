# documentdriver

Two command-line tools that let an agent read and edit Office documents
without breaking them, built on one lossless OOXML engine in standard-library
Go:

- `pptx` reads and edits the objects on PowerPoint slides (shapes,
  connectors, groups, pictures and their text) as native DrawingML.
- `docx` reads Word documents (text, tables, sections, comments, tracked
  changes, equations as LaTeX). Editing is being ported from docxdriver
  0.5.2; see [docs/plan.md](docs/plan.md) for what is in.

Both leave everything they did not target untouched, byte for byte. They
derive facts and never decide layout, routing, size or style.

## For agents

Both tools follow one contract, [docs/contract.md](docs/contract.md):

```sh
pptx read deck.pptx                       # slides, as JSON
pptx read deck.pptx --slide slide3        # the slide's objects
pptx help ops                             # the operations of a plan
pptx apply deck.pptx plan.json --dry-run  # check a plan; writes nothing
pptx apply deck.pptx plan.json -o out.pptx

docx read report.docx                     # markup, tables, sections, comments
docx find report.docx "indemnif"          # paragraphs containing the text
```

- A result is one JSON document on stdout. A failure is one errors object on
  stderr (`code`, `path`, `message`), exit 1; a wrong command line is exit 2.
- An edit is a plan: `{"ops": [{"op": "object.move", "at": "slide3/s17",
  "dx": 10}]}`, applied all or nothing. Values take the form `read` shows.
- `<tool> help`, `help <command>`, `help ops` and `help <op>` are the
  reference; the operation lists are generated from the tables the tools
  decode with.

## Layout

| Path | Role |
| --- | --- |
| `cmd/pptx`, `cmd/docx` | The two CLIs |
| `internal/cli` | The shared contract: command grammar, errors object, exit codes, plan envelope and decoder, generated help |
| `internal/opc` | Zip package: lazy reads, copy-through, atomic save, input limits |
| `internal/ooxml/tree` | Lossless XML tree (byte spans, prefixes, quoting) |
| `internal/ooxml/edit`, `schema`, `ctypes` | Edit core, schema-ordered inserts, content types from Office-authored files |
| `internal/plan` | Transactions, write set, edit result, hazards |
| `internal/htmlx` | The HTML front end both tools parse text input with |
| `internal/pptx` | Read model, derivations, operations, JSON and SVG views, render, laws, coverage |
| `internal/docx` | Read model, read and find, HTML projection, equations, plan grammar, integrity checks |
| `internal/laws`, `internal/archtest` | The law harness and the architecture rules |
| `eval` | pptxbench: corpus, tasks, graders, the agent arms and the Pi extension |
| `tools` | Dev-only: parity oracle, conformance gate, generators |
| `tools/release`, `packaging/python` | Release builds, the `documentdriver` wheels and SHA256SUMS ([RELEASING.md](RELEASING.md)) |

## Build and check

```sh
make build   # ./pptx and ./docx
make check   # vet, tests, gofmt, no-require
make ci      # the full local gate (see AGENTS.md)
make release VERSION=0.1.0   # dist/: binaries, wheels, SHA256SUMS (RELEASING.md)
```

Standard library only: `go.mod` has no `require` lines, and builds use
`CGO_ENABLED=0`. Read [AGENTS.md](AGENTS.md) before changing anything, then
[docs/plan.md](docs/plan.md) and [docs/design.md](docs/design.md).

## License

Apache License 2.0; see [LICENSE](LICENSE). The test corpora, their
manifest and the ECMA-376 schemas the conformance gate reads are not part
of this repository.
