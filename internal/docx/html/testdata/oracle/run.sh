#!/bin/sh
# Rebuild the docxdriver oracle results the differential test reads.
#
#   run.sh DOCXDRIVER_CHECKOUT OUTDIR
#
# DOCXDRIVER_CHECKOUT is a docxdriver checkout at commit 1ffb0d7 (0.5.2),
# read only. OUTDIR receives the harness crate, its build and results.json;
# keep it outside the docxdriver checkout. Needs cargo (with xmloxide 0.4.3,
# regex-lite, latex2mathml 0.2.3 and serde_json in the local registry) and
# perl. Then:
#
#   DOCX_HTMLDIFF=OUTDIR/results.json go test ./internal/docx/html -run TestDifferential -v
#   (add DOCX_HTMLDIFF_WRITE=1 to rewrite testdata/golden.json)
set -eu
R="$1/crates/docxdriver-core/src"
O="$2"
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$O/src"
cp "$here/Cargo.toml" "$O/"
cp "$1/Cargo.lock" "$O/"
sed "s|DOCXDRIVER|$1|" "$here/src/main.rs" > "$O/src/main.rs"
cp "$here/src/harness.rs" "$O/src/"
# The plain-data items input.rs and parse.rs use from their siblings,
# extracted by line range from the pinned commit.
sed -n 13,108p "$R/api.rs" | sed 's/#\[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, JsonSchema)\]/#[derive(Debug, Clone, Copy, PartialEq, Eq)]/; s/#\[serde(rename_all = "camelCase")\]//' > "$O/src/api.rs"
sed -n 24,297p "$R/html/mod.rs" > "$O/src/html_types.rs"
{ echo 'use super::{escape_attr, Fmt, VertAlign};'; echo 'use std::collections::BTreeMap;'; sed -n 7,169p "$R/html/styles.rs"; sed -n 357,414p "$R/html/styles.rs"; } > "$O/src/styles.rs"
sed -n 535,617p "$R/html/surface.rs" > "$O/src/surface.rs"
sed -n '/^fn parse_leading_chrome_element/,/^}/p' "$R/html/build.rs" > "$O/src/chrome_elem.rs"
(cd "$O" && cargo build --offline --release)
perl "$here/extract.pl" "$1" > "$O/inputs.json"
perl "$here/extra.pl" < "$O/inputs.json" > "$O/cases.json"
"$O/target/release/htmldiff" < "$O/cases.json" > "$O/results.json"
