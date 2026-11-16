"""run(): one call of a bundled binary, in a private directory.

The binaries follow the agent contract (docs/contract.md): exit 0 with one
JSON document on stdout, exit 1 or 2 with one errors object on stderr.
run() turns that into a return value or an EngineError, so callers do not
each reimplement the subprocess, temp-file and error handling.
"""

import collections
import json
import os
import subprocess
import tempfile

__all__ = ["run", "Run", "EngineError", "DEFAULT_TIMEOUT", "STDOUT_CAP", "STDERR_CAP"]

DEFAULT_TIMEOUT = 60.0  # seconds
STDOUT_CAP = 64 << 20  # a whole document's markup and an apply's changed units fit
STDERR_CAP = 1 << 20

Run = collections.namedtuple("Run", ["result", "output"])
Run.__doc__ = """What run() returns: the result (a dict, or str when parse=False)
and the bytes of the `output` file (None when no output was asked for or
the tool wrote none, as a no-op apply or a dry run does)."""


class EngineError(ValueError):
    """A binary refused or failed with the contract's errors object.

    errors: the list from {"errors": [...]}, each with code and message and
    optionally path, hint, span, matches, detail. exit_code is 1 (refused or
    failed) or 2 (wrong command line, code "usage"). It is a ValueError:
    the input was wrong, not the installation.
    """

    def __init__(self, tool, errors, exit_code):
        self.tool = tool
        self.errors = errors
        self.exit_code = exit_code
        super().__init__(_error_text(tool, errors))

    @property
    def code(self):
        """The first error's code, e.g. "not_found"."""
        return self.errors[0].get("code", "")

    @property
    def path(self):
        """The first error's path, or None when it has none."""
        return self.errors[0].get("path")


def _error_text(tool, errors):
    """One line for a reader: '<tool>: <code>[ at <path>]: <message>; ...', at most five errors."""
    parts = []
    for error in errors[:5]:
        path = error.get("path")
        text = "%s at %s: %s" % (error.get("code", ""), path, error.get("message", "")) if path \
            else "%s: %s" % (error.get("code", ""), error.get("message", ""))
        if error.get("hint"):
            text += " Hint: %s" % error["hint"]
        parts.append(text)
    if len(errors) > 5:
        parts.append("and %d more" % (len(errors) - 5))
    return "%s: %s" % (tool, "; ".join(parts))


def _check_name(name):
    """Refuse a file name that is not one plain component, so files and output stay in the run's directory."""
    if (not isinstance(name, str) or name in ("", ".", "..") or "/" in name or "\\" in name
            or "\0" in name or name.startswith("-")):
        raise ValueError("documentdriver.run: file name %r must be a plain file name" % (name,))


def _stdin_bytes(stdin):
    """The bytes to feed on stdin: str as UTF-8, bytes as given, dict or list as compact JSON."""
    if isinstance(stdin, (bytes, bytearray, memoryview)):
        return bytes(stdin)
    if isinstance(stdin, str):
        return stdin.encode("utf-8")
    if isinstance(stdin, (dict, list)):
        return json.dumps(stdin, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    raise TypeError("documentdriver.run: stdin must be str, bytes, dict or list, not %s" % type(stdin).__name__)


def _read_capped(path, cap, tool, stream):
    """The file's bytes, or RuntimeError when it is larger than cap."""
    if os.path.getsize(path) > cap:
        raise RuntimeError("%s: %s is larger than its %d-byte limit" % (tool, stream, cap))
    with open(path, "rb") as f:
        return f.read()


def _failure(tool, code, stderr):
    """The exception for a non-zero exit: EngineError when stderr is the contract's errors object."""
    snippet = stderr.decode("utf-8", "replace")[:300]
    if code in (1, 2):
        try:
            payload = json.loads(stderr.decode("utf-8"))
        except ValueError:
            payload = None
        errors = payload.get("errors") if isinstance(payload, dict) else None
        if (isinstance(errors, list) and errors
                and all(isinstance(e, dict) and "code" in e and "message" in e for e in errors)):
            return EngineError(tool, errors, code)
    return RuntimeError("%s: exit %d: %s" % (tool, code, snippet))


def run(tool, args, files=None, *, stdin=None, output=None, parse=True,
        timeout=DEFAULT_TIMEOUT, stdout_cap=STDOUT_CAP, stderr_cap=STDERR_CAP):
    """Run the bundled binary `tool` once with `args` and return a Run(result, output).

    Each call gets a new private directory (mode 0700, removed on every
    path) that is the binary's working directory. `files` maps plain file
    names to bytes (or str, written as UTF-8); they are written there first
    and `args` name them, e.g. run("docx", ["read", "in.docx"], {"in.docx": data}).
    `stdin` (str, bytes, or a dict or list sent as JSON) feeds a `-`
    argument, the way a plan or HTML is given without a file; without it
    stdin is empty. `output` names a file the command writes (its `-o`);
    its bytes come back in Run.output.

    The binary runs from argv (no shell) with an empty environment; stdout
    and stderr go to files in the directory, so a large result cannot
    deadlock a pipe. With exit 0, Run.result is the parsed JSON object
    (parse=False: the stdout text, for svg, xml and help output).

    Raises EngineError for the contract's errors object (exit 1 or 2),
    RuntimeError when the binary runs past `timeout` seconds (it is
    killed), writes more than stdout_cap or stderr_cap bytes, prints
    something the contract does not allow, or cannot be started, and
    ValueError for a bad file name or tool name.
    """
    from . import binary  # late: __init__ imports this module

    exe = binary(tool)
    args = [str(a) for a in args]
    files = files or {}
    for name in files:
        _check_name(name)
    if output is not None:
        _check_name(output)
    data_in = _stdin_bytes(stdin) if stdin is not None else None

    with tempfile.TemporaryDirectory(prefix="documentdriver-") as private:
        directory = os.path.join(private, "work")  # the binary's cwd; stdout and stderr stay outside it
        os.mkdir(directory, 0o700)
        for name, data in files.items():
            with open(os.path.join(directory, name), "wb") as f:
                f.write(data.encode("utf-8") if isinstance(data, str) else bytes(data))
        out_path = os.path.join(private, "stdout")
        err_path = os.path.join(private, "stderr")
        with open(out_path, "wb") as out_f, open(err_path, "wb") as err_f:
            try:
                completed = subprocess.run(
                    [exe] + args, cwd=directory, env={},
                    input=data_in, stdin=None if data_in is not None else subprocess.DEVNULL,
                    stdout=out_f, stderr=err_f, timeout=timeout)
            except subprocess.TimeoutExpired:
                raise RuntimeError("%s: ran past its %g s limit" % (tool, timeout)) from None
            except OSError as error:
                raise RuntimeError("%s: cannot run the binary: %s" % (tool, error)) from None
        stdout = _read_capped(out_path, stdout_cap, tool, "stdout")
        stderr = _read_capped(err_path, stderr_cap, tool, "stderr")
        if completed.returncode != 0:
            raise _failure(tool, completed.returncode, stderr)
        if parse:
            try:
                result = json.loads(stdout.decode("utf-8"))
            except ValueError:
                raise RuntimeError("%s: output is not JSON: %s" % (tool, stdout[:300].decode("utf-8", "replace"))) from None
            if not isinstance(result, dict):
                raise RuntimeError("%s: output is not a JSON object" % tool)
        else:
            result = stdout.decode("utf-8", "replace")
        produced = None
        if output is not None:
            path = os.path.join(directory, output)
            if os.path.isfile(path):
                with open(path, "rb") as f:
                    produced = f.read()
        return Run(result, produced)
