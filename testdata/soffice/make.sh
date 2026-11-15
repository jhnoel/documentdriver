#!/bin/sh
# Regenerates diagram.pptx from diagram.fodp with headless LibreOffice.
# A throwaway user profile keeps the conversion independent of any running
# LibreOffice instance. The output is committed; tests never run soffice.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
profile=$(mktemp -d)
trap 'rm -rf "$profile"' EXIT
soffice -env:UserInstallation="file://$profile" --headless \
	--convert-to pptx:"Impress Office Open XML" --outdir "$here" "$here/diagram.fodp"
