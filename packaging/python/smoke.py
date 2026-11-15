"""Smoke check of an installed documentdriver wheel (make release-smoke).

Run with the interpreter of a venv the wheel was pip-installed into:
    python smoke.py <release version> <PEP 440 version>
It checks the installed distribution's version, that binary() returns
runnable binaries reporting the release version, and that the SHA-256
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
    assert documentdriver.names() == ["docx", "pptx"], documentdriver.names()
    for name in documentdriver.names():
        path = documentdriver.binary(name)
        out = subprocess.run([path, "--version"], capture_output=True, check=True).stdout
        v = json.loads(out)
        assert v["tool"] == name and v["version"] == release, out
        print("ok %s %s" % (path, out.decode().strip()))

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


if __name__ == "__main__":
    main()
