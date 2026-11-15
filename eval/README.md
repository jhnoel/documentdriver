# Evaluation harness (M0)

Every milestone is judged by measured task success on this corpus of real
diagram edits. The harness has four parts:

- `corpus/`: the survey of candidate decks (`web-sources.tsv`; the
  survey writes `manifest.jsonl` into the corpus directory, out of git) and the scripts that fetch them. Decks are not in
  the git tree: they live in `<repo>/corpus/` (gitignored;
  override with `$PPTX_CORPUS` or the tools' `--corpus` flag; fill it with
  `eval/corpus/fetch.sh` and `eval/corpus/fetch-web.sh`) and are never
  committed.
- `tasks/<task-id>/task.json`: 30 tasks over 10 decks, each with a
  machine-checkable expectation; Author and Freeform tasks also have a
  `rubric.md` for a later render judge.
- `grade/` (package) and `cmd/pptxbench` (command): materialise bundles,
  grade trial outputs, write reference outputs, self-test the graders.
- `oracle/`: reference solutions that edit slide XML directly through
  `internal/opc` and `internal/ooxml/tree` (positions and sites come from
  the read model), independent of pptxdriver's own write operations.

```sh
go run ./eval/cmd/pptxbench materialise           # eval/bundles/<task>/ (gitignored)
go run ./eval/cmd/pptxbench selftest              # graders against reference, no-op, sloppy outputs
go run ./eval/cmd/pptxbench grade --bundle eval/bundles/<task> --trial <trial dir>
go run ./eval/cmd/pptxbench list                  # the task table below
```

## Decks

Ten decks, chosen so every diagram kind in the design appears, most tasks
sit on glued connectors (where faithful derivation matters), and the
redistributable web decks carry most of the load.

| Deck (corpus path) | Licence | Slide | Kind | Why |
| --- | --- | --- | --- | --- |
| `web/openwhisk-architecture.pptx` | Apache-2.0 | slide1 | architecture, icon grid | glued elbow connectors into a hub, nested groups of hexagon gear icons, pictures, empty space below for authoring |
| `web/spade-standards-roadmap.pptx` | CC-BY-4.0 | slide7, slide3 | architecture (UML-style) | slide7: boxes joined by glued straight and elbow connectors with markers, the densest connect/insert target; slide3: free space beside a framed diagram |
| `web/elixir-greece-timeline.pptx` | CC-BY-4.0 | slide2 | timeline | year labels with uneven spacing, grouped milestone boxes with glued leader lines, one box off the slide |
| `web/healthdcat-workflow.pptx` | CC-BY-4.0 | slide4 | process graphic | seven numbered steps joined by loose arrow shapes (not connectors), labels off-centre, a free strip for a timeline |
| `web/hpc-assistant-flow.pptx` | CC-BY-4.0 | slide8 | process flow | five step cards, one off-colour card, one misaligned joiner line |
| `repos/apache-poi/.../aascu.org_workarea_downloadasset.aspx_id=5864.pptx` | Apache-2.0 (POI test data) | slide33 | hub / concept map | circles and satellite ovals with glued straight connectors at ellipse sites, flowchart-merge triangles |
| `repos/apache-poi/.../aascu.org_hbcu_leadershipsummit_cooper_.pptx` | Apache-2.0 (POI test data) | slide11 | flowchart | mostly unglued arrows, four boxes sharing one name (ids are the only unique handle) |
| `repos/apache-poi/.../customGeo.pptx` | Apache-2.0 (POI test data) | slide6 | org chart | SmartArt converted to a group of freeforms, plus preset callouts |
| `repos/libreoffice-core/sd/qa/unit/data/pptx/connectors.pptx` | MPL-2.0 (LibreOffice test data) | slide1 | connector topology | elbow connectors at every site of a rect, triangle and ellipse |
| `repos/libreoffice-core/sd/qa/unit/data/pptx/slide-sections.pptx` | MPL-2.0 (LibreOffice test data) | slide1 | numbered graphic | big custom-geometry numerals 1, 2, 3 to imitate in a freeform |

Not used: `dementia-policy-timeline` (no glue, text-heavy), `hra-vasculature`
(a stress case, not a business diagram), `hpc` slide 3 and others that
repeat what is above, `typical.pptx` slide 14 and `077 grouped shape.pptx`
(little diagram structure on the slide).

## Bundle format

`pptxbench materialise` copies each task's deck from the corpus (checking
its sha256) into `eval/bundles/<task-id>/`:

| File | For | Contents |
| --- | --- | --- |
| `input.pptx` | runner, grader | the corpus deck, byte for byte |
| `task.md` | agent | the instruction, the slide, and the answer or edit contract |
| `task.json` | grader only | the spec: deck ref, slide part, instruction, type, targets, checks, answer ids |
| `rubric.md` | judge only | Author and Freeform: the render rubric (not yet automated) |

The agent must never see `task.json` or `rubric.md`: they hold the
labelled answers and the target ids.

`task.json` fields: `id`, `type` (identify, restyle, move, connect, insert,
author, freeform), `category`, `deck` (`path` relative to the corpus root,
`sha256`, `licence`), `slide` (part name; part names never shift),
`instruction`, `targets` (input object ids the task may change or delete),
`max_new` (new objects allowed; -1 any), `checks`, `answer.ids`
(Identify), `rubric`, `notes` (authoring notes, not shown).

## Tasks

| Task | Type | Deck | Slide | Instruction |
| --- | --- | --- | --- | --- |
| `id-aa33-teachers-links` | identify | aascu-5864 | slide33 | Which ovals are linked to the 'Teachers' circle by connectors? |
| `id-cg6-topics-callout` | identify | customGeo | slide6 | Which callout bubble explains the 'Topics' level of the chart? |
| `id-co11-studying-target` | identify | aascu-cooper | slide11 | Which box does the arrow leaving 'Too little studying' point to? |
| `id-ow-gateway` | identify | openwhisk-architecture | slide1 | Which object is the rounded box in the middle that all four event-source connectors run into? |
| `id-ow-python` | identify | openwhisk-architecture | slide1 | Which picture is the Python logo among the runtime gears? |
| `id-sp7-twin-links` | identify | spade-standards-roadmap | slide7 | Which connectors are attached to the green 'Digital twin' box? |
| `rs-aa33-triangles` | restyle | aascu-5864 | slide33 | Fill the five large 'Goals, Strategies, Policies…' triangles inside the big circles with accent 4. Leave the small triangles alone. |
| `rs-cg6-callouts` | restyle | customGeo | slide6 | Fill the three blue callout bubbles with accent 2. |
| `rs-hpc8-answer-card` | restyle | hpc-assistant-flow | slide8 | Make the 'Answer' card the same purple as the other four step cards. |
| `rs-ow-source-links` | restyle | openwhisk-architecture | slide1 | Make the four connectors that run from the event sources into the central box 2 pt red (FF0000) lines. |
| `rs-sp7-white-boxes` | restyle | spade-standards-roadmap | slide7 | Fill every white labelled box in the diagram with accent 2. Leave the green boxes, the small triangle and diamond markers and the text labels as they are. |
| `mv-el2-offslide-box` | move | elixir-greece-timeline | slide2 | The '2nd All-Hands meeting' box runs off the right edge of the slide. Move it left so its right edge is 12 pt inside the slide edge, at the same height, and keep its leader line attached to it and running straight up to the timeline bar. |
| `mv-el2-year-labels` | move | elixir-greece-timeline | slide2 | Space the ten year labels under the timeline evenly from 2011 to 2020: leave the 2011 and 2020 labels where they are, give all ten the same top as the 2011 label, and do not resize them. |
| `mv-hd4-center-labels` | move | healthdcat-workflow | slide4 | Centre each step's label horizontally under its numbered square. Do not change the labels' height, size or vertical position. |
| `mv-hpc8-level-dash` | move | hpc-assistant-flow | slide8 | The little joining line between 'Ollama + RAG' and 'Answer' sits lower than the other three joiners. Move it up to the same height as them without changing its length or horizontal position. |
| `mv-sp7-top-row` | move | spade-standards-roadmap | slide7 | Align the tops of the three boxes in the top row (Observation data, Characteristic or property, Observable entity) with the top of 'Observation data', and keep every connector attached. |
| `cn-aa33-employers-peers` | connect | aascu-5864 | slide33 | Link the 'Employers' circle to the 'Peers' circle with a straight line from Employers' right side to Peers' left side, glued at both ends and styled like the other links (2 pt, the same blue). |
| `cn-co11-attach-arrow` | connect | aascu-cooper | slide11 | The arrow from 'Work/Loan Burden' to 'Stress' is not attached to either box. Attach it so it runs from the right side of 'Work/Loan Burden' to the left side of 'Stress', keeping its arrowhead at 'Stress'. |
| `cn-hd4-step1-step2` | connect | healthdcat-workflow | slide4 | Replace the loose arrow between square 1 and square 2 with a connector glued to the right side of square 1 and the left side of square 2, with the arrowhead at square 2. |
| `cn-lc1-square-circle` | connect | connectors | slide1 | Connect the square to the circle with an elbow connector that leaves the bottom of the square and enters the top of the circle, glued at both ends. |
| `cn-sp7-physical-digital` | connect | spade-standards-roadmap | slide7 | Draw a straight arrow from the right side of 'Physical Interface' to the left side of 'Digital Interface', glued at both ends, with the arrowhead at 'Digital Interface'. |
| `in-aa33-mentoring` | insert | aascu-5864 | slide33 | Add a new oval labelled 'Mentoring Programs' and link it to the 'Community' circle with a straight connector glued at both ends, styled like the other links (2 pt, the same blue). |
| `in-ow-webhooks` | insert | openwhisk-architecture | slide1 | Add a fifth event source: a box labelled 'Webhooks' below the bottom antenna, joined to the central box by an elbow connector glued to the box and to the central box's left side, like the other four sources. |
| `in-sp7-sync` | insert | spade-standards-roadmap | slide7 | Split the 'twinned with' link between 'Digital twin' and 'Target entity' with a small box labelled 'Sync': Digital twin → Sync → Target entity, both connectors glued at both ends, arrowhead at 'Target entity'. |
| `in-sp7-twin-model` | insert | spade-standards-roadmap | slide7 | Insert a box labelled 'Twin model' on the link from 'Digital twin' to 'Interface', so the path becomes Digital twin → Twin model → Interface. Both connectors must be glued at both ends, with the arrowhead at 'Interface'. |
| `au-hd4-quarters` | author | healthdcat-workflow | slide4 | In the strip between the three panels and the footnote, add a horizontal timeline: one straight line with four evenly spaced circular milestone dots on it, labelled Q1, Q2, Q3 and Q4 from left to right. |
| `au-ow-chevrons` | author | openwhisk-architecture | slide1 | Below the diagram, draw a five-step chevron process reading Event, Trigger, Rule, Action, Result from left to right: equal-sized chevrons on one line, evenly spaced. |
| `au-sp3-orgchart` | author | spade-standards-roadmap | slide3 | In the empty space under the SPADE logo, draw a small org chart: a 'Coordinator' box on top and two boxes, 'Partners' and 'Users', side by side beneath it, each joined to the Coordinator by a connector glued from the bottom of the Coordinator to the top of the child. |
| `ff-sp7-callout` | freeform | spade-standards-roadmap | slide7 | Draw a freeform speech-bubble callout reading 'Core concept' in the empty area below the 'uses' label, with its tail pointing up at the 'Digital twin' box. Make it a single custom shape, not a preset callout. |
| `ff-ss1-numeral-four` | freeform | slide-sections | slide1 | In the empty space at the bottom left of the slide, draw an outline numeral 4 as a single freeform shape, in the same style as the big 1, 2 and 3 (a filled custom shape tracing the outline, no text). |

Six Identify, five Restyle, five Move and align, five Connect, four Insert,
three Author, two Freeform. The labelled answers and target ids are in
each `task.json` (`answer`, `targets`, `notes`), labelled from LibreOffice
renders with `pptx render --overlay ids` and `pptx read`.

## Grading

`pptxbench grade` reads the input and the output through the read model
(`internal/pptx/model`, `internal/pptx/derive`) and writes `grade.json`. All
geometry is in slide-space points.

### Identity of objects

Graders accept any well-formed output deck, whoever wrote it. Input and
output objects are paired by (1) the same short id (`cNvPr` id with the
duplicate suffix) and kind, with the same name unless the object is a
target; then (2) the same kind, name and plain text, which recovers objects
a tool renumbered. Unpaired input objects were deleted; unpaired output
objects are new. Checks name input objects by their input id, new objects
through aliases bound by earlier checks (a new shape found by its text, a
connector found by its glue). `grade.json` records how many objects paired
by id and by signature.

### Checks per type

| Type | Checks |
| --- | --- |
| Identify | The answer (`answer.txt`) names exactly the labelled objects. Format, told to the agent: one object per line, by shape id as stored (`17`, `s17`, `slide3#17`) or by a name that is unique on the slide. Unresolvable or extra objects set `wrong_object`. The deck must be unchanged. |
| Restyle | `fill` / `line` on every target: solid fill (or line) whose colour is the named scheme slot without modifiers, or whose resolved hex is within 8 per channel of the slot's or the given hex; line width exact to 0.01 pt. |
| Move and align | `box` (expected x, y, w, h, or `keep` = equal to the input), `align` (an edge equal to a value or a reference object), `distribute` (even spacing between the first and last), within 0.5 pt unless stated (0.25 pt for the year-label tops). |
| Connect | `connector`: a new or targeted connector glued A to B (either direction), at a named site index or any site on the named side (top, right, bottom, left, judged from the site's direction from the box centre; an ellipse's diagonal sites count for both neighbours), with each glued end within 0.5 pt of its site, plus geometry family, an arrowhead at B and line style where the instruction asks. `deleted` / `resolved` for an arrow being replaced or attached. |
| Insert | `new_shape` with the given text, `connector` checks through it, `no_connector` for the link it replaces. |
| Author | `sequence` (new shapes with given texts, in order, of given geometry), `new_line`, `new_objects`, `align`, `same_size`, `distribute`, `order`, `region` (every new object inside a box), all from the model; `rubric.md` for the render, pending. |
| Freeform | `new_shape` with `custGeom` (and text where asked), `path` (at least N points), `region`; `rubric.md` pending. |

### Rules every task applies

- **Unintended changes** (a count, with a reason per change):
  - each untargeted object deleted, changed, moved into another group, or
    moved in paint order (objects outside a longest increasing run of
    output positions);
  - new objects beyond `max_new`;
  - the slide outside its objects (background, colour map, timing,
    extensions): canonical comparison of the slide with its objects removed;
  - every other part of the package: removed, added or changed.

  An object or part is unchanged when its bytes are equal, or when its XML
  is canonically equal (names resolved to namespace URIs, attributes
  sorted, namespace declarations and whitespace between elements dropped,
  run text kept exactly); a group is compared without its children.
  Relationship parts and `[Content_Types].xml` compare as sets of entries.
  Canonical-only differences are counted as `reserialised`, not as changes.
  `docProps/app.xml` and `docProps/core.xml` may change (tools rewrite
  timestamps and statistics); such changes are listed in
  `allowed_changes`.
- **Hazards left**: stale connectors, dangling glue and group-frame
  hazards in the output that the input did not have.
- **PowerPoint** (optional, `--powerpoint`): runs `go run
  ./tools/pptoracle smoke --source input.pptx output.pptx` from `--repo`.
  It needs PowerPoint on the user's machine and is off by default.

A trial passes when every check passes, the unintended-change count is
zero, no hazard is left, the Identify answer is exact, and (when run)
PowerPoint opens the output cleanly. For Author and Freeform, `pass`
covers the model checks and `rubric_pending` is set.

### grade.json

`task`, `type`, `pass`, `checks` (type, pass, detail, `err_pt`),
`geom_error_pt` (largest deviation any check measured), `wrong_object`
(Identify: the answer named an unexpected or unknown object; edits: a
target expectation failed while an untargeted object changed),
`unintended_changes` and `unintended_detail`, `reserialised`,
`allowed_changes`, `hazards_left`, `match` (`by_id`, `by_signature`,
`new`, `deleted`), `answer`, `powerpoint`, `rubric_pending`, `error`.

### Not machine-checkable

- Render quality of Author and Freeform results: legibility, overlap with
  existing content, whether the freeform looks like a 4 or a speech
  bubble. `rubric.md` per task; no judge exists yet.
- Text fit anywhere: LibreOffice refits `normAutofit` text where
  PowerPoint does not (design open question 4).
- Whether an Insert keeps the diagram tidy (the new box may overlap
  labels): graph checks only.
- Repair-free opening, unless `--powerpoint` is run on the user's machine.

## Self-test

`pptxbench selftest` materialises every task into a temporary directory
and grades, per task:

| Variant | Expected |
| --- | --- |
| reference (oracle edit; Identify: the labelled answer, written in the accepted forms across tasks: `17`, `s17`, `slideN#17`, unique names) | pass |
| no-op (input unchanged) | fail (edits) |
| sloppy (reference plus a 1 pt nudge of the first untargeted top-level object) | fail, with unintended changes > 0 |
| reserialised (reference with whitespace inserted inside every `spPr`) | pass, with `reserialised` > 0 |
| Identify: wrong answer / no answer | fail; a wrong answer sets `wrong_object` |

`go test ./eval/...` runs the same self-test when `PPTX_CORPUS` is set.

To eyeball references in LibreOffice:

```sh
go run ./eval/cmd/pptxbench reference --bundle eval/bundles/<task> -o /tmp/ref.pptx
go run ./cmd/pptx render /tmp/ref.pptx --slide slide7 --overlay glue -o /tmp/ref.png
```

PowerPoint smoke of every reference (user's machine; PowerPoint closed):

```sh
go run ./eval/cmd/pptxbench references -out /tmp/pptx-refs
go run ./tools/pptoracle smoke --all /tmp/pptx-refs
```

The directory holds `<deck>--source.pptx` and `<deck>--<task>.pptx` per
editing task, the naming `pptoracle smoke` pairs outputs with sources by.

## Runner contract

Trials run in Claude Code headless (`claude -p`, the default) with
`claude-haiku-5-5`, or in the Pi CLI (`--runner pi`, see "Pi runner"), with
one model for every condition; `pptxbench run` is the runner (see "Running trials"
below). Any other runner honours the same contract; nothing in the bundle
format or grader depends on it.

### Arm and conditions

There is one arm: the agent edits with the pptxdriver CLI (`pptx read`,
`pptx apply`, `pptx xml`, `pptx render`). Conditions vary only what the
agent reads:

| Condition | Read view | Render |
| --- | --- | --- |
| `json` | `pptx read --slide` (JSON) | not available |
| `json+render` | `pptx read --slide` (JSON) | `pptx render` available |
| `svg` | `pptx read --slide --format svg` | not available |
| `svg+render` | `pptx read --slide --format svg` | `pptx render` available |

Plans are JSON and text is HTML in every condition. Each task runs at
least five trials per condition, in randomised order.

### Inputs

For each trial the runner:

1. creates a fresh working directory, outside the repository, holding
   only the bundle's `input.pptx` (read-only) and `task.md`;
2. gives the agent `task.md` as the user message, plus the condition's
   instructions (`eval/arms/pptxdriver/<condition>.md`); the agent writes
   `answer.txt` for Identify, or the edited deck as `output.pptx` for
   edit tasks, in the working directory;
3. never shows the agent `task.json` or `rubric.md`.

### Outputs

The runner writes a trial directory (for example
`eval/results/<run>/<task>/<condition>/<n>/`, gitignored) containing:

- `output.pptx`: the deck the agent wrote (for Identify tasks, which
  leave the deck alone, `input.pptx` as it was; omitted for an edit task
  that wrote no deck);
- `answer.txt`: Identify tasks, the agent's final answer;
- `trial.json`:

| Field | Meaning |
| --- | --- |
| `task` | task id |
| `bundle_sha256` | sha256 of `input.pptx` |
| `arm` | `pptxdriver` |
| `condition` | `json`, `json+render`, `svg` or `svg+render` |
| `runner` | runner name and version |
| `model` | exact model id served |
| `repeat` | trial number |
| `status` | `ok`, `timeout`, `runner_error`, `provider_error` |
| `tokens` | `{input, cached_input, output, reasoning}` |
| `tool_calls` | count, plus `{by_tool: {...}, errors}` |
| `wall_seconds` | wall time of the trial |
| `final_answer` | the agent's last message |
| `started_at` | RFC 3339 |

- optionally `transcript.json`.

The agent never grades itself; `pptxbench run` calls the grader after each
trial. Grading is
`pptxbench grade --bundle eval/bundles/<task> --trial <trial dir>`, which
reads `output.pptx` and `answer.txt` and writes `grade.json` beside them.
A trial whose status is not `ok` is a harness error and is reported apart
from task failures.

Metrics per condition: task success, wrong-object rate, geometric error
in points, unintended changes, hazards left, tool calls, tokens and wall
time.

### Running trials

```sh
go run ./eval/cmd/pptxbench run --condition json|svg [--render] \
    --tasks 'id-*' --repeats 5 --out results/<name>/<timestamp> \
    [--jobs N] [--model claude-haiku-5-5] [--timeout 15m] [--dry-run]
go run ./eval/cmd/pptxbench report results/<name>/<timestamp>
go run ./eval/cmd/pptxbench compare <run A> <run B>   # paired by task
```

One run is one condition; the four conditions are four runs. `--tasks` is
comma-separated ids or globs. Trial order is shuffled (seed in `run.json`);
a trial whose `trial.json` exists is skipped, so an interrupted run
resumes. Only Identify tasks run until `pptx apply` exists (M3); other
types need `--allow-edit`. `--dry-run` prints the plan and writes the work
directories under `<out>/dry-run` without calling claude.

Each trial runs `claude -p --output-format stream-json` with the task on
stdin and the condition's instructions as an appended system prompt, in the
work directory, in its own process group (killed on timeout). Isolation:
`--safe-mode` (no CLAUDE.md, memory, skills, plugins, hooks, MCP, custom
commands or agents from the user's setup), `--setting-sources ''`,
`--strict-mcp-config` with an empty config, `--disable-slash-commands`,
`--no-session-persistence`, `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`, commit
attribution text switched off through `--settings`. Tools: `--tools Bash`
(plus `Read` with `--render`), `--permission-mode dontAsk` and an allow list
of Bash commands (`pptx`, ls, cat, head, tail, grep, wc, echo, cp, mkdir,
jq, sort), edit rights on the work directory for shell redirection, and
Read of the work directory only. `pptx` on PATH is a wrapper that fixes the
read view and rejects the other view flag and, outside render conditions,
`render`. The login is the user's own Claude Code login; no key is passed.
The one account-level item Claude Code still injects is the user's email
address, in a system reminder.

### Pi runner

```sh
go run ./eval/cmd/pptxbench run --runner pi --model opencode-go/deepseek-v4.1-flash \
    --thinking medium --condition json [--render] --tasks 'id-*' --out results/<name>/<timestamp>
```

`--runner claude` stays the default. With `--runner pi`, `--model` is
`provider/id` and `--thinking` (`off|minimal|low|medium|high|xhigh|max`) is
required; `--pi` names the executable; `--max-budget-usd` is not supported.
Each trial runs `pi --print --mode json --no-session --offline
--no-extensions --no-mcp --no-skills --no-context-files
--no-prompt-templates --no-themes --no-builtin-tools -e eval/pi/pptx-extension.mjs
--tools <names> --model ... --thinking ... --append-system-prompt <instructions>
<prompt>`. The model has no bash, read, edit or write tool. It gets only the
tools of the pptx extension, which run the pptx binary (`execFile`, no shell)
on fixed files in the trial's work directory, so the model never supplies a
path:

| Tool | When | Does |
| --- | --- | --- |
| `pptx_list` | always | `pptx read input.pptx` |
| `pptx_read` | always | `pptx read input.pptx` with `slide`, `ids`, `region`, `detail`, `units`; the view (json or svg) is fixed by the condition |
| `pptx_xml` | always | `pptx xml input.pptx slideN#id` |
| `submit_answer` | Identify | writes `answer.txt` from a list of ids |
| `pptx_apply` | edit tasks | plan as a JSON object, `dry_run`; runs `pptx apply input.pptx plan.json -o output.pptx` (a rejected plan leaves an earlier `output.pptx`; every call starts from `input.pptx`) |
| `pptx_read_output` | edit tasks | `pptx_read` on `output.pptx` |
| `pptx_render` | render conditions | `pptx render` of the input or output deck with overlays; returns the PNG as an image |

The trial configuration (binary, work directory, view, render, task kind)
is passed in `PPTXBENCH_*` environment variables of the pi process, which the
model has no tool to read or change. The descriptions of `pptx_read`,
`pptx_xml`, `pptx_apply` and `pptx_render` carry the CLI's own `--help` text,
read from the binary when the extension loads. The instructions are
`eval/arms/pptxdriver-pi/<condition>.md` and describe the tools. The task
text is `task.md` rendered by `Task.ToolMarkdown` (the answer and edit
contracts name `submit_answer` and `pptx_apply`); the work directory is not
named. The extension enforces the tool set (`pi.setActiveTools`) and reports
it on stderr; the runner records it as `tools_active` and a trial whose set
differs from the expected one is a `runner_error`. The work directory, a
copy of the pptx binary (built with `-trimpath`), a copy of `node` and a
fresh per-trial `PI_CODING_AGENT_DIR` (a symlink to the user's `auth.json`)
live in a temp directory outside the repository; `HOME` is an empty
directory there, `TMPDIR` is inside it, `PI_OFFLINE=1`, and `PATH` is the
system directories and the node directory. The runner records every tool
call and its arguments (`bash_commands` in trial.json) and notes arguments
mentioning the repository, `/Users/`, `~/`, `$HOME`, `.claude`, `.pi`,
`eval/bundles`, `results` or `../` (`access_notes`). A mention of `task.json`
or `rubric` is a leak: `leak: true`, status `runner_error`, excluded from
pass rates. The process group gets SIGTERM, then SIGKILL after 5 s, on
timeout. `node eval/pi/extension_test.mjs <pptx> <deck>` (run by the Go tests)
exercises the extension without a model.

`trial.json` for Pi (`runner` `pptxbench-pi/1`) adds `pi_version`,
`model_requested`, `model_observed` and `model_match` (a mismatch is a
`runner_error`), `thinking`, `stop_reason`, `exit_code`, `leak`, and the
exact argv in `command` (instructions shown as `instructions.md`). `model`
is the observed `provider/id`. `provider_error` covers an assistant message
that ended in `error` or `aborted`, and auth or API failures found in
stderr. `transcript.jsonl` is Pi's raw event stream and `transcript.md` a
readable trace (tool calls, results, assistant text). `report` and
`compare` read Pi runs unchanged.

A run directory holds `run.json` and, per trial, `trial.json` (the
contract's fields plus cost, turns, the exact command line, claude version,
instruction hash, the Bash commands, isolation problems and access notes),
`transcript.jsonl` (stream-json), `stderr.txt`, `answer.txt`,
`output.pptx`, `grade.json` and a copy of `instructions.md`. Tokens:
`input` excludes cache reads (`cached_input`) and cache writes
(`cache_write`). A trial is a harness error, excluded from pass rates and
reported apart, when its status is not `ok`, when the init message shows
anything beyond the allowed tools and no MCP, skills or user plugins, or
when grading fails.
