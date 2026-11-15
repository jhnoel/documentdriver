#!/bin/sh
# Runner for excel-smoke (make excel-smoke): opens a source .xlsx and its
# outputs in Microsoft Excel and reports, per output, whether Excel asked to
# repair it or failed to open it, judged relative to the source.
#
#   tools/excel-smoke/run.sh SOURCE.xlsx OUTPUT.xlsx...
#
# It FORCE-QUITS Microsoft Excel before and after every file. Run it only
# when the maintainer has approved this run and Excel is not open. Files
# are copied flat into the folder Excel has been granted (corpus/pptoracle
# of the main checkout, or $PPTORACLE_WORKDIR) and removed afterwards.
# The JSON report is kept under results/excel-smoke/ (gitignored).
set -u

if [ "$#" -lt 2 ]; then
	echo "usage: tools/excel-smoke/run.sh SOURCE.xlsx OUTPUT.xlsx... (force-quits Excel; the maintainer approves each run)" >&2
	exit 2
fi
if pgrep -x "Microsoft Excel" >/dev/null 2>&1; then
	echo "excel-smoke: Microsoft Excel is running; save your work and quit it first" >&2
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
mkdir -p bin results/excel-smoke || exit 2
CGO_ENABLED=0 go build -trimpath -o bin/excel-smoke ./tools/excel-smoke || exit 2

log="results/excel-smoke/$(date -u +%Y%m%dT%H%M%SZ).jsonl"
# Split the output list on newlines only, so names with spaces survive.
old_ifs=$IFS
IFS='
'
# shellcheck disable=SC2086 # deliberate split on newlines
set -- $outs
IFS=$old_ifs
bin/excel-smoke --json --source "$src" "$@" >"$log"
code=$?
cat "$log"
echo "excel-smoke: exit $code; report $root/$log" >&2
exit "$code"
