#!/usr/bin/env bash
# Reproduce the vsdx corpus (VSDX_CORPUS) at pinned commits.
# Usage: eval/corpus/fetch-vsdx.sh [CORPUS_DIR]
#
# Sources, pins and licences are in vsdx-sources.tsv beside this script;
# files and bytes there are the counts of Visio OOXML files (.vsdx, .vsdm,
# .vstx, .vssx, .vdx) under the source path at the pin, measured from the
# GitHub tree listing. The binary .vsd format is out of scope and never
# fetched. Each source's file list comes from the GitHub git-trees API at
# the pinned commit and each file from raw.githubusercontent.com, both over
# https with curl. Downloads are untrusted data: they are only hashed and
# listed (unzip -l for the zip formats, xmllint --noout for .vdx), never
# opened or run. A file failing that check is kept and recorded.
#
# CORPUS_DIR defaults to $VSDX_CORPUS, else the vsdx/ directory of the
# main checkout's corpus/ (tools/corpusdir). Files land in
# CORPUS_DIR/repos/<name>/<upstream path>, with each source's licence
# files at their upstream paths. CORPUS_DIR/MANIFEST.tsv lists source,
# path, upstream URL, commit, sha256, bytes, licence and sanity result.
# The corpus is gitignored and never committed; eval/corpus/vsdx-manifest.tsv
# is a committed copy of the manifest.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE" && git rev-parse --show-toplevel)"
DIR="${1:-${VSDX_CORPUS:-$(cd "$ROOT" && go run ./tools/corpusdir)/vsdx}}"
TSV="$HERE/vsdx-sources.tsv"
WANT='\.(vsdx|vsdm|vstx|vssx|vdx)$'
mkdir -p "$DIR/repos"
MAN="$DIR/MANIFEST.tsv"
printf 'source\tpath\tupstream_url\tcommit\tsha256\tbytes\tlicence\tsanity\n' > "$MAN"

sanity() { # file
  case "$1" in
    *.[vV][dD][xX]) if xmllint --noout "$1" >/dev/null 2>&1; then echo ok; else echo "FAIL: xmllint"; fi ;;
    *) if unzip -l "$1" >/dev/null 2>&1; then echo ok; else echo "FAIL: unzip -l"; fi ;;
  esac
}

tail -n +2 "$TSV" | while IFS=$'\t' read -r name _kind url commit path lic lic_files _files _bytes _notes; do
  slug="${url#https://github.com/}"
  d="$DIR/repos/$name"
  mkdir -p "$d"
  tree="$(curl -fsSL "https://api.github.com/repos/$slug/git/trees/$commit?recursive=1")"
  if printf '%s' "$tree" | grep -q '"truncated": *true'; then
    echo "fetch-vsdx: $name: tree listing truncated" >&2; exit 1
  fi
  { printf '%s' "$tree" | grep -o '"path": *"[^"]*"' | sed -E 's/^"path": *"//; s/"$//' \
      | grep -i -E "$WANT" | grep "^$path/" || true
    printf '%s\n' "$lic_files" | tr ',' '\n'; } > "$d/.want.txt"
  while IFS= read -r f; do
    [ -f "$d/$f" ] || {
      mkdir -p "$d/$(dirname "$f")"
      curl -fsSL --max-filesize 50000000 -o "$d/$f.part" \
        "https://raw.githubusercontent.com/$slug/$commit/$f"
      mv "$d/$f.part" "$d/$f"
    }
    if printf '%s' "$f" | grep -q -i -E "$WANT"; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$f" \
        "https://raw.githubusercontent.com/$slug/$commit/$f" "$commit" \
        "$(shasum -a 256 "$d/$f" | cut -d' ' -f1)" "$(wc -c < "$d/$f" | tr -d ' ')" \
        "$lic" "$(sanity "$d/$f")" >> "$MAN"
    fi
  done < "$d/.want.txt"
  echo "ok $name: $(grep -c -i -E "$WANT" "$d/.want.txt") Visio files"
done
echo "done: $DIR/repos, $MAN"
