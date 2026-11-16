# Releasing documentdriver

A release is built locally by `make release` (the stdlib Go tool
`tools/release`) and published by the owner by hand. Nothing in this
repository uploads, tags remotely or pushes: there is no hosted CI and no
git remote.

## What a release contains

`make release VERSION=x.y.z` writes into `dist/` (gitignored):

| File | What |
| --- | --- |
| `<cmd>-<version>-<os>-<arch>` | Every CLI under `cmd/` (today `docx`, `pptx` and `xlsx`) for `linux-amd64`, `linux-arm64` and `darwin-arm64`: static, `CGO_ENABLED=0 -trimpath`, with the version stamped into `--version` |
| `documentdriver-<pep440>-py3-none-<tag>.whl` | One Python wheel per platform carrying that platform's `docx`, `pptx` and `xlsx`. The linux wheels use a compressed platform tag set (PEP 425: tags joined with `.`), so one file per architecture covers both libc families: `manylinux2014_x86_64.musllinux_1_1_x86_64` and `manylinux2014_aarch64.musllinux_1_1_aarch64`; darwin is `macosx_11_0_arm64`. The `WHEEL` file lists one `Tag:` line per expanded tag, in a fixed order |
| `SHA256SUMS` | The SHA-256 of every file above, in `sha256sum -c` format |

The wheel's import package (`packaging/python/documentdriver`, standard
library only, Python 3.8 and later) is small:

- `documentdriver.run(tool, args, files=None, *, stdin=None, output=None,
  parse=True, timeout=60)` runs one command in a new private directory
  and returns `Run(result, output)`. `files` (`{name: bytes}`) are written
  there first and `args` name them; `stdin` (str, bytes, or a dict or
  list sent as JSON) feeds a `-` argument; `output` names the `-o` file
  whose bytes come back. The binary runs from argv with an empty
  environment, stdout and stderr go to files (capped at 64 MiB and
  1 MiB), and past `timeout` it is killed. Exit 1 or 2 with the
  contract's errors object raises `EngineError` (a `ValueError` with
  `errors`, `exit_code`, `code` and `path`); anything else the contract
  does not allow raises `RuntimeError`. It knows the contract's streams
  and exit codes, not its commands or operations: those stay in
  `help`.
- `documentdriver.binary("docx")` returns the bundled binary's path
  after checking it against the SHA-256 recorded at build time (it raises
  `RuntimeError` on a mismatch), and `documentdriver.names()` lists the
  binaries (`wheelCommands` in `tools/release/build.go`).

`packaging/python/smoke.py` (`make release-smoke`) checks both on the
installed wheel.

Versions are `MAJOR.MINOR.PATCH`, optionally with one pre-release label.
The binaries report the version as given; the wheel uses its PEP 440
spelling:

| `VERSION` | `--version` | wheel |
| --- | --- | --- |
| `0.1.0` | `0.1.0` | `0.1.0` |
| `0.1.0-dev`, `0.1.0-dev3` | as given | `0.1.0.dev0`, `0.1.0.dev3` |
| `0.1.0-a1`, `-b1`, `-rc1` | as given | `0.1.0a1`, `0.1.0b1`, `0.1.0rc1` |

A plain `go build` (no release flags) reports the default in each
`cmd/<cmd>/main.go`.

Everything is reproducible: two releases of one commit with one Go
toolchain give byte-identical files, so identical `SHA256SUMS`. The
binaries are built with `-trimpath -buildvcs=false -ldflags "-s -w
-buildid="` and the build environment pinned (`GOAMD64=v1`,
`GOARM64=v8.0`, no `GOFLAGS` or `GOEXPERIMENT`); the wheels are zips with
sorted entries, one fixed time and fixed modes.

## Cutting a release

1. Merge everything for the release to `main` and run `make ci` there.
2. Build and check, from a clean checkout of the commit:

   ```sh
   make release VERSION=0.1.0         # build dist/, then check it
   make release-check VERSION=0.1.0   # two clean builds from git archive HEAD: SHA256SUMS identical
   make release-smoke VERSION=0.1.0   # pip-install this machine's wheel into a fresh venv (uv), run binary()
   ```

   `make release` checks what it wrote: every digest in `SHA256SUMS`, each
   wheel's `RECORD` (every entry's hash and size, nothing missing or
   extra), the `WHEEL` tag, the recorded digests, and `--version` of the
   binaries for this machine, raw and unpacked from its wheel.
   `go run ./tools/release -version 0.1.0 -check` repeats the check alone.
3. Tag the commit locally: `git tag -a v0.1.0 -m "documentdriver 0.1.0"`.
4. Publish (the owner's call; nothing here does it):
   - the wheels to PyPI, e.g. `uv publish dist/documentdriver-0.1.0-*.whl`
     with the owner's token from the environment;
   - the binaries and `SHA256SUMS` to the download host consumers fetch
     from, at a URL per version (for example `<base>/v0.1.0/<file>`).

   Publish the files exactly as built: a rebuild of the same commit gives
   the same bytes, but a different Go toolchain does not.

## Pinning a release as a consumer

### noah (Rust, Docker): download and verify

Fetch the binary for the platform and check it against a digest pinned in
the script, copied from the release's `SHA256SUMS`, as
`scripts/fetch-pdfium.sh` does for Pdfium:

```sh
#!/usr/bin/env bash
set -euo pipefail
version=0.1.0
base="<download host>/v$version"
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)  platform=linux-amd64;  sha=<docx-$version-linux-amd64 digest from SHA256SUMS> ;;
  Linux-aarch64) platform=linux-arm64;  sha=<docx-$version-linux-arm64 digest> ;;
  Darwin-arm64)  platform=darwin-arm64; sha=<docx-$version-darwin-arm64 digest> ;;
  *) echo 'Unsupported documentdriver platform' >&2; exit 1 ;;
esac
file="docx-$version-$platform"
curl --fail --location --retry 3 "$base/$file" -o "$file.part"
actual=$( (command -v sha256sum >/dev/null && sha256sum "$file.part" || shasum -a 256 "$file.part") | cut -d ' ' -f 1)
if [[ "$actual" != "$sha" ]]; then
  echo "documentdriver SHA-256 mismatch: expected $sha, got $actual" >&2
  exit 1
fi
chmod +x "$file.part" && mv "$file.part" docx
```

Pinning the digest in the script (not fetching `SHA256SUMS` next to the
binary) means a replaced file on the host fails the build. To check a
directory of downloads against the release's list instead, run
`sha256sum -c --ignore-missing SHA256SUMS` (macOS:
`shasum -a 256 -c --ignore-missing SHA256SUMS`). In a Dockerfile, run the
script in a build stage and copy the verified binary into the image; it is
static, so any base image works (also `scratch` or Alpine).

### Open WebUI tools (Python): `requirements:`

Stock Open WebUI images pip-install a tool's `requirements:` line, so pin
the exact version in the tool's frontmatter; pip picks the wheel for the
image's platform:

```python
"""
title: Word documents
requirements: documentdriver==0.2.0
"""
import documentdriver

def read_docx(data: bytes) -> dict:
    result, _ = documentdriver.run("docx", ["read", "in.docx"], {"in.docx": data})
    return result

def apply_docx(data: bytes, plan: dict) -> bytes:
    _, out = documentdriver.run("docx", ["apply", "in.docx", "-", "-o", "out.docx"],
                                {"in.docx": data}, stdin=plan, output="out.docx")
    return out if out is not None else data  # a no-op plan writes nothing
```

`binary()` hashes the binary on its first call in a process and raises if
it is not the one the wheel recorded. To pin the files as well as the
version, use a requirements file with hashes
(`documentdriver==0.1.0 --hash=sha256:<wheel digest from SHA256SUMS>`, one
`--hash` per platform wheel) and `pip install --require-hashes`.

The linux wheels are tagged `manylinux2014` (glibc) and `musllinux_1_1`
(musl) for both `x86_64` and `aarch64`, and the darwin wheel
`macosx_11_0` (arm64): pip on a musl system (Alpine) picks the linux
wheels too. The binaries are static (no cgo, no libc), so the same
binaries serve both linux tags: there is no separate musl build.
