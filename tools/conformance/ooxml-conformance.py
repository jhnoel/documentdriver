#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["lxml"]
# ///
"""Differential ECMA-376 conformance gate for documentdriver outputs.

Adapted from docxdriver scripts/ooxml-conformance.py (docxdriver git
1ffb0d7afed108a36a25c4a95b489d0fbf7a58bb): the three validators and the
differential rule are unchanged; the docx-specific CLI and operation
scenarios are replaced by `pptx apply`.

Every input deck is checked, and every plan is applied to its deck with
`pptx apply`. With --docx-cli, docx outputs are checked too: a sample of
the write baseline's plans (tools/oracle/baseline/write/plans) for each
ported operation, positive and dirty kinds, applied with `docx apply` in
track and direct mode to their sources, and `docx create` on each
--docx-create HTML file. Each input and each output is checked three ways:

- the ECMA-376 Transitional and OPC schemas (lxml), from
  tools/conformance/ooxml-schemas or $OOXML_SCHEMAS; they are not in git,
  so copy them there first;
- package and cross-part integrity rules no schema expresses: content types
  Office writes for each relationship type (from internal/ooxml/ctypes via
  ctypes.json), relationship targets and r:id references
  (tools/conformance/ooxml_integrity.py);
- the Open XML SDK validator for the Microsoft 365 format, which also covers
  the Office extension namespaces the XSD pass strips
  (tools/openxml-validator, built here with `dotnet`).

The gate fails when applying a plan introduces an error kind the input did
not already have (a created document has no input: any error fails). Pre-existing invalid content is not ours to fix: errors
already in an input deck are listed as INPUT lines and do not fail the gate.
It also fails if no plan was applied at all.

Inputs, all optional but at least one plan source is needed:

  --decks DIR           the fixture decks (`make conformance` exports them with
                        internal/testdeck); each is checked, and every
                        --fixture-plans plan is applied to it
  --fixture-plans DIR   plans for the fixture decks (default: tools/conformance/plans)
  --repo-plans DIR      plans (eval/plans) applied to the corpus deck their task
                        names (--tasks DIR, task.json deck.path under --corpus)

    uv run tools/conformance/ooxml-conformance.py --cli bin/pptx --decks DIR

Requires uv (lxml comes from the inline script metadata) and the .NET SDK
(10 or later). `--keep-outputs DIR` saves every applied output.
"""
import argparse
import collections
from concurrent.futures import ProcessPoolExecutor
import hashlib
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

from lxml import etree

sys.path.insert(0, str(Path(__file__).resolve().parent))
from ooxml_integrity import integrity_errors  # noqa: E402

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
SCHEMA_DIR = Path(os.environ.get('OOXML_SCHEMAS', HERE / 'ooxml-schemas'))
SDK_PROJECT = ROOT / 'tools' / 'openxml-validator'
SDK_BUILD = ROOT / 'bin' / 'openxml-validator'
MC = 'http://schemas.openxmlformats.org/markup-compatibility/2006'
# Namespaces of the Office extensions (w14, p14, a16, ...) the XSD pass
# strips; the SDK validator covers them.
OFFICE_EXTENSIONS = 'http://schemas.microsoft.com/office/'
PARSER = etree.XMLParser(resolve_entities=False, no_network=True, load_dtd=False)
SEED_ARGS = ['--seed', '1', '--clock', '2000-01-01T00:00:00Z']
DOCX_CLOCK = ['--clock', '2000-01-01T00:00:00Z']
WRITE_PLANS = ROOT / 'tools' / 'oracle' / 'baseline' / 'write' / 'plans'
DOCX_MODES = ['track', 'direct']


def docx_ported():
    """The ported docx operations (contract names), from the op table:
    those whose Spec no longer embeds pending (go run ./tools/oracle write
    ported), so the list is never kept here."""
    result = subprocess.run(['go', 'run', './tools/oracle', 'write', 'ported'], cwd=ROOT,
                            capture_output=True, text=True, check=True)
    return set(result.stdout.split())


def build_sdk_validator():
    """Build tools/openxml-validator (incremental); returns an error or None."""
    if shutil.which('dotnet') is None:
        return ('the .NET SDK is required for Open XML SDK validation: '
                'brew install dotnet (macOS) or https://dotnet.microsoft.com/download')
    result = subprocess.run(['dotnet', 'build', str(SDK_PROJECT), '-c', 'Release', '-o', str(SDK_BUILD), '--nologo', '-v', 'q'],
                            capture_output=True, text=True)
    if result.returncode != 0:
        return 'openxml-validator build failed:\n' + (result.stdout + result.stderr)[-2000:]
    return None


class SdkValidator:
    """One long-lived `dotnet openxml-validator.dll` process per gate worker."""

    def __init__(self):
        self.process = subprocess.Popen(['dotnet', str(SDK_BUILD / 'openxml-validator.dll')], stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, text=True, bufsize=1)
        self.scratch = Path(tempfile.mkdtemp(prefix='conformance-sdk-'))

    def errors(self, data, extension='.pptx'):
        path = self.scratch / ('package' + extension)
        path.write_bytes(data)
        self.process.stdin.write(str(path) + '\n')
        self.process.stdin.flush()
        result = json.loads(self.process.stdout.readline())
        found = collections.Counter()
        if 'fatal' in result:
            message = re.sub(r'\s+', ' ', result['fatal'])
            # The SDK stops at the first missing part. Rewriting relationships
            # can change which existing dangling target it encounters first;
            # integrity_errors separately checks every relationship target.
            message = re.sub(r"Part: /\S+ doesn't exist in the package", "Part: <missing> doesn't exist in the package", message)
            found[('package', 'sdk: cannot open: ' + re.sub(r'\d+', 'N', message))] += 1
        for error in result.get('errors', []):
            message = re.sub(r' List of possible elements expected:.*', '', error['description'])
            found[(error['part'].lstrip('/'), f"sdk {error['id']}: " + re.sub(r'\d+', 'N', message))] += 1
        return found


class Validator:
    def __init__(self):
        self.paths = {}
        if not any(SCHEMA_DIR.glob('*.xsd')):
            sys.exit(f'ooxml-conformance: no schemas in {SCHEMA_DIR} (not in git; copy them there or set OOXML_SCHEMAS)')
        for path in SCHEMA_DIR.glob('*.xsd'):
            namespace = etree.parse(str(path), PARSER).getroot().get('targetNamespace')
            if namespace:
                self.paths[namespace] = path
        self.compiled = {}
        self.sdk = SdkValidator()

    def errors(self, data, extension='.pptx'):
        """Error kinds per part: schema, integrity, and Open XML SDK."""
        found = self.schema_errors(data)
        with zipfile.ZipFile(io.BytesIO(data)) as package:
            found.update(integrity_errors({entry.filename: package.read(entry) for entry in package.infolist()}))
        found.update(self.sdk.errors(data, extension))
        return found

    def schema_errors(self, data):
        """Schema error kinds per part, after ECMA-376 Part 3 markup-compatibility processing."""
        found = collections.Counter()
        with zipfile.ZipFile(io.BytesIO(data)) as package:
            for entry in package.infolist():
                if not entry.filename.lower().endswith(('.xml', '.rels')):
                    continue
                try:
                    root = etree.fromstring(package.read(entry), PARSER)
                except etree.XMLSyntaxError as error:
                    found[(entry.filename, 'malformed XML: ' + str(error).splitlines()[0])] += 1
                    continue
                for alternate in list(root.iter(f'{{{MC}}}AlternateContent')):
                    parent = alternate.getparent()
                    if parent is None:
                        continue
                    fallback = alternate.find(f'{{{MC}}}Fallback')
                    index = parent.index(alternate)
                    for child in list(fallback) if fallback is not None else []:
                        parent.insert(index, child)
                        index += 1
                    parent.remove(alternate)
                # Snapshot the walk: removing children while iterating skips nodes.
                for element in list(root.iter()):
                    for name in list(element.attrib):
                        namespace = etree.QName(name).namespace or ''
                        if namespace == MC or namespace.startswith(OFFICE_EXTENSIONS):
                            del element.attrib[name]
                    for child in list(element):
                        if isinstance(child.tag, str) and (etree.QName(child).namespace or '').startswith(OFFICE_EXTENSIONS):
                            element.remove(child)
                namespace = etree.QName(root).namespace
                if namespace not in self.paths:
                    continue
                if namespace not in self.compiled:
                    self.compiled[namespace] = etree.XMLSchema(etree.parse(str(self.paths[namespace]), PARSER))
                schema = self.compiled[namespace]
                if not schema.validate(root.getroottree()):
                    for issue in schema.error_log:
                        # The expected-element list depends on the preceding
                        # siblings, so an edit near a pre-existing ordering
                        # error would otherwise read as a new kind.
                        message = re.sub(r' Expected is .*', '', issue.message)
                        found[(entry.filename, re.sub(r'\d+', 'N', message))] += 1
        return found




def unmasked(introduced, removed):
    """Drop schema content-model errors an edit merely uncovered.

    Validators report a content-model error and skip the rest of
    it, so repairing an invalid source element (format_table adding the
    missing tblPr of an invalid table) exposes pre-existing errors further
    down the same part. A new "not expected" schema error counts only when
    the edit did not also remove one from that part.
    """
    def content_model(message):
        if message.startswith('sdk Sch_UnexpectedElementContentExpectingComplex:'):
            return 'sdk'
        if not message.startswith(('integrity:', 'sdk')) and 'This element is not expected' in message:
            return 'schema'
        return None
    repaired = {(part, content_model(message)) for part, message in removed if content_model(message)}
    # A tracked edit keeps the old invalid properties in pPrChange. The SDK
    # still counts that snapshot's error, even when XSD reports the repair in
    # the live properties; its newly exposed ordering error has the same cause.
    repaired.update((part, 'sdk') for part, kind in list(repaired) if kind == 'schema')
    return {(part, message) for part, message in introduced if (part, content_model(message)) not in repaired}


_process_validator = None



class Pptx:
    """The pptx CLI, run in a directory (plan paths to images are relative to it)."""

    def __init__(self, binary, cwd):
        self.binary, self.cwd = binary, cwd

    def apply(self, deck, plan, out):
        """Apply `plan` to `deck`, writing `out`; returns None or the reason it was rejected."""
        result = subprocess.run([self.binary, 'apply', str(Path(deck).resolve()), str(Path(plan).resolve()), '-o', str(Path(out).resolve()), *SEED_ARGS],
                                cwd=self.cwd, capture_output=True, text=True)
        if result.returncode != 0 or not Path(out).exists():
            return (result.stderr or result.stdout).strip().splitlines()[0][:300] if (result.stderr or result.stdout).strip() else 'failed'
        return None


_process_validator = None


def check_deck(job):
    """One input deck and its plans, with a private validator and scratch directory."""
    global _process_validator
    label, deck, plans, binary, cwd, keep = job
    if _process_validator is None:
        _process_validator = Validator()
    validator = _process_validator
    work = Path(tempfile.mkdtemp(prefix='conformance-'))
    cli = Pptx(binary, cwd)
    stem = f'{deck.stem}-{hashlib.sha256(str(deck).encode()).hexdigest()[:6]}'
    result = {'label': label, 'input': collections.Counter(), 'applied': [], 'rejected': [], 'failures': []}
    try:
        source = work / 'source.pptx'
        shutil.copyfile(deck, source)
        before = validator.errors(source.read_bytes())
        result['input'] = before
        for name, plan in plans:
            out = work / 'out.pptx'
            out.unlink(missing_ok=True)
            reason = cli.apply(source, plan, out)
            if reason:
                result['rejected'].append((name, reason))
                continue
            result['applied'].append(name)
            after = validator.errors(out.read_bytes())
            if keep:
                shutil.copyfile(out, Path(keep) / f'{stem}--{name}.pptx')
            for kind, message in sorted(unmasked(after.keys() - before.keys(), before - after)):
                result['failures'].append(f'{label}: plan {name} introduced {kind}: {message[:240]}')
        return result
    finally:
        shutil.rmtree(work, ignore_errors=True)


class Docx:
    """The docx CLI."""

    def __init__(self, binary):
        self.binary = binary

    def apply(self, source, plan, out):
        """Apply plan (a JSON file) to source, writing out; returns None or why it was refused."""
        result = subprocess.run([self.binary, 'apply', str(source), str(plan), '-o', str(out), *DOCX_CLOCK],
                                capture_output=True, text=True)
        if result.returncode != 0:
            return (result.stderr or result.stdout).strip()[:300] or 'failed'
        if not Path(out).exists():
            # A plan that changes nothing writes nothing: the output is the input.
            shutil.copyfile(source, out)
        return None

    def create(self, html, out):
        result = subprocess.run([self.binary, 'create', str(html), '-o', str(out)], capture_output=True, text=True)
        if result.returncode != 0 or not Path(out).exists():
            return (result.stderr or result.stdout).strip()[:300] or 'failed'
        return None


def check_docx_source(job):
    """One docx source and its sampled plans, each in both modes."""
    global _process_validator
    label, source, plans, binary, keep = job
    if _process_validator is None:
        _process_validator = Validator()
    validator = _process_validator
    work = Path(tempfile.mkdtemp(prefix='conformance-docx-'))
    cli = Docx(binary)
    stem = f'{source.stem}-{hashlib.sha256(str(source).encode()).hexdigest()[:6]}'
    result = {'label': label, 'input': collections.Counter(), 'applied': [], 'rejected': [], 'failures': []}
    try:
        src = work / 'source.docx'
        shutil.copyfile(source, src)
        before = validator.errors(src.read_bytes(), '.docx')
        result['input'] = before
        for name, ops in plans:
            for mode in DOCX_MODES:
                plan = work / 'plan.json'
                plan.write_text(json.dumps({'author': 'Conformance', 'change_mode': mode, 'ops': ops}))
                out = work / 'out.docx'
                out.unlink(missing_ok=True)
                reason = cli.apply(src, plan, out)
                if reason:
                    result['rejected'].append((f'{name} {mode}', reason))
                    continue
                result['applied'].append(f'{name} {mode}')
                after = validator.errors(out.read_bytes(), '.docx')
                if keep:
                    shutil.copyfile(out, Path(keep) / f'{stem}--{name.replace("/", "~")}--{mode}.docx')
                for kind, message in sorted(unmasked(after.keys() - before.keys(), before - after)):
                    result['failures'].append(f'{label}: plan {name} ({mode}) introduced {kind}: {message[:240]}')
        return result
    finally:
        shutil.rmtree(work, ignore_errors=True)


def check_docx_create(job):
    """docx create on one HTML file: the output may have no error at all."""
    global _process_validator
    label, html, binary, keep = job
    if _process_validator is None:
        _process_validator = Validator()
    work = Path(tempfile.mkdtemp(prefix='conformance-create-'))
    result = {'label': label, 'input': collections.Counter(), 'applied': [], 'rejected': [], 'failures': []}
    try:
        out = work / 'created.docx'
        reason = Docx(binary).create(html, out)
        if reason:
            result['failures'].append(f'{label}: create refused: {reason}')
            return result
        result['applied'].append('create')
        if keep:
            shutil.copyfile(out, Path(keep) / f'create--{html.stem}.docx')
        for (part, message), count in sorted(_process_validator.errors(out.read_bytes(), '.docx').items()):
            result['failures'].append(f'{label}: created document has {part}: {message[:240]} (x{count})')
        return result
    finally:
        shutil.rmtree(work, ignore_errors=True)


def docx_source_path(args, set_name, file):
    """The file of a write-baseline source, or None when its set is not available."""
    roots = {'write': ROOT / 'tools' / 'oracle' / 'testdata' / 'write', 'authored': ROOT / 'testdata' / 'docx'}
    if args.docx_corpus:
        roots['corpus-lo'] = Path(args.docx_corpus)
    if args.docxdriver:
        roots['fixtures'] = Path(args.docxdriver)
    root = roots.get(set_name)
    if root is None:
        return None
    path = root / file
    return path if path.exists() else None


def load_docx_jobs(args):
    """(label, source, [(plan id, ops)]): per ported op, the first --docx-per-op
    positive and dirty plans of each available source set, by id."""
    by_source = collections.defaultdict(list)
    ported = docx_ported()
    for plans_file in sorted(WRITE_PLANS.glob('*.jsonl')):
        picked = collections.Counter()
        for line in plans_file.read_text().splitlines():
            plan = json.loads(line)
            if plan['kind'] not in ('positive', 'dirty') or 'plan' in plan or not plan.get('ops'):
                continue
            if not all(op.get('op') in ported for op in plan['ops']):
                continue
            key = (plan['kind'], plan['set'])
            if picked[key] >= args.docx_per_op:
                continue
            source = docx_source_path(args, plan['set'], plan['file'])
            if source is None:
                continue
            picked[key] += 1
            by_source[source].append((plan['id'], plan['ops']))
    return [(f'docx {source.name}', source, plans) for source, plans in sorted(by_source.items())]


def load_jobs(args):
    """(label, deck, [(plan name, plan path)]) for every deck to check."""
    jobs, skipped = [], []
    if args.decks:
        plans = sorted(Path(args.fixture_plans).glob('*.json'))
        for deck in sorted(Path(args.decks).glob('*.pptx')):
            jobs.append((f'fixture {deck.name}', deck, [(f'{p.stem}', p) for p in plans]))
    if args.repo_plans:
        corpus = Path(args.corpus)
        by_deck = collections.defaultdict(list)
        for plan in sorted(Path(args.repo_plans).glob('*.json')):
            task = Path(args.tasks) / plan.stem / 'task.json'
            deck = corpus / json.loads(task.read_text())['deck']['path']
            if not deck.exists():
                skipped.append(f'{plan.name}: deck {deck.relative_to(corpus)} is not in the corpus')
                continue
            by_deck[deck].append((plan.stem, plan))
        for deck, plans in sorted(by_deck.items()):
            jobs.append((f'corpus {deck.relative_to(corpus)}', deck, plans))
    return jobs, skipped


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--cli', default=str(ROOT / 'bin' / 'pptx'))
    parser.add_argument('--decks', help='directory of fixture .pptx decks (and assets/ for plans)')
    parser.add_argument('--fixture-plans', default=str(HERE / 'plans'))
    parser.add_argument('--repo-plans', help='directory of plans whose task names a corpus deck (eval/plans)')
    parser.add_argument('--tasks', default=str(ROOT / 'eval' / 'tasks'))
    parser.add_argument('--corpus', help='corpus directory (decks named by task.json deck.path)')
    parser.add_argument('--jobs', type=int, default=1, help='independent deck workers (default: 1)')
    parser.add_argument('--keep-outputs', metavar='DIR', help='save every applied output to DIR')
    parser.add_argument('--docx-cli', help='the docx CLI: also check docx apply and create outputs')
    parser.add_argument('--docx-per-op', type=int, default=4,
                        help='plans per ported operation, kind (positive, dirty) and source set (default: 4)')
    parser.add_argument('--docx-corpus', help='docxdriver corpus/lo, for the write baseline plans on corpus files')
    parser.add_argument('--docxdriver', help='the docxdriver checkout, for the plans on its fixtures')
    parser.add_argument('--docx-create', nargs='*', default=sorted(str(p) for p in (HERE / 'docx').glob('*.html')),
                        help='HTML files for docx create (default: tools/conformance/docx/*.html)')
    args = parser.parse_args()
    if args.jobs < 1:
        parser.error('--jobs must be positive')
    if args.repo_plans and not args.corpus:
        parser.error('--repo-plans needs --corpus')
    problem = build_sdk_validator()
    if problem:
        print('FAIL', problem)
        return 1
    keep = str(Path(args.keep_outputs).resolve()) if args.keep_outputs else None
    if keep:
        Path(keep).mkdir(parents=True, exist_ok=True)
    binary = str(Path(args.cli).resolve())
    cwd = str(Path(args.decks).resolve()) if args.decks else str(ROOT)
    loaded, skipped = load_jobs(args)
    jobs = [(label, deck.resolve(), plans, binary, cwd, keep) for label, deck, plans in loaded]

    if args.jobs == 1:
        results = list(map(check_deck, jobs))
    else:
        with ProcessPoolExecutor(max_workers=args.jobs) as pool:
            results = list(pool.map(check_deck, jobs))
    if args.docx_cli:
        docx = str(Path(args.docx_cli).resolve())
        keep_docx = keep
        docx_jobs = [(label, source.resolve(), plans, docx, keep_docx) for label, source, plans in load_docx_jobs(args)]
        create_jobs = [(f'docx create {Path(h).name}', Path(h).resolve(), docx, keep_docx) for h in args.docx_create]
        if args.jobs == 1:
            results += list(map(check_docx_source, docx_jobs)) + list(map(check_docx_create, create_jobs))
        else:
            with ProcessPoolExecutor(max_workers=args.jobs) as pool:
                results += list(pool.map(check_docx_source, docx_jobs)) + list(pool.map(check_docx_create, create_jobs))

    failures, applied = [], 0
    for r in results:
        applied += len(r['applied'])
        failures += r['failures']
        print(f"{r['label']}: input has {sum(r['input'].values())} errors in {len(r['input'])} kinds; "
              f"{len(r['applied'])} plans applied, {len(r['rejected'])} rejected, {len(r['failures'])} new error kinds")
        for (part, message), count in sorted(r['input'].items()):
            print(f'  INPUT {part}: {message[:200]} (x{count})')
        for name, reason in r['rejected']:
            print(f'  not applied: {name}: {reason}')
    for line in skipped:
        print('skipped', line)
    if applied == 0:
        failures.append('no plan was applied; nothing was checked')
    for failure in failures:
        print('FAIL', failure)
    print(f'{len(results)} inputs, {applied} outputs checked, {len(failures)} failures')
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
