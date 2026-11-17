"""The documentdriver command-line tools, as static binaries.

This wheel carries the docx, pptx, vsdx and xlsx binaries for one platform.
Their command line, results and errors follow the agent contract
(docs/contract.md in the documentdriver repository). run() makes one call
in a private directory and returns the parsed result, or raises
EngineError with the contract's errors:

    import documentdriver
    facts, _ = documentdriver.run("docx", ["read", "in.docx"], {"in.docx": data})
    res, out = documentdriver.run("docx", ["apply", "in.docx", "-", "-o", "out.docx"],
                                  {"in.docx": data}, stdin=plan, output="out.docx")

binary() returns a binary's path, to run it yourself.

binary() checks the file against the SHA-256 recorded when the wheel was
built and raises if it differs, so a damaged or replaced install fails
loudly instead of running something else.
"""

import hashlib
import os
import stat

from ._digests import DIGESTS, VERSION
from ._run import DEFAULT_TIMEOUT, STDERR_CAP, STDOUT_CAP, EngineError, Run, run

__all__ = ["binary", "names", "run", "Run", "EngineError", "DEFAULT_TIMEOUT",
           "STDOUT_CAP", "STDERR_CAP", "__version__"]

__version__ = VERSION

_HERE = os.path.dirname(os.path.abspath(__file__))
_checked = set()


def names():
    """Return the names of the binaries this wheel carries, sorted."""
    return sorted(DIGESTS)


def binary(name):
    """Return the absolute path of the bundled binary `name` ("docx", "pptx", "vsdx", "xlsx").

    The first call per binary hashes it and compares the digest recorded at
    build time; a mismatch raises RuntimeError. It also restores the
    executable bit if an installer dropped it.
    """
    want = DIGESTS.get(name)
    if want is None:
        raise ValueError(
            "documentdriver has no binary %r (it has: %s)"
            % (name, ", ".join(names()))
        )
    path = os.path.join(_HERE, "bin", name)
    if path in _checked:
        return path
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    got = h.hexdigest()
    if got != want:
        raise RuntimeError(
            "documentdriver: %s has SHA-256 %s, but this wheel recorded %s; "
            "reinstall documentdriver==%s" % (path, got, want, VERSION)
        )
    mode = os.stat(path).st_mode
    if mode & stat.S_IXUSR == 0:
        os.chmod(path, mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    _checked.add(path)
    return path
