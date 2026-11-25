"""Smoke check of an installed documentdriver wheel (make release-smoke).

Run with the interpreter of a venv the wheel was pip-installed into:
    python smoke.py <release version> <PEP 440 version>
It checks the installed distribution's version, that binary() returns
runnable binaries reporting the release version, that run() round-trips
results, outputs and the contract's errors, and that the SHA-256
self-check refuses a binary whose recorded digest differs. Dev only; it
is not part of the wheel.
"""

import json
import subprocess
import sys
from importlib import metadata

import documentdriver


def main():
    release, py_version = sys.argv[1], sys.argv[2]
    got = metadata.version("documentdriver")
    assert got == py_version, "installed version %s, want %s" % (got, py_version)
    assert documentdriver.__version__ == py_version, documentdriver.__version__
    assert documentdriver.names() == ["docx", "pptx", "vsdx", "xlsx"], documentdriver.names()
    check_licences()
    for name in documentdriver.names():
        path = documentdriver.binary(name)
        out = subprocess.run([path, "--version"], capture_output=True, check=True).stdout
        v = json.loads(out)
        assert v["tool"] == name and v["version"] == release, out
        print("ok %s %s" % (path, out.decode().strip()))

    check_run(release)

    # The self-check: a recorded digest that differs must refuse the binary.
    documentdriver._checked.clear()
    documentdriver.DIGESTS["docx"] = "0" * 64
    try:
        documentdriver.binary("docx")
    except RuntimeError as e:
        print("ok self-check refuses a mismatch: %s" % str(e).split(";")[0])
    else:
        raise AssertionError("binary() accepted a digest mismatch")
    try:
        documentdriver.binary("nope")
    except ValueError:
        pass
    else:
        raise AssertionError("binary('nope') did not raise ValueError")
    print("release-smoke OK: documentdriver %s" % py_version)


LICENCE_FILES = ["LICENSE", "fonts/OFL.txt", "fonts/LICENSE-DejaVu.txt", "fonts/PROVENANCE.md"]


def check_licences():
    """The installed wheel names and carries the tools' and the
    embedded fonts' licence files, under the combined expression."""
    meta = metadata.metadata("documentdriver")
    expr = meta.get("License-Expression")
    want = "Apache-2.0 AND OFL-1.1 AND Bitstream-Vera AND LicenseRef-Arev"
    assert expr == want, "License-Expression %r, want %r" % (expr, want)
    named = meta.get_all("License-File") or []
    files = {str(f): f for f in (metadata.files("documentdriver") or [])}
    for name in LICENCE_FILES:
        assert name in named, "METADATA names no License-File %s (has %s)" % (name, named)
        path = [p for p in files if p.endswith(".dist-info/licenses/" + name)]
        assert path, "the wheel has no licenses/%s" % name
        text = files[path[0]].read_text(encoding="utf-8")
        assert text.strip(), "licenses/%s is empty" % name
    print("ok licences: %s" % ", ".join(LICENCE_FILES))


def check_run(release):
    """run(): results, stdin, outputs, parse=False, the errors object, and file-name refusals."""
    run, EngineError = documentdriver.run, documentdriver.EngineError
    for name in documentdriver.names():
        v, out = run(name, ["--version"])
        assert v["tool"] == name and v["version"] == release and out is None, v

    res, docx = run("docx", ["create", "-", "-o", "new.docx"], stdin="<p>Hello run</p>", output="new.docx")
    assert docx and docx[:2] == b"PK", res
    found, _ = run("docx", ["find", "in.docx", "Hello"], {"in.docx": docx})
    assert found["matches"], found

    res, xlsx = run("xlsx", ["create", "-o", "new.xlsx"], output="new.xlsx")
    assert xlsx and xlsx[:2] == b"PK", res
    facts, _ = run("xlsx", ["read", "in.xlsx"], {"in.xlsx": xlsx})
    assert isinstance(facts, dict), facts

    text, _ = run("docx", ["help"], parse=False)
    assert isinstance(text, str) and text, text

    try:
        run("docx", ["read", "missing.docx"])
    except EngineError as e:
        assert e.exit_code == 1 and e.code == "io" and str(e).startswith("docx: io"), (e.exit_code, e.errors)
    else:
        raise AssertionError("run() on a missing file did not raise EngineError")
    try:
        run("pptx", ["read", "--no-such-flag"])
    except EngineError as e:
        assert e.exit_code == 2 and e.code == "usage", (e.exit_code, e.errors)
    else:
        raise AssertionError("run() with an unknown flag did not raise EngineError")
    try:
        run("docx", ["apply", "in.docx", "-", "--dry-run"], {"in.docx": docx}, stdin={"ops": []})
    except EngineError as e:
        assert e.code == "invalid_plan" and e.path == "ops", e.errors
    else:
        raise AssertionError("run() with an empty plan did not raise EngineError")
    for bad in ["../x.docx", "a/b.docx", "", "..", "-o"]:
        try:
            run("docx", ["read", "x"], {bad: b""})
        except ValueError as e:
            assert not isinstance(e, EngineError), e
        else:
            raise AssertionError("run() accepted file name %r" % bad)
    print("ok run(): results, outputs, errors and file names")


if __name__ == "__main__":
    main()
