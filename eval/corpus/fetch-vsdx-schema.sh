#!/usr/bin/env bash
# Fetch the Visio VSDX schema (MS-VSDX's XSD) into a gitignored cache.
# Usage: eval/corpus/fetch-vsdx-schema.sh [CACHE_DIR]
#
# The owner's ruling (2026-11-17): the schema may be used but never
# committed. Microsoft publishes it inline in the "Schema map (Visio
# XML)" page; this script downloads that page's Markdown source at a
# pinned commit of MicrosoftDocs/office-developer-client-docs, checks
# its sha256, extracts the one ```XML block (the XSD, unmodified), checks
# the XSD's sha256 and writes CACHE_DIR/visio.xsd and the source page
# beside it. Nothing is opened or run: the files are hashed and, when
# xmllint is present, checked for well-formedness.
#
# CACHE_DIR defaults to $VSDX_SCHEMA, else the vsdx-schema/ directory of
# the main checkout's corpus/ (tools/corpusdir), which is gitignored. The
# script refuses a CACHE_DIR inside a tracked, non-ignored path.
#
# Consumers: tools/vsdorder (the child-order table, chunk S3) and the
# opt-in `make vsdx-schema-check`. Neither make check nor make ci needs it.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE" && git rev-parse --show-toplevel)"
DIR="${1:-${VSDX_SCHEMA:-$(cd "$ROOT" && go run ./tools/corpusdir)/vsdx-schema}}"

COMMIT=f8c5dfed613fafd2fb8c8f9b1e43454f29517831
PAGE=docs/visio/schema-mapvisio-xml.md
URL="https://raw.githubusercontent.com/MicrosoftDocs/office-developer-client-docs/$COMMIT/$PAGE"
PAGE_SHA256=4e6935f7177e41f3ec0b012c48eb3cb13d1b51bf04547a242991c9bd229c9fe0
XSD_SHA256=29caea2b747dcc560cca0b112de30e3cbefe751ccd0e874618ff7dfb3ba0db76

mkdir -p "$DIR"
abs="$(cd "$DIR" && pwd -P)"
if top="$(cd "$abs" && git rev-parse --show-toplevel 2>/dev/null)"; then
  probe="$abs/visio.xsd"
  if ! git -C "$top" check-ignore -q "$probe"; then
    echo "fetch-vsdx-schema: $DIR is not gitignored; the schema must never be committed" >&2
    exit 1
  fi
fi

sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
page="$DIR/schema-mapvisio-xml.md"
if [ ! -f "$page" ] || [ "$(sum "$page")" != "$PAGE_SHA256" ]; then
  curl -fsSL --max-filesize 5000000 -o "$page.part" "$URL"
  mv "$page.part" "$page"
fi
got="$(sum "$page")"
[ "$got" = "$PAGE_SHA256" ] || { echo "fetch-vsdx-schema: page sha256 $got, want $PAGE_SHA256" >&2; exit 1; }

awk 'f && /^```$/ {exit} f {print} /^```XML$/ {f=1}' "$page" > "$DIR/visio.xsd.part"
mv "$DIR/visio.xsd.part" "$DIR/visio.xsd"
got="$(sum "$DIR/visio.xsd")"
[ "$got" = "$XSD_SHA256" ] || { echo "fetch-vsdx-schema: visio.xsd sha256 $got, want $XSD_SHA256" >&2; exit 1; }
if command -v xmllint >/dev/null; then xmllint --noout "$DIR/visio.xsd"; fi
printf 'source\t%s\ncommit\t%s\npage_sha256\t%s\nxsd_sha256\t%s\n' "$URL" "$COMMIT" "$PAGE_SHA256" "$XSD_SHA256" > "$DIR/SOURCE.tsv"
echo "done: $DIR/visio.xsd ($XSD_SHA256)"
