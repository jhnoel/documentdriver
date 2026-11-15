# planoracle

A throwaway harness that runs docxdriver 0.5.2's `parse_plan_json`,
`Plan::canonical_json` and `Plan::preview_key` (docxdriver-core at
1ffb0d7) over JSON lines `{"text": "<plan>"}` and prints one result line
each. The docx CLI's `plan` command reads TOML, so the harness calls the
JSON entry point directly.

Build it outside the docxdriver checkout (which stays read-only), with the
target directory in this repository's ignored `bin/`:

    cp -r internal/docx/ops/testdata/oracle/planoracle "$SCRATCH/planoracle"
    sed "s|DOCXDRIVER|$DOCXDRIVER|" "$SCRATCH/planoracle/Cargo.toml.in" > "$SCRATCH/planoracle/Cargo.toml"
    cp "$DOCXDRIVER/Cargo.lock" "$SCRATCH/planoracle/"
    cargo build --release --manifest-path "$SCRATCH/planoracle/Cargo.toml" --target-dir bin/planoracle-target
    DOCX_PLAN_ORACLE=$PWD/bin/planoracle-target/release/planoracle \
        go test ./internal/docx/ops -run TestOracleRegenerate

Copying docxdriver's Cargo.lock pins serde_json 1.0.150, whose number
parsing and float formatting the Go parser reproduces.
