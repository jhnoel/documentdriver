#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["xmlschema==4.3.2"]
# ///
"""Opt-in MS-VSDX schema check for documentdriver vsdx inputs and outputs.

Fetches nothing and commits nothing: the XSD comes from the gitignored
cache filled by `make vsdx-schema` (eval/corpus/fetch-vsdx-schema.sh),
never from this repository. The check is differential, like the OOXML
gate in ooxml-conformance.py: real Visio files do not validate cleanly
(V1's page alone gives three known kinds: xml:space not allowed on
PageContents, an assertion on Section, character data in Text), so an
output fails only on error kinds -- (reason, element path with indexes
removed) -- that it has and its input did not. The input's own kinds
are listed as known and never fail the check. This target is opt-in
only: it is never part of `make check` or `make ci`.

Usage:
  uv run --script tools/conformance/vsdx-schema-check.py \
      --input in.vsdx --output out.vsdx
  uv run --script tools/conformance/vsdx-schema-check.py in.vsdx out.vsdx
  uv run --script tools/conformance/vsdx-schema-check.py page1.xml

With no arguments the V1 fixture page is checked as an input.
One line is printed per error kind, sorted: KNOWN for kinds the input
already had, NEW for kinds only an output has (exit 1). Without a
valid schema cache the check skips: one line, exit 0.
"""
import argparse
import hashlib
import io
import os
import re
import sys
import zipfile
from pathlib import Path
import xml.etree.ElementTree as ET

import xmlschema

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
VISIO_2012 = "http://schemas.microsoft.com/office/visio/2012/main"
VISIO_2011 = "http://schemas.microsoft.com/office/visio/2011/1/core"
XSD_SHA256 = "29caea2b747dcc560cca0b112de30e3cbefe751ccd0e874618ff7dfb3ba0db76"
SKIP_LINE = "no schema cache: run make vsdx-schema"
DEFAULT_INPUT = ROOT / "testdata/vsdx/v1-minimal/visio/pages/page1.xml"
INDEX_RE = re.compile(r"\[\d+\]")
CLARK_RE = re.compile(r"\{[^}]+\}")


def main_checkout(root):
    """Return the main checkout of a linked worktree, or None (repo.Corpus)."""
    dotgit = root / ".git"
    if not dotgit.is_file():
        return None
    line = dotgit.read_text(encoding="utf-8").strip()
    if not line.startswith("gitdir:"):
        return None
    gitdir = Path(line[len("gitdir:"):].strip())
    if not gitdir.is_absolute():
        gitdir = root / gitdir
    common = gitdir
    commondir = gitdir / "commondir"
    if commondir.is_file():
        common = Path(commondir.read_text(encoding="utf-8").strip())
        if not common.is_absolute():
            common = gitdir / common
    common = common.resolve()
    return common.parent if common.name == ".git" else None


def schema_path(explicit):
    """Resolve visio.xsd, exclusively: --schema, then $VSDX_SCHEMA, then the cache.

    An explicitly set $VSDX_SCHEMA that points nowhere is a cache miss
    (skip), not a reason to fall back to the default cache.
    """
    if explicit:
        p = Path(explicit)
        return p if p.suffix == ".xsd" else p / "visio.xsd"
    env_dir = os.environ.get("VSDX_SCHEMA")
    if env_dir is not None:
        p = Path(env_dir)
        return p if p.suffix == ".xsd" else p / "visio.xsd"
    candidates = [ROOT / "corpus/vsdx-schema/visio.xsd"]
    main = main_checkout(ROOT)
    if main is not None:
        candidates.append(main / "corpus/vsdx-schema/visio.xsd")
    for candidate in candidates:
        if candidate.is_file():
            return candidate
    return candidates[0]


def render_reason(reason):
    """Render Clark notation readably: xml:space stays xml:space, Visio tags bare."""
    reason = reason.replace("{http://www.w3.org/XML/1998/namespace}", "xml:")
    reason = reason.replace(
        "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}", "r:"
    )
    return CLARK_RE.sub("", reason)


def normalise_path(path):
    """Remove sibling indexes from an element path, per the kind definition."""
    return INDEX_RE.sub("", path or "")


def declared_elements(xsd_path):
    """Element names declared in the XSD, read from the cache at run time."""
    declared = set()
    root = ET.parse(xsd_path).getroot()
    for elem in root.iter():
        if isinstance(elem.tag, str) and elem.tag.rsplit("}", 1)[-1] == "element":
            name = elem.get("name")
            if name:
                declared.add(name)
    return declared


def iter_parts(source):
    """Yield (part_name, bytes) for every .xml member of a zip, directory, or file."""
    source = Path(source)
    if source.is_dir():
        for path in sorted(source.rglob("*.xml")):
            yield path.relative_to(source).as_posix(), path.read_bytes()
    elif source.is_file() and zipfile.is_zipfile(source):
        with zipfile.ZipFile(source) as zf:
            for name in zf.namelist():
                if name.endswith(".xml"):
                    yield name, zf.read(name)
    elif source.is_file():
        yield source.name, source.read_bytes()
    else:
        raise FileNotFoundError(str(source))


def is_visio_part(part_name, data, single):
    """Visio parts live under visio/ and have a Visio root; single files always count."""
    if single:
        return True
    if not (part_name == "visio" or part_name.startswith("visio/")):
        return False
    if "/_rels/" in part_name or part_name.endswith(".rels"):
        return False
    if part_name.startswith("visio/theme/"):
        return False
    try:
        root = ET.fromstring(data)
    except ET.ParseError:
        return True  # malformed Visio part: report it, do not silently drop it
    tag = root.tag if isinstance(root.tag, str) else ""
    return tag.startswith("{" + VISIO_2012 + "}") or tag.startswith("{" + VISIO_2011 + "}")


def shape_child_errors(data, declared):
    """Undeclared Visio-namespace direct children of Shape.

    ShapeSheet's choice holds an xsd:any (lax), so the schema alone
    accepts a misspelt Visio element such as <Bogus/> under a Shape.
    Surface those as kinds so the Bogus row fails naming it; foreign
    namespaces remain allowed extensions, as in the schema.
    """
    try:
        root = ET.fromstring(data)
    except ET.ParseError:
        return []
    parents = {child: parent for parent in root.iter() for child in parent}
    kinds = []
    for elem in root.iter():
        if not isinstance(elem.tag, str):
            continue
        if elem.tag.rsplit("}", 1)[-1] != "Shape":
            continue
        for position, child in enumerate(elem, start=1):
            if not isinstance(child.tag, str):
                continue
            if child.tag.startswith("{"):
                namespace, local = child.tag[1:].split("}", 1)
            else:
                namespace, local = "", child.tag
            if namespace not in ("", VISIO_2011) or local in declared:
                continue
            segments = []
            node = child
            while node is not root:
                segments.append(node.tag.rsplit("}", 1)[-1])
                node = parents[node]
            segments.append(root.tag.rsplit("}", 1)[-1])
            path = "/" + "/".join(reversed(segments))
            reason = f"Unexpected child with tag '{local}' at position {position}."
            kinds.append((path, reason))
    return kinds


def validate_source(source, schema, declared):
    """Return {(part, path, reason)} kinds for every Visio part of one source."""
    parts = list(iter_parts(source))
    single = len(parts) == 1 and Path(source).is_file() and not zipfile.is_zipfile(source)
    rows = set()
    for part_name, data in parts:
        if not is_visio_part(part_name, data, single):
            continue
        # Instances are written in 2012/main; the schema targets 2011/1/core.
        rewritten = data.replace(VISIO_2012.encode(), VISIO_2011.encode())
        try:
            errors = list(schema.iter_errors(io.BytesIO(rewritten)))
        except Exception as exc:  # malformed XML etc.: one kind per part, never a crash
            rows.add((part_name, "", f"XML parse error: {exc}"))
            continue
        for err in errors:
            reason = render_reason(getattr(err, "reason", None) or err.message)
            reason = " ".join(reason.splitlines())
            rows.add((part_name, normalise_path(err.path), reason))
        for path, reason in shape_child_errors(rewritten, declared):
            rows.add((part_name, path, reason))
    return rows


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("positional", nargs="*", help="INPUT [OUTPUT ...]")
    parser.add_argument("--input", "-i", action="append", default=[], dest="inputs")
    parser.add_argument("--output", "-o", action="append", default=[], dest="outputs")
    parser.add_argument("--schema", "--schema-dir", dest="schema", default=None)
    args = parser.parse_args(argv)

    inputs = list(args.inputs)
    outputs = list(args.outputs)
    if args.positional:
        if not inputs:
            inputs = [args.positional[0]]
            if not outputs:
                outputs = list(args.positional[1:])
        elif not outputs:
            outputs = list(args.positional)
    if not inputs and not outputs:
        inputs = [str(DEFAULT_INPUT)]

    xsd = schema_path(args.schema)
    try:
        xsd_bytes = xsd.read_bytes()
    except OSError:
        print(SKIP_LINE)
        return 0
    if hashlib.sha256(xsd_bytes).hexdigest() != XSD_SHA256:
        print(SKIP_LINE)
        return 0

    try:
        schema = xmlschema.XMLSchema11(str(xsd))
        declared = declared_elements(xsd)
    except Exception as exc:
        print(f"vsdx-schema-check: cannot load schema: {exc}", file=sys.stderr)
        return 2

    known_by_label = {}
    try:
        for source in inputs:
            known_by_label[("INPUT", source)] = validate_source(source, schema, declared)
        output_by_label = {}
        for source in outputs:
            output_by_label[("OUTPUT", source)] = validate_source(source, schema, declared)
    except (FileNotFoundError, OSError, zipfile.BadZipFile) as exc:
        print(f"vsdx-schema-check: {exc}", file=sys.stderr)
        return 2

    known_kinds = {(path, reason) for rows in known_by_label.values() for _, path, reason in rows}
    all_rows = []
    for (label, source), rows in sorted(known_by_label.items()):
        for part, path, reason in sorted(rows):
            all_rows.append((label, source, part, path, reason, (path, reason) in known_kinds))
    new_rows = []
    for (label, source), rows in sorted(output_by_label.items()):
        for part, path, reason in sorted(rows):
            is_new = (path, reason) not in known_kinds
            all_rows.append((label, source, part, path, reason, not is_new))
            if is_new:
                new_rows.append((source, part, path, reason))

    for label, source, part, path, reason, is_known in all_rows:
        tag = "KNOWN" if is_known else "NEW"
        print(f"{tag} {source} {part} {path}: {reason}")
    if new_rows:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
