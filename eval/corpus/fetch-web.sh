#!/usr/bin/env bash
# Download the directly hosted web decks listed in web-sources.tsv and verify
# sha256. Usage: eval/corpus/fetch-web.sh [CORPUS_DIR]
# Decks land in CORPUS_DIR/web/<slug>.pptx. Licence per deck is in the TSV;
# "use-only" decks must not be redistributed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && git rev-parse --show-toplevel)"
DIR="${1:-${PPTX_CORPUS:-$ROOT/corpus}}"
TSV="$(cd "$(dirname "$0")" && pwd)/web-sources.tsv"
mkdir -p "$DIR/web"
tail -n +2 "$TSV" | while IFS=$'\t' read -r slug _cat _src url sha _size _lic _ev _sl _desc; do
  out="$DIR/web/$slug.pptx"
  if [ -f "$out" ] && [ "$(shasum -a 256 "$out" | cut -d' ' -f1)" = "$sha" ]; then
    echo "ok (cached) $slug"; continue
  fi
  curl -fsSL --max-filesize 50000000 -o "$out.part" "$url"
  got="$(shasum -a 256 "$out.part" | cut -d' ' -f1)"
  if [ "$got" != "$sha" ]; then
    rm -f "$out.part"; echo "sha256 mismatch for $slug: $got != $sha" >&2; exit 1
  fi
  mv "$out.part" "$out"; echo "ok $slug"
done
