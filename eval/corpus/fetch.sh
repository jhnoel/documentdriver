#!/usr/bin/env bash
# Reproduce the corpus clones at pinned commits.
# Usage: eval/corpus/fetch.sh [CORPUS_DIR]
# Shallow, blob-filtered clones; only deck blobs and licence files are
# materialised. Raw clones live in the gitignored <repo>/corpus (or $PPTX_CORPUS) and are never committed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && git rev-parse --show-toplevel)"
DIR="${1:-${PPTX_CORPUS:-$ROOT/corpus}}"
mkdir -p "$DIR/repos"

# name|url|pinned commit
REPOS=(
  "libreoffice-core|https://github.com/LibreOffice/core|83d264f2f0a6f2944d524c9a55cb2a0764d12255"
  "apache-poi|https://github.com/apache/poi|1243387d7ffbac84d361323673f1b75a5b3c26f3"
  "open-xml-sdk|https://github.com/dotnet/Open-XML-SDK|431ab05cf160248cc3885a4a766026d4f8243792"
  "python-pptx|https://github.com/scanny/python-pptx|278b47b1dedd5b46ee84c286e77cdfb0bf4594be"
  "apache-tika|https://github.com/apache/tika|eedcbd14a72f536f8bc1ef3d669037540a2cc196"
  "shapecrawler|https://github.com/ShapeCrawler/ShapeCrawler|2ab474918ac89e7bbb8d7c649aca708d298171e2"
  "pptx-automizer|https://github.com/singerla/pptx-automizer|2ef0cf78ff4bf2f53b92049f176aa7221d81cef0"
  "markitdown|https://github.com/microsoft/markitdown|ed178952a6672abdfd35b5f9e76a80b6cefc086e"
  "pandoc|https://github.com/jgm/pandoc|300d24fdb143cff23080120c13acbe7df21cd5a3"
  "open-xml-powertools|https://github.com/OfficeDev/Open-XML-PowerTools|5881422a881f6ccefce2b9801b5dc6a753670d6e"
)

for r in "${REPOS[@]}"; do
  IFS='|' read -r name url commit <<<"$r"
  d="$DIR/repos/$name"
  if [ ! -d "$d/.git" ]; then
    git init -q "$d"
    git -C "$d" remote add origin "$url"
    git -C "$d" config extensions.partialClone origin
    git -C "$d" config remote.origin.promisor true
    git -C "$d" config remote.origin.partialclonefilter blob:none
  fi
  git -C "$d" fetch -q --depth 1 --filter=blob:none origin "$commit"
  git -C "$d" checkout -q --detach --force FETCH_HEAD 2>/dev/null || true
  # Fetch just the wanted blobs in one batch, then write them to the worktree.
  git -C "$d" ls-tree -r --name-only "$commit" \
    | grep -i -E '\.(pptx|potx|ppsx|pptm)$|(^|/)(LICENSE|LICENSE\.md|LICENSE\.txt|COPYING|LICENSE\.TXT)$' \
    > "$d/.git/want.txt" || true
  git -C "$d" sparse-checkout init --no-cone
  git -C "$d" sparse-checkout set --no-cone --stdin < "$d/.git/want.txt"
  git -C "$d" checkout -q --detach --force "$commit"
done
echo "done: $DIR/repos"
