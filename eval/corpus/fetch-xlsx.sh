#!/usr/bin/env bash
# Reproduce the xlsx corpus (XLSX_CORPUS) at pinned commits.
# Usage: eval/corpus/fetch-xlsx.sh [CORPUS_DIR]
#
# Sources, pins and licences are in xlsx-sources.tsv beside this script;
# files and bytes there are the counts of spreadsheet files (.xlsx, .xlsm,
# .xltx, .xltm) at the pin, measured from the hosts' tree listings. Git
# sources are shallow, blob-filtered, sparse clones of the listed path, as
# in fetch.sh: only spreadsheet blobs and the licence file are
# materialised. openpyxl is Mercurial on Heptapod, so its files are
# fetched one by one through the GitLab API at the pinned commit.
#
# CORPUS_DIR defaults to $XLSX_CORPUS, else the xlsx/ directory of the
# main checkout's corpus/ (tools/corpusdir), which `make corpus` passes
# as XLSX_CORPUS. Files land in CORPUS_DIR/repos/<name>/<path>. The corpus
# is gitignored and never committed. EPPlus samples are excluded
# (Polyform Noncommercial licence); SheetJS test_files and a web set of
# public-sector workbooks are planned separately (docs/design.md, xlsx).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE" && git rev-parse --show-toplevel)"
DIR="${1:-${XLSX_CORPUS:-$(cd "$ROOT" && go run ./tools/corpusdir)/xlsx}}"
TSV="$HERE/xlsx-sources.tsv"
WANT='\.(xlsx|xlsm|xltx|xltm)$'
mkdir -p "$DIR/repos"

fetch_git() { # name url commit path licence_file
  local name="$1" url="$2" commit="$3" path="$4" lic="$5" d="$DIR/repos/$1"
  if [ ! -d "$d/.git" ]; then
    git init -q "$d"
    git -C "$d" remote add origin "$url"
    git -C "$d" config extensions.partialClone origin
    git -C "$d" config remote.origin.promisor true
    git -C "$d" config remote.origin.partialclonefilter blob:none
  fi
  git -C "$d" fetch -q --depth 1 --filter=blob:none origin "$commit"
  local scope=()
  if [ "$path" != "." ]; then scope=(-- "$path"); fi
  { git -C "$d" ls-tree -r --name-only "$commit" "${scope[@]}" | grep -i -E "$WANT" || true
    echo "$lic"; } > "$d/.git/want.txt"
  git -C "$d" sparse-checkout init --no-cone
  git -C "$d" sparse-checkout set --no-cone --stdin < "$d/.git/want.txt"
  git -C "$d" checkout -q --detach --force "$commit"
}

fetch_heptapod() { # name url commit path licence_file
  local name="$1" url="$2" commit="$3" path="$4" lic="$5" d="$DIR/repos/$1"
  local host project
  host="$(printf %s "$url" | sed -E 's|^(https://[^/]+)/.*|\1|')"
  project="$(printf %s "${url#"$host"/}" | sed 's|/|%2F|g')"
  local api="$host/api/v4/projects/$project/repository"
  mkdir -p "$d"
  local list="$d/.files.txt" page=1 got
  : > "$list"
  while :; do
    got="$(curl -fsSL "$api/tree?recursive=true&ref=$commit&per_page=100&page=$page")"
    [ "$got" = "[]" ] && break
    printf '%s\n' "$got" | grep -o '"path":"[^"]*"' | sed 's/^"path":"//; s/"$//' >> "$list"
    page=$((page + 1))
  done
  { grep -i -E "$WANT" "$list" | { if [ "$path" = "." ]; then cat; else grep "^$path/"; fi; } || true
    echo "$lic"; } > "$d/.want.txt"
  while IFS= read -r f; do
    [ -f "$d/$f" ] && continue
    mkdir -p "$d/$(dirname "$f")"
    curl -fsSL --max-filesize 50000000 -o "$d/$f.part" \
      "$api/files/$(printf %s "$f" | sed 's|/|%2F|g')/raw?ref=$commit"
    mv "$d/$f.part" "$d/$f"
  done < "$d/.want.txt"
  echo "$commit" > "$d/.commit"
}

tail -n +2 "$TSV" | while IFS=$'\t' read -r name kind url commit path _lic lic_file _files _bytes _notes; do
  case "$kind" in
    git) fetch_git "$name" "$url" "$commit" "$path" "$lic_file" ;;
    heptapod) fetch_heptapod "$name" "$url" "$commit" "$path" "$lic_file" ;;
    *) echo "fetch-xlsx: $name: unknown kind $kind" >&2; exit 1 ;;
  esac
  n="$(find "$DIR/repos/$name" -type f \( -iname '*.xlsx' -o -iname '*.xlsm' -o -iname '*.xltx' -o -iname '*.xltm' \) -not -path '*/.git/*' | wc -l | tr -d ' ')"
  echo "ok $name: $n spreadsheet files"
done
echo "done: $DIR/repos"
