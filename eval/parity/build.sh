#!/usr/bin/env bash
# Builds the pdraw reference set: the PDrawML examples of the sibling pdraw
# repo compiled to PPTX by `pdraw create`, plus image examples, into a
# scratch directory outside both repos.
#
#   eval/parity/build.sh [outdir]
#
# PDRAW_REPO (default: ../pdraw, the pdraw checkout beside this repository's
# root) is used read-only; cargo writes only its own target directory
# there. Decks are never committed: the committed input of make check is
# eval/coverage/pdraw-profile.json, produced from the decks by
#
#   go run ./tools/coverage --profile <outdir>/decks
#
# Writes <outdir>/COMMIT (pdraw's git commit, with "-dirty" when tracked
# files have uncommitted changes), <outdir>/src (the inputs) and
# <outdir>/decks/*.pptx.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here" && git rev-parse --show-toplevel)"
repo="${PDRAW_REPO:-$root/../pdraw}"
out="${1:-${TMPDIR:-/tmp}/pdraw-parity}"

[ -f "$repo/Cargo.toml" ] || { echo "no pdraw repo at $repo (set PDRAW_REPO)" >&2; exit 1; }
(cd "$repo" && cargo build -p pdraw-cli)
bin="$repo/target/debug/pdraw"
[ -x "$bin" ] || { echo "no pdraw binary at $bin" >&2; exit 1; }

commit="$(git -C "$repo" rev-parse HEAD)"
if [ -n "$(git -C "$repo" status --porcelain --untracked-files=no)" ]; then commit="$commit-dirty"; fi

rm -rf "$out"
mkdir -p "$out/decks" "$out/src"
echo "$commit" >"$out/COMMIT"

# Examples. skills/pdraw/examples: system and system-lanes (positioned),
# method-linear and method-branching (method dialect), communication
# (communication dialect), leader, loss-lanes. The benchmark's
# branching-positioned is the positioned form of a branching figure.
for f in "$repo"/skills/pdraw/examples/*.pdrawml \
         "$repo"/benchmarks/figure-authoring/skills/pdrawml/examples/branching-positioned.pdrawml; do
  cp "$f" "$out/src/$(basename "$f")"
done

# examples/vocabulary.pdrawml (committed beside this script) uses the
# positioned options the upstream examples leave out: dashed boxes, bottom
# and top labels, arrows at both ends, elbow and straight connectors,
# plain and arrowed polylines.
cp "$here"/examples/*.pdrawml "$out/src/"

# Image examples: a 1x1 PNG and a small SVG, written here so the set is
# reproducible without binary files in either repo.
printf 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==' | base64 -d >"$out/src/cell.png"
cat >"$out/src/cell.svg" <<'SVG'
<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 40 40"><circle cx="20" cy="20" r="16" fill="none" stroke="#000" stroke-width="2"/></svg>
SVG
cat >"$out/src/image.pdrawml" <<XML
<document>
  <slide size="letter" orientation="portrait">
    <image id="photo" x="72" y="72" width="200" height="150" path="$out/src/cell.png"/>
    <text id="caption" x="72" y="240">FIG. 2</text>
  </slide>
</document>
XML
cat >"$out/src/image-svg.pdrawml" <<XML
<document>
  <slide size="letter" orientation="portrait">
    <image id="drawing" x="72" y="72" width="120" height="120" path="$out/src/cell.svg"/>
  </slide>
</document>
XML

fail=0
for f in "$out"/src/*.pdrawml; do
  n="$(basename "$f" .pdrawml)"
  if ! "$bin" create "$out/decks/$n.pptx" --from "$f" >"$out/decks/$n.log" 2>&1; then
    echo "pdraw create failed for $n (see $out/decks/$n.log)" >&2
    fail=$((fail + 1))
    rm -f "$out/decks/$n.pptx"
  fi
done
echo "pdraw $commit: $(ls "$out"/decks/*.pptx | wc -l | tr -d ' ') decks in $out/decks, $fail failed"
