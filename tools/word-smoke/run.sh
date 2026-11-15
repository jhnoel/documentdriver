#!/bin/sh
# Runner for word-smoke (make word-smoke): opens a source .docx and its
# outputs in Microsoft Word and reports, per output, whether Word asked to
# repair it or failed to open it, judged relative to the source.
#
#   tools/word-smoke/run.sh SOURCE.docx OUTPUT.docx...
#
# It FORCE-QUITS Microsoft Word before and after every file. Run it only
# when the maintainer has approved this run and Word is not open. Files
# are copied flat into the folder Word has been granted (corpus/pptoracle
# of the main checkout, or $PPTORACLE_WORKDIR) and removed afterwards.
# The JSON report is kept under results/word-smoke/ (gitignored).
set -u

if [ "$#" -lt 2 ]; then
	echo "usage: tools/word-smoke/run.sh SOURCE.docx OUTPUT.docx... (force-quits Word; the maintainer approves each run)" >&2
	exit 2
fi
if pgrep -x "Microsoft Word" >/dev/null 2>&1; then
	echo "word-smoke: Microsoft Word is running; save your work and quit it first" >&2
	exit 2
fi

abs() {
	case "$1" in
	/*) printf '%s\n' "$1" ;;
	*) printf '%s/%s\n' "$PWD" "$1" ;;
	esac
}

src=$(abs "$1")
shift
outs=""
for f in "$@"; do
	outs="$outs
$(abs "$f")"
done

root=$(git rev-parse --show-toplevel) || exit 2
cd "$root" || exit 2
mkdir -p bin results/word-smoke || exit 2
CGO_ENABLED=0 go build -trimpath -o bin/word-smoke ./tools/word-smoke || exit 2

log="results/word-smoke/$(date -u +%Y%m%dT%H%M%SZ).jsonl"
# Split the output list on newlines only, so names with spaces survive.
old_ifs=$IFS
IFS='
'
# shellcheck disable=SC2086 # deliberate split on newlines
set -- $outs
IFS=$old_ifs
bin/word-smoke --json --source "$src" "$@" >"$log"
code=$?
cat "$log"
echo "word-smoke: exit $code; report $root/$log" >&2
exit "$code"
