# Releasing documentdriver

A release is built locally by `make release` (the stdlib Go tool
`tools/release`) and published by the owner: the wheels with
`make publish`, which uploads to PyPI with a token the owner gives it.
Nothing uploads, tags remotely or pushes on its own: there is no hosted
CI.

## What a release contains

`make release VERSION=x.y.z` writes into `dist/` (gitignored):

| File | What |
| --- | --- |
| `<cmd>-<version>-<os>-<arch>` | Every CLI under `cmd/` (today `docx`, `pptx`, `vsdx` and `xlsx`) for `linux-amd64`, `linux-arm64` and `darwin-arm64`: static, `CGO_ENABLED=0 -trimpath`, with the version stamped into `--version` |
| `documentdriver-<pep440>-py3-none-<tag>.whl` | One Python wheel per platform carrying that platform's `docx`, `pptx`, `vsdx` and `xlsx`. The linux wheels use a compressed platform tag set (PEP 425: tags joined with `.`), so one file per architecture covers both libc families: `manylinux2014_x86_64.musllinux_1_1_x86_64` and `manylinux2014_aarch64.musllinux_1_1_aarch64`; darwin is `macosx_11_0_arm64`. The `WHEEL` file lists one `Tag:` line per expanded tag, in a fixed order |
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

The short way, on a clean `main` with everything for the release merged:

```sh
UV_PUBLISH_TOKEN=<pypi token> make ship VERSION=0.2.1
```

`make ship` runs the steps below in order: `make ci` (skipped when this
tree already passed), `release-check`, `release`, `release-smoke`, the
tag, `make publish` and `git push origin main v0.2.1`. If it stops
partway, fix the cause and run the same command again: a tag already at
HEAD is kept and wheels already on PyPI are skipped. It does not upload
the raw binaries to a download host (step 4).

The steps one by one:

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
4. Publish (the owner's call; nothing runs it automatically):
   - the wheels to PyPI with `make publish` (below);
   - the binaries and `SHA256SUMS` to the download host consumers fetch
     from, at a URL per version (for example `<base>/v0.1.0/<file>`).

   Publish the files exactly as built: a rebuild of the same commit gives
   the same bytes, but a different Go toolchain does not.
5. Push the tag: `git push origin v0.1.0`.

## Publishing to PyPI

`make publish VERSION=x.y.z [PYPI=pypi|testpypi] [PUBLISH_DRY_RUN=1]`
uploads the three wheels in `dist/` with `uv publish`. It re-runs the
`-check` of step 2 first, wants exactly three wheels for the version,
and for `PYPI=pypi` (the default) also a clean tree with HEAD tagged
`vVERSION`. `--check-url` makes a rerun after a partial upload skip the
files already there with the same hash. Only the wheels go to PyPI.

One-time setup:

1. Create accounts on [PyPI](https://pypi.org) and
   [TestPyPI](https://test.pypi.org) (separate sites, separate
   accounts), with two-factor authentication.
2. The first upload creates the `documentdriver` project, so the first
   token has to be account-scoped. After that, replace it with a token
   scoped to the `documentdriver` project (Account settings, API tokens)
   and delete the account-wide one.
3. Keep each token in your password manager. Never put one in the
   repository, a Makefile, `~/.pypirc` or a shell profile. Give it to a
   single command:

   ```sh
   UV_PUBLISH_TOKEN=<testpypi token> make publish VERSION=0.2.0-rc1 PYPI=testpypi
   ```

Each release:

1. Rehearse on TestPyPI with a pre-release version (TestPyPI, like PyPI,
   never accepts the same file name twice, so a rehearsal burns that
   version there), and install from it in a fresh venv:

   ```sh
   d="$(mktemp -d)" && uv venv --seed "$d/venv"
   "$d/venv/bin/python" -m pip install --index-url https://test.pypi.org/simple/ documentdriver==0.2.0rc1
   "$d/venv/bin/python" -c 'import documentdriver as d; print(d.run("docx", ["--version"]).result)'
   ```

2. Cut the real release (steps 1 to 3 above), then
   `UV_PUBLISH_TOKEN=<pypi token> make publish VERSION=0.2.0`.
3. A version on PyPI is permanent: it can be yanked but its files are
   never replaced. A broken release is fixed by the next patch version.

`PUBLISH_DRY_RUN=1` runs everything except the upload and needs no token.

The repository has no hosted CI, so publishing uses an API token, not
PyPI's trusted publishing (which needs a CI identity such as GitHub
Actions).

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
