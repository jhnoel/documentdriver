.PHONY: build test vet fmt norequire wasip1 check corpus fuzz tools tool-golangci-lint tool-govulncheck lint vuln race repro release release-check release-smoke publish ship ci hooks powerpoint-smoke word-smoke excel-smoke conformance oracle-write-gen oracle-write-record oracle-write-compare oracle-write-gate

# Builds and tests are pure Go: no cgo. The one exception is `make race`,
# because the race detector needs cgo on macOS and Linux (see below).
export CGO_ENABLED := 0

# Dev tools that are modules are pinned here and installed into the
# repo-ignored bin/ with `go install pkg@version`, which never touches
# go.mod.
GOLANGCI_LINT_VERSION = v2.15.0
GOVULNCHECK_VERSION = v1.9.0
BIN = $(CURDIR)/bin

build:
	go build -o pptx ./cmd/pptx
	go build -o docx ./cmd/docx
	go build -o xlsx ./cmd/xlsx
	go build -o vsdx ./cmd/vsdx

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

# Standard library only: go.mod must not require any module.
norequire:
	@if grep -Eq '^[[:space:]]*require([[:space:](]|$$)' go.mod; then echo "go.mod has a require line; the module is stdlib only"; exit 1; fi

# Portability guard: everything (cmd/... is pptx, docx and xlsx) but the
# bench harness eval/cmd/pptxbench (which sets process groups through
# syscall.SysProcAttr.Setpgid) builds for wasip1.
WASIP1_PKGS = ./cmd/... ./internal/... ./tools/... ./eval/grade/... ./eval/oracle/... ./eval/corpus/...

wasip1:
	GOOS=wasip1 GOARCH=wasm go build $(WASIP1_PKGS)

check: norequire vet test fmt

# corpus is the full corpus sweep: every test of the module with both
# corpora set, PPTX_CORPUS (default: the repository's corpus/, from a
# worktree too, via tools/corpusdir) and DOCX_CORPUS (default: corpus/lo
# of DOCXDRIVER_REPO, which defaults to the docxdriver checkout beside the
# main checkout), XLSX_CORPUS (default: the xlsx/ directory of the
# pptx corpus, where eval/corpus/fetch-xlsx.sh puts it; passed through
# only when it exists, and an error when set to a missing directory),
# and VSDX_CORPUS (default: the vsdx/ directory of the pptx corpus,
# files under repos/<source>/, where eval/corpus/fetch-vsdx.sh puts it;
# passed through only when it exists, and an error when set to a
# missing directory). The
# corpus suites check their files in parallel
# (internal/corpustest): CORPUS_PARALLEL files at once per suite (default
# GOMAXPROCS). SHARD=i/n (1 <= i <= n) runs only shard i of n of every
# suite's files, by a stable hash of each file's corpus-relative name: the
# n shards together check exactly the full set, so n machines can each run
# one with no coordination. CORPUS_P is go test -p, the packages tested
# at once.
# Usage: make corpus [SHARD=i/n] [CORPUS_PARALLEL=k] [CORPUS_P=p]
SHARD ?=
CORPUS_P ?= 2
CORPUS_TIMEOUT ?= 3h

corpus:
	@set -e; \
	main="$$(go run ./tools/corpusdir)"; \
	pptx="$${PPTX_CORPUS:-$$main}"; \
	dd="$${DOCXDRIVER_REPO:-$$(dirname "$$(dirname "$$main")")/docxdriver}"; \
	docx="$${DOCX_CORPUS:-$$dd/corpus/lo}"; \
	for d in "$$pptx" "$$docx"; do \
		[ -d "$$d" ] || { echo "corpus: $$d is not a directory (set PPTX_CORPUS and DOCX_CORPUS)"; exit 2; }; \
	done; \
	xlsx="$${XLSX_CORPUS:-}"; \
	if [ -n "$$xlsx" ]; then \
		[ -d "$$xlsx" ] || { echo "corpus: XLSX_CORPUS=$$xlsx is not a directory"; exit 2; }; \
	elif [ -d "$$main/xlsx" ]; then xlsx="$$main/xlsx"; fi; \
	vsdx="$${VSDX_CORPUS:-}"; \
	if [ -n "$$vsdx" ]; then \
		[ -d "$$vsdx" ] || { echo "corpus: VSDX_CORPUS=$$vsdx is not a directory"; exit 2; }; \
	elif [ -d "$$main/vsdx" ]; then vsdx="$$main/vsdx"; fi; \
	echo "corpus: PPTX_CORPUS=$$pptx DOCX_CORPUS=$$docx XLSX_CORPUS=$${xlsx:-(none)} VSDX_CORPUS=$${vsdx:-(none)} shard $${SHARD:-all}, CORPUS_PARALLEL=$${CORPUS_PARALLEL:-GOMAXPROCS}, -p $(CORPUS_P)"; \
	PPTX_CORPUS="$$pptx" DOCX_CORPUS="$$docx" XLSX_CORPUS="$$xlsx" VSDX_CORPUS="$$vsdx" CORPUS_SHARD="$(SHARD)" \
		go test -count=1 -timeout $(CORPUS_TIMEOUT) -p $(CORPUS_P) ./...

# Native Go fuzzing, FUZZTIME per target (go test fuzzes one target per
# run). Not part of check; make ci runs it with the default FUZZTIME. A crasher lands in the
# package's testdata/fuzz; commit it with its fix. When -fuzztime runs out,
# go test can report "context deadline exceeded" without having found
# anything (the coordinator sees its deadline before the workers' context
# is cancelled; most often under load); a run whose only failure is that,
# with no failing input written, counts as a pass.
FUZZTIME ?= 10s
FUZZ_TARGETS = \
	./internal/ooxml/tree:FuzzRoundTrip \
	./internal/ooxml/tree:FuzzDifferential \
	./internal/ooxml/tree:FuzzSetAttr \
	./internal/opc:FuzzOpen \
	./internal/cli:FuzzParseJSON \
	./internal/docx/ops:FuzzParsePlan \
	./internal/pptx/ops:FuzzPlan \
	./internal/pptx/ops/text:FuzzParse \
	./internal/pptx/derive:FuzzParsePath \
	./internal/pptx/derive:FuzzUnplace \
	./internal/pptx/view:FuzzRead \
	./internal/pptx/view:FuzzReadEdits \
	./internal/docx/integrity:FuzzCheck \
	./internal/docx/integrity:FuzzCheckDocument \
	./internal/htmlx:FuzzParseHTML \
	./internal/docx/html:FuzzParseFragment \
	./internal/docx/math:FuzzOMMLToLaTeX \
	./internal/docx/math:FuzzLaTeXToOMML \
	./internal/docx/read:FuzzRead \
	./cmd/xlsx:FuzzCommandLine \
	./internal/xlsx/integrity:FuzzCheck \
	./internal/xlsx/integrity:FuzzCheckParts \
	./internal/ooxml/splice:FuzzPatch \
	./internal/xlsx/formula:FuzzLex \
	./internal/xlsx/formula:FuzzShift \
	./internal/xlsx/formula:FuzzDisplay \
	./internal/xlsx/numfmt:FuzzClassify \
	./internal/xlsx/sheet:FuzzScan \
	./internal/xlsx/sheet:FuzzSST \
	./internal/xlsx/read:FuzzRead \
	./internal/xlsx/read:FuzzReadParts \
	./internal/xlsx/read:FuzzFind \
	./internal/xlsx/formula:FuzzStore \
	./internal/xlsx/edit:FuzzLexNode \
	./internal/xlsx/ops:FuzzParsePlan \
	./internal/xlsx/ops:FuzzDecodeValue \
	./internal/xlsx/shift:FuzzSheetData \
	./internal/xlsx/apply:FuzzApply \
	./internal/ascii:FuzzLower

fuzz:
	@set -e; log=$$(mktemp); trap 'rm -f "$$log"' EXIT; \
	for t in $(FUZZ_TARGETS); do \
		pkg=$${t%%:*}; name=$${t##*:}; \
		echo "fuzz $$pkg $$name ($(FUZZTIME))"; \
		if go test "$$pkg" -run '^$$' -fuzz "^$$name\$$" -fuzztime $(FUZZTIME) >"$$log" 2>&1; then \
			tail -n 1 "$$log"; \
		elif grep -q 'context deadline exceeded' "$$log" && ! grep -q 'Failing input written' "$$log"; then \
			tail -n 4 "$$log"; echo "fuzz: $$name: deadline race only, no failing input; counted as a pass"; \
		else \
			cat "$$log"; exit 1; \
		fi; \
	done

# Install the pinned dev tools into bin/ (not into go.mod, not globally).
tools: tool-golangci-lint tool-govulncheck

tool-golangci-lint:
	GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

tool-govulncheck:
	GOBIN=$(BIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

# lint runs a locally installed golangci-lint (bin/ first, then PATH); it
# never installs anything itself.
lint:
	@l="$$(if [ -x $(BIN)/golangci-lint ]; then echo $(BIN)/golangci-lint; else command -v golangci-lint; fi)"; \
	if [ -z "$$l" ]; then \
		echo "golangci-lint not found. Install the pinned $(GOLANGCI_LINT_VERSION) into bin/ with: make tools"; \
		exit 1; \
	fi; \
	"$$l" run ./...

# vuln runs govulncheck over the module. With no dependencies it reports
# Go toolchain and standard-library issues, so it needs a current Go.
vuln:
	@v="$$(if [ -x $(BIN)/govulncheck ]; then echo $(BIN)/govulncheck; else command -v govulncheck; fi)"; \
	if [ -z "$$v" ]; then \
		echo "govulncheck not found. Install the pinned $(GOVULNCHECK_VERSION) into bin/ with: make tools"; \
		exit 1; \
	fi; \
	"$$v" ./... || { rc=$$?; echo "govulncheck failed ($$rc). A standard-library finding means the local Go toolchain ($$(go env GOVERSION)) is older than the fixed release: update Go."; exit $$rc; }

# race is the one documented exception to CGO_ENABLED=0: the race detector
# needs cgo on macOS and Linux (a C toolchain must be installed). Nothing
# else builds with cgo, and archtest still forbids `import "C"` in source.
# The timeout is 60m, not go test's 10m: from a worktree the docx and
# xlsx corpus suites fall back to the local corpus, which takes longer
# than 10m per package under the race detector.
race:
	CGO_ENABLED=1 go test -race -timeout 60m ./...

# scaling is the scaling gate (internal/laws.Scales; every test whose
# name starts TestScaling, so a family adds its own): create, read, find
# and apply in docx (and create's families: tables, lists), read and
# apply in pptx, each on inputs of 1,000 and
# 4,000 units, may cost at most 5.5 times as much (bytes allocated and
# allocations; time is reported only) on the larger. Linear work
# gives about 4, quadratic 16. With it runs the docx memory budget
# (TestMemoryBudget, laws.WithinMemory): peak live heap at checkpoints
# and bytes allocated of each docx command at 4,000 paragraphs. The
# checks are deterministic, so they hold on a loaded machine. It runs in
# seconds; make test runs it too, this target runs it alone and uncached.
scaling:
	go test -count=1 -run '^(TestScaling|TestMemoryBudget)' ./internal/docx/apply ./internal/docx/create ./internal/pptx/apply ./internal/xlsx/read ./internal/xlsx/apply ./internal/xlsx/shift ./internal/vsdx/read

# repro builds each CLI of REPRO_CMDS twice from `git archive HEAD` in two
# separate clean directories (fresh build caches, different paths) with
# CGO_ENABLED=0 -trimpath and fails if any binary differs. It checks the
# committed HEAD, not uncommitted changes.
REPRO_CMDS = pptx docx xlsx vsdx

repro:
	@set -e; \
	a="$$(mktemp -d)"; b="$$(mktemp -d)"; trap 'rm -rf "$$a" "$$b"' EXIT; \
	for d in "$$a" "$$b"; do \
		mkdir "$$d/src"; \
		git archive HEAD | tar -x -C "$$d/src"; \
		for c in $(REPRO_CMDS); do \
			(cd "$$d/src" && GOCACHE="$$d/cache" CGO_ENABLED=0 go build -trimpath -o "$$d/$$c" ./cmd/$$c); \
		done; \
	done; \
	for c in $(REPRO_CMDS); do \
		ha="$$(shasum -a 256 "$$a/$$c" | cut -d' ' -f1)"; hb="$$(shasum -a 256 "$$b/$$c" | cut -d' ' -f1)"; \
		echo "$$c build 1: $$ha"; echo "$$c build 2: $$hb"; \
		if [ "$$ha" != "$$hb" ]; then echo "$$c builds differ: not reproducible"; exit 1; fi; \
	done; \
	echo "reproducible: $(REPRO_CMDS)"

# release builds a release into dist/ (gitignored) with tools/release:
# every CLI under cmd/ for linux/amd64, linux/arm64 and darwin/arm64 as
# <cmd>-<VERSION>-<os>-<arch> (CGO_ENABLED=0 -trimpath, VERSION stamped
# into --version), one documentdriver wheel per platform carrying docx,
# pptx and xlsx (packaging/python), and SHA256SUMS. It then checks what it wrote
# (digests, each wheel's RECORD, --version of this machine's binaries).
# It publishes nothing; RELEASING.md has the procedure.
# Usage: make release VERSION=0.1.0
VERSION ?=

release:
	@if [ -z "$(VERSION)" ]; then echo "usage: make release VERSION=x.y.z[-devN|-aN|-bN|-rcN]"; exit 2; fi
	go run ./tools/release -version $(VERSION) -out dist

# release-check is the release's reproducibility check: it runs the
# release twice from `git archive HEAD` in two clean directories (fresh
# build caches, different paths) and fails if the SHA256SUMS differ. Like
# repro, it checks committed work only. Manual (it cross-builds every
# target twice from cold caches), not part of ci.
# Usage: make release-check [VERSION=0.1.0-dev]
release-check:
	@set -e; v="$(or $(VERSION),0.1.0-dev)"; \
	a="$$(mktemp -d)"; b="$$(mktemp -d)"; trap 'rm -r "$$a" "$$b"' EXIT; \
	for d in "$$a" "$$b"; do \
		mkdir "$$d/src"; \
		git archive HEAD | tar -x -C "$$d/src"; \
		(cd "$$d/src" && GOCACHE="$$d/cache" go run ./tools/release -version "$$v" -out "$$d/dist"); \
	done; \
	cat "$$a/dist/SHA256SUMS"; \
	if ! cmp -s "$$a/dist/SHA256SUMS" "$$b/dist/SHA256SUMS"; then diff "$$a/dist/SHA256SUMS" "$$b/dist/SHA256SUMS"; echo "release-check: the two releases differ: not reproducible"; exit 1; fi; \
	echo "release-check: reproducible ($$v)"

# release-smoke installs this machine's wheel from dist/ with pip into a
# fresh venv (made by uv, seeded with pip, as Open WebUI installs a
# tool's requirements) and runs packaging/python/smoke.py there with uv
# run (python -I, so the source package beside it is not imported): every
# bundled binary runs and reports VERSION, run() returns results, outputs
# and the contract's errors, and the SHA-256 self-check refuses a mismatch. SMOKE_DIR is an empty directory
# for the venv (default: a temporary one, removed after). Needs uv.
# Usage: make release-smoke VERSION=0.1.0 [SMOKE_DIR=<empty dir>]
SMOKE_DIR ?=

release-smoke:
	@if [ -z "$(VERSION)" ]; then echo "usage: make release-smoke VERSION=x.y.z [SMOKE_DIR=<empty dir>]"; exit 2; fi
	@command -v uv >/dev/null || { echo "uv is required (Python runs through uv run)"; exit 1; }
	@set -e; \
	whl="$$(go run ./tools/release -version $(VERSION) -out dist -host-wheel)"; \
	[ -f "$$whl" ] || { echo "release-smoke: no $$whl; run make release VERSION=$(VERSION) first"; exit 1; }; \
	d="$(SMOKE_DIR)"; \
	if [ -z "$$d" ]; then d="$$(mktemp -d)"; trap 'rm -r "$$d"' EXIT; fi; \
	if [ -n "$$(ls -A "$$d" 2>/dev/null)" ]; then echo "release-smoke: $$d is not empty"; exit 1; fi; \
	py="$$(basename "$$whl" | cut -d- -f2)"; \
	uv venv --quiet --seed "$$d/venv"; \
	"$$d/venv/bin/python" -m pip install --quiet --no-index --no-deps "$$whl"; \
	uv run --no-project --python "$$d/venv/bin/python" python -I packaging/python/smoke.py "$(VERSION)" "$$py"

# publish uploads the wheels of a built and checked release from dist/
# to PyPI (PYPI=pypi, the default) or TestPyPI (PYPI=testpypi) with uv
# publish. It is the owner's step (RELEASING.md); nothing runs it
# automatically. The token comes from UV_PUBLISH_TOKEN in the environment
# (a PyPI API token scoped to the documentdriver project), never from a
# file in the repository. Before uploading it re-checks dist/ (go run
# ./tools/release -check: SHA256SUMS, every wheel's RECORD and tags), and
# for PyPI it also wants a clean tree with HEAD tagged vVERSION. Only the
# wheels go up: the raw binaries and SHA256SUMS belong to the download
# host. --check-url makes a rerun skip files already uploaded with the
# same hash. PUBLISH_DRY_RUN=1 passes uv publish --dry-run (no token).
# Usage: make publish VERSION=0.2.0 [PYPI=testpypi] [PUBLISH_DRY_RUN=1]
PYPI ?= pypi
PUBLISH_DRY_RUN ?=

publish:
	@if [ -z "$(VERSION)" ]; then echo "usage: make publish VERSION=x.y.z [PYPI=pypi|testpypi] [PUBLISH_DRY_RUN=1]"; exit 2; fi
	@command -v uv >/dev/null || { echo "uv is required (uv publish uploads the wheels)"; exit 1; }
	@set -e; \
	case "$(PYPI)" in \
		pypi) up=https://upload.pypi.org/legacy/; check=https://pypi.org/simple/ ;; \
		testpypi) up=https://test.pypi.org/legacy/; check=https://test.pypi.org/simple/ ;; \
		*) echo "publish: PYPI must be pypi or testpypi, not $(PYPI)"; exit 2 ;; \
	esac; \
	if [ "$(PYPI)" = pypi ]; then \
		[ -z "$$(git status --porcelain)" ] || { echo "publish: the tree is not clean; publish a committed, tagged release"; exit 1; }; \
		git tag --points-at HEAD | grep -qx 'v$(VERSION)' || { echo "publish: HEAD is not tagged v$(VERSION) (git tag -a v$(VERSION) -m 'documentdriver $(VERSION)')"; exit 1; }; \
	fi; \
	[ -n "$(PUBLISH_DRY_RUN)" ] || [ -n "$$UV_PUBLISH_TOKEN" ] || { echo "publish: set UV_PUBLISH_TOKEN to a $(PYPI) API token for documentdriver"; exit 1; }; \
	go run ./tools/release -version $(VERSION) -out dist -check; \
	py="$$(basename "$$(go run ./tools/release -version $(VERSION) -out dist -host-wheel)" | cut -d- -f2)"; \
	set -- dist/documentdriver-$$py-py3-none-*.whl; \
	{ [ "$$#" = 3 ] && [ -f "$$1" ]; } || { echo "publish: want 3 wheels for $$py in dist/, found: $$*"; exit 1; }; \
	echo "publish: $$* -> $(PYPI)"; \
	uv publish --trusted-publishing never --publish-url "$$up" --check-url "$$check" $(if $(PUBLISH_DRY_RUN),--dry-run) "$$@"

# ship cuts a release in one command, from a clean main: make ci (skipped
# when this tree already passed with this Go), release-check, release,
# release-smoke, the annotated tag vVERSION, publish to PyPI, and git
# push of main and the tag. A rerun after a failure picks up where it
# stopped: a tag already at HEAD is kept, uploaded files are skipped and
# the push is quick once ci passed. The token is UV_PUBLISH_TOKEN, as for
# publish. It publishes and pushes, so it is the owner's command.
# Usage: UV_PUBLISH_TOKEN=<pypi token> make ship VERSION=0.2.1
ship:
	@if [ -z "$(VERSION)" ]; then echo "usage: UV_PUBLISH_TOKEN=<token> make ship VERSION=x.y.z"; exit 2; fi
	@set -e; \
	[ -n "$$UV_PUBLISH_TOKEN" ] || { echo "ship: set UV_PUBLISH_TOKEN to a PyPI API token for documentdriver"; exit 1; }; \
	[ "$$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "ship: check out main (releases are cut from main)"; exit 1; }; \
	[ -z "$$(git status --porcelain)" ] || { echo "ship: the tree is not clean"; exit 1; }; \
	if t="$$(git rev-parse -q --verify 'refs/tags/v$(VERSION)^{commit}')"; then \
		[ "$$t" = "$$(git rev-parse HEAD)" ] || { echo "ship: v$(VERSION) already tags another commit ($$t)"; exit 1; }; \
	fi; \
	passed="$$(git rev-parse --git-common-dir)/$(CI_PASSED)"; \
	if [ -f "$$passed" ] && grep -qx "$$(git rev-parse HEAD^{tree}) $$(go env GOVERSION)" "$$passed"; then \
		echo "ship: this tree already passed make ci"; \
	else \
		$(MAKE) --no-print-directory ci; \
	fi; \
	$(MAKE) --no-print-directory release-check VERSION=$(VERSION); \
	$(MAKE) --no-print-directory release VERSION=$(VERSION); \
	$(MAKE) --no-print-directory release-smoke VERSION=$(VERSION); \
	git rev-parse -q --verify 'refs/tags/v$(VERSION)' >/dev/null || git tag -a v$(VERSION) -m "documentdriver $(VERSION)"; \
	$(MAKE) --no-print-directory publish VERSION=$(VERSION) PYPI=pypi; \
	git push origin main v$(VERSION); \
	echo "ship: documentdriver $(VERSION) is on PyPI and v$(VERSION) is pushed"

# ci is the single local gate (run before merging to main, and by the
# pre-push hook for a commit it has not passed). It runs every step even
# after a failure and lists what failed. fuzz is included only when the
# target exists.
#
# When it passes on a clean tree (git status --porcelain empty, so no
# untracked or modified file took part), and the tree is still the same
# afterwards, it appends "<tree hash> <go version>" to CI_PASSED in the
# repository's common git directory, which every worktree shares. The
# pre-push hook pushes a commit whose tree and Go toolchain are listed
# without running ci again; a fast-forward merge keeps the tree, so a
# pass on a wt/ branch covers main. The file is local and untracked.
CI_STEPS = norequire vet test fmt lint vuln race repro wasip1 scaling
CI_PASSED = documentdriver-ci-passed

ci:
	@steps="$(CI_STEPS)"; \
	if grep -Eq '^fuzz:' Makefile; then steps="$$steps fuzz"; fi; \
	tree=""; \
	if [ -z "$$(git status --porcelain)" ]; then tree="$$(git rev-parse HEAD^{tree})"; fi; \
	failed=""; \
	for s in $$steps; do \
		echo "=== make $$s"; \
		$(MAKE) --no-print-directory $$s || failed="$$failed $$s"; \
	done; \
	if [ -n "$$failed" ]; then echo "make ci FAILED:$$failed"; exit 1; fi; \
	echo "make ci OK: $$steps"; \
	if [ -n "$$tree" ] && [ -z "$$(git status --porcelain)" ] && [ "$$(git rev-parse HEAD^{tree})" = "$$tree" ]; then \
		echo "$$tree $$(go env GOVERSION)" >> "$$(git rev-parse --git-common-dir)/$(CI_PASSED)"; \
		echo "make ci: recorded tree $$tree as passed"; \
	else \
		echo "make ci: the tree was not clean (or changed during the run), so this pass is not recorded for git push"; \
	fi

# hooks points this clone at the committed hooks in .githooks (per clone;
# it writes only to this clone's config). Never bypass them with
# --no-verify.
hooks:
	git config core.hooksPath .githooks

# powerpoint-smoke opens decks in PowerPoint and reports repair prompts. It
# FORCE-QUITS PowerPoint: run it only after the maintainer agrees, never from
# a hook. Usage: make powerpoint-smoke DIR=<directory of decks>
powerpoint-smoke:
	@if [ -z "$(DIR)" ]; then echo "usage: make powerpoint-smoke DIR=<directory of decks> (force-quits PowerPoint; ask the maintainer first)"; exit 2; fi
	go run ./tools/pptoracle smoke $(DIR)

# word-smoke opens a source .docx and its outputs in Microsoft Word and
# reports repair prompts and failed opens, each output judged relative to
# the source. It FORCE-QUITS Word: the maintainer approves every run, never
# from a hook. Usage: make word-smoke SOURCE=<in.docx> OUT="<out.docx> ..."
word-smoke:
	@if [ -z "$(SOURCE)" ] || [ -z "$(OUT)" ]; then echo "usage: make word-smoke SOURCE=<in.docx> OUT=\"<out.docx> ...\" (force-quits Word; the maintainer approves each run)"; exit 2; fi
	sh tools/word-smoke/run.sh $(SOURCE) $(OUT)

# excel-smoke opens a source workbook (.xlsx or .xlsm) and its outputs in
# Microsoft Excel and reports repair prompts and failed opens, each output
# judged relative to the source. It FORCE-QUITS Excel: the maintainer
# approves every run, never from a hook or an agent.
# Usage: make excel-smoke SOURCE=<in.xlsx> OUT="<out.xlsx> ..."
excel-smoke:
	@if [ -z "$(SOURCE)" ] || [ -z "$(OUT)" ]; then echo "usage: make excel-smoke SOURCE=<in.xlsx> OUT=\"<out.xlsx> ...\" (force-quits Excel; the maintainer approves each run)"; exit 2; fi
	sh tools/excel-smoke/run.sh $(SOURCE) $(OUT)

# conformance is the OOXML conformance gate over the pptx test decks: the
# fixture decks (exported by internal/testdeck) and the outputs of applying
# plans to them, and eval/plans to the corpus decks their tasks name. Each
# deck and output is checked against the ECMA-376 schemas, the package
# integrity rules (content types from internal/ooxml/ctypes) and the Open
# XML SDK validator. It is slow and needs uv and dotnet (the validator is
# built into bin/ on first use), so it is manual: not part of check or ci.
# Errors already in an input deck are listed, not failed on. Usage:
#   make conformance [CONFORMANCE_ARGS="--jobs 4"]
CONFORMANCE_ARGS ?=

conformance:
	@command -v uv >/dev/null || { echo "uv is required (Python runs through uv run)"; exit 1; }
	@command -v dotnet >/dev/null || { echo "dotnet (SDK 10 or later) is required for the Open XML SDK validator: brew install dotnet"; exit 1; }
	go build -o bin/pptx ./cmd/pptx
	go build -o bin/docx ./cmd/docx
	@set -e; d="$$(mktemp -d)"; trap 'rm -r "$$d"' EXIT; \
	TESTDECK_EXPORT="$$d" go test ./internal/testdeck -run '^TestExport$$' -count=1 >/dev/null; \
	docx=""; if [ -n "$$DOCX_CORPUS" ]; then docx="$$docx --docx-corpus $$DOCX_CORPUS"; fi; \
	if [ -n "$$DOCXDRIVER_REPO" ]; then docx="$$docx --docxdriver $$DOCXDRIVER_REPO"; fi; \
	uv run --script tools/conformance/ooxml-conformance.py --cli bin/pptx --decks "$$d" \
		--repo-plans eval/plans --tasks eval/tasks --corpus "$$(go run ./tools/corpusdir)" \
		--docx-cli bin/docx $$docx $(CONFORMANCE_ARGS)
	uv run --script tools/conformance/test_ooxml_conformance.py

# The docx write baseline (tools/oracle/baseline/write, README there):
# every plan in the agent contract's form, run by the pinned docxdriver
# 0.5.2 (translated down) in track and direct mode. Needs the oracle CLI
# (bin/oracle-target, or DOCX_ORACLE), the docxdriver checkout and its
# corpus. OPS narrows to a comma-separated list of docxdriver op names,
# e.g. make oracle-write-compare OPS=format_paragraph,set_page_margins
OPS ?=
oracle-write-gen:
	go run ./tools/oracle write gen

oracle-write-record:
	go run ./tools/oracle write record $(if $(OPS),-ops $(OPS))

oracle-write-compare:
	go run ./tools/oracle write compare $(if $(OPS),-ops $(OPS))

# The write-parity gate: the full, unnarrowed compare, which fails on any
# differ, no-baseline or not-implemented run. A narrowed compare (OPS) is
# for a wave's triage only; its counts never stand for the gate.
oracle-write-gate:
	@[ -z "$(OPS)" ] || { echo "oracle-write-gate runs every operation; unset OPS"; exit 2; }
	go run ./tools/oracle write compare -require-implemented
