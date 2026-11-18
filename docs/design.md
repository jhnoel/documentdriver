# PPTX CLI — Revised Design

Nov 12, 2026 · @jamie

> **The agent surface moved (2026-11-13).** What an agent sees (commands,
> plans, results, errors, addresses, HTML input) is now defined by
> `docs/contract.md`, for `pptx` and `docx` alike. This document remains the
> record of the engine and of every pptx design decision, written before
> that move; where its examples show the old surface, read them with this
> table. The engine, the laws and what the operations write did not change:
> the bench plans give byte-identical decks before and after.
>
> | In this document | Now |
> | --- | --- |
> | `{"slide": "slide3", "operations": [...]}` | `{"ops": [...]}`; each op names its slide in `at` |
> | `"target": "s17"`, `"targets": [...]` | `"at": "slide3/s17"`, `"at": [...]` |
> | `"parent": "s40"` on `object.add` | `"at": "slide3/s40"` (or `"at": "slide3"` for the slide) |
> | `"alias": "x"` | `"as": "x"` |
> | `"find"` in `text.replace` | `"select"`, with `occurrence` or `all` |
> | glue `"start"`, `"end"` | `"from_glue"`, `"to_glue"` |
> | `object.reorder` `"to": "before", "ref": "s5"` | `"to": "slide3/s5", "position": "before"` |
> | `flipH`, `flipV`, `fillRect`, `txBox`, `numCol`, … | `flip_h`, `flip_v`, `fill_rect`, `tx_box`, `num_col`, … (`cli.Snake`) |
> | ids `s17`; `slide3#17` for `pptx xml` | addresses `slide3/s17` everywhere |
> | `pptx read --json`, `--svg` | `pptx read --slide slideN [--format svg]`; the slide list is JSON |
> | errors as text on stderr | one errors object on stderr: `code`, `path`, `message` |
> | `pptx apply --help` lists the operations | `pptx help ops`, `pptx help <op>`, generated from the op table |
> | strict XML-like text grammar (`htmlx.ParseStrict`) | HTML5 syntax, strict meaning (`htmlx.ParseHTML` with `Strict`) |
> | `data-lvl` | `data-level` |
> | slide-level `omitted` | `notes`: `{at, reason}` |

## Purpose and premise

The tool lets an agent read and edit the diagrams and graphics in real
PowerPoint decks as native, editable objects, and leaves everything it did
not target untouched.

It is a thin, faithful layer over DrawingML primitives. The agent does the
diagram reasoning: where things go, how connectors run, how big a box is. The
tool reports what the file says, in a form the agent can read and write back
unchanged.

Priorities, in order:

1. Diagrams and graphics: shapes, connectors, groups, freeform paths,
   pictures.
2. Text inside those objects.
3. Slide structure, only as far as diagram tasks need it.

The success test is empirical. On a corpus of real diagram edits, an agent
using this tool must succeed more often, and damage less, than the same agent
using python-pptx or raw XML.

## Core rule: derive facts, never decide

The tool computes what the file already implies. It never chooses a
position, route, size or style.

A derivation is arithmetic or a table lookup that turns stored values into
what they mean. A decision is anything with more than one reasonable answer.

| In the tool (derives) | Left to the agent (decides) |
| --- | --- |
| Slide-space bounds through nested groups | Where a new object goes |
| Connector endpoints from transform and flips | Which sites a connector joins |
| Connection-site coordinates for common presets | How a connector bends |
| Theme and style colours resolved to hex | Which colour to use |
| Inherited placeholder values and their origin | Whether text fits |
| Detection of stale connectors and group frames | Whether and how to fix them |

Excluded outright: layout, routing, obstacle avoidance, text measurement,
auto-sizing, diagram lint and a graph DSL.

When the tool cannot derive a fact faithfully, it omits it and says so. It
never substitutes a default.

## Contract

Four outcomes hold on every save and are tested on every operation:

1. If PowerPoint opened the input without a repair prompt, it opens the
   output without one.
2. Objects the plan did not target are unchanged: same XML, same
   relationships, same media.
3. A plan applies completely or not at all. A failed plan leaves the file
   untouched.
4. A no-op save leaves the original file unchanged.

Byte-level preservation is how outcomes 2 and 4 are met, not a goal in
itself. The tool does not repair, normalise or validate content it did not
touch, and it edits decks that contain unrelated defects.

## Primitive model

The model covers the DrawingML objects diagrams are built from. Everything
else is visible but read-only.

### Objects

| Kind | OOXML | Access |
| --- | --- | --- |
| shape | `p:sp` with `prstGeom` or `custGeom` | Read and write, including text |
| connector | `p:cxnSp` | Read and write, including glue |
| group | `p:grpSp` | Read and write; children in group space |
| picture | `p:pic` | Read, add, move, resize, delete, duplicate; image, crop, fit and locks (see Pictures) |
| other | graphic frames, SmartArt, charts, OLE, ink | Read-only: id, kind, bounds, raw XML |

### Properties

Every property below can be read and written:

- Transform: x, y, width, height, rotation, flipH, flipV, z-order.
- Geometry: preset name with adjust values, or a path.
- Fill: none or solid. Gradient, pattern and picture fills appear as raw XML
  and are preserved.
- Line: width, colour, dash, cap, and head and tail arrow type and size.
- Text: paragraphs (alignment, level, bullet) containing runs (bold, italic,
  underline, size, colour, font), plus body insets, anchor and wrap.
  Since 2026-11-13 also paragraph spacing, margins, indents, tabs and
  bullet font, size, colour and start; run strike, baseline, caps,
  letter spacing, highlight, language and hyperlinks; and the body's
  autofit, direction, columns and flags ("Text writes (M3)").
- Non-visual: name, alternative text (`descr`) and title, hidden, and a
  shape's text-box flag (`txBox`).
- Glue (connectors): start and end shape plus site index.

Colours are `{"scheme": "accent1", "hex": "#4472C4"}` or `{"hex": ...}`.
Modifiers such as `lumMod` are shown and preserved.

Paths are written as an SVG path subset (M L H V C Q Z), translated
one-to-one into `custGeom` commands. Arcs (A) are excluded because DrawingML
`arcTo` has different semantics; agents approximate them with cubic curves.

### Identity

- Address: `<slide part>#<shape id>`, for example `slide3#17`. Part names do
  not shift when slides are added or deleted; slide ordinals are never used
  as addresses.
- Short form: within one slide's view, objects appear as `s17`.
- Duplicate shape ids on one slide get an order suffix (`slide3#17~2`). The
  tool never renumbers existing ids.
- New objects get the next free id on the slide. Plan aliases such as
  `$cache` resolve to them.

### Units

The interface uses points, with decimals allowed. One point is 12,700 EMU,
and font sizes and line widths are already in points. Values are rounded to
the nearest EMU only when written, and only for properties the agent sets.
`--units emu` shows exact values. Relative moves apply to the exact stored
EMU, so repeated nudges never drift.

### Round-trip rule

Any property shown on read can be written back in the same form. Writing an
unchanged value is a no-op and touches no XML.

## Required derivations

Five derivations are unavoidable. Without them the primitives give the agent
wrong answers. Each is pure arithmetic or lookup, and each is inverted on
write where it applies.

### 1. Group transforms

A child's transform is stored in its group's coordinate space, which is
mapped to the slide through `chOff`/`chExt` → `off`/`ext`, plus the group's
rotation and flips, recursively.

- Read: slide-space box and rotation.
- Write: the agent gives slide-space values and the tool inverts the
  mapping. A write that would need a transform DrawingML cannot express,
  such as skew under a non-uniformly scaled rotated group, fails.
- Group frame: never adjusted implicitly. A child moved outside its group's
  frame produces a group-frame hazard, and the explicit `group.fit`
  operation recomputes the frame.

### 2. Connector endpoints

A connector is stored as a box plus rotation and flips. The tool exposes it
as `from: [x1, y1]` and `to: [x2, y2]` in slide space.

- Straight connectors: endpoints alone are enough. On write, the tool keeps
  rotation at 0 and sets the box and flips.
- Elbow connectors (`bentConnector2`–`5`): also expose `first: "h" | "v"`
  (direction of the first segment, which is what rotation encodes) and the
  elbow positions as slide coordinates, translated from the adjust values.
- Curved connectors: endpoints plus raw adjust values.

When the existing encoding can still express the new endpoints, the tool
keeps it. Otherwise it uses rotation 0, or rotation 90° for `first: "v"`.

### 3. Connection sites

A fixed table covers about 15 presets common in diagrams, including rect,
roundRect, ellipse, diamond, triangle, parallelogram, hexagon, can and the
basic flowchart shapes. Site indices follow the order in the specification's
preset definitions; for rect, 0 is top, 1 left, 2 bottom and 3 right.

- Site coordinates come from the shape's slide-space box and rotation.
- `custGeom` sites are shown when their guides are literal values.
- Other shapes show no sites. Glue to them can still be written by index,
  but staleness cannot be detected.

### 4. Colours

- `schemeClr` resolves through the master's colour map, and any layout or
  slide override, to the theme's colour scheme.
- Shape style references (`p:style` with `fillRef`, `lnRef`, `fontRef`),
  common on connectors, resolve through the theme's format scheme.
- Modifiers are applied for the displayed hex, which is approximate. Writes
  keep the scheme reference.

### 5. Inheritance

- Placeholders: position and size come from the matching layout or master
  placeholder, and text defaults from the master's text styles.
- Other shapes: text defaults come from the master's other-text style and
  the presentation's default text style.
- Origin: each inherited value is labelled with where it came from: slide,
  layout, master, theme or style.
- Writes always go to the object itself as local overrides. Moving a
  placeholder that has no transform inserts one.

## Operations and plans

About a dozen operations cover the primitive surface. None of them makes a
decision for the agent.

| Operation | Effect |
| --- | --- |
| `object.add` | Create a shape, connector or picture with the given properties |
| `object.set` | Write any listed property |
| `object.move` | Translate by dx, dy, applied to exact EMU |
| `object.delete` | Remove an object. Connectors glued to it are reported, not changed |
| `object.duplicate` | Copy on the same slide with new ids. Pictures share the media part |
| `object.reorder` | Change z-order: front, back, or before or after an object |
| `group.create` / `group.ungroup` | Group with a frame enclosing the children, or ungroup with transforms baked into the children |
| `group.fit` | Recompute a group's frame from its children |
| `text.set` | Replace a text body with an HTML fragment (p, b, i, u, span style, ul/ol/li for levels) |
| `text.replace` | Replace a substring within one object, keeping the formatting of the run where it starts |
| `slide.add` / `slide.delete` / `slide.duplicate` | Minimal slide structure |
| `xml.set` | Replace one object's raw XML. Explicitly unsafe, but still atomic and checked for well-formedness |

### Plans

A plan is an ordered list of operations, written in JSON in every
condition. Each operation sees the results of the ones before it, and
targets resolve against the current working state, not a snapshot. Aliases
name objects created earlier in the plan. `--dry-run` returns the edit
result without saving.

```json
{"slide": "slide3", "operations": [
  {"op": "object.add", "alias": "cache", "kind": "shape", "geom": "rect",
   "box": [300, 200, 90, 60], "fill": {"scheme": "accent2"}, "html": "Cache"},
  {"op": "object.add", "kind": "connector", "geom": "straightConnector1",
   "start": "s12:3", "end": "$cache:1", "from": [252, 147], "to": [300, 230],
   "line": {"w": 1, "tail": "triangle"}}
]}
```

The agent supplies both the glue and the endpoints. The tool checks that
they agree, reports any mismatch as a hazard, and never moves either one.

### Edit result

Every plan returns:

- Changed objects, re-projected in the format the agent reads, with the ids
  assigned to aliases.
- Hazards: places where the file now contradicts itself. There are only
  three kinds:
  - `stale-connector`: a glued endpoint is no longer on its site.
  - `dangling-glue`: a connector is glued to an object that no longer
    exists.
  - `group-frame`: a child lies outside its group's frame.
- Parts touched, for audit.

Hazards are reported and never fixed automatically. Overlaps, off-slide
objects and text overflow are not hazards, because they are not
contradictions; the agent sees them in the render.

## Projections

The JSON and SVG views are two serialisations of one in-memory model. They
use the same ids and values, and neither parses OOXML itself.

### JSON

```json
{"slide": "slide3", "units": "pt", "size": [960, 540],
 "colors": {"accent1": "#4472C4", "accent2": "#ED7D31"},
 "objects": [
  {"id": "s12", "kind": "shape", "geom": "rect", "box": [72, 120, 180, 54],
   "fill": {"scheme": "accent1"}, "html": "<b>API</b> Gateway"},
  {"id": "s13", "kind": "connector", "geom": "straightConnector1",
   "from": [252, 147], "to": [360, 147], "start": "s12:3", "end": "s14:1",
   "line": {"w": 1, "tail": "triangle"}}
]}
```

### SVG

```svg
<svg data-slide="slide3" data-units="pt" viewBox="0 0 960 540">
  <style>:root { --accent1: #4472C4; --accent2: #ED7D31 }</style>
  <g id="s12" data-geom="rect">
    <rect x="72" y="120" width="180" height="54" fill="var(--accent1)"/>
    <foreignObject x="72" y="120" width="180" height="54"><b>API</b> Gateway</foreignObject>
  </g>
  <line id="s13" data-geom="straightConnector1" x1="252" y1="147" x2="360" y2="147"
        stroke-width="1" marker-end="url(#triangle)" data-start="s12:3" data-end="s14:1"/>
</svg>
```

Scheme colours become CSS variables, so `{"scheme": "accent1"}` and
`var(--accent1)` carry the same fact. Preset shapes other than rectangles
and ellipses are drawn approximately; `data-geom` is authoritative.

### Detail and selection

- Default: id, kind, geometry name, box, rotation, fill, line, text and
  glue.
- `--detail sites`: adds connection-site coordinates.
- `--detail full`: adds every modelled property with its origin.
- `--ids` and `--region`: limit output to some objects.
- `pptx xml`: prints an object's exact source.

Unsupported properties are omitted, never defaulted, and every omission is
visible in `--detail full`.

An equivalence test parses both views back into a normalised property set
and requires them to match.

## Engine

Edits change a lossless XML tree of the original parts. The tree preserves
every original byte it does not need to change, and the read model is never
serialised back.

### Package

- Use Go's `archive/zip`. Read entries lazily.
- Copy untouched members with `zip.Writer.Copy`, which keeps their
  compressed bytes. Recompress only modified members.
- Save to a temporary file, then rename it over the original atomically.
- A no-op save writes nothing.

### Lossless XML tree

- Parsing: a custom tokenizer keeps each node's original byte span,
  attribute order, namespace prefixes, quote style and whitespace. Go's
  `encoding/xml` loses prefixes and spans, so it is not used. The package is
  named `ooxml/tree`, not `xml`, to avoid shadowing the standard library.
- Writing: untouched nodes are emitted by copying their original bytes. A
  modified node re-emits only what changed, and its unchanged attributes
  keep their order and quoting.
- Scope: only parts a plan touches, plus parts needed for derivations
  (theme, layout, master, relationships), are parsed.
- Namespaces: new elements reuse prefixes already declared in the part. A
  missing declaration is added on the new element, never on the root.

The tree replaces byte-offset patching. Operations see each other's
effects, aliases resolve naturally, and no offsets go stale mid-plan.

### Relationships and content types

These are edited through the same tree. New relationship ids take the next
free `rIdN`. Media parts are shared, never copied, when a picture is
duplicated.

### Transactions

- A plan runs against an in-memory working copy.
- Before writing, each modified part is re-tokenised as a well-formedness
  check. This is local and cheap, and it is the only validation the tool
  performs.
- Any failure discards the working copy, and nothing is written.

## Hard cases

Each case gets one decision so that implementation does not have to
improvise.

| Case | Decision |
| --- | --- |
| `mc:AlternateContent` | Project the branch PowerPoint would read (the Choice whose Requires it supports). An edit applies the same change to the object in every branch; if it cannot, the operation fails. |
| Duplicate shape ids | Address them with an order suffix. Never renumber them. |
| Deleting a shape with glued connectors | Leave the connectors untouched and report dangling-glue. The agent deletes or re-glues them. |
| Moving a glued shape | Leave connectors untouched and report stale-connector. |
| Rotated or flipped groups | Derive fully. Fail any write whose result DrawingML cannot express. |
| Placeholder without a transform | Show the inherited box with its origin. A move inserts a transform. |
| Duplicating an object | New shape ids. Generate a new `a16:creationId`. Copy other extensions as they are. Pictures share media. |
| `slide.delete` | Remove the slide from the slide list, presentation relationships, sections and custom shows. Delete the slide part, its relationships and its notes slide. Delete media only when nothing else references it. |
| SmartArt, charts, OLE | Read-only. For identification, SmartArt's stored drawing appears as read-only children. |
| Gradients, effects, 3D | Preserved, shown as raw XML, editable only through `xml.set`. |

## Feedback loop

The agent checks its work through two channels: the edit result returned by
every plan, and a render it can request at any time.

```sh
pptx render deck.pptx --slide slide3 --overlay ids,sites -o slide3.png
```

- Renderer: headless LibreOffice, converting to PDF and then PNG.
- Overlays: drawn from the model in the same coordinate system as the
  render.
  - `ids`: object ids, or a numbered key for dense slides.
  - `sites`: connection-site dots with their indices, so the agent can pick
    glue visually.
  - `glue`: each connector's glued ends highlighted.
- Limits: LibreOffice differs from PowerPoint on some presets, text layout
  and effects. The render is for checking, not proof. Benchmark grading uses
  PowerPoint renders where they are available.

## Evaluation

The evaluation harness is built first, and every milestone is judged by
measured task success against baselines.

### Corpus

About 30 tasks over about 10 real decks covering architecture diagrams,
flowcharts, org charts, timelines, process graphics and icon grids.

| Task type | Example | Graded by |
| --- | --- | --- |
| Identify | "Which shape is the database?" | Answer against labelled ids |
| Restyle | "Make every service box accent 2" | Property diff in the model |
| Move and align | "Align the three boxes on their tops" | Geometry within tolerance |
| Connect | "Arrow from cache to DB, right side to left side" | Glue and endpoints in the model |
| Insert | "Add a queue between API and worker, keep both connectors glued" | Graph check in the model, no hazards left |
| Author | "Draw a five-step chevron process" | Rubric on the render, plus model checks |
| Freeform | "Add a custom callout arrow" | Rubric on the render |

Every task also checks that untargeted objects are unchanged (model and XML
diff) and that PowerPoint opens the file without repair.

### Conditions

- Baselines: the same model using python-pptx, and the same model editing
  raw XML after unzipping.
- A/B: JSON view against SVG view. Plans are JSON and text is HTML in both
  arms, so only the read format varies.
- Render access: each arm runs with and without the render tool, to measure
  how much the structured view carries on its own.

### Metrics

Task success, wrong-object rate, geometric error in points, unintended
changes, hazards left unresolved, tool calls, tokens and wall time. Each
task runs at least five trials per arm, in randomised order.

## Milestones

Each milestone is a vertical slice that ends with a measured result on the
corpus, not a finished layer.

1. **M0 — Harness and corpus.** Tasks, graders and both baselines running.
   - Exit: baseline success rates recorded for every task type.
2. **M1 — Lossless engine.** Package, XML tree, transactions and
   preservation tests.
   - Exit: setting one attribute changes nothing else; a failed plan writes
     nothing; PowerPoint opens the result without repair.
3. **M2 — Faithful read.** Primitives, groups, connectors, sites, colours
   and inheritance; JSON and SVG views; render with overlays.
   - Exit: projection equivalence tests pass, and a read-only A/B on
     Identify tasks is complete.
4. **M3 — Primitive writes.** Add, set, move, delete, duplicate, reorder,
   connectors with glue, hazards and text.
   - Exit: Restyle, Move and align, Connect and Insert tasks beat both
     baselines. Run the full A/B here.
5. **M4 — Groups and paths.** Group create, ungroup and fit, plus the SVG
   path subset.
   - Exit: Author and Freeform tasks beat both baselines.
6. **M5 — Slide structure.** Add, delete and duplicate slides.
   - Exit: a multi-slide diagram task succeeds without hazards or repair
     prompts.

## Non-goals

- Layout, routing, obstacle avoidance, auto-sizing, text measurement,
  diagram lint, graph DSL.
- Editing SmartArt, charts, OLE objects, animations or transitions.
- Structured editing of gradients, effects and 3D (preserved; raw XML only).
- Serialising the read model back into OOXML.
- Rendering that matches PowerPoint.
- Validating or repairing content no operation touched.

## Open questions

1. Does PowerPoint open files with dangling glue or a stale group frame
   without a repair prompt? Test this in M1; the hazard policy depends on
   it.
   **Answered 2026-11-12 (PowerPoint for Mac 16.115, `tools/pptoracle
   smoke`):** yes. Decks with a dangling `a:stCxn` or `a:endCxn`, duplicate
   shape ids, or a child well outside its group's `chOff`/`chExt` all
   opened without a prompt, against clean sources; a positive control
   (`a:off x="notanumber"`) was caught. Hazards stay report-only. The same
   run confirmed the M1 exit check: 26 of 26 single-attribute edits of
   clean corpus decks opened cleanly.
2. Are points with decimals easier for agents than EMU or pixels? Measure it
   in the M2 read-only A/B.
3. Which presets belong in the connection-site table? Derive the list from
   the corpus, and check each site order against the specification's preset
   definitions.
4. Does PowerPoint recompute `normAutofit` text scaling on open? The answer
   decides whether overflow seen in a LibreOffice render matches what users
   will see.
   **Partly answered 2026-11-12:** on open and PDF export PowerPoint 16.115
   applies the stored `fontScale` as written (an overflowing box at 100%
   spills off the slide; a stale 25% stays tiny). LibreOffice ignores the
   stored value and refits. So LibreOffice renders can show text fitting
   where PowerPoint shows overflow, and the reverse; judge text fit on
   PowerPoint renders (`tools/pptoracle render`). Whether PowerPoint
   recomputes the scale on edit or interactive save is untested: scripted
   .pptx saves from PowerPoint write empty files, so `pptoracle resave`
   fails by design for now.

5. Does PowerPoint open a picture added without `a:prstGeom` or a fill
   mode (`connectors--add-bare` in the picture smoke) as it shows its
   own? (Whether `object.delete` should collect a picture's media was
   settled 2026-11-13: it does, as an image replace does; see Pictures.)

## Engine decisions (M1)

Settled during M1 review, 2026-11-12:

- Untouched zip members are copied with `zip.Writer.Copy`: compressed
  bytes and central-directory headers are identical, but archive/zip
  rebuilds local headers, so PowerPoint's local-only padding extra field
  (0xA220) and redundant local CRC/size values are not kept. This meets
  contract outcomes 2 and 4. A raw local-header copier is deferred unless
  a PowerPoint repair prompt is traced to it.
- New relationship ids are max(rIdN)+1; ids are never reused.
- Modified members are recompressed with flate at the default level.
- New zip members take the header of `[Content_Types].xml`.
- `Save` refuses when the file changed on disk since it was opened.
- The debug selector resolves prefixes through the root's declarations;
  the operations in M3 address objects by id, not by selector.

## Read decisions (M2)

Settled while building the read model, 2026-11-12. None of these was
checked against PowerPoint, which was not available; each says what it
rests on.

- Preset data: guides, connection sites and connector paths come from an
  extract of ECMA-376 `presetShapeDefinitions.xml` (LibreOffice core at the
  commit pinned by `eval/corpus/fetch.sh`), generated by
  `internal/pptx/derive/gen_presets.go`. Guides are evaluated with the full
  formula language, so adjust values move sites. The site table holds 21
  presets: rect, roundRect, ellipse, diamond, triangle, rtTriangle,
  parallelogram, trapezoid, hexagon, octagon, can and ten flowchart
  shapes. `custGeom` sites are evaluated whenever their guides evaluate.
- Units: points rounded to 0.01 pt (127 EMU); `--units emu` shows EMU,
  rounded to 0.01 only where a derivation (group scaling) is fractional.
- Custom paths are given relative to the slide-space box's top-left,
  before rotation and flips, in the view's units: the stored path scaled
  by any enclosing groups. `arcTo` omits the path.
- Group transforms: slide-space flips are the parity of the flips on each
  axis; the rotation follows. A child whose image is not a rectangle is a
  skew. PowerPoint stores group extents in whole EMU, so nested rotated
  groups carry tiny non-uniform scales (4e-6 to 0.045 degrees of skew in
  the corpus); a skew moving a corner by at most 0.5 pt is accepted and
  reported as `box.skew` in `--detail full`. Larger skews (2.7 and 8.1
  degrees in the corpus) omit the box. The inverse (`derive.Unplace`)
  uses the same tolerance and fails otherwise.
- Hazard tolerances: a glued end more than 0.5 pt from its site is
  stale. In the corpus 140 of 142 checkable glued ends are within 0.5 pt
  (138 within 0.1 pt); the other two are 1.25 pt off, exactly half the
  target's outline width, in a deck converted from the binary format.
  A group-frame hazard needs a child more than 0.5 pt outside its
  group's `chOff`/`chExt`. The child is measured by its stored box, with
  width and height exchanged when its rotation is within 45 degrees of 90
  or 270: groups saved by PowerPoint in the corpus fit that rule (children
  at 270 degrees fill the frame by exchanged extents; children at 7
  degrees fit by their unrotated boxes), and no other rule fitted them.
- Placeholder matching follows LibreOffice's importer: same idx and first
  type, then first type, then same idx and second type, then second type,
  then same idx (first/second types ctrTitle/title, subTitle/body,
  obj/body). A `p:ph` without a type is `obj`, or takes the type of the
  same-idx placeholder on the layout.
- Text defaults: placeholders inherit through their own `lstStyle`, the
  style `fontRef`, the layout and master placeholders' `lstStyle`, then
  the master `titleStyle` (title, ctrTitle), `bodyStyle` (body, subTitle,
  obj and content types) or `otherStyle` followed by the presentation
  `defaultTextStyle` (dt, ftr, sldNum, hdr). Other shapes use their own
  `lstStyle`, `fontRef`, master `otherStyle`, then `defaultTextStyle`, as
  this document says; LibreOffice instead uses `defaultTextStyle` for
  shapes with text, which is an open question.
- Body properties (insets, anchor, wrap) not stated anywhere take the
  values ECMA-376 states are implied when omitted, labelled with origin
  `spec`. Lines and fills found nowhere are omitted, not defaulted.
- Origins are `slide`, `layout`, `master`, `theme`, `style`,
  `presentation`, `group` (a:grpFill) and `spec`.
- Colours: modifiers are applied with LibreOffice's method (luminance and
  saturation in HSL, tint and shade in linear RGB with gamma 2.3); the
  hex is approximate. `prstClr` values come from LibreOffice's table.
  `phClr` in a theme style shows as the reference colour with the
  style's modifiers appended.
- `mc:AlternateContent`: the first Choice whose Requires prefixes all
  resolve to these namespaces is projected, else the Fallback; the set is
  taken as what PowerPoint 2016 reads: a14, p14, a15, p15, a16
  (`http://schemas.microsoft.com/office/drawing/2010/main`,
  `.../powerpoint/2010/main`, `.../drawing/2012/main`,
  `.../powerpoint/2012/main`, `.../drawing/2014/main`), c14
  (`.../drawing/2007/8/2/chart`), cx and cx1 (chartex 2014 and
  2015/9/8) and VML (`urn:schemas-microsoft-com:vml`). Later namespaces
  (p159, am3d, cx2 and others) get the Fallback. `pptx xml` prints the
  whole AlternateContent element of an object inside one.
- HTML text carries only the object's own paragraph and run formatting
  (`<b>`, `<i>`, `<u>`, `<span style>`, `<br/>`, `<ul>`/`<ol>`/`<li>` for
  paragraphs that set their own bullet, `data-lvl` for levels above 1).
  Inherited formatting appears as `text.lvlN.*` properties with origins
  in `--detail full`. Scheme colours and theme fonts appear as CSS
  variables (`var(--accent1)`, `var(--mj-lt)`), so both views share them.
- SVG: facts that the drawing does not carry exactly ride on `data-*`
  attributes (`data-box` where the element is not a rect, `data-rot` on
  groups, `data-fill`/`data-stroke` for colours with modifiers, `data-dash`,
  `data-cap`, `data-head`, `data-tail`, and JSON-valued `data-origins`,
  `data-props`, `data-omitted`, `data-hazards`). Rotation is a
  `rotate()` about the box centre and flips a reflection `matrix()` on
  the geometry element; group elements carry no transform because their
  children are already in slide space.
- SmartArt's stored drawing is not yet projected as children; the frame
  records the omission.

## Write foundations (pre-M3)

Settled while hardening the engine before M3, 2026-11-12, so that the
contract holds by construction rather than by care. None of these was
checked against PowerPoint; each says what it rests on.

### Commit path

- `ops.Tx` owns the working copy and never hands out the package (since
  P2 it wraps a `plan.Tx`; see "Plan transactions (P2)").
  Operations (`ops.Op`, implementable only inside `internal/pptx/ops`) are the
  only way to change it; `ops.Apply(path, ops...)` and `ApplyTo(src, dst,
  ops...)` replace the whole-package callback. A failed operation poisons
  the transaction: `Commit` then fails with `ErrTxFailed`.
- `Tx.Commit` uses `opc.Package.SaveAndClose`, which never reopens: after
  the rename nothing that can fail runs, so a nil error means the file is
  saved and any error means it is untouched. `Package.Save` still reopens
  for its own callers; a reopen failure wraps `ErrReopen` ("saved, but").
- Save stages (create, write, chmod, sync, close, rename, dirsync) pass
  through a fault hook (`opc.SetFaultHook`, tests only). Failures before
  the rename leave the file and directory as they were; the directory sync
  after the rename is best effort and cannot fail a save.
- `pptx debug set-attr` runs as `ops.DebugSetAttr` through `Apply`/`ApplyTo`.
  It is explicitly unsafe like `xml.set`, but declares the nearest
  enclosing unit (object, container or slide root) of each matched
  element, so the verifier holds it to what it matched.

### Architecture test

`internal/archtest` type-checks every package of the module (export data
from `go list -export`) and runs in `make check`. It fails on:

- `encoding/xml` imported outside `eval/` (the survey tool there reads
  docProps only).
- `opc` `Save`, `SaveAs` and `SaveAndClose` called outside `internal/opc`
  and `internal/plan` (P2; before, `internal/pptx/ops`); `AddPart`,
  `DeletePart` and `Part.SetBytes` outside `internal/opc` and
  `internal/pptx/ops`;
  `opc.SetFaultHook` outside tests.
- `*tree.Node` mutators (`SetAttr`, `RemoveAttr`, `Insert*`, `Remove*`,
  `Replace*`, `SetText`, `AppendChild`, ...) called outside
  `internal/ooxml/tree`, `internal/opc` and `internal/pptx/ops/edit` (stricter
  than "ops": operations themselves mutate only through Handles); inside
  `edit`, element insertion only in `schema.go` (`Handle.Ensure`). Since
  P4 the edit core `internal/ooxml/edit` replaces `internal/pptx/ops/edit`
  here, which mutates only through it, and insertion is confined to the
  core's `place.go` and `unit.go` (see "Edit core and law harness (P4)").
- any dependency of `internal/pptx/ops/...` on `internal/pptx/model`.
- an import of `internal/pptx/...` from anywhere but inside `internal/pptx`,
  `cmd/pptx`, and the dev-only `eval/` and `tools/`; likewise for
  `internal/docx/...` (once it exists) with `cmd/docx`. The shared engine
  never depends on a format, and one format never depends on the other.
- any dependency, direct or through other packages, of the shared engine
  (`internal/opc`, `internal/ooxml/...`, `internal/plan`, `internal/ids`)
  on `internal/pptx` or `internal/docx` (P2).

Test exceptions, each for a stated reason: tree, opc and edit tests test
their own mutation; plan and ops tests make undeclared changes to prove
the verifier rejects them, and ops tests inject save faults; `internal/pptx/render/render_test.go` builds a
two-slide fixture with `SaveAs` and tree edits. `tools/` and `cmd/` get no
exception: the debug command goes through ops.

### Types and absence

- Stored values have their own types in `derive`: `EMU` (int64), `Angle`
  (int64 60000ths of a degree) and `Points` for display; `StoredXfrm` and
  `StoredGroupXfrm` are transforms exactly as stored. Derived values stay
  float64 EMU. `derive.FormatNum` is the one number format of both views
  and of the property codecs.
- `derive.Val[T]` is a value with its origin, or absent with a reason; the
  zero `Val` is absent. Readers (`AttrInt`, `AttrEMU`, `AttrAngle`,
  `AttrBool`, `ReadXfrm`, `ReadGroupXfrm`, `ReadTransform`) return errors
  instead of zero, and the model records an omission. The model's
  `attrF`, `attrOK` and lenient `boolAttr` and derive's `attrInt` are gone.
- Schema defaults are not decisions: `rot`, `flipH`, `flipV` and `hidden`
  take the default ECMA-376 states (0, false) when they are absent, and
  only then. A written but unreadable value (`rot="1.5"`, `flipH="yes"`)
  omits the box. A missing or non-integer `a:off`, `a:ext`, `a:chOff` or
  `a:chExt` attribute omits the box (and, for a group, its children's
  boxes); coordinates must be whole EMU, and universal measures ("2in")
  are reported as unreadable rather than converted. An `a:xfrm` that is
  present but incomplete is reported, not skipped in favour of the
  layout's. Non-integer line widths and body insets, invalid `hslClr` and
  `scrgbClr` components (no longer read as 0) and an unreadable
  `p:sldSz` (the views then drop `size` and `viewBox`) are omitted with
  reasons. No corpus deck changed its omissions.
- Addresses: `edit.Ref{part, id, ord}` has unexported fields and is built
  only by `Workspace.Resolve`/`ResolveIn`, after finding the object; the
  ordinal counts the projected `mc:AlternateContent` branch as the read
  model does. `edit.Alias` is a separate type, bound once per plan.
- `Workspace.NextID` takes one more than the largest id of any `cNvPr`
  in the part, every `mc` branch and group included, and of any
  `stCxn`/`endCxn` reference, so a dangling connector never becomes glued
  to a new object (the diagram fixture's next id is 1000, not 72). Ids are
  never reused within a plan.
- Plans decode with `DisallowUnknownFields` at every level, unknown
  operations and trailing data are errors, and optional fields are
  `ops.Opt[T]`, which tells absent from zero and rejects `null`. Spec types
  exist for every operation in the design; before M3 only the transform
  fields of `object.set` and `object.move` compile, the rest fail with
  `ErrNotImplemented` before the deck is opened.

### Handles and the commit-time verifier

- `edit.Workspace.Target(ref)` returns a `Handle` over every branch
  element of one address: outside `mc:AlternateContent` the element itself;
  inside one, the element in each Choice and the Fallback with the same id
  and ordinal within its branch. A branch without a counterpart fails the
  target (`ErrBranch`); nested `mc:AlternateContent` is refused for writes.
  Handle mutators (`SetAttr`, `RemoveAttr`, `ApplyEach`, `Ensure`) resolve
  their paths in every branch first and change nothing unless all have
  them; paths cannot step into another object.
- `Target` declares the object (every branch) as a target;
  `SlideContainer` and `Container` declare containers for add, delete and
  reorder; `DeclarePart` declares a whole part (relationships, content
  types, media).
- `Workspace.Verify`, run by `Tx.Commit`, compares the working copy with
  the part as stored in the file, so it needs no snapshot and sees changes
  made by any means. A changed part must be declared. In a changed part
  with a shape tree, units (the slide root, the shape tree, each object,
  each object-level `mc:AlternateContent` and its branches) are compared by
  their own bytes, nested units replaced by markers naming them by their
  original byte offset (`tree.Node.SpanIn`): an undeclared unit keeps its
  start tag, content and child list; a target may change only its own
  content, a container only its child list; a removed unit must be a
  target. Bytes are compared, so a value set and set back is not a change
  (the tree's `Modified` stays set after A→B→A; `StartTagModified`
  compares bytes).
- Glue to an id several objects share is ambiguous: the read model
  resolves it to none of them (`Glue.Candidates`, an omission, no stale or
  dangling hazard) and `Workspace.ResolveGlue` fails with `ErrAmbiguous`
  naming every candidate. It never picks the first.

### Property codecs

- `internal/pptx/ops/prop` holds `Codec[T]{Name, Read, Write, Show}` (since
  P4 an alias of the edit core's `Codec`) and
  `Set`: when the current value shows as the wanted value, `Set` returns
  without touching XML (the round-trip rule decided in the displayed form:
  a box written back from the view differs from the stored EMU by up to 63
  EMU and is still a no-op); otherwise it writes, re-reads every branch and
  fails with `ErrInexpressible` unless each shows the wanted value.
- The transform codec is the first. Its read half is
  `derive.ReadTransform` over `derive.PlaceholderChain`, the same calls the
  read model makes; a law test checks, object by object, that its `Show`
  equals what the JSON view shows. `Show` uses the views' units (0.01 pt,
  or EMU) and shows a whole turn as 0.
- Writes go through `derive.Unplace` with each branch's own parent map,
  then round to whole EMU (halves away from zero) and to whole 60000ths,
  normalised to [0, 360°). Only attributes whose value changes are
  written; `flipH="true"` stays when true is wanted, and absent `rot` or
  flips stay absent for their defaults. New flips are written `1`.
- `object.move` maps the slide-space delta into each branch's child space
  and rounds it there, adding it to the stored offset: a move and its
  opposite cancel exactly, repeated nudges never drift, and a group moves
  with its children. The move fails unless every branch then shows the
  box moved by exactly the delta.
- A placeholder without a transform gets one (schema-ordered, through
  `Ensure`) on its first set or move, holding the moved inherited values;
  setting it to the inherited box is a no-op. A later move back keeps the
  inserted transform (it now stores the inherited values; the box origin
  becomes the slide), so that `[d, -d]` equals `[d]` then `[-d]`; for such
  objects move-and-back is identical in what is shown, not in bytes. A
  group without a transform is not given one: its child space is unknown.
- `derive.Unplace` now validates its result through `Place`. `FuzzUnplace`
  found that it measured skew in the group's child space, so under a
  strongly non-uniformly scaled rotated group it accepted a transform
  skewed 13° in slide space.
- `derive.ConnectorXfrm` inverts the connector endpoints: every tabulated
  connector path runs from the local top-left to the bottom-right (a test
  checks every preset), so box, rotation and flips place the endpoints. It
  keeps the stored rotation and flips when they still express the
  endpoints and the first segment, else uses rotation 0, or 90° for
  `first: "v"`, choosing between 0 and 90 in slide space under rotated
  groups. There is no connector codec yet.

### Schema-ordered insertion

- `internal/ooxml/schema/gen_schema.go` reads `pml.xsd`, `dml-main.xsd`,
  `wml.xsd` and `shared-commonSimpleTypes.xsd` from
  `OfficeOpenXML-XMLSchema-Transitional.zip` in Apache POI at commit
  1243387d (the commit pinned by `eval/corpus/fetch.sh`; SHA-256
  d3418752...04fc, `schema.ZipSHA256`, checked by the generator) and
  writes one table per vocabulary (see "Schema tables per vocabulary
  (P2)" below). The pml and dml tables cover the complex types M3 inserts into:
  `p:sp`, `p:cxnSp`, `p:pic`, `p:grpSp`, their non-visual properties,
  `cNvSpPr`, `cNvCxnSpPr` (stCxn, endCxn), `spPr`, `grpSpPr`, shape and
  group `xfrm`, `prstGeom`, `custGeom`, `ln`, `solidFill`, `txBody`,
  `bodyPr`, `a:p`, `pPr`, `r`, `rPr` and `endParaRPr`. Each is a list of
  slots in schema order; a choice (fills, geometries, dashes, joins,
  autofit, bullets, colours) is one slot; repeating slots are marked.
- `Handle.Ensure(parent, child)` is the only insertion path. It keeps an
  existing child; replaces a sibling from the same choice slot with a new,
  empty child; otherwise inserts before the first existing child the
  schema orders after it, else after the last it orders before it. It
  fails for parents whose type is not tabulated, children the schema does
  not allow there, repeating slots (runs, paragraphs, group children:
  M3 adds a placed insert for those), and parents whose only elements are
  unknown to the table. New elements reuse the namespace URI (transitional
  or strict) and prefix in scope.
- The same generator also tabulates attributes and simple types, from
  which plans are validated ("Schema values" under M3).
- The edit layer reads the tables through `internal/ooxml/schema`
  (`Slots`, `Find`, `ChildType`); it keeps no copy. An object element's
  complex type is the type the schema gives it as a child of
  `p:CT_GroupShape`.

### Laws

(Since P4 these laws run through the format-neutral harness
`internal/laws`; see "Edit core and law harness (P4)".)

Tests run over the hand fixtures in `make check` and, with
`PPTX_CORPUS` set, over every PowerPoint-native deck in
`$PPTX_CORPUS/manifest.jsonl` (`internal/pptx/laws`, `internal/pptx/ops`):

- Identity write: every shown transform written back as the view shows it,
  all in one transaction per deck, leaves the file's bytes and mtime
  unchanged; the codec shows exactly what the view shows.
- Inverse: a random nearby transform (moved, resized, rotated, flipped)
  set on every object reads back as set, under rotated and flipped
  groups, or fails with `ErrInexpressible`; the verifier accepts the
  result. `FuzzUnplace` checks the group-transform inverse.
- Untargeted bytes: random single-object plans leave every other object's
  bytes and every other part identical, and the model reads back the set
  value; a deliberately sneaky operation that touches an undeclared object
  is rejected and writes nothing.
- Atomicity: a failure at operation k of n, and at each save stage before
  the rename (first and second occurrence), leaves the file identical with
  no temporary file; a failure after the rename is not reported.
- Sequencing: move by d then -d in one plan writes nothing (shown-identical
  for inheriting placeholders, above); `[a, b]` equals `[a]` then `[b]`
  part for part.

Corpus run, 2026-11-12: 1024 native decks read (2 unreadable, 7 listed but
missing on disk), 2189 slides, 9820 objects (25 without a box, none
unwritable). Identity: 1024 one-transaction plans, all byte- and
mtime-identical, codec and view agreeing on every object. Inverse: 7820
random sets read back as set; 1975 failed with `ErrInexpressible`, 445 as
skew (rotated boxes under non-uniformly scaled groups) and 1530 because
rounding to whole EMU in a group's child space moved what is shown by
0.01 to 0.2 pt (child spaces coarser than slide EMU); none was written
wrongly. Untargeted: 1901 single-object plans (16 more inexpressible),
no untargeted byte changed. Sequencing: 2169 plans, 409 of them
move-and-back of inheriting placeholders. The run found one class of
bug, fixed: ink `contentPart`s whose `mc:Fallback` picture stores no
transform (POI's stress013), now given one per branch.

## Write operations (M3)

Settled while building the primitive writes, 2026-11-12. None of these was
checked against PowerPoint; the task outputs are ready for `pptoracle
smoke`. Text (`html`, `text.set`, `text.replace`) is in "Text writes
(M3)" below; group operations and paths in M4; slides in M5.

### Plans and the edit result

- `pptx apply deck.pptx plan.json|- [--dry-run] [-o out.pptx]`. A plan
  names one slide; operations run in order against the working state and
  every target (`s17`, `s17~2`, `$alias`) resolves when its operation
  runs. A plan for several slides is several plans; no task needed more.
- Fields take the form `pptx read --json` shows them in, so any shown
  value can be pasted back: `fill` is `"none"`, a colour or `{"type":
  "gradFill"}`; `line` is `"none"` or `{w, color, dash, cap, head, tail}`
  with arrows as `"triangle w=sm len=lg"`; `geom` and `adj` as shown;
  connectors take `from`, `to`, `first`, `start` and `end`; glue is
  `"s12:3"`, `"$alias:1"`, `"#17:3"` (a raw id, as shown for dangling
  glue) or `"none"`. The pre-M3 spec shapes (`{"none": true}`,
  `{"color": {...}}`) are gone.
- Text takes the `html` the view shows: `text.set {target, html}`,
  `text.replace {target, find, with, all}` (`all` optional; `find` must
  not be empty; `with: ""` deletes), and `html` on `object.add` (shapes
  only, written after every other field) and `object.set` (after the
  other fields). `html` and `with` are required where they appear; `""`
  is a value (one empty paragraph, or a deletion), not an absence.
- `internal/pptx/apply` runs the plan in an `ops.Tx`, takes a snapshot
  (`Tx.Snapshot`, now through `plan.Tx.Finish`: the commit checks, then `SaveAs` to a temporary file,
  without finishing the transaction) and reads input and snapshot through
  the read model. It is outside `internal/pptx/ops`, which never sees the
  model. The result, printed as JSON:
  - `aliases`: alias to short id;
  - `changed`: each new or changed object exactly as the JSON view shows
    it. An object is changed when its own bytes differ (a group without
    its children's bytes) or when its paint order moved relative to the
    others (outside a longest increasing run);
  - `deleted`: input ids that are gone;
  - `hazards`: every hazard the read model finds on the slide after the
    plan, `new` when the input had none of that kind on that object.
    Operations never compute or fix hazards; glue and endpoints the agent
    gives are written as given, and a mismatch is a `stale-connector`;
  - `parts`: the parts the plan changed.
  `--dry-run` prints the same result and writes nothing; any failure
  prints the error and writes nothing.

### Codecs

Each codec reads with the same derive call the read model makes and
shows through `internal/pptx/show`, which the JSON view also uses, so the
value shown and the value a write compares cannot diverge. To make that
true, the model's fill, line and geometry reading moved into `derive`
(`ColorCtx` with `SlideColorCtx`, `ReadFill`, `ReadLine`, `ReadGeom`) and
the view's colour, fill, line, arrow and adjust formatting into
`internal/pptx/show`; `--detail full` JSON and SVG stayed byte-identical on all
2260 slides of 1031 native corpus decks.

- Geometry: a preset name and its adjust values, written to the object's
  own `a:prstGeom` (inserted through `Ensure` for a placeholder that
  inherits its geometry). `adj` updates values by name and keeps the
  rest; a new preset starts from the values given (the old preset's mean
  nothing to it); new guides are appended in name order. `avLst` is
  written only when there are values. `custGeom` is read-only until M4;
  `"geom": "custom"` written back is a no-op.
- Fill (shapes and groups): `none` or solid, written locally; `p:style`
  stays. Scheme colours are written as references with their modifiers
  in the order given; an `rgb`/`hex` colour as `srgbClr` in upper case.
  The six colour types (`scheme`, `rgb`, `sys`, `preset`, `hsl`, `scrgb`)
  are written alike, each with modifiers (fills and line colours share
  one writer); an element of the kind given is kept, so the file's
  reference type stays unless the plan names another. A system colour's
  `lastClr` (the displayed value PowerPoint caches) is kept when the plan
  names the same system colour, else is the `hex` given without
  modifiers (as `text.set` writes it), else is removed.
  A `hex` next to a reference is the derived value and must match the
  slide's resolution, else the write fails naming both. Gradient,
  pattern and picture fills are shown by type, writable back unchanged,
  and replaced only by an explicit `none` or solid fill.
- Line (shapes, connectors, pictures): per property. A write compares
  each property with what is shown and writes only those that differ,
  into the object's own `a:ln`, so a line from `lnRef` keeps its style
  and gains a local override (`<a:ln w="38100"/>` before `p:style`).
  Inherited values cannot be unset (there is no way to say "inherit"
  without deleting someone else's choice); custom dashes and non-solid
  line fills are writable back unchanged only; setting `w`, `dash`,
  `cap` or arrows on a line that is `none` fails until a colour is given.
- Glue: `a:stCxn`/`a:endCxn` id and site, inserted or removed. Glue to an
  id several objects share fails (`edit.ErrAmbiguous`); the endpoints are
  never moved.
- Endpoints: `derive.ConnectorXfrm` under each branch's parent map, then
  whole EMU. A field not given keeps the value shown; an elbow
  connector keeps its first-segment direction unless `first` is given.
  Adjust values are kept, so elbows stay at the same fractions of the
  box. `elbows` are writable back as shown; moving them is done through
  `adj` (an inverse from elbow points to adjust values is not built).
- Name: `cNvPr@name` in every branch.
- Text body properties (2026-11-13; read since M2 but not writable,
  although Properties lists them): `"text": {"anchor", "wrap",
  "insets": [l, t, r, b]}` (left, top, right, bottom; not CSS order;
  since 2026-11-18 also the named form `{"left","top","right","bottom"}`
  with any subset, after an agent wrote CSS order and put 230 pt into
  bIns of a 46 pt box; the `inset-overflow` hazard reports insets that
  leave no room, on changed shapes only, as a report, not a refusal:
  such insets are schema-valid and the tool derives facts, never
  decides), the form `--detail full` shows, on
  `object.set` and on `object.add` with `html`. The read half,
  `derive.ReadBodyAttr` over the placeholder chain (values found nowhere
  are those the specification implies, origin `spec`), moved out of the
  read model, which now calls it too. A write compares property by
  property and sets only those that differ from what is shown, as
  attributes of the shape's own `a:bodyPr` (through the Handle in every
  branch); the rest of `a:bodyPr` keeps its bytes, and an inherited
  value cannot be unset. Values are checked against
  `ST_TextAnchoringType`, `ST_TextWrappingType` and `ST_Coordinate32`
  (insets in whole EMU) at compile. Only `html` creates a text body, so
  the properties are written after `html`, and a shape without a text
  body and no `html` fails (`prop.ErrNoTextBody`). Extended 2026-11-13
  to autofit, vert, rot, columns and the flags ("Text writes (M3)").
- Non-visual properties (2026-11-13): `descr` (alternative text) and
  `title` on `cNvPr` of every writable object kind, shown when not empty,
  `""` removing the attribute; `hidden` (shown since M2, now writable)
  and a shape's `txBox` (`cNvSpPr@txBox`), shown as `true` and written
  `1`, `false` removing the attribute (the schema default; graphic
  frames show `descr` and `title` but stay read-only). The read halves
  are the read model's (`derive.AttrBool` with the schema default false).
  `txBox` on a connector fails at compile.
- Connector fill (2026-11-13): connectors were refused fills by design
  ("connectors have no fill": every tabulated connector path is
  unfilled, so a fill is never drawn). PowerPoint and pdraw nevertheless
  write `a:noFill` in a connector's `spPr`. The read model and the fill
  codec now read a connector's own fill (its `spPr` only: a style's
  fill reference shows nothing, and no fill is no value, not an
  omission), and a plan can set `"fill": "none"`; any other fill on a
  connector is refused at compile and by the codec, so the design
  decision stands for everything a connector does not already hold.

Laws (`internal/pptx/laws/m3_test.go`, fixtures in `make check`, corpus with
`PPTX_CORPUS`): codec and view agree object by object; every writable
property written back in the view's form, all in one transaction per
deck, leaves bytes and mtime unchanged; new fills, lines, presets and
endpoints read back in the view as set or fail with `ErrInexpressible`;
single-operation plans leave every other part and every other object's
own bytes identical.

Corpus run, 2026-11-12: 1024 native decks, 9820 objects, all targetable.
Codec and view agreed on every shown property (geom 8624, name 9054,
fill 5762, line 5745, endpoints 316, start 73, end 72). Identity: 30454
fields written back in one transaction per deck, all byte- and
mtime-identical. Inverse: 20791 sets read back as set (fill 5762, geom
6129, line 8638, endpoints 262), none inexpressible; 3 refused with
`ErrBranch` (an `mc:Choice` shape whose `mc:Fallback` has no `spPr` to
take the same line). Single-operation plans on 1308 sampled objects
(add shape, add connector, delete, duplicate, reorder, set fill 1140,
set line 1286; 3 refused as inexpressible): no other part or object
changed. Duplicate-then-delete was a pure id rename 1308 times;
add-then-delete wrote nothing 1308 times.

### Operations

- `object.add` (shape or connector) starts from templates trimmed from a
  PowerPoint 16 deck (`internal/pptx/ops/fragments`, provenance in
  `internal/pptx/ops/set.go`): the non-visual properties and an empty `spPr`.
  Everything else comes from the plan through the codecs. Required, else
  the plan fails at compile naming the field: `name` (DrawingML requires
  `cNvPr@name`; `""` is allowed), `geom`, and `box` for shapes or `from`
  and `to` for connectors, plus `first` for elbow connectors. Nothing
  else is written: no `p:style`, no fill or line unless given, no text
  body unless `html` is given, no `a16:creationId`. A new object goes to
  the front of the slide's shape tree (PowerPoint's insertion order);
  `object.reorder` moves it. Pictures are added too (2026-11-13; see
  "Pictures" below). A shape with `html` gets `p:txBody` with an empty `a:bodyPr`
  (no `a:lstStyle`, which is optional) through `Ensure`, then its
  paragraphs through `text.Set`; `html` on a connector fails at compile.
- `object.set`: fields in a fixed order (name, geometry, transform or
  endpoints, glue, fill, line, then `html` through `text.Set`, then the
  body properties `text`); a connector takes `from`/`to` or `box`, not
  both.
- `text.set` and `text.replace`: `text.Set` and `text.ReplaceText` on the
  target's Handle (below). Like every operation they report the target in
  `changed` when its bytes differ, so the result shows the new `html`.
- `object.move`: unchanged from the foundations.
- `object.delete`: removes the object, a group with its children, or the
  `mc:AlternateContent` around an object that is alone in each branch.
  Connectors glued to it are untouched (`dangling-glue` in the result).
  Media its pictures (a group's included) used is released as an image
  replace releases it ("Pictures" below).
- `object.duplicate`: copies the object (or its own
  `mc:AlternateContent`) immediately after the original in the same
  container, before any whitespace, so that deleting the original leaves
  the copy in its place. Each distinct object id in the copy gets a new
  id from `NextID` (branches stay paired), glue between objects inside
  the copy follows the new ids, glue to outside objects is kept, each
  `a16:creationId` gets a new random GUID, and every other extension,
  name and relationship id is copied, so pictures share media. Names are
  not changed: PowerPoint renames copies, but a new name is a choice.
- `object.reorder`: front or back of the object's own container, or
  before or after another object in the same container; never into
  another group. A move to where the object is changes nothing.
- Read-only objects (2026-11-13; the coverage tracker found graphic
  frames accepting name, transform, move, delete, duplicate and
  reorder): every operation that writes an object (`object.set`,
  `object.move`, `object.delete`, `object.duplicate`, `object.reorder`,
  `text.set`, `text.replace`) refuses a graphic frame (chart, table,
  SmartArt, OLE) or content part (ink) with `ops.ErrReadOnlyObject`,
  as the Objects table says. The check runs when the operation resolves
  its target, before it changes anything, and the plan is all or
  nothing; it cannot run in `Compile`, which does not open the deck and
  cannot know what an alias will name. Such objects can still be named
  as a glue end or as the `ref` of a reorder, which writes the other
  object only. Pictures keep move, resize, delete and duplicate, and
  also name and reorder: z-order is part of the transform the
  Properties list gives ("move" in depth), and since `object.duplicate`
  copies names unchanged, renaming the copy is what tells it apart, so
  both follow from what the Objects table grants. (Geometry and line on
  pictures, which the M3 codecs already allowed, are unchanged.)
- Add then delete in one plan writes nothing. The id counter question:
  within a plan ids are never reused, but `NextID` is recomputed from the
  file, so a later plan can give a new object the id a deleted one had.
  Nothing in the slide refers to a deleted id except dangling glue, and
  `NextID` already counts glue ids, so such glue never attaches to the
  new object.

### Placed inserts

`edit.Handle.Ensure` cannot fill repeated slots. The placed inserts,
also in `schema.go` (the one file of the edit layer allowed to insert, so
the architecture test is unchanged), say which occurrence:
`Handle.AppendEach` appends at the end of a repeated slot inside an
object (guides, colour modifiers), after the last child of the same or an
earlier slot and before any later one; `Workspace.InsertObject`,
`MoveObject` and `DuplicateObject` place objects at an `edit.Anchor`
(`Front`, `Back`, `Before(ref)`, `After(ref)`), with the ends of a shape
tree or group taken from the `CT_GroupShape` table (after `grpSpPr`,
before `extLst`; object-level `mc:AlternateContent` counts as an object).
They declare the container, so the verifier holds them to its child
list. The schema table now also covers `CT_GeomGuideList`,
`CT_NonVisualDrawingProps` and all six colour types (`CT_SRgbColor`,
`CT_SchemeColor`, `CT_SystemColor`, `CT_PresetColor`, `CT_HslColor`,
`CT_ScRgbColor`; the last four added 2026-11-13, when modifiers on
system and preset colours failed as untabulated), regenerated from the
same pinned schemas. `Handle.ReplaceParagraphs`
(text, below) is the last placed insert, also in `schema.go`; the
architecture test's rule did not change, only its wording, which now
names `schema.go` rather than `Handle.Ensure` alone.

### Schema values

Added 2026-11-13, after the coverage tracker found that the geometry,
line and colour codecs wrote any value as given (`"geom": "notAShape"`,
dash `"foo"`, arrows `"foo"`/`"huge"`, unknown colour names).

- `gen_schema.go` (same pinned zip, same SHA-256 check) also writes
  each vocabulary's attribute table, the attributes of every tabulated
  complex type and of its children's types with their simple types, and
  its simple-type table, each
  of those simple types resolved through its restriction chain (the
  built-in it ends in, union members, enumeration, inclusive integer
  bounds, patterns, lengths), reading `shared-commonSimpleTypes.xsd` for
  the `s:` types. No value list is written by hand: the text grammar's
  alignment, autonumber, underline, scheme-colour and modifier lists are
  gone too.
- `schema.CheckAttr(type, attr, v)` (`edit.CheckAttr` forwards to it)
  checks a value against an attribute's type; `edit.CheckColor` checks a colour's name or components, a system
  colour's `lastClr` and each modifier (a child the colour type allows,
  with a `val` exactly when its type has one, in that type's range).
  Errors wrap `schema.ErrSchema` (also `edit.ErrSchema`) and name the type and some valid values or
  the bounds, for example `geom: value outside the schema: "notAShape" is
  not a:ST_ShapeType (want one of line, lineInv, triangle, ... (187
  values))`.
- `Plan.Compile` checks every value `object.add` and `object.set` would
  write, and parses every `html` (`text.set`, `object.add`,
  `object.set`), before the deck is opened, so a bad value fails the plan
  and nothing is read or written. Validated: `ST_ShapeType` (geom),
  `ST_PresetLineDashVal`, `ST_LineCap`, `ST_LineEndType`,
  `ST_LineEndWidth`, `ST_LineEndLength`, `ST_LineWidth` (0 to 20116800
  EMU, after rounding the plan's units), `ST_SchemeColorVal`,
  `ST_SystemColorVal`, `ST_PresetColorVal`, `s:ST_HexColorRGB` (rgb, hex,
  `lastClr`), `ST_PositiveFixedAngle` and `ST_Percentage` (hsl and scRGB
  components), each modifier's type (`ST_PositiveFixedPercentage`,
  `ST_FixedPercentage`, `ST_PositivePercentage`, `ST_Percentage`,
  `ST_PositiveFixedAngle`, `ST_Angle`), `ST_Coordinate` and
  `ST_PositiveCoordinate` (box and endpoints, slide-space EMU),
  `xsd:unsignedInt` (glue site) and `ST_DrawingElementId` (raw glue ids);
  in the text grammar `ST_TextAlignType`, `ST_TextAutonumberScheme`,
  `ST_TextUnderlineType`, `ST_TextFontSize`, `ST_TextIndentLevelType`
  and the colour types as above; for body properties
  `ST_TextAnchoringType`, `ST_TextWrappingType` and `ST_Coordinate32`.
  Not typed by the schema, so not limited: adjust-value formulas
  (`ST_GeomGuideFormula` is a string), names, typefaces, bullet
  characters. The tool's own words (`"custom"` geometry and dashes,
  fills shown by type, `first`) keep their own checks.
- The codecs check again before each attribute they write, so a caller
  that bypasses plans cannot write outside the schema either; a
  transform whose stored values (in the object's own child space) fall
  outside `ST_Coordinate`, `ST_PositiveCoordinate` or `ST_Angle` fails
  with `ErrInexpressible`.
- A value outside the schema that a non-conforming deck already holds is
  shown, but cannot be written back through a plan: leave the field out.
- `phClr` is in `ST_SchemeColorVal` but is the placeholder colour of a
  theme's style matrices (the colour a style reference supplies), so a
  plan colour of `phClr` (fill, line colour, `color:var(--phClr)` or
  `data-color` in HTML) is refused at compile (`edit.CheckSlideColor`,
  also checked by the colour writer). The views never show it on a
  slide: a style's `phClr` is shown resolved. No slide of the 1312 corpus
  decks holds a literal `phClr`.
- Corpus run, 2026-11-13, after these fixes (1024 native decks): the
  stricter values broke no identity law; codec and view agreed on 7492
  text bodies' properties; identity wrote back 37946 fields (body
  properties included) byte- and mtime-identically; 7491 body-property
  sets read back as set; 1111 single-object body-property plans left
  every other object and part unchanged. The transform laws now skip
  the 766 graphic frames, which plans refuse.

### Pictures

Settled 2026-11-13 while building picture writes (`internal/pptx/ops/picture.go`,
`internal/pptx/ops/prop/picture.go`, `internal/pptx/derive/picture.go`). None of
these was checked against PowerPoint; the smoke pairs are ready for
`pptoracle smoke --all`.

- What the views show of a `p:pic`, each only where the file says it:
  `image` (the `a:blip` `r:embed` target part, as before), `svg` (the
  target of the SVG extension's `asvg:svgBlip`), `media` and `svgMedia`
  (`{"type", "sha256", "px"}`: the part's content type from the
  package, the SHA-256 of its bytes and, for PNG, JPEG and GIF, the pixel
  size from the header through the standard library's decoders; `px` is
  otherwise omitted with the reason as `media.px`, as for EMF and WMF),
  `crop` (`a:srcRect`), `fit` (`stretch` or `tile`), `fillRect` (the
  stretch's `a:fillRect`) and `locks` (`a:picLocks` flags that are set,
  `{"noChangeAspect": true}`). The SVG view carries them as `data-svg`,
  `data-media`, `data-svg-media`, `data-crop`, `data-fit`,
  `data-fillrect` and `data-locks`, and draws the image as a nested `svg`
  whose viewport is the box (inset by the fill rectangle) and whose
  `viewBox` is the cropped part of the image in fractions, holding an
  `image` stretched over the unit square (`preserveAspectRatio="none"`),
  so the crop clips it; rotation and flips go on an enclosing `g`, and a
  tile is drawn stretched (`data-fit` is authoritative).
- Crop and fill rectangle are `[l, t, r, b]` in percent of the image (of
  the box for `fillRect`), exact to 0.001%: `a:ST_Percentage` stores
  1000ths of a percent, and `show.Percent` prints them exactly rather
  than through the views' 0.01 rounding (33333 is 33.333; PowerPoint's
  own crop of a third). PowerPoint's Crop pane shows lengths derived
  from these and the frame instead; lengths would change with the box
  and need rounding both ways, so the view shows the stored fractions.
  Negative values (padding) are shown and written. Strict decks' `"33.333%"`
  form reads the same.
- `crop` and `fillRect` are shown exactly when their element is present
  (absent attributes are 0, the schema default), so `<a:srcRect/>` shows
  `[0,0,0,0]` and no `a:srcRect` shows nothing. Absent and empty mean
  the same, but showing presence keeps write-back a no-op on both forms;
  `crop: [0,0,0,0]` on a picture without one inserts an empty
  `a:srcRect`, and sides written 0 are removed. `a:stretch` without
  `a:fillRect` fills the box (the specification's text for `stretch`);
  PowerPoint 16 writes `<a:stretch><a:fillRect/></a:stretch>` for an
  uncropped picture and `<a:stretch/>` for a cropped one (059 deck
  below), and the view shows each as it is. A fill mode that becomes
  `stretch` is written in the uncropped form, with an empty `a:fillRect`,
  unless the plan gives `fillRect`. `tile` is read-only: its offset,
  scale, alignment and flip are not modelled, so it is writable back
  unchanged only; the fill mode cannot be unset.
- `locks` on a plan sets the flags given (`true` writes `"1"`, adding
  `a:picLocks` through `Ensure` when there is none; `false` removes the
  attribute, false being the schema default) and keeps the others; the
  flag names are checked against `a:CT_PictureLocking` at compile.
- `object.add` with `kind: "picture"`: required `name`, `box` and
  `image`, else the plan fails at compile naming the field. Optional:
  `geom` and `adj`, `rot` and flips, `line`, `svg`, `crop`, `fit`,
  `fillRect`, `locks`. Nothing else is written: no `a:prstGeom` unless
  `geom` is given (the schema does not require one), no fill mode unless
  `fit` is given, no `a:picLocks` unless `locks` is, no
  `a16:creationId`. `fill`, `html`, `text` and connector fields fail at
  compile. Plans that want what PowerPoint writes give `geom: "rect"`,
  `fit: "stretch"` and `locks: {"noChangeAspect": true}`; whether
  PowerPoint renders a picture without geometry or fill mode as it
  renders PowerPoint's own is for the smoke (`connectors--add-bare`).
  The template (`internal/pptx/ops/fragments/pic.xml`) is trimmed from
  ShapeCrawler's `ShapeCrawler.Tests/Assets/059_crop-images.pptx`
  (PowerPoint 16.0000, powerpoint_native in the manifest), slide1's
  `p:pic` "None": `p:nvPicPr` (`p:cNvPr`, an empty `p:cNvPicPr`,
  `p:nvPr`), `p:blipFill` with an empty `a:blip`, an empty `p:spPr`.
- `image` and `svg` take a part of the deck as the views show it (writing
  the part shown is a no-op; another image part of the deck is
  embedded as it is) or a local file (relative paths against the working
  directory); a value naming both a part and an existing file is refused.
  The format comes from the bytes, never the file name: PNG, JPEG and
  GIF by signature, and their header must decode (the pixel size above);
  SVG as a well-formed XML document whose root is `svg` in the SVG
  namespace. `image` must be PNG, JPEG or GIF; an SVG given as `image`
  fails naming `svg`. `svg: "none"` removes the extension.
- Media parts: bytes whose SHA-256 equals a part under `/ppt/media/` with
  the format's content type reuse that part (listed once per
  transaction); otherwise a new part `/ppt/media/imageN.<ext>`, N one
  more than the largest `imageN` there or targeted by any relationship of
  the package, a dangling one included (PowerPoint's naming, numbered
  across extensions), with the content-type `Default` for the extension
  added when the deck has none (an `Override` only if a `Default` gives
  another type). Extensions and types are those of the PowerPoint-native
  corpus decks: `png` `image/png` (180 decks), `jpeg` `image/jpeg` (948;
  PowerPoint names JPEG media `.jpeg`), `gif` `image/gif` (16), `svg`
  `image/svg+xml` (12). The slide reuses its existing image relationship
  to the part (PowerPoint shares one between pictures of one image), else
  gets the next free `rIdN` with the image relationship type of the
  slide's own namespace (from an image relationship it has, else its
  layout relationship's type with `image` for `slideLayout`; PowerPoint
  uses the image type for SVG parts too) and a relative target
  (`../media/image3.png`). New parts, the slide's relationships and the
  content types are declared to the verifier as whole parts.
- SVG: written exactly as PowerPoint 16 writes it in the 059 deck, the
  only form in the corpus (11 slides with `svgBlip`): the main `a:blip`
  embeds the raster fallback, and its `a:extLst` holds `<a:ext
  uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}"><asvg:svgBlip
  xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main"
  r:embed="rIdN"/></a:ext>`, appended after any other extension
  (`Handle.AppendExt`, a placed insert in `edit/schema.go`; `a:extLst`
  through `Ensure`). pdraw's emitter writes the same. The fallback is the
  agent's: `image` is required with `svg`, and the tool never rasterises
  (the standard library has no SVG renderer, and a fallback of its own
  making would be a decision). Replacing `image` on a picture that shows
  an `svg` without giving `svg` fails: PowerPoint would go on showing the
  old SVG over a new fallback, so the plan gives the new SVG, the shown
  part to keep it, or `none`.
- Replace: `object.set` `image` or `svg` sets the `r:embed` through the
  `prop.Image` and `prop.SVG` codecs (in every `mc` branch; a picture
  whose other branch is not a `p:pic`, such as a 3D model's graphic frame
  with a picture fallback, fails with `ErrBranch`), keeping the frame,
  crop, effects, extensions and everything else. Then, for each id the
  picture embedded before, the slide's relationship is removed when no
  attribute anywhere in the slide part holds the id any more, and its
  target part is deleted when no relationship of any part in the package
  targets it (the rule `slide.delete` states for media). A duplicate
  shares the relationship, so replacing either picture leaves the other
  and the shared media as they were.
- Delete (added 2026-11-13, after review): `object.delete` releases the
  media of every picture it removes, the object itself or any picture
  inside a deleted group, in every `mc` branch, by the same rule: each
  image and SVG relationship id those pictures embedded loses its slide
  relationship when no attribute left in the slide holds it, and the
  target part goes when no relationship in the package targets it. A
  duplicate or any other picture, slide or layout still using the part
  keeps it. Only picture blips are released; other relationship ids
  inside a deleted shape (hyperlinks, say) are left as they were.
- A content-type `Default` that a plan added for new media is removed
  again when the plan deletes the last part with that extension, so that
  adding and deleting pictures in one plan leaves the package
  byte-identical (the relationships part and the content types return to
  their original bytes; added parts that are deleted are never written).
  A `Default` the deck had before the plan is never removed, even when
  the plan deletes the last part using it: it belongs to the deck, and a
  `Default` without parts is harmless. `Override`s go with their part
  (`opc.DeletePart`), as before.
- Picture fills of shapes (`a:blipFill` in `p:spPr`) stay out of scope:
  shown by type with raw XML, replaced only by `none` or a solid fill.
- The edit schema table now also covers `CT_BlipFillProperties`,
  `CT_Blip`, `CT_StretchInfoProperties`, `CT_OfficeArtExtensionList` and
  `CT_NonVisualPictureProperties` (and with them the attribute types of
  `CT_RelativeRect`, `CT_PictureLocking`, `CT_TileInfoProperties`),
  regenerated from the same pinned schemas. New edit mutators:
  `SetRelAttr` (a relationship-id attribute in the namespace in scope),
  `SetExtRelAttr`, `RemoveExt` (with the `a:extLst` when it empties) and
  the placed insert `AppendExt`.
- Laws (`internal/pptx/laws/pictures_test.go`, fixtures in `make check`,
  corpus with `PPTX_CORPUS`): the picture codecs show what the view
  shows; every picture field written back as shown, in one transaction
  per deck, leaves bytes and mtime unchanged; a new crop, fill rectangle,
  fit and locks read back as set and leave every other part and object
  unchanged; duplicate then replace the copy leaves the original's image
  and the shared media; replace in place removes the old media part
  exactly when nothing else refers to it, adds at most the new media
  parts and changes no other part or object; replace followed by a
  failing operation leaves the file's bytes and mtime unchanged; adding
  a PNG twice, a JPEG, a GIF and an SVG with its fallback shares one part
  for the twice-added PNG, adds no second relationship to a part, and
  leaves the four content-type `Default`s. Corpus run, 2026-11-13, 1024
  native decks (2 unreadable): 555 pictures; 2461 fields paired and
  written back identically; 243 inverse sets read back, and 12 inverse
  or replace plans refused (graphic-frame branches, a `p:pic` without
  `p:blipFill`, values inexpressible under groups); 233 replaces, the
  shared media kept after duplicate-and-replace all 233 times, the old
  media collected 174 times and kept 59 times because something else
  referred to it, never wrongly; 233 failing plans wrote nothing;
  picture adds on 1018 decks.
- Delete laws (added with the delete rule): deleting a picture removes
  its media exactly when nothing else refers to it, and otherwise the
  slide's relationships only lose entries and only media no relationship
  targets may go (the M3 untargeted check now allows exactly that, for
  every delete, groups included); adding a PNG twice, a JPEG, a GIF and
  an SVG with its fallback, duplicating one, then deleting all six in one
  plan leaves the file's bytes and mtime unchanged. Corpus run,
  2026-11-13: 237 picture deletes, media collected 177 times and kept 60
  times because still referenced, never wrongly; add-then-delete
  byte-identical on all 1018 decks where pictures can be added; the M3
  laws (1308 sampled deletes) pass under the widened check. The run found
  one bug, fixed: a new media part took the name `image5.png` that a
  relationship elsewhere in the deck already targeted although the part
  was missing (LibreOffice's `tdf169781.pptx`), so the new part became
  that dangling relationship's target and could not be collected. New
  media names now skip every `imageN` any relationship targets.
- Coverage: the registry claims crop, fit, fill rectangle, locks and the
  SVG extension (`a:extLst`, `a:ext`, `@uri`) as read-modelled and
  image, crop, fit, fill rectangle and locks as round-trip-law, checked by
  the probes (the probe picture now carries an SVG blip; the extension
  URI is probed with the SVG and useLocalDpi URIs). The wildcard content
  of the extension (`(a:CT_Blip)/a:extLst/a:ext/*`, where pdraw's
  `asvg:svgBlip` sits) cannot be probed, since only a non-goal claim
  decides an `xsd:any`, so it is exempt from both requirements with that
  reason. pdraw parity moved from 145 of 185 items (78.4%) to 156
  (84.3%); read-modelled 162 → 170, settable 126 → 132; no picture item
  fails. The operation matrix gains `object.set` rows for `image, svg`,
  `crop, fit, fillRect` and `locks`, and `object.add` of a picture is
  implemented.

### Task self-check

`eval/plans/<task>.json` solves each Restyle, Move and align, Connect and
Insert task, written from `pptx read` output. Graded with `pptxbench
grade`, 2026-11-12, with text wired: all 19 pass (5 Restyle, 5 Move and
align, 5 Connect, 4 Insert) with no unintended change, no hazard and no
wrong object. The four Insert plans' `html` was written from the read
view before the text grammar existed and needed no change. The outputs
and their sources, named `<deck>--source.pptx` and `<deck>--<task>.pptx`,
are ready for `pptoracle smoke --all`; not yet opened in PowerPoint.
Re-run 2026-11-13 after the coverage fixes (schema validation, colour
types, body properties, read-only graphic frames, `phClr`): all 19 plans
apply unchanged and pass, with no unintended change, no new hazard and
no wrong object; a fresh set of source/output pairs was written for a
later smoke.

## Text writes (M3)

Settled while building `text.set` and `text.replace`
(`internal/pptx/ops/text`), 2026-11-12. None of these was checked against
PowerPoint; each says what it rests on.

### Grammar

- `text.set` takes exactly the HTML the read view emits (Read decisions
  (M2)), parsed strictly; anything else is an error naming the element,
  attribute or value, never dropped. Blocks are `<p>`, or `<ul>`/`<ol>`
  holding `<li>`; bare inline content is one paragraph (so `"Cache"`
  works), and whitespace between blocks is ignored. Paragraphs take
  `style="text-align:left|center|right|justify"`, `data-algn` (other
  `algn` values), `data-lvl` (1 to 9; levels are never nested lists),
  `data-bullet="none|picture"` on `<p>`, `data-char` on `<li>` in `<ul>`
  and `data-type` (an autonumber scheme) on `<li>` in `<ol>`. An `<li>`
  without them is an error, not a chosen bullet: the read view uses
  `<ul>`/`<ol>` only for paragraphs that set their own bullet, and `<p>`
  for inherited ones.
- Inline: text, `<br/>` (or `<br>`), `<b>`, `<i>`, `<u>` (`data-u` for
  underline types other than `sng`) and `<span>` with `style`
  (`font-weight:normal`, `font-style:normal`, `text-decoration:none`,
  `font-size:Npt`, `color:var(--scheme)|#RRGGBB|transparent`,
  `font-family:var(--theme-font)|'name'`), `data-color` and `data-fill`.
  Extended 2026-11-13 with paragraph spacing, margins, indents, tabs,
  direction, bullet font, size, colour and start, `<s>`, `<a>`, `lang`
  and more span properties ("Formatting, body settings and links",
  below). Formatting elements nest in any order and must not contradict each
  other (`<b>` inside `font-weight:normal`, two sizes). Only the five XML
  entities and numeric references are accepted; a bare `&` is an error.
- Font sizes round to hundredths of a point (`sz`), 1 to 4000 pt.
  `data-color` is the read view's written colour (`accent2 lumMod=75000`,
  `srgb:...`, `sys:...`, `prst:...`, `hsl:...`, `scrgb:...`, modifiers
  checked against CT_Color's transforms) optionally followed by the
  displayed hex. The displayed hex is derived: it is never written or
  compared, but `color:#X` beside a `data-color` must equal its hex, so a
  colour edited in one place and not the other is an error rather than
  a guess. A system colour is written with its displayed hex as
  `lastClr`, the value PowerPoint caches there and the views show.
- `text.Normalise(h)` is the parse rendered back: it canonicalises tag
  nesting (`b`, `i`, `u`, `span`), style order, list grouping and
  defaults (`data-lvl="1"` dropped), and merges adjacent unformatted text,
  whose run boundary HTML cannot show. Formatted runs keep their
  boundaries (`<b>A</b><b>B</b>` stays two runs). The law is
  `html(read(set(h))) == Normalise(h)` for display hexes that match the
  deck.

### What text.set keeps and carries

- `a:bodyPr` and `a:lstStyle` are never touched; only the range from the
  first `a:p` to the last is replaced. A shape without a text body gets
  `p:txBody` with an empty `a:bodyPr` (the schema requires it), through
  `Handle.Ensure`; connectors and pictures cannot hold text and fail
  there.
- Writing what the body shows is a no-op that touches no XML (colours
  compared as written). Otherwise paragraphs whose HTML is unchanged keep
  their bytes (a longest common subsequence of paragraph HTML).
- A changed paragraph is paired with the one it replaces: by order
  within each stretch between unchanged paragraphs when the counts agree
  (nothing says one was inserted), else by the order-keeping alignment of
  greatest text similarity (common prefix and suffix over the longer
  text, at least 0.3). Pairing by order alone shifted every paragraph's
  hidden formatting by one when an agent inserted a paragraph and edited
  the others, as the corpus law showed. A paired paragraph keeps its
  start tag and `a:endParaRPr`, and its `a:pPr` with only the shown
  properties (algn, lvl, bullet choice) set to what the HTML says: `marL`,
  `indent`, spacing, bullet font, colour and size, tabs and `defRPr` are
  carried. (Since 2026-11-13 margins, indents, spacing, tabs, direction
  and the bullet's font, size, colour and start are shown, so set; what
  is carried is `defRPr`, `fontAlgn`, the line-break flags and the parts
  of the bullet font HTML does not show.)
- Within it, runs whose HTML is unchanged keep their bytes (a common
  subsequence of run HTML), fields included. A new or changed run takes
  the `a:rPr` of the old run covering the position where it starts,
  positions mapped through the common prefix and suffix of the old and
  new paragraph text (inside the change, the run where the change
  starts, as typing does), with `b`, `i`, `u`, `sz`, the fill choice and
  `a:latin` set to what the HTML says and nothing else changed: `lang`,
  `altLang`, `dirty`, `kern`, `baseline`, `cap`, `spc`, `ea`/`cs`/`sym`
  fonts, effects, highlight and hyperlinks are carried (since 2026-11-13
  `lang`, `strike`, `baseline`, `cap`, `spc`, highlight and click
  hyperlinks are shown, so set). This is what
  PowerPoint does when formatting a selection; a carried value is the
  run's own, never a default. A run whose shown formatting is unchanged
  keeps its `a:rPr` bytes.
- A paragraph with no counterpart takes the `a:pPr` of the paragraph
  before it (after it, when first), shown properties set as the HTML
  says, as pressing Enter does, so an added bullet keeps the list's
  `marL`/`indent`/`buFont`. Its runs and its end get only what the HTML
  says (no `lang`, no `endParaRPr`): run properties belong to text, not
  to position.
- A changed or removed paragraph that holds content HTML does not show
  (an equation in `mc:AlternateContent`, or any element outside `pPr`,
  `r`, `br`, `fld`, `endParaRPr`) is refused (`ErrUnsupported`): keep its
  HTML unchanged, or remove it with `xml.set`. A picture bullet or a
  gradient, picture, pattern or group text fill can be kept where it is
  but not created from HTML (`ErrInexpressible`).
- Lost on rewrite, by construction: in a changed paragraph, empty
  unformatted runs and the boundary between adjacent unformatted runs
  (the merged run takes the first run's `a:rPr`), whitespace and comments
  between runs, and whitespace between paragraphs of a changed body. A
  new line break is a bare `a:br`. Editing a field's text makes it a
  plain run, as typing over a field does.
- New elements reuse the DrawingML prefix (transitional or strict) in
  scope at the `p:txBody`; with none, each new `a:p` declares `a`.
  Children go in schema order (the CT_TextParagraphProperties and
  CT_TextCharacterProperties tables, read through `edit.ChildOrder` from
  the generated `internal/ooxml/schema/dml_table.go` that `Ensure` uses,
  so there is one table), attributes in schema attribute order (kept in
  `internal/pptx/ops/text/schema.go`; the generated table holds children
  only). After writing, every branch is
  read back and must show the HTML, else `ErrInexpressible`.
- `mc:AlternateContent`: each branch is built from its own body and
  carries its own properties; a branch without a text body fails the
  operation.

### text.replace

- Matches are found in the text the body shows, runs of a paragraph
  joined; `find` must be non-empty. No match is `ErrNoMatch`; more than
  one without `all` is `ErrManyMatches` (with the count); `all` replaces
  every non-overlapping match, left to right. Matches crossing a
  paragraph break, a line break, a field (`a:fld`, regenerated by
  PowerPoint) or content in `mc:AlternateContent` are `ErrCrossing`.
- The replacement goes in the run where the match starts, keeping its
  `a:rPr`; runs the match spans into are merged into it (removed, with
  what follows the match in the last kept). Every other run, paragraph
  and byte keeps its bytes. Replacing a text with itself checks the
  matches and touches nothing. Every branch must hold the same number of
  matches.

### Wiring

- `text.Object` is what the operations need from `edit.Handle`:
  `Branches`, `Elem`, `Ensure` and `ReplaceParagraphs(frags [][]byte)
  error`. `*edit.Handle` satisfies it; the text package's tests use a
  byte-splicing fake.
- `Handle.ReplaceParagraphs` (in `edit/schema.go`, the edit layer's one
  inserting file): in each branch with a non-nil fragment, the children
  of `p:txBody` from the first `a:p` to the last inclusive, whatever lies
  between them included, become the fragment's nodes; with no `a:p`
  they go after `a:lstStyle`, else after `a:bodyPr`. `a:bodyPr`,
  `a:lstStyle` and anything after the last `a:p` keep their bytes. A
  fragment must parse, with the prefixes in scope at the `p:txBody` (or
  declared on its own elements), as one or more `a:p` (the schema needs
  one) with only whitespace, comments or processing instructions
  between. Everything is checked in every branch before anything
  changes: a branch without `p:txBody` or `a:bodyPr` fails with
  `ErrBranch`, a bad fragment names the branch, and no branch changes.
  Like the other Handle mutators it changes only the object `Target`
  declared, so the verifier holds it to that object's own content.
- Plan specs are `ops.TextSetSpec {op, target, html}` and
  `ops.TextReplaceSpec {op, target, find, with, all}`, compiled to
  `ops.TextSet` and `ops.TextReplace`; the mirror spec types the text
  package carried while it stood alone are gone.

### Laws

Over the hand fixtures in `make check` and, with `PPTX_CORPUS` set, every
PowerPoint-native corpus deck (`internal/pptx/ops/text`, `internal/pptx/laws`).
Corpus run, 2026-11-12, 1024 decks (7 listed but missing, 1 unreadable):

- Agreement: the read model's HTML of all 7492 text bodies parses, is a
  fixed point of `Normalise`, and shows what `text.ReadBody` shows of the
  same `p:txBody`.
- Set round trip: an agent-like edit of every body (text appended, bold
  toggled, a scheme colour, alignment, a paragraph inserted), written
  into the deck and read back by the read model, shows `Normalise(h)` for
  7486 of 7492; the other 6 hold equations and are refused.
- Identity: building from the shown HTML is a no-op for all 7501 bodies
  (every branch). Forced rewrite of every paragraph from its own HTML
  keeps 11011 of 11496 paragraphs byte-identical; the others merge
  adjacent unformatted runs (369), drop whitespace between runs (86) or
  empty unformatted runs (30); 6 bodies with equations are refused. From
  HTML alone, with nothing carried, 1 of 11407 paragraphs comes out
  identical: almost every paragraph holds something HTML does not show
  (`endParaRPr` in 6615, then `lang`, `dirty`, spacing, bullet fonts,
  `cs` fonts), which is why carrying matters; 37 bodies cannot be built
  from HTML alone at all (8 picture bullets, 23 gradient or picture text
  fills, 6 equations).
- Through plans (`internal/pptx/laws/textops_test.go`, on the objects the M3
  laws sample, with `Handle.ReplaceParagraphs`), corpus run 2026-11-12:
  1121 `text.set` plans of the agent edit and 16 `object.set` `html`
  plans on shapes without text read back as `Normalise(h)`; 776
  `text.replace` plans (`all`); each left every other part and every
  other object's own bytes identical. Refused, not written: 30 replaces
  that cross a field or break, 3 sets on paragraphs holding equations.
  Atomicity: 1124 plans of a text.set followed by a failing operation
  left the file's bytes and mtime unchanged.
- Replace: 4228 single-run and 647 two-run replacements leave every other
  paragraph and run byte-identical, keep the starting run's `a:rPr`, and
  read back as the text replaced.

### Formatting, body settings and links (2026-11-13)

Added for parity with pdraw's text boxes and labels and for the
paragraph and run properties with corpus weight. None of it was checked
against PowerPoint; the smoke pairs are ready for `pptoracle smoke`.

- One reader and one renderer: the read model now projects a text body
  through `text.ReadBodyWith` (with a `text.Ctx` that resolves displayed
  colours through the slide's theme, recording failures as before, and
  hyperlink targets through the slide's relationships) and
  `text.Render`, instead of its own copy of the grammar. The agreement
  law still runs; it now checks the context-free reader against the
  model's. `internal/pptx/model` imports `internal/pptx/ops/text` (the reverse is
  still forbidden).
- Encoding: CSS in `style` where the property maps onto a CSS property,
  read with the DrawingML meaning; `data-*` attributes otherwise, as
  before. Lengths in HTML are always points, whatever `--units` says
  (font sizes already were). Values are exact: percentages and points
  are decimal renderings of the stored integers (`150000` is `150%`,
  `1250` hundredths of a point `12.5pt`), lengths in EMU are shown to
  0.01 pt and compared as shown, like the views' other lengths.

  | HTML | DrawingML (`a:pPr`) |
  | --- | --- |
  | `style="line-height:150%"`, `line-height:18pt` | `a:lnSpc/a:spcPct@val` (1000ths of a percent of single spacing), `a:spcPts@val` (100ths of a point) |
  | `margin-top`, `margin-bottom` (same two forms; the percentage is of a line, not CSS's width) | `a:spcBef`, `a:spcAft` |
  | `margin-left`, `margin-right`, `text-indent` (may be negative), `tab-size` (pt) | `@marL`, `@marR`, `@indent`, `@defTabSz` |
  | `data-tabs="36pt 72pt:ctr"` | `a:tabLst/a:tab@pos[@algn]`; a stop without a position shows no stops |
  | `dir="rtl"` / `dir="ltr"` | `@rtl` 1 / 0 |
  | `data-bu-font="'Wingdings'"`, `var(--mn-lt)`, `text` | `a:buFont@typeface`, `a:buFontTx` |
  | `data-bu-size="75%"`, `12pt`, `text` | `a:buSzPct` (1000ths, whole 25% to 400% as `a:ST_TextBulletSizePercent` allows), `a:buSzPts`, `a:buSzTx` |
  | `data-bu-color` (the `data-color` syntax), `text` | `a:buClr`, `a:buClrTx` |
  | `data-start="3"` on `<li>` in `<ol>` | `a:buAutoNum@startAt` |
  | `lang` on `<p>`/`<li>` | the language of every run of the paragraph, when they all have the same one |

  | HTML | DrawingML (`a:rPr`) |
  | --- | --- |
  | `<s>`, `<s data-strike="dblStrike">`, `<span data-strike="noStrike">` | `@strike` |
  | `style="vertical-align:30%"` (negative for subscript) | `@baseline` (1000ths of a percent); no `<sup>`/`<sub>`, which would choose an offset |
  | `text-transform:uppercase`, `font-variant:small-caps`, `text-transform:none` | `@cap` all, small, none |
  | `letter-spacing:1.5pt` | `@spc` (100ths of a point) |
  | `background-color` with `data-highlight`, as `color` with `data-color` | `a:highlight` |
  | `lang` on `<span>` (`lang=""` for none inside a paragraph that has one) | `@lang` |
  | `<a href="..." title="...">`, outermost; `<a data-link="...">` | `a:hlinkClick`; see links below |

- Language: 12982 of 13207 corpus runs carry `lang`, so it is shown on
  the paragraph tag when every run of the paragraph has the same one,
  and on a run only where it differs; adjacent runs that differ in
  nothing but an equal language are merged (HTML cannot show the
  boundary). A run written without a language gets none: the tool does
  not guess one. Not shown, still carried: `altLang`, `dirty`, `err`,
  `kern`, `noProof`, `smtClean`, the `ea`, `cs` and `sym` fonts,
  `fontAlgn` and the East Asian line-break flags.
- What is shown is set: every property above moved from "carried" to
  "shown", so a paired or new paragraph or run has it set exactly as the
  HTML says (the round-trip law requires it). An agent that adds a
  bullet next to others restates the list's `margin-left`,
  `text-indent` and `data-bu-font`, as the read view shows them; the
  new paragraph still starts from its neighbour's `a:pPr`, so what HTML
  does not show (`fontAlgn`, `defRPr`, the bullet font's charset) is
  carried as before. Text written before these were shown loses them
  where its HTML is rewritten without them; the 19 task plans were not
  affected.
- Links: `href` is shown for an `a:hlinkClick` without `action` whose
  relationship is external; anything else shows as `data-link` (the
  action, `internal`, or `unresolved`) and can be kept but not created
  (`ErrInexpressible`). A new or changed link reuses an external
  hyperlink relationship of the slide with the same target (PowerPoint
  shares one among the runs of a link: 3 to 7 runs per relationship on
  slide 5 of `web/spade-standards-roadmap`) or adds one with the next
  free id and `TargetMode="External"`, its type the transitional
  `.../relationships/hyperlink` PowerPoint writes in the web decks (the
  base taken from the part's own relationship types, so a strict part
  gets the strict one). `r:id` uses the relationships prefix in scope, or
  declares `xmlns:r` on the element. A changed tooltip edits the
  element in place. After a `text.set`, each relationship a removed link
  referred to is deleted when no attribute in the relationships
  namespace anywhere in the part still names it (`edit.PartRels.Prune`),
  and the relationships part is declared to the verifier. `text.set`
  takes the part's relationships through `text.SetWith` and
  `edit.Handle.Rels()`; without them (`text.Set`) a link shows as
  unresolved and none can be created.
- Schema values: the new values are checked at compile against the
  generated tables (`ST_TextSpacingPercentOrPercentString`,
  `ST_TextMargin`, `ST_TextIndent`, `ST_Coordinate32`,
  `ST_TextBulletSizePercent`, `ST_TextFontSize`, `ST_TextTypeface`,
  `ST_TextBulletStartAtNum`, `ST_TextStrikeType`, `ST_Percentage`,
  `ST_TextCapsType`, `ST_TextPoint`, `s:ST_Lang`, the colour types).
  The types one level below the tabulated ones (`CT_TextSpacingPercent`,
  `CT_TextSpacingPoint`, `CT_TextTabStop` with `ST_TextSpacingPoint` and
  `ST_TextTabAlignType`) come from the generated tables: `gen_schema.go`
  lists `CT_TextSpacing` and `CT_TextTabStopList` among its wanted types
  (folded in at the parity integration; they were briefly hand-entered
  and checked against `universe.json`, and the generated entries match).

### Text body settings

- `"text"` on `object.set` and `object.add` (with `html`) takes, besides
  `anchor`, `wrap` and `insets`: `autofit` (`"none"`, `"shape"` for
  `a:spAutoFit`, `"shrink"` for `a:normAutofit`) with `fontScale` and
  `lnSpcReduction` in percent, `vert`, `rot` (degrees), `numCol`,
  `spcCol` (the plan's units), `rtlCol`, `anchorCtr`, `upright`,
  `vertOverflow`, `horzOverflow`, `compatLnSpc`, `spcFirstLastPara`,
  `fromWordArt` and `forceAA` (booleans): every `a:bodyPr` attribute
  with corpus weight (`prstTxWarp`, 265 bodies, stays carried). The views
  show all of them in `--detail full` (`text` in JSON, `data-body` beside
  `data-anchor` in SVG) through `show.BodyJSON`, which the body codec's
  `Show` also uses, so view and codec cannot disagree. A value found
  nowhere in the placeholder chain is the one ECMA-376 states is implied
  (origin `spec`): `noAutofit`, `horz`, 0, 1 column, false, `overflow`.
- Autofit: the choice is read from the first link of the chain whose
  `a:bodyPr` holds one of the three elements and written to the shape's
  own `a:bodyPr` through `Ensure`, which replaces the choice's other
  element. Writing `a:normAutofit` never invents `fontScale` or
  `lnSpcReduction`: PowerPoint applies the stored scale on open and
  does not recompute it (Open question 4), so a scale the tool wrote
  would be a guess at text measurement, which is excluded. A new
  `a:normAutofit` holds only the scales the plan gives (none: 100%, which
  PowerPoint shows until the text is next edited there); an existing one
  is shown (`fontScale` 62.5) and kept while the mode stays `shrink`, and
  other body properties leave it alone; a change of mode drops it with
  its element. Scales are refused at compile with any other mode.

### Laws and checks for these properties

Corpus run 2026-11-13 (1024 native decks, `PPTX_CORPUS`; 7 listed but
missing, 2 unreadable), after these changes:

- Text through the read model: all 7492 bodies parse, are fixed points
  of `Normalise` and agree with `text.ReadBodyWith` (the law now reads
  links through the slide's relationships: without them 35 bodies with
  links differ, as they should). The agent edit, which now also sets
  line spacing, space before, letter spacing, a strike, and a new
  paragraph with margin, indent, language, small caps, baseline and
  highlight, reads back as `Normalise(h)` for 7489 bodies; 6 holding
  equations are refused.
- `internal/pptx/ops/text`: building from the shown HTML is a no-op for all
  7501 bodies; a forced rewrite keeps 11200 of 11496 paragraphs
  byte-identical (11011 before: language and the other shown properties
  now keep run boundaries that were merged). From HTML alone, links that
  HTML cannot create are refused (57: actions, internal targets,
  unresolved relationships) besides the picture bullets, text fills and
  equations as before.
- M3 laws: codec and view agree on every object for `descr`, `title`,
  `hidden` (9054 each), `txBox` (7824), `text` (7492) and `fill` (5790,
  connectors' own fills included). Identity: 39789 fields written back
  as shown, byte- and mtime-identical. Inverse: 43901 sets read back
  (descr 9050, txBox 6310, connector fill none in fill 6023, text 7491
  with autofit and vert toggled); 1 inexpressible, 3 `ErrBranch` (as
  before, `mc:Choice` without a `spPr` counterpart). Plans: 807 link and
  807 unlink plans: each link added exactly one relationship and changed
  no other part, read back as set, and the unlink restored the
  relationships part byte for byte; 1110 `text.set`, 789
  `text.replace` and 1111 body-property plans left every other object
  and part unchanged; 1113 failed text plans left the file untouched.
- Earlier laws (transform identity, inverse, untargeted, sequencing) and
  the projection equivalence over the corpus pass unchanged in kind.
- The 19 task plans apply unchanged and pass `pptxbench grade` with no
  unintended change, hazard or wrong object; `pptxbench selftest` is as
  expected. A smoke set (`<deck>--source.pptx` and one output per
  property: autofit modes and scale, vertical text, columns, body
  flags, spacing, indents and tabs, bullets, strike, baseline, caps,
  letter spacing, highlight, hyperlinks shared and new, language, alt
  text, hidden, txBox, connector noFill, a pdraw-style text box) was
  written from the PowerPoint-authored `connectors.pptx` and
  `spade-standards-roadmap.pptx` for `pptoracle smoke --all`; not yet
  opened in PowerPoint.

## Paths and groups (M4)

Settled while building freeform paths and the group operations,
2026-11-13 (M4 pulled forward). None of these was checked against
PowerPoint; the outputs are ready for `pptoracle smoke --all`. This
supersedes "custGeom is read-only until M4" under Codecs.

### Paths: grammar and mapping

- `path` takes the SVG subset of Primitive model: M L H V C Q Z, absolute
  (upper case) and relative (lower case), numbers separated by spaces or
  commas, SVG's implicit repeats (extra pairs after M are lines).
  `derive.ParsePath` parses it; each command becomes one DrawingML
  command: M `a:moveTo`, L `a:lnTo` (H and V are lines from the current
  point), C `a:cubicBezTo`, Q `a:quadBezTo`, Z `a:close`. A (arcs) is
  refused with a message to approximate arcs with cubic curves, S and T
  (smooth curves, no DrawingML counterpart) with one to give the control
  points with C or Q. A path must start with M.
- Coordinates are what the read view shows: the plan's units, relative
  to the slide-space box's top-left before rotation and flips, scaled by
  enclosing groups. The read now scales each axis by the placed map's
  axis scale (`derive.AxisScales`), which is the box/extent ratio and
  stays defined when an extent is zero (horizontal or vertical
  polylines).
- `path` is a string for one a:path with the default modes (fill norm,
  stroke on), else an array of `{"d", "fill", "stroke"}` objects, one per
  a:path, with only the modes that differ (`fill` is ST_PathFillMode,
  `stroke: false`). This replaces the `geom.path.modes` omission: fill
  and stroke modes are now read-modelled and writable; `extrusionOk` is
  not shown (it has no 2D meaning) and is carried. The SVG view carries
  the array as `data-paths` beside the combined `d`. A custom geometry
  whose paths have no commands shows no path (omission
  `geom.path`); one with `arcTo` still omits the path.
- Writing: paths follow the transform (fields order: geometry, transform,
  endpoints, glue, then path and custom, then fill and line), so a box and
  a path in one operation give exactly that path. Setting `box` alone
  stretches the path with the box, as in PowerPoint. Each written a:path
  keeps its own coordinate space (w, h) when the new points, rounded to
  whole units there, read back as shown; otherwise it takes the shape's
  stored extent, where points are whole EMU of the shape's coordinates.
  A rewritten path keeps its start tag (attributes in order; w, h, fill,
  stroke set only when they change; `fill="norm"` is never written, it
  is the default), and every command whose literal points are unchanged
  keeps its bytes. Writing back what is shown is a no-op (the round-trip
  rule decided in the shown form).
- Formula guides stay read-only. A path whose points refer to guides is
  shown evaluated; writing a different path stores literal points and
  keeps the guides (now unused by the path, still used by sites or the
  text rectangle if they were). `custom` (below) shows the guides.
- New elements go through two edit-layer additions: `Handle.ReplaceChildren`
  (a placed insert in `schema.go`: replaces the children of a list
  element, `a:pathLst`, `a:cxnLst`, `a:ahLst`, `a:avLst` or `a:gdLst`,
  checked against the generated table) and `Ensure` for the lists
  themselves. The generated schema table gained `CT_Path2DList`,
  `CT_Path2D`, `CT_AdjustHandleList`, `CT_ConnectionSiteList`,
  `CT_ConnectionSite`, `CT_XYAdjustHandle`, `CT_PolarAdjustHandle` (same
  pinned zip and SHA-256).

### custGeom defaults and the custom field

- A preset becomes a custom geometry when a path is written (object.set
  `path` on a prstGeom shape, or `geom: "custom"` with `path`): Ensure
  replaces `a:prstGeom` by `a:custGeom` in the same choice slot. The new
  custGeom is `<a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="l"
  t="t" r="r" b="b"/><a:pathLst>`. PowerPoint writes all four lists, empty
  for freeforms without handles; pdraw writes the same empty lists. For
  the rectangle the corpus has two forms: `l="0" t="0" r="r" b="b"` (1339
  occurrences, 1244 of them in one PowerPoint 12 deck) and `l t r b` (117);
  every PowerPoint freeform with connsite guides (66) uses `l t r b`, and
  both mean the whole box, so new geometries use it. pdraw's literal
  `0 0 cx cy` means the same box at its size but does not follow a
  resize. Writing a preset (`geom`) over a custom geometry is the
  explicit switch back; adjust values of a preset are not carried into a
  custom geometry or back.
- An existing custGeom keeps its avLst, gdLst, ahLst, cxnLst and rect
  when only the path changes.
- `custom` (shown in `--detail full`, writable on object.set and
  object.add) holds the other lists: `av` and `gd` as `[name, formula]`
  pairs and `ah` as handles (`{"ahXY": {attributes}, "pos": [x, y]}`), all
  as written; `cxn` as `[angle, x, y]` sites and `rect` as `[l, t, r, b]`,
  where a literal coordinate is a number in the view's units relative to
  the box (scaled like the path; sites and the rectangle are in the
  shape's own coordinates, not a path's) and a literal angle is degrees,
  while guide references stay strings. A list's key is present exactly
  when the list is written, even empty.
- Writable: `cxn` (connection sites can be set from the plan; names are
  written as given and checked as ST_AdjCoordinate/ST_AdjAngle, numbers
  become whole EMU and whole 60000ths) and `rect`. Read-only: `av`, `gd`
  and `ah` (formulas and handles the tool does not evaluate for writing):
  they are written back as shown, and only empty lists may be added or
  removed. A key left out keeps the list shown (as other fields do).
- Paths go on shapes and pictures (a picture is clipped to it). A
  connector is drawn by its preset, so a path on one is refused; an open
  polyline is a shape with `fill: "none"`, arrows through the existing
  line head and tail, as pdraw draws it.

### Groups

- `group.create {targets, name, alias}`: the targets must share one
  container (the slide or one group) and are units as for reorder (an
  object alone in its mc:AlternateContent moves with it). The new
  `p:grpSp` (template trimmed from PowerPoint 16's
  `sd/qa/unit/data/pptx/group.pptx`, provenance in `internal/pptx/ops/group.go`)
  goes where the frontmost target was; the targets move into it in their
  paint order with their bytes unchanged. Its frame is the union of the
  targets' `derive.BoundsBox` (the group-frame hazard's rule) widened to
  whole EMU, with chOff and chExt equal to off and ext, as PowerPoint
  writes a new group, so every child keeps its stored transform and its
  slide-space box exactly, and ids (hence glue) are unchanged. `name` is
  required, as on object.add. Refused: targets in different containers,
  a target without a transform (ErrNotDerived), and a target that takes
  its group's fill (`a:grpFill`, ErrInexpressible: a new group would
  change it).
- `group.ungroup {target}`: the children move to the group's container
  at the group's place in the paint order, and the group is removed. Each
  child's transform is baked through `derive.Place` (under the group) and
  `derive.Unplace` (under the container), rounded to whole EMU; a child
  whose stored values do not change is not written (no transform is
  inserted for an inheriting placeholder under an identity group).
  `prop.Bake` checks every corner of the result against the child's
  current image: more than `prop.BakeTolerance` (0.005 pt, half of what
  the views show) fails with ErrInexpressible. That refuses children whose
  image under the group is a slight skew (accepted on read within
  `derive.SkewTolerance`; ungrouping would square them) as well as true
  skews. Also refused with ErrInexpressible, because removing the group
  would change what is drawn: a hidden group, group effects or 3D
  (`a:effectLst` with content, `a:effectDag`, `a:scene3d`), a child that
  takes the group's fill (`a:grpFill`), and a child without a transform
  under a group that moves it. Read-only children (graphic frames, ink)
  may move with the group only when their transform does not change
  (ErrReadOnlyObject otherwise). Groups inside mc:AlternateContent are
  refused (ErrBranch).
- `group.fit {target}`: the child space becomes the children's bounds
  (the same rule, in the group's child space, whole EMU) and off/ext the
  image of those bounds under the group's current map (rotation and flips
  kept), so the map from child space to slide space is unchanged up to
  rounding the frame to whole EMU; the operation fails with
  ErrInexpressible if any child would move by more than
  `prop.FitTolerance` (2 EMU). A frame that already fits writes nothing.
- `object.add` takes `parent` (a group, its front); the box stays in slide
  space and the transform codec inverts the group map. `object.move` and
  `object.set` on group children were already slide space.
- Group-frame hazard behaviour is the design's: a child moved or added
  outside its group's frame produces `group-frame` in the edit result;
  nothing refits implicitly; `group.fit` is the explicit fix.
- Edit layer: `Workspace.GroupObjects` and `Workspace.Ungroup` are placed
  inserts in `schema.go`; anchors `InFront(g)`/`InBack(g)` place new
  objects inside a group; `Workspace.Peek` gives a handle that declares
  nothing (reading objects an operation does not write), and
  `Workspace.Children` lists a group's direct children.

### Coverage and laws

- pdraw parity (coverage.md): 145/185 items (78.4%) before, 167/185
  (90.3%) after; read-modelled 162 → 168, settable 126 → 148. All 22
  geometry items that failed (the custGeom lists, path, w, h, moveTo and
  lnTo with their points, ahLst, rect and its attributes) now have
  parity. The headline: settable by plans 7.0% → 8.1% of items, 18.1% →
  38.6% of corpus weight, 21.6% → 70.0% of object weight (custom path
  points are most of the corpus's object weight).
- Operation matrix: object.set `path` and `custom (cxn, rect)`
  implemented for shapes and pictures, refused for connectors and groups;
  object.add `path` and `parent`; group.create on every kind (a read-only
  object moves without being written), group.ungroup and group.fit on
  groups.
- Laws (`internal/pptx/laws/m4_test.go`; M3 identity and codec/view pairing now
  include `path` and `custom`). Corpus run, 2026-11-13, 1024 native decks:
  - M3 laws with paths: codec and view agree on 1547 paths and 1619
    `custom` values; identity (41112 fields written back as shown, paths
    and custom included) left every deck's bytes and mtime unchanged.
  - Path rebuild: of 1619 custom geometries, 1447 own paths with literal
    points were built again from what the EMU view shows, through the
    write path: 1446 byte-identical; 1 differs only in the whitespace a
    pretty-printed deck had between commands (LibreOffice
    tdf156649.pptx). Built fresh, without the original to lend start tag
    and command bytes, 51 are identical (PowerPoint's own form; the rest
    differ in w and h, which the corpus often writes in another space).
    Skipped: 72 with arcTo, 110 whose points use formula guides, 3
    inherited.
  - Inverse: 50 jittered paths read back as set (3 ErrInexpressible,
    under skewed or rounding group spaces); 1449 preset → custom → preset
    switches read back (6 ErrInexpressible). Untargeted: 168 path, ungroup
    and fit plans changed no other object or part. Atomicity: 29 plans of
    a path set followed by a failing operation left the file untouched.
  - create then ungroup on 940 adjacent sibling pairs: every object shown
    identically, 920 slides byte-identical, 18 differ only in whitespace
    between objects (pretty-printed decks: the pair is regrouped without
    the whitespace between them) and 2 in object order (pairs whose
    elements are not adjacent in the document); refused 6
    (ErrInexpressible: a target takes its group's fill, or the container
    group is degenerate) and 2 (ErrNotDerived: no transform).
  - ungroup of all 413 groups: 344 done (287 shown identically, the rest
    within 0.01 pt, the views' rounding); refused 65 ErrInexpressible
    (squared skew, grpFill children, effects, children without
    transforms) and 4 ErrReadOnlyObject (a graphic frame that would
    move). fit of every group: 412 done (63 changed the frame; no child
    moved by more than 0.01 pt as shown), 1 ErrInexpressible.
- Author and Freeform self-check: `eval/plans/au-*.json` and `ff-*.json`
  were written from `pptx read` alone and apply with `pptx apply`; the
  model checks of all five graders pass with no unintended change and no
  hazard (rubrics, judged on renders, stay manual). The 19 editing plans
  still pass.

## Coverage tracker

Added 2026-11-13 so that coverage is measured, not claimed
(`internal/pptx/coverage`, `tools/coverage`, output in `eval/coverage`).

- Universe: every element and attribute path below the object kinds
  (`p:sp`, `p:cxnSp`, `p:grpSp`, `p:pic`, `p:graphicFrame`,
  `p:contentPart`) and the slide-level context the read model reads
  (slide, layout and master colour maps, master text styles, slide size,
  default text style, theme colour, font and format schemes), generated
  from `pml.xsd` and `dml-main.xsd` in the schema zip `gen_schema.go`
  reads (same pin, same SHA-256) by `go run ./tools/coverage --xsd zip`.
  Each item records its XSD type, choice, occurrence bounds and
  enumeration. Caps: a type is not expanded below itself; objects inside
  groups are their own roots; transitions, timing and the slide
  background are not expanded; and value types (the six colour types,
  gradient and pattern fill bodies, effect containers, 3D, list-style
  levels, blips) are enumerated once under a pseudo-root `(type)`, since
  one function reads each wherever it is read. Without the last rule the
  universe has three million paths.
- Registry (`registry.go`): claims per reader and codec, by path pattern,
  with a read status (read-modelled, preserved-raw as raw XML,
  omitted-with-reason, non-goal) and a write status (round-trip-law,
  write-codec, preserved-raw when carried by the codec that rewrites the
  element, non-goal). Facets mark per-value capabilities read from the
  code's own tables (connection sites, connector endpoints) and which
  settable enumerations refuse values outside the schema.
- Verification, in `make check`: every probeable path is set to two
  values (or absent and present) in a probe deck, in up to three
  contexts, and the full JSON view compared; a read claim must match the
  outcome exactly in both directions, value by value for enumerations.
  Write claims run a plan per value and check the XML, then write the
  shown value back and require no change. Carry claims run each codec's
  rewrite with the path set. The operation matrix runs every cell's plan.
  A further test fails when the committed `coverage.json` or
  `coverage.md` is stale; corpus counts live in `corpus.json`, refreshed
  only with `--corpus`.
- First findings: the geometry, line and colour codecs write any value as
  given, including values outside the schema enumerations (preset names,
  dashes, caps, arrows, scheme, preset and system colour names); the text
  grammar refuses them. Modifiers on system and preset colours fail (the
  edit schema table holds only `CT_SchemeColor` and `CT_SRgbColor`).
  Body insets, anchor and wrap are read but not writable, although
  "Properties" lists them as writable. Graphic frames, read-only in the
  design, accept name, transform, move, delete, duplicate and reorder.
- pdraw parity (2026-11-13): the requirement is at least parity with the
  sibling project pdraw for the styles pdraw uses. `eval/parity/build.sh`
  builds pdraw's CLI (read-only use of its repo; cargo's target directory
  only) and runs `pdraw create` over its skill examples (positioned,
  method, branching, communication, leader, loss lanes), the benchmark's
  positioned branching example, `eval/parity/examples/vocabulary.pdrawml`
  (dashed boxes, bottom and top labels, arrows at both ends, elbow and
  straight connectors, plain and arrowed polylines, which the upstream
  examples leave out) and a PNG and an SVG image, into a scratch
  directory, recording pdraw's commit (`-dirty` if its tracked files have
  uncommitted changes). Decks are never committed.
  `go run ./tools/coverage --profile <dir>` counts those decks with the
  corpus counter, restricted to objects, into `pdraw-profile.json`: every
  universe path and enumerated value pdraw's output uses, with counts.
  pdraw's emitter has no roundRect and no object groups (only the
  spTree root), so neither is in the profile; the profile is what it
  does emit, not the vocabulary one might expect. Without `--profile`
  the profile is read as committed, so `make check` needs no pdraw build.
  The "pdraw parity" section of `coverage.md` (and `parity` in
  `coverage.json`) looks every profile item up in the registry: parity
  needs read-modelled and settable (round-trip-law or write-codec), the
  headline gives both as counts and percentages, and the items that fail
  are listed by area (geometry, line, fill, connector glue, text body,
  paragraph, run, picture, group, non-visual properties). Derived items
  (object roots and shared-type roots: strongest status of their
  content) are listed but not counted. Exemptions from the settable
  requirement are explicit in `ParityExemptions` and kept small: shape
  ids (allocated, referenced by glue), relationship ids, the SVG
  extension list, wrapper elements without a value of their own (their
  attributes and children are separate items), and the proofing `dirty`
  flag (also exempt from reading). Transform children (`a:off`, `a:ext`,
  `a:chOff`, `a:chExt`) need no exemption: the transform codec settles
  them at their parent and the registry marks them settable. Tests fail
  when the committed report is stale against the registry or the profile,
  when the profile names items the universe lacks, when a path has no
  area, and when an exemption matches nothing.
- Fixed 2026-11-13 (each described where it now lives under M3): values
  are validated at compile against tables generated from the same
  schemas ("Schema values"); all six colour types take modifiers
  (CT_SystemColor, CT_PresetColor, CT_HslColor and CT_ScRgbColor
  tabulated); `phClr` is refused as a slide colour; body insets, anchor
  and wrap are writable ("Codecs"); graphic frames and ink refuse every
  write ("Operations"). After the fixes the headline moved: settable by
  plans 3.8% → 7.0% of items (17.7% → 18.1% of corpus weight, 21.0% →
  21.6% of object weight); settable or carried 28.4% → 31.9% (45.3% →
  45.9%, 86.2% → 87.3%); the gap 51.0% → 50.6% (30.5% → 30.3%, 3.9% →
  3.4%). Every settable enumerated attribute is now validated (no
  "**no**" rows: `prstGeom/@prst`, `prstDash/@val`, `ln/@cap`,
  `headEnd`/`tailEnd` `@type`/`@w`/`@len`, the scheme, system and preset
  colour names, `bodyPr/@anchor`, `bodyPr/@wrap`), and `bodyPr` anchor
  and wrap went from 0 to all values settable. `phClr` is the one scheme
  value not settable (24281 occurrences, all in theme style matrices).
  In the operation matrix the six "other" cells that were implemented
  (name, transform, move, delete, duplicate, reorder) are
  refused-by-design, and a row for `object.set` `text` (anchor, wrap,
  insets) is implemented for shapes.
- Text formatting, body settings, non-visual properties and connector
  fill (2026-11-13, "Text writes (M3)"): every pdraw parity item in the
  text body, run, non-visual and fill areas now has parity
  (`bodyPr/a:spAutoFit`, `rPr/@lang`, `cNvPr/@descr` on shapes and
  connectors, `cNvSpPr` and `@txBox`, `p:cxnSp/p:spPr/a:noFill`): parity
  145 → 152 of 185 items (78.4% → 82.2%; paths 116 → 123 of 156, 74.4%
  → 78.8%; read-modelled 162 → 169, settable 126 → 132). What still
  fails is geometry (`custGeom`) and pictures, other work. The overall
  headline: read-modelled 16.4% → 17.9% of items (57.2% → 58.9% of
  corpus weight, 78.5% → 82.6% of object weight); settable 7.0% → 8.2%
  (18.1% → 19.5%, 21.6% → 25.1%); settable or carried 31.9% → 31.9%
  (45.9% → 45.9%, 87.3% → 87.4%); the gap 50.6% → 50.1% (30.3% → 29.9%,
  3.4% → 2.7%). The operation matrix gained rows for connector `fill
  none`, the further body properties, `descr`/`title`/`hidden` and
  `txBox`. The probe fixture's slide relationships are now read back
  with the slide, so a hyperlink's new relationship survives the
  identity check; bullet sizes are probed in their percentage-string
  form. A tab's alignment and a field's language are not claimed
  read-modelled: a tab shows only with its position, which the probe
  does not set beside it, and an empty field shows no language.

## Shared engine (P2)

Settled 2026-11-13 while lifting the format-neutral parts of the write
path out of `internal/pptx` so that the docx side builds on the same
engine. pptx outputs are byte-for-byte unchanged (see each part).

### Schema tables per vocabulary

- `internal/ooxml/schema` holds the generated tables, one file per
  vocabulary: `pml_table.go` (prefix `p`), `dml_table.go` (`a`),
  `wml_table.go` (`w`) and `shared_table.go` (`s`, the shared common
  simple types). Each has the vocabulary's child orders (`Slot`s of
  `Child`ren), the attributes of its tabulated types and their
  children's types, and the simple types those attributes use, each in
  the table of its own prefix. Lookups dispatch on the prefix: `Slots`,
  `Find`, `ChildType`, `ChildOrder`, `AttrType`, `SimpleType`, `Enum`,
  and the value checks `CheckAttr`/`CheckValue` (`ErrSchema`), moved
  from the edit layer. The package imports nothing format-specific.
- `gen_schema.go` (`//go:build ignore`, run from the package directory
  with the zip; it writes the four files) keeps the pinned zip and its
  SHA-256 check; `schema.ZipSHA256` and `schema.ZipURL` are the pin the
  coverage universe uses too, and a test keeps the generator's constant
  equal to them.
- pml and dml tabulate the same fixed list of types as before
  (`wantedPD`). The regenerated tables were compared with the deleted
  `internal/pptx/ops/edit/schema_table.go` by a one-off test that loaded
  both: the 46 complex types, the attributes of 78 types and the 72
  `a:` simple types are identical, the 11 `s:` types the old table held
  are identical in `shared_table.go` (which has 24 now, the rest used by
  wml), and the four object roots the old table listed (`p:sp` is
  `p:CT_Shape`, ...) equal what the schema gives them as children of
  `p:CT_GroupShape`, which is where the edit layer now reads them.
  `go run ./tools/coverage --xsd zip` regenerates the coverage files
  unchanged (7,213 universe items).
- wml tabulates every complex type of `wml.xsd` (282 of 285), so the
  docx side starts from the whole vocabulary. The generator learned
  what wml needs: complex-content extensions (the base type's content,
  then the extension's), element references into schemas that are not
  tabulated (`m:oMath`, `wp:inline`: the child keeps its prefix and has
  no type), a choice of one element (that element, repeated if either
  repeats), sequences and repetitions inside a repeating choice
  (flattened into its alternatives, whose order is free anyway), and
  repeated sequences of `xsd:any` (foreign VML content: nothing to
  place). Content the slot model cannot express exactly is not
  tabulated; the reason is in `wmlUntabulated` (`schema.Untabulated`):
  `w:CT_DocPartPr` (`xsd:all`), `w:CT_RunTrackChange` (a group from the
  math schema) and `w:CT_Tbl` (`w:bookmarkStart` and the other range
  markup appear both before `w:tblPr` and among the rows, so a name does
  not fix a position). The same checks run over the pml and dml types
  and none trips. wml attributes are qualified in documents (`w:val`)
  and tabulated by local name.
- The pinned transitional schema makes run properties (`EG_RPrBase`) a
  repeated choice, so the table fixes no order among them (paragraph
  properties are a sequence); Word writes them in the ECMA-376
  first-edition sequence. The strict schema (5th edition) has the same
  repeated choice, so no schema fixes the order; see "Engine additions
  (P4a)" for how new run properties are placed.

### Plan transactions

- `internal/plan` holds the transaction machinery both formats apply
  plans through, lifted from `internal/pptx/ops` and `internal/pptx/apply`
  unchanged in behaviour:
  - `Tx`: `Begin(path, open, opts...)` opens the package and builds the
    format's workspace over it with `open`, the only other holder of
    the package; `Run(name, fn)` runs one operation, and a failure
    poisons the transaction (`ErrTxFailed`); `Snapshot`, `Commit`
    (through `opc.Package.SaveAndClose`: atomic, nothing written for a
    no-op), `CommitTo`, `Rollback`; `WithIDs` and `IDs` carry the
    `ids.Source`.
  - `WriteSet`: the parts, targets and containers a plan declared, and
    `Verify(pkg, ws, units)`, the commit-time verifier, over a format's
    `Units` (`HasUnits`, `IsUnit`, `Describe`). The pptx edit layer's
    `Workspace` records into a `WriteSet` and its `Verify` passes the
    slide units (shape tree, objects, object-level
    `mc:AlternateContent` and branches) as before.
  - `Finish(opt, build)`: snapshot to a temporary file, let the format
    build the edit result from it through its read model, then commit
    to `opt.Out` (or in place) unless `opt.DryRun`. `Options` is the
    dry run, the id source and the output file.
  - `Result`, the envelope (`dry_run`, `saved`, `aliases`, `changed`,
    `deleted`, `hazards`, `parts`) with `JSON(head...)` for the format's
    leading keys, and `Hazard` with `MarkNew`. `JSON` now returns the
    encoder's error instead of dropping it.
- pptx keeps on top: its operations (`ops.Op`, still implementable only
  inside `internal/pptx/ops`; `ops.Tx` wraps a `plan.Tx` and adds the
  media bookkeeping), the plan JSON and its schema, the slide units, and
  the hazard kinds (`stale-connector`, `dangling-glue`, `group-frame`)
  from the read model; `apply.Result` is the slide and units followed
  by `plan.Result`. `ops.ErrTxDone`, `ops.ErrTxFailed` and
  `edit.ErrUndeclared` are the plan errors; their messages now start
  with `plan:`, as do the commit and snapshot wrappers.
- `internal/archtest` names `internal/plan` as the one package besides
  `internal/opc` that saves a package, and fails when the shared engine
  depends on a format.
- Byte identity, 2026-11-13: `pptx` built from main (6f00f49) and from
  the P2 branch, run over the same plans with `--seed 0 --clock
  2000-01-01T00:00:00Z -o`, on the 3 fixtures and the 1026
  PowerPoint-native corpus decks on disk: the 24 `eval/plans` on their
  task decks, the 3 `FuzzPlan` seeds on each fixture, and generated
  plans on each deck's first slide with objects (add a shape and a
  glued connector, the same as a dry run, move, move and back, set name
  and rotation, set fill and line, delete, duplicate, reorder, group,
  `text.set`, `text.replace`). 10835 runs, 8377 succeeding (7357
  writing a deck) and 2458 failing: exit code, stdout (the edit
  result), stderr, the output deck's SHA-256 and `pptx read --json` of
  the output (slide list, and the slide with `--detail full`) were
  identical in all 10835.

## docx integrity (P4, pulled forward)

`internal/docx/integrity` ports docxdriver 0.5.2's integrity checks with
the same findings (docs/docx-integrity.md lists the measured parity and
every difference). Intended differences from docxdriver, decided
2026-11-13:

- No round-trip check. docxdriver refused an edit when a part it rewrote
  did not survive its reader and writer unchanged, because it
  re-serialised whole parts. The Go engine edits a lossless tree, copies
  untouched spans and members byte for byte, and verifies the declared
  write set at commit (`plan.WriteSet`), so untouched content cannot
  change; the check and its hook are dropped.
- The commit gate is scoped: `integrity.Gate` checks the changed parts and
  their cross-referenced partners in both packages, and gives the same
  answer as comparing full checks (`TestGateMatchesFullCheck`). Full
  checks stay for comparing inputs with the oracle (`integrity.Check`).
- Expected content types come from `internal/ooxml/ctypes`, not a hand-kept
  list; the entries docxdriver had and the table lacks are listed in
  docs/docx-integrity.md. ctypesgen keeps types two Office-authored files
  use (three dropped commentsExtensible; one would admit a rewritten test
  file's application/xml settings and notes parts).
- One schema source: the content model integrity checks (ranks, counts,
  required children and attributes, wildcards) is generated into
  `internal/ooxml/schema` from the pinned zip, beside the slot tables;
  docxdriver's wml-model.json is not kept.
- Integrity reads parts with the engine's tokenizer, so it is not
  independent of the engine's parser; `make conformance` (lxml, Open XML
  SDK) is the independent check.
- Where docxdriver's checks are wrong (non-well-formed XML it accepted,
  attribute errors it named "position ", Rust character classes for
  NCNames, lax xsd:double and xsd:dateTime, unreachable zip checks), the
  Go checks are right; docs/docx-integrity.md gives the evidence, and the
  parity tests accept only the listed differences.

## Edit core and law harness (P4)

Settled 2026-11-13 so that the docx operations (P5) reuse the pptx edit
layer and law tests instead of re-implementing them. pptx output is
byte-for-byte unchanged (below).

### Package layout

- `internal/ooxml/edit` is the format-neutral edit core. It sits in the
  shared engine beside `tree` and `schema` because what it does is defined
  by the OOXML vocabulary tables, not by a format: it imports `tree`,
  `schema`, `opc` and `plan` and nothing format-specific (the archtest
  shared-engine rule covers it through `internal/ooxml/...`). A separate
  top-level `internal/edit` was considered and rejected: it would be a
  sixth engine root for the same rule, and the core is the one consumer
  of the schema tables' child orders.
- What the core holds, each parameterised by the caller:
  - `NS` (a namespace family: transitional and strict URIs, the prefix a
    new element takes, the schema table prefix), `Name`, `Path`, `Vocab`
    (the families a format writes; `Remap` maps a fragment's transitional
    URIs to the member in scope). `REL` (relationships) is the one family
    the core spells, shared by every vocabulary.
  - `Format`: the vocabulary, the verifier's `plan.Units`, `Type` (the
    complex type of a handle's element: `p:sp` is `p:CT_Shape`, the docx
    side will give `w:p` as `w:CT_P`) and `Bound` (elements a path may
    not step onto: another object).
  - `Workspace`: the write set keyed by part (`DeclarePart`,
    `DeclareContainer`, `Target`/`Peek` returning Handles over the branch
    elements a format resolved), `Verify` through `plan.Verify`, and
    `RawSetAttr` (declaring the nearest enclosing unit).
  - `Handle`: `Find`, `FindIn`, `Elem`, `Branches`; attribute edits
    (`SetAttr`, `RemoveAttr`, `ApplyEach` with `AttrEdit`, `SetAttrAt`
    and `RemoveAttrAt` with a `Locator` for elements a fixed path cannot
    name, `SetChildAttr`, `SetRelAttr`, `SetChecked` which checks the value
    with `schema.CheckAttr` on the type the path reaches); removals
    (`Remove`, `RemoveChildren`, `RemoveExt`); and the schema-placed
    inserts: `Ensure`/`EnsureEach`, `AppendEach`, `AppendExt` (an
    extension list named by the caller: `a:extLst`, `p:extLst`),
    `ReplaceChildren` (a list's content) and `ReplaceRun` (the run of one
    repeated child under a parent, placed by the schema when the run is
    empty: the generalisation of `ReplaceParagraphs`). Every method applies
    to every branch or to none.
  - Unit placement: `Position` (made only by `SlotEnd`, which reads the
    schema's child order of a container type, and by `Before`, `After` and
    `JustAfter`, next to an existing element), `Parse` and `Copy` (detached
    units), `Insert` (with `NodeAttr` assignments on the detached unit's
    own elements, such as a new id), `Move` and `Delete`; each declares
    the containers it changes (and, for `Delete`, the unit).
  - The codec machinery: `Codec[H, T, R]` (a format's handle type, the
    value type and its reading type with `Get` and `Reason`), `Set` (the
    round-trip rule) and `Check`, each taking the format's
    "inexpressible" error so that messages and `errors.Is` stay the
    format's.
- `internal/pptx/ops/edit` is now the pptx layer: slide addresses (`Ref`,
  `Alias`, `Resolve`, `ResolveIn`, `ResolveGlue`, `NextID`), the slide
  units and object kinds, mc:AlternateContent counterparts (which branch
  elements a handle covers), `Anchor` and the object-level wrappers
  (`InsertObject`, `MoveObject`, `RemoveObject`, `DuplicateObject`,
  `GroupObjects`, `Ungroup`, `ReplaceParagraphs`), the slide reads
  (`ParentMap`, `Chain`, `Groups`, `ColorCtx`), relationships (`PartRels`)
  and the pptx names (`P`, `A`, `Q` over the core's `Name`; `Path`,
  `Name`, `AttrEdit` are aliases). Its `Handle` embeds the core's.
  All of that knows slides, shapes or `p:`/`a:` specifics; none of it
  mutates a tree itself any more.
- `internal/pptx/ops/prop` keeps the pptx codec table; `prop.Codec[T]` is a
  generic alias of `core.Codec[*edit.Handle, T, derive.Val[T]]` and
  `prop.Set` passes `derive.ErrInexpressible`.

### Architecture test

- Tree mutators are allowed only in `internal/ooxml/tree`,
  `internal/opc`, `internal/ooxml/edit` and the eval oracle (and in the
  tests named before, plus pptx edit tests, which make undeclared
  changes on purpose). `internal/pptx/ops/edit` no longer has the
  exception: the rule is tightened to the core.
- Element insertion in the core only in `place.go` and `unit.go`.
- `internal/laws` is shared engine (it may not depend on a format), may
  install save faults (`opc.SetFaultHook`), may use seeded randomness and
  the clock for its private copies (determinism exception), and is
  imported only by `_test.go` files.

### Law harness

- `laws.Suite[V]` is one law suite as a table entry: `Read` (every target
  of a document, `Target{Part, Addr}`, with its value as shown), `Show`,
  `Set` (the operation that sets a value; operations are opaque
  `laws.Op`), `Vary`, `Apply` (a plan, in place or to another file),
  `Refused` (the format's documented refusals), `Units` and `Owns` (which
  units of the target's part may change), `Fail` (an operation that
  fails), optional `Undo` (an operation and its exact inverse) and
  `Sticky` (targets whose undo pair restores only what is shown), and a
  per-document `Budget`.
- `Check` runs the five laws on a private copy (`laws.Private`, its own
  directory, mtime two days old): identity (all targets written back as
  read in one plan: bytes and mtime unchanged); inverse (a varied value on
  one target reads back, or is refused); untargeted (`laws.Untargeted`:
  every other part byte for byte, and in the target's part the own bytes
  of every unit not owned, nested units masked, plus the document outside
  its units); atomicity (a failing operation at every position of a plan
  of accepted changes, and a fault at each save stage before the rename,
  first and second occurrence, leave the file and its directory as they
  were; a fault after the rename is not reported); sequencing (undo pairs
  write nothing, `[a, b]` equals `[a]` then `[b]` part for part).
- pptx entries (`internal/pptx/laws/laws_test.go`): `transform` (box,
  rotation, flips through `object.set`; undo through `object.move`;
  inheriting placeholders sticky), `name` and `hidden`. The transform
  suite keeps two pptx-only checks beside the harness: the codec shows
  exactly what the view shows, and the in-memory inverse over every
  object (children before groups) passes the verifier. The atomicity
  tests of `internal/pptx/ops` moved into the harness; the verifier's
  rejection of an operation that changes what it did not declare stays
  there (it needs a test operation only that package can define). The
  M3, M4, picture and text suites still check operations the harness
  does not model (add, delete, duplicate, group, media, text), through
  its `Private` and `Parts`.
- An object id several objects share (`s40~2`) owns every unit with that
  id in the untargeted law: units are not numbered as the views number
  objects (the old per-object comparison keyed objects by view id and
  had no such gap; the harness trades it for comparing every unit and the
  part outside them).

### How the docx operations sit on the core

- `internal/docx/ops/edit` (or a file of `internal/docx/ops`) builds one
  `core.Format`: `Vocab{W, R, ...}` with `w` tabulated, `Units` for the
  body (the body, block-level `w:p`, `w:tbl`, `w:sdt`; header, footer,
  comment and footnote parts likewise), `Type` (`w:p` is `w:CT_P`,
  `w:tbl` would be `w:CT_Tbl`, untabulated today: see the P2 notes) and
  `Bound` (another block). Its resolver turns the plan's addresses
  (paragraph indices, bookmark names, comment ids) into elements and
  calls `Workspace.Target`; a docx handle embeds `*core.Handle` as the
  pptx one does, with one branch (mc:AlternateContent in WML is rare and
  can be refused, as nested pptx branches are).
- Paragraph and run properties go through `Ensure` and `SetChecked`;
  run-property order is not in the transitional table (`EG_RPrBase` is a
  repeated choice), so the docx side supplies that order from
  Word-authored files rather than through `Ensure`.
- Inserting and deleting paragraphs and tables is unit placement:
  `SlotEnd(body, "w:CT_Body", W("p"), ...)` is the end before
  `w:sectPr`; `Before`/`After`/`JustAfter` place next to an existing
  block; `Delete` removes one. Replacing a paragraph's runs is
  `ReplaceRun(nil, W("r"), ...)`.
- Each docx property or operation lands as a `laws.Suite` entry whose
  `Read` goes through the docx read model, as the pptx ones go through
  the views.

### Engine additions (P4a, 2026-11-13)

- **Created-node record.** The core records every element it creates
  (Ensure, the placed inserts, Insert, the structural verbs) per part:
  `Workspace.Created`, `CreatedIn`, `Handle.Created`. It is for write-set
  verification and id assignment only, never for choosing which
  revisions to accept: isolating a span splits runs and revision
  wrappers, so a created element can carry another author's existing
  revision.
- **Structural verbs** (`structure.go`, an archtest place file), each on
  an element strictly inside a handle's branch and outside any bound
  unit (`ErrOutside`): `SplitAt` (copying chosen property children such
  as `w:rPr` to the new half), `SplitText`, `Wrap`, `Unwrap`, `Rename`
  (same namespace; `tree.Node.Rename`), `SetText`, `SetNodeAttr`,
  `RemoveNode`. `Into` places at the end of a list in a vocabulary the
  schema does not tabulate (next to its last element).
- **Write-set kinds.** `plan.WriteSet.DeclareRootNamespace` (a part root
  may gain `xmlns:p` and append declared prefixes to `mc:Ignorable`) and
  `DeclareRepair` (an element anywhere may gain or rewrite one identity
  attribute: a repaired `w14:paraId`, even in a part no operation
  targeted). Verify compares those start tags without the declared
  attributes (`tree.Node.StartTagWithout`) and checks the values instead.
  The core writes them with `EnsureRootNamespace` (docxdriver's
  `ensure_prefix_declared`, which refuses a prefix bound to another
  namespace instead of writing into it), `Repair`, and `SetCreatedAttr`
  (ids on created elements only).
- **docx units** (`internal/docx/edit`): the body; every `w:p`, `w:tbl`,
  `w:tr`, `w:tc`; `w:sectPr` outside `w:sectPrChange`; a `w:sdt` not
  inside a paragraph, with its `w:sdtContent`; in the main document,
  header and footer parts (other parts are declared whole). `Type` gives
  the schema type by walking up to a paragraph, row, cell, body or part
  root (`w:CT_Tbl` is untabulated; its own children are fixed by name);
  `Bound` is any unit.
- **Paragraph identity** is resolved once when the docx workspace opens
  (`edit.New(pkg, Options{Source})`), as the read model resolves it, and
  bound to the nodes; `Finish` (below) persists missing ids and gives a
  created paragraph without a unique valid id (a copy) a fresh one, so an
  original keeps its id wherever its copy lands.
- **One allocator** on the docx workspace, deterministic: revision ids per
  part (`RevisionID`, `RenumberRevisions` for cloned revisions),
  inserted paragraph ids from `sha256(source) | op index | slot` (slot 0
  for `insert_paragraph`, 1..n for table cells; the plan's bytes are not
  mixed in, unlike docxdriver, and the comparator relabels inserted ids
  by document order), comment ids, comment paragraph ids unique across
  every part, durable ids, bookmark ids across stories, `numId` and
  `abstractNumId` from the numbering part's working tree (no cache),
  relationship ids and part names.
- **Registry** (`Workspace.Registry`): tables, rows and cells bound at
  open by position and later by alias, refreshed after structural
  operations; paragraphs by bound id; aliases and groups; section
  handles; each paragraph's visible text at open for the "changed by an
  earlier operation" diagnostic.
- **Comment parts** are kept consistent as one unit: the four parts are
  found through the main document's relationships (docxdriver always
  used `word/comments*.xml`), missing ones are created with their content
  type and relationship, and removal touches only existing parts
  (docxdriver's delete wrote empty, unregistered parts).
- Content and relationship types for the comment, header and footer
  parts were already in `internal/ooxml/ctypes` from at least two
  Office-authored files each (commentsExtensible from two).
- **Run-property order.** The premise that the strict schema fixes the
  run-property order is wrong: in ECMA-376 Part 1 5th edition
  (`OfficeOpenXML-XMLSchema-Strict.zip`, SHA-256 73fab4b0...16a816f7,
  inside the edition's zip 9d0bcad9...fd464d4129, checked 2026-11-13 and
  not kept) `EG_RPrBase` is the same repeated choice as in the
  transitional schema, so neither fixes an order and any order is valid.
  No override table is generated. `Format.PropertyChoices` names the
  types whose repeated choice holds once-only properties (`w:CT_RPr`,
  `w:CT_ParaRPr` and their originals); Ensure places a new one by the
  alternatives' declaration order in the table (the order Word writes),
  keeps an existing one, and never reorders or flags existing children.
- **Pre-existing defects never block an edit.** Placement and the order
  tables only position new children. Elements the table does not know
  stay attached to the element before them and never stop a placement;
  with no known sibling the new child goes first. Placement refuses only
  a child the schema does not allow under that parent. Existing children
  out of order stay as they are.
- **Opening despite defects.** `opc.Open` refuses only a file that is not
  a zip or trips a safety limit (a missing main part is the format's
  refusal). A duplicate part name keeps the first entry and copies the
  rest through on save; a content types stream whose root is not Types
  reads as declaring nothing and is copied through byte for byte when the
  plan adds no part. An operation that must register a part checks
  `Package.RegistersTypes` and refuses up front, before any work; nothing
  fails at write time. Both defects are listed in `Package.Notes`.
- **Regions.** `Workspace.Range` (a run of one container's units),
  `Story` (a story container's content) and `Part` open a Handle whose
  verbs work across units; the write set is declared when the region is
  opened, from the operation's scope, never at mutation time. Moves
  (`PutBefore`, `PutAfter`, `Append`, `Prepend`) take only elements
  already in the region; new elements go through `Ensure`, `EnsureIn`,
  `PlaceNew` (schema-placed) or `ReplaceWith` (same name, same place).
  `Private(format, root)` is a region over a never-saved copy.
- **Paragraph joins.** `Workspace.JoinParagraphs(region, survivor,
  absorbed)` decides by `IdentityMoves` (absorbed has live content and
  survivor has none), the one rule the settled views use too; it moves
  the id binding and stored attributes, records the absorbed id so the
  registry refuses it (`Registry.Joined` names the survivor), and lists
  the join (`Joins`).
- **Persisting ids.** On a save that is not a no-op, `Finish` writes every
  paragraph id missing from the main document (untargeted paragraphs
  included: left unstored they would be re-derived from the changed
  bytes and agents' addresses would break; the one stated exception to
  leaving untargeted content alone). Stored malformed or duplicate ids
  are never rewritten; duplicates are addressed by position.

## docx read side (P3)

Settled 2026-11-13 with the port of docxdriver 0.5.2's read and find.

- `internal/docx/model` is the one read model: `Package` (every part read
  at open, like docxdriver, so a corrupt member fails the open; exact,
  case-sensitive part names over `opc.OpenBytes`), `XML` with docxdriver's
  name rules (elements by local name, a `w` check also accepting no
  namespace, attributes by local name whatever the prefix), paragraphs and
  their views, revision markers, `w14:paraId` resolution, segments,
  numbering (list labels and membership). `Document`
  bundles the package, the main document and the input hash and computes
  the shared derived facts once (paragraph ids, list markers and lists per
  view) for every view over it: read, find, and next the HTML
  projection.
- `internal/docx/read` is the read and find surface; its results are the
  CommandResult JSON docxdriver prints, written by its own encoder with
  serde_json's key order and escaping. Equations are LaTeX, the CLI's one
  equation format: `read.Options.Equation` (from `internal/docx/math`)
  converts them for paragraph entries (`equations[].latex`) and the HTML
  projection alike; there is no MathML on the docx side.
- `cmd/docx` follows docxdriver's clap grammar and error wording under its
  own program name, and its path rule (the input must lie inside the
  working directory, symlinks resolved).
- Parity: `tools/oracle` runs the pinned docxdriver CLI and `cmd/docx` on
  the same inputs; `tools/oracle/baseline/README.md` has the commands, the
  counts and the known differences (one docxdriver panic not copied).
- `internal/docx/view` is the HTML projection (docxdriver's `html/render.rs`,
  `surface.rs`, `ctx.rs`, `styles.rs`, `chrome.rs`, `compact.rs`,
  `assets.rs`), a third view over `model.Document`: sections, explicit
  headers and footers, body blocks with nested lists and tables, notes, the
  style cascade, agent markup (`AgentHTML`) and asset aliases. `read --kind
  document` (the default) and `document_ui` assemble its JSON in
  `internal/docx/read`. `AffectedMarkup` gives the per-paragraph block
  markup edit reports will carry.
- Equations: LaTeX is the docx CLI's one equation format. The projection
  renders `<equation display="inline|block" data-docx-equation="N">` with
  the LaTeX text-escaped (N: 1-based position among the paragraph's current
  equations, the address equation operations take), through an injected
  `view.EquationFunc` (`math.OMMLToLaTeX` in the CLI); no `<math>`. A
  document with equations and no converter is an error, never a silent
  omission. The converter needs no n-ary limit defaults from the settings
  part: an n-ary without `m:limLoc` is LaTeX without `\limits` or
  `\nolimits` and converts back without `m:limLoc`, so it keeps meaning
  "the document's default" both ways (docxdriver's MathML needed the
  defaults; `model.MathLimits` is gone).
- Equation losses (settled 2026-11-13). `math.OMMLToLaTeX` lists what the
  LaTeX cannot carry. Read never drops content silently: each equation
  whose LaTeX leaves out content (`math.LossContent`: tracked revisions
  inside the equation, hidden limits or degrees with content, anchors and
  other WordprocessingML inside it, unknown elements, ...) names it, sorted
  and without repeats (`view.ContentLosses`), in `data-docx-loss` on its
  `<equation>` (separated by `; `) and in `equations[].loss` (a list) in
  paragraph entries; both are absent when nothing is lost. Formatting
  losses (`w:rPr` fonts, size, kerning, colour, language, and `m:ctrlPr`)
  are not listed (planner decision 2026-11-13; corpus `w:rPr` inside
  `m:r`: rFonts 414, Word's Cambria Math, sz/szCs 33, kern 22, color 14,
  lang 8, b/i 3): run formatting is not part of the LaTeX surface, which
  carries the semantic styles only (`\mathrm`, `\mathbf`, `\boldsymbol`,
  italic letters by default, `\mathbb`/`\mathcal`/... alphabets). HTML
  input ignores `data-docx-loss`.
- Write side (P5): an equation an edit leaves unchanged keeps its OMML
  bytes. A replaced equation's new runs inherit the replaced equation's
  run formatting, its most common `w:rPr` (`math.RunProperties`), through
  `math.LaTeXToOMMLWith(latex, display, math.Options{RunProperties: ...})`,
  so a replacement does not visibly change font, size or colour. The
  option declares `xmlns:w` on the output root; the HTML input's block
  merge copies only `m:oMath` children under a root declaring `m`, so
  formatted output must not go through that merge until it carries the
  `w` declaration too.
- `replace_equation`'s `latex` is checked and canonicalised with
  `math.CanonicalLaTeX` in display math unless `display` is false: with
  `display` unset the target keeps its form, known only at apply, so the
  plan accepts the display superset (top-level `\\` lines) and apply
  refuses lines on an inline target. Malformed LaTeX is an
  invalid_operation at `ops[i].latex`.
- The final and original views render what accept-all and reject-all
  leave, as docxdriver does: `revise.Settle` (the revision core, below)
  settles a private copy of the main document and its headers and footers
  through `edit.Private`, and the copy renders under the source's
  paragraph addresses (a join moves the identity, as Word does). The copy
  is parsed for the purpose and never saved. Move range markers and
  gridAfter padding are settled too (the projection shows neither); list
  definitions a rejection orphans are dropped only by revision_settle.
- Recursion: the renderer recurses only over trees the tokenizer built;
  nested HTML lists stop nesting at 256 levels (Word has nine).
- Text and offsets (B8, B8b): `model.Classify` is the one taxonomy of
  paragraph content and `model.LeafText` derives from it (table in "docx
  span engine (P4e)" below): text leaves their text, tabs, breaks and
  hyphens one fixed character, objects one U+FFFC (nothing inside them).
  Read text, find, segments (the edit side's selection space), selections
  and comment anchors all count this way, so no selection spans a break,
  tab, symbol, equation or drawing unseen; docxdriver counts only `w:t` and
  `w:delText` (an intended difference).

## docx span engine (P4e)

Settled 2026-11-13. `internal/docx/span` is the one span engine of
replace_text, revise_text, replace_paragraph, format_text, comment_add and
the bookmark operations; comment and bookmark anchors use the same Select
and Isolate, with no anchor code of their own. It authors no revisions:
the revision core (P4c) consumes isolated spans.

### Taxonomy (`model.Classify`)

| Kind | Elements (w namespace, transitional or Strict, unless noted) | Text | Span ops |
| --- | --- | --- | --- |
| text | `t`, `delText` | their characters | split at any character |
| char | `tab`, `ptab` "\t"; `br`, `cr` "\n"; `noBreakHyphen` U+2011; `softHyphen` U+00AD | one character | whole |
| object | `m:oMath`, `m:oMathPara` (any namespace), `sym`, `drawing`, `pict`, `object`, `contentPart`, `footnoteReference`, `endnoteReference`, `footnoteRef`, `endnoteRef`, `annotationRef`, `separator`, `continuationSeparator`, `pgNum`, `dayShort`, `dayLong`, `monthShort`, `monthLong`, `yearShort`, `yearLong` | one U+FFFC | whole, never split or dropped |
| marker | `bookmarkStart/End`, `commentRangeStart/End`, `commentReference`, `permStart/End`, `proofErr`, move and custom-XML range markers, `lastRenderedPageBreak`, `fldChar`, `instrText`, `delInstrText` | none | inside a span only between its first and last character; at an end it stays outside |
| wrapper | `r`, `hyperlink`, `smartTag`, `customXml`, `dir`, `bdo`, `fldSimple`, `sdt`, `sdtContent`, `ins`, `del`, `moveFrom`, `moveTo`, `ruby`, `rt`, `rubyBase`, `mc:AlternateContent` (effective branch) | its content's | runs split; other wrappers kept whole or descended |
| properties | `pPr`, `rPr`, `sdtPr`, `sdtEndPr`, `smartTagPr`, `customXmlPr`, `rubyPr` | none | `rPr` copied to both halves of a split run |
| story | a nested `p` (text box) | none (its own story) | untouched |
| unknown | anything else | its content's, as read | preserved byte-identical; a span end below it is refused (`ErrUnknown`) |

Blockers are docxdriver's labels (`span.Blocker`): comment markers and
references "comment", `fldSimple`/`fldChar` "field", `drawing`/`object`/
`pict` "drawing", note references "note", `sdt` "structured document tag".
Unknown content is not a blocker: `Isolated.Unknown` lists it, and an
operation that would drop or rewrite it keeps it whole or refuses; one that
only marks the span leaves it.

### API

- `span.Read(p)`: a paragraph of the working tree (or the read model) with
  its all-view segments and per-view visibility; `Text(view)` equals
  `model.ParagraphText`.
- `Para.Select(view, start, end)` and `Para.Find(view, needle)`: a view's
  character range (non-overlapping matches, as docxdriver counts them) to
  a `Span` in all-view offsets; a non-empty selection runs from its first
  visible character to its last, hidden content between included; an
  empty one is a point.
- `span.Isolate(handle, span)`: splits the text leaf and the run at each
  end through the edit core's `SplitText` and `SplitAt` (w:rPr copied),
  sets `xml:space` on halves that need it, and returns `Units`, the
  maximal elements wholly inside. Every refusal (`ErrBareText`,
  `ErrAlternateContent`, `ErrNestedLeaf`, `ErrUnknown`, `ErrRunOrder`,
  `ErrRange`) is found before anything changes; it is an operation-level
  error and the document is unchanged.

### One tree, normalised at the accessor

- `model.Package` wraps the opc package and reads parts lazily; parsed
  parts are the opc package's own trees (`Package.XML`, `Part`), from the
  original bytes, so read and edit share one tree per part and
  `model.OpenPackage`/`NewDocument` read an edit workspace's working copy.
  Line ends are normalised only by `model.TextContent` (text and CDATA);
  untouched parts keep CRLF and are copied through. Parse errors are
  positioned in the normalised bytes, as docxdriver counts. Every read and
  find output on the fixtures, authored files and corpus/lo is
  byte-identical to the eager, pre-normalising model (14,405 corpus XML
  parts contain CR).
- UTF-16 rule: the read model transcodes a part with a UTF-16 byte order
  mark into a private tree; the working tree reads UTF-8 only, so such a
  part fails to open for editing and no span is computed over text read
  differently (no corpus/lo part is UTF-16).
- Strict: `model.WordML` is the transitional and Strict family; Strict
  documents read like transitional ones (docxdriver refuses them). An
  operation that cannot write Strict refuses with `model.ErrStrict`
  (`Document.IsStrict`), never read.
- Damaged parts: a part that cannot be read or is not well formed disables
  only its own facet (styles, numbering, comments, a story part, an image)
  with a `model.Note` {at, reason}, carried on read and find results as
  `notes`; only a read targeting that part (the styles listing) fails. An
  unread damaged member never fails an open or an edit.

## Revision core and apply pipeline (P4c)

Settled 2026-11-13.

- `internal/docx/revise.Settle(editor, scope, Options{Accept, Select,
  Moved})` is the one settle engine: all revisions, a set, or one, under a
  scope (a body, a header or footer root, a paragraph). Order is
  docxdriver's: rows, content wrappers first first, paragraph marks last
  first (a join moves the joined paragraph's identity onto an empty next
  paragraph), cells (rows that lose one are padded with w:gridAfter),
  property records, then the range markers of settled moves. It replaces
  docxdriver's three implementations (accept_reject_all,
  accept_reject_one, accept_new_revisions), which disagree: the last one
  skipped row marks and picked "new" revisions by author and node age, so
  it also accepted split copies of existing revisions.
- The `Reviser` authors content wrappers (`Insert`, `Delete`), marks
  (`MarkParagraph`, `MarkRow`) and property records (`Property`:
  snapshot, mutate, record only if the base properties changed; an
  existing record keeps its snapshot). It edits the author's own pending
  insertion instead of revising it (Word's rule). It keeps a ledger of
  the elements it authored; `Finish` numbers them in document order from
  the part's highest revision id (track) or accepts exactly the ledger
  (direct). Nothing else decides what direct mode accepts.
- Direct mode per operation is table data: `OpDef.Direct` is
  AcceptAuthored, OwnPath, Same or OnlyDirect, with `TrackGuards` and
  `DirectGuards` for refusals in one mode only; `ops.Contract` is the
  contract's 42 operations with their docxdriver mapping. Two rows may
  share one Spec and Run (the contract's revision.accept and revision.reject
  share RevisionSettle's Run with a fixed action).
- Mutation goes through region handles of the edit core (`Story` per
  settled scope, `Part` for numbering, `Private` for the projection's
  copies), whose write set is declared when they open; an edit that
  widens past its region is refused (`ErrOutside`) and would fail
  verification (`TestRegionScoping`). A join calls the workspace's
  `JoinParagraphs` before content moves (identity and the gone id); a
  table emptied by a cell deletion becomes an empty paragraph in place
  (`ReplaceWithNew`); a property record is parsed whole and placed by the
  schema; a short row's `w:trPr` is rebuilt in place with `w:gridAfter`
  first (`CT_TrPrBase` is a repeated choice the schema cannot place in).
- Oracle bug fixed: rejecting a paragraph mark's `rPrChange` whose
  snapshot holds the mark's own revision brought that revision back
  (n830205.docx keeps a `w:del` after reject-all, so a second reject-all
  changes the file); the restored snapshot leaves mark revisions out.
- `internal/docx/apply.Run(path, plan, options)` returns a Result value
  (outcome, diagnostic, report with each operation's affected markup from
  the snapshot's HTML projection, changed parts); the CLI contract
  renders it. A plan that changes nothing writes nothing, not even the
  paragraph-id repair (reading the file gives the same ids); docxdriver
  writes the repair.
- The integrity gate compares before and after only, per instance:
  `integrity.Introduced` refuses a kind of which the output has more
  instances than the input in that part (messages carry no positions, so
  instances are equal), so a new instance of an existing fault is
  caught and existing faults never block a save. A refusal is
  invalid_output at the operation that first changed the part. Attributing
  findings to created and changed nodes needs located findings from
  `internal/docx/integrity` (open).
- Parity of revision_settle (live differential against the pinned
  docxdriver CLI, both modes, all and first id, accept and reject, read
  back through docxdriver's reader): fixtures 7/7, authored 1 (+1 oracle
  panic), corpus-lo 1531 files on the repair path and all 8 plans on the
  75 with revisions, every one identical. The oracle lists repaired_ids in
  HashSet order, which changes between runs; the comparison treats them as
  a set.

### Settling by author and by paragraph (D6, 2026-11-14)

- revision.accept and revision.reject take the selection
  revision.convert_to_formatting already took: `at` one revision,
  `all`, or `at` a list of body paragraphs, with `author` narrowing
  `all` or the paragraphs. It is one `revise.Settle` run per part with
  `Options.Select` from `revise.Selection`; nothing else changed in the
  engine's order.
- A revision belongs to every w:p it sits in (`revise.Owners`): its own
  and, in a text box, the story paragraph anchoring it, where Settle and
  the listing place it. Row and cell markers and table, row and cell
  property records belong to no paragraph: settling one changes a row
  or cell, so a paragraph list never selects them. With paragraphs,
  only the body is settled; with `all`, the headers and footers the
  sections use too, each opened as a region only when it has a revision
  the selection picks.
- `w:tblGridChange` has no author (CT_Markup). By author it goes with
  that author's table, row or cell changes in its table: rejecting
  restored cell spans against the new grid otherwise breaks the grid
  (testTrackChangesInsertedTableCell.docx was refused with
  invalid_output).
- Fixed in the engine while porting: rejecting a paragraph mark's
  rPrChange also removed the mark's current insertion, deletion or move
  (the restored snapshot leaves its own mark revision out, P4c, but the
  current one went with the replaced properties). Under a narrowed
  selection that dropped another author's pending revision
  (tdf123460.docx); it now stays first in the mark's w:rPr.
- Laws (`internal/docx/apply/laws_settle_by_test.go`, on the law inputs
  and the corpus): the change-mode law of each plan; settling author by
  author equals settling all; settling paragraphs leaves every other
  paragraph's own bytes, in order (`laws.Kept`, the untargeted law for
  operations that may merge or remove their targets), and every part
  but the document and numbering; accept-by-author then reject-all
  equals the direct form (the AcceptAuthored mode law with the
  pipeline's accept by author); atomicity at an operation and at each
  save stage.
- Oracle: docxdriver's revision_settle takes one id or all, so the
  write baseline records each settle-by case through an equivalent:
  settle-all when every revision of the source is that author's, else
  the selected listed revisions one id at a time, last first (a nested
  revision before the one that would remove it), rows last. The
  differences that remain are listed in `known/write-ops.tsv`.

### paragraph.delete and comment.add (P4, 2026-11-13)

- `Reviser.DeleteParagraphs` is delete_paragraphs in the revision core:
  the author's own pending paragraph goes outright; otherwise
  `DeleteContent` wraps each run (and paragraph-level equation) in its own
  w:del, inside hyperlinks, fields, smart tags, custom XML, bidi wrappers
  and foreign insertions (nested w:del), and removes the author's own
  insertion (Word's rule; docxdriver nests a w:del in it and leaves it
  empty in direct mode, W11). The mark is deleted when the next block is
  a joinable paragraph and the paragraph ends no section; otherwise the
  previous joinable paragraph's mark is deleted and the target adopts its
  properties (`AdoptProperties`, a w:pPrChange with its own), so
  accepting leaves no empty paragraph and keeps the section break. A
  content control in the live content is refused before any change.
  docxdriver leaves paragraph-level equations, fields and custom XML
  live; Go deletes their runs too.
- `Reviser.SetJoin` gives the reviser the workspace's `JoinParagraphs`,
  so joins it makes (deleting the author's own inserted mark, and direct
  mode accepting a deleted mark) record the join and move the identity
  as revision_settle's do.
- comment.add anchors through the span engine: `Para.Find` in the final
  view (or the whole paragraph in all-view offsets, deleted text
  included, as docxdriver), `span.Isolate`, and `Isolated.Blocker` for a
  selection. The range markers sit exactly at the span's edges, inside
  hyperlinks and revision wrappers too (docxdriver widens the range to
  the paragraph child holding each edge, W13). The reference run follows
  the range end at paragraph level.
- `w:CT_RunTrackChange` (the content of w:ins, w:del, w:moveFrom,
  w:moveTo) is tabulated: gen_schema.go loads shared-math.xsd for its
  groups only (`foreignGroups`), so m:EG_OMathMathElements enters the
  slot as untyped m: names, as element references into other schemas
  do. PlaceAt and the other schema-placed verbs now work inside revision
  wrappers; only that one wml type changed, pptx tables are unchanged. Comment
  parts that cannot be read, or registered when missing, refuse the
  operation with unreadable_target before any change.
- `oracle write compare` runs plans through `docx apply` itself (it
  builds `./cmd/docx` unless `-docx` names a binary); the stand-in
  `tools/oracle/goapply` is gone.

## HTML lowering and paragraph.insert (P4, 2026-11-13)

- `internal/docx/build` is the one lowering from `html.Item`s to
  WordprocessingML for every op that takes `html` and for `create`.
  `Inline(handle, p, before, atoms, Options)` places one paragraph's
  content: runs (consecutive characters of one formatting share a run and
  a `w:t`; a break or tab rides in the run before it), one `w:hyperlink`
  per link (relationship from `Options.Link`), `w:fldSimple` for PAGE and
  NUMPAGES, equations. Elements are parsed detached and placed with
  `PlaceAt`/`PlaceNew`; run properties go in through `EnsureIn`, so they
  stand in declaration order whatever order the HTML nested them in.
  `Properties` writes a paragraph's style (h1-h6, class) and CSS where
  the schema puts them (docxdriver wrote `w:jc` before `w:spacing`).
  `InlineAtoms` is the safe inline gate of one paragraph's content
  (docxdriver's parse_with). It authors no revisions: `Content.Groups`
  are the sibling runs one `w:ins` may hold (a link's runs are their own
  group, inside the hyperlink), which the op hands to its Reviser.
- Identity: an equation carrying `data-ref` resolves through
  `Options.Identity` to the original element and is copied byte for
  byte; its LaTeX must be the original's (a changed text under a ref is
  refused, never silently dropped); an unknown ref is `not_found`. New
  equations go through `math.LaTeXToOMMLWith` with
  `Options.EquationRunProperties`. The docx vocabulary now tabulates
  Office Math (`edit.M`), so equations are placed like runs.
- `paragraph.insert` places the new paragraphs empty beside the anchor,
  opens one region over them, gives each its allocator id (slot n for
  item n), the explicit style or the anchor's inherited base properties
  (or a heading's next style), and its content; content and mark are
  authored through the Reviser in both modes. Inserts after one anchor
  keep plan order (`Registry.InsertAfter`). `table:N` counts the body's
  effective tables as they stand, as docxdriver does.
- Aliases (`internal/docx/apply/aliases.go`): before each op, aliases in
  its paragraph address fields are replaced by ids on a copy of the op (a
  group stands for its first member before, its last after, and expands
  in a list field; a group in a one-paragraph field is
  invalid_operation); a reused `as` is refused before the op runs; after
  it, `Env.Created` binds `as` (a list binds the group and `name_N`). The
  result's `aliases` maps each alias to its address (groups by their
  members).

## Scaling and memory budget (P4 close-out, 2026-11-13)

Every command's cost is linear in the document: bytes allocated and
allocations made on an input of four times the units may be at most 5.5
times as much (linear gives about 4, quadratic 16); time is reported,
not checked. `make scaling` checks it
(`internal/laws.Scales`, run by `TestScaling` in `internal/docx/apply`
and `internal/pptx/apply` at 1,000 and 4,000 paragraphs or shapes, about
seven seconds); it is a `make ci` step and part of `make test`. On the
P4 base it fails: create allocated 10.2 times as much and took 14 times
as long at 4,000 paragraphs as at 1,000.

Memory budget (P5 memory work): `TestMemoryBudget` in
`internal/docx/apply`, run by `make scaling`, measures each docx command
once on a created document of 4,000 paragraphs (`laws.WithinMemory`) and
fails over its budget in `memoryBudgets`: the peak live heap over the
command's checkpoints (an Options field, `Checkpoint`, of apply and
read, and `create.BuildAt`, called where the command holds the most;
each runs a full collection and reads the live heap, less what was live
before), and the bytes allocated. Both are deterministic, so the gate
holds on a loaded machine. A budget is the measured number with about
twenty percent of margin; a change that shrinks a command lowers its
budget in the same commit.

The scaling gate likewise fails on deterministic costs only: the ratio
of bytes allocated and of allocations made. Time is logged, never
checked: under go test ./... a linear read measured 10.9 times.

Budget for a one-paragraph edit: `docx apply` at most three times
`docx read` of the same document, in time and peak memory, at 20,000
paragraphs. Peak memory is the parsed tree's order: one working tree,
plus at most one of the integrity check's view or the output's read
model at a time.

Measured (Apple M3 Pro, `docx` CLI, wall time, peak RSS and the largest
live heap after a GC; documents of n paragraphs with bold, italic and a
link each, a heading every fifty; document.xml is 8.6 MB at 20,000):

| n | command | before (992cd3e) | after |
| --- | --- | --- | --- |
| 10,000 | create | 64.9 s, 1,912 MB (live 1,014) | 0.97 s, 239 MB (live 105) |
| 20,000 | create | 309.5 s, 3,230 MB | 1.95 s, 419 MB (live 201) |
| 10,000 | read | 0.64 s, 247 MB | 0.46 s, 178 MB |
| 20,000 | read | 1.30 s, 495 MB (live 221) | 0.94 s, 341 MB (live 162) |
| 20,000 | find | 0.64 s, 323 MB | 0.34 s, 185 MB |
| 10,000 | apply (paragraph.format, direct) | 6.34 s, 443 MB | 1.39 s, 329 MB |
| 20,000 | apply (paragraph.format, direct) | 12.84 s, 863 MB (live 455) | 2.80 s, 580 MB (live 304) |

At 20,000 paragraphs apply is 3.0 times read in time and 1.7 times in
peak memory; a parsed document.xml (the tree alone) is 74 MB, the
integrity check's view of it 90 MB. `BenchmarkApplyLarge`
(`internal/docx/apply`, 4,000 paragraphs with tables and revisions,
in process) went from 498 ms and 565 MB allocated per insert to 254 ms
and 281 MB. The tree's own `BenchmarkParse` went from 540 KB and 9,127
allocations per op to 363 KB and 5,751; `BenchmarkFindAll` from 29 µs
to 18 µs; the OPC benches are unchanged. pptx is linear as it was
(`TestScaling`: read and an object.set at 1,000 and 4,000 shapes, ratios
4.0 and 4.0) and its tests pass unchanged.

What changed, path by path:

- **The CLI ran the pipeline twice** (a review, then the commit with
  the preview key). It runs once; a refusal at an operation or at the
  gate still writes nothing.
- **apply re-read the document five times** (the input model at open,
  for the repaired ids and for the changed paragraphs; the output for
  the report and for the changed paragraphs) and the CLI twice more
  (`read.Read` of input and output for the changed units). The input is
  read once, and what the run needs of it (repaired ids, paragraphs by
  address, the CLI's units through `Options.Input`) is taken before the
  operations run; the output is read once, after the working copy is
  released, for the report's markup, the changed paragraphs and the
  CLI's units (`Result.Output`, `Result.Markup`, `read.Units`).
- **Hazards ran a full `integrity.Check` of the output.** They are the
  gate's own findings on the changed parts (`GateEditReport`), complete
  there by construction (asserted on the rule packages and the corpus in
  `TestGateMatchesFullCheck`, and on every apply test run by
  `sameHazards`).
- **The gate unzipped each package three times and held both packages'
  parsed parts at once,** and re-parsed the input part to index its
  elements. Each package is unzipped once; the input side is checked
  first and only its findings and element offsets kept; the offsets come
  from the check's own parse.
- **The commit verified the write set twice** (snapshot and commit);
  `plan.Tx.Finish` now verifies once, and releases the working copy when
  done.
- **Saving re-parsed every changed part into a full tree** to check it is
  well formed. `tree.Check` now tokenises without building a tree
  (memory in the depth, not the size), fuzzed against `Parse`.
- **tree nodes were 224 bytes** (a 24-byte source slice, the start tag's
  trailing space and the end tag as slices, two maps per node). They are
  112: one shared source pointer, int32 offsets, the rare fields behind
  one pointer. Names are interned per parse. Namespace lookups decoded
  the declaration on every call; the decoded URI is cached on the
  declaring element.
- **create was quadratic**: each hyperlink scanned and decoded every
  relationship (`Rels.All`, `NextID`), each placement listed all the
  body's children (`PlaceAt`, `fits`, `insertAfterLast`, `indexOf`), and
  each closed paragraph copied its items (`mergeBlockEquations`).
  Hyperlinks are looked up in a map, `NextID` is cached between adds,
  placements scan from the end without building lists, and paragraphs
  without a block equation are not copied.
- **HTML input held one 664-byte item per character.** A run of
  characters with the same formatting is one atom (`AtomChar` with
  `Text`), and the fields of milestones, notes, equations and images are
  behind a pointer: items are 296 bytes, per run.

Known and left: create still allocates about 130 KB per paragraph
(linear; the lowering parses one fragment per run through the edit
core), and the integrity check's view of a part is larger than the
part's tree. Neither is super-linear.

### Tree storage (tree-flat, 2026-11-13)

The parsed tree was about nine times its part (document.xml of 8.0 MB
at 20,000 paragraphs: 70.5 MB, 542,410 nodes, 136 bytes per node, every
node a 112-byte `*tree.Node` and every attribute its own object). Its
storage is rebuilt behind the unchanged `*tree.Node` API, pointer
identity included, one mergeable step at a time with byte-identical
output. The safety net, from the first step:

- `internal/ooxml/tree/internal/treeoracle` is the implementation as it
  stood (2b3911a), frozen and imported only by tests. `FuzzDifferential`
  (`internal/ooxml/tree`, beside `FuzzRoundTrip`) parses one input with
  both, runs one random edit sequence on both (attributes set and
  removed, text set, nodes removed, moved and replaced, fragments
  inserted, elements, text and raw nodes built) and requires the same
  bytes, the same errors and the same answer to every query on every
  node after each edit. The oracle had one panic (`NewText("")` gave a
  text node without bytes); it is fixed in both.
- `TestBytesPerNode` (`maxBytesPerNode`) gates the heap a parsed Word
  body holds per node on 64-bit platforms (not on wasip1, whose pointers
  are 32-bit); `BenchmarkBytesPerNode` reports it. The budget is lowered
  with each step.

The steps:

1. **Iterators.** `ChildrenSeq`, `ElementsSeq`, `AttrsSeq` and
   `AttrNamesSeq` (`iter.Seq`, no allocation), with `HasElements`,
   `FirstElement` and `HasAttrs`. A child iterator survives the loop
   body removing, moving or replacing the child it was given: it goes on
   with the child that followed it while that one is still a child, and
   stops otherwise. The read-only callers (pptx derive, model, view,
   coverage and render; docx read, span, model, view and integrity;
   eval/grade) and the edit core use them. The operations' loops stay on
   `Elements`: they are cold, and several edit what they walk.
2. **Lazy attributes.** An element keeps no attribute storage: its
   attributes are read from its start tag's bytes when asked for, their
   names looked up in the parse's name table (a fragment's nodes drop
   theirs: a create parses one fragment per run, and the tables kept
   live doubled its peak). The first change makes them the element's own
   (`ownAttrs`); a rebuilt start tag of an element whose attributes did
   not change copies them as parsed. A bit per element (`decls`) says
   whether it may declare a namespace, so namespace lookups skip the
   elements that do not; the declaration cache is keyed by attribute
   index and cleared on any change to the element's attributes.
3. **Source held as a string.** A tree keeps the text it was parsed
   from as a string (`ParseString` takes one without a copy; `Parse`,
   `Check`, `Scan` and `ParseFragment` copy their bytes once). Names,
   attribute values and text without references or CRs are substrings
   of it, so `Name`, `Attr`, `Data` and a single-child `Text` copy
   nothing; `Raw`, `AttrRaw`, `Original` and `StartTag` return copies,
   and `RawString` and `OriginalString` are the copy-free forms. opc
   keeps a part's original bytes as that string and parses its tree from
   it, so the two share one copy. Each declaring element keeps its
   namespace declarations, decoded, from the parse (remade when its
   attributes change); lookups read that list, the per-element cache is
   gone, and reading a tree no longer writes to it. `SpanIn` now means
   "parsed by the same call as d": a second parse of the same bytes is
   another document (no caller compared across parses). The integrity
   view clones the strings it keeps, since its scan's text is a copy
   made for the scan.
4. **Slab allocation.** A parse takes its nodes from fixed chunks
   (`slab.go`), each one allocation, never grown, moved or reused: a
   removed node stays where it is, since the edit core's registry,
   created-node record and `SourceOf` keep removed nodes. Each parse has
   its own slab (each fragment, so each copy and each
   mc:AlternateContent branch; each re-parse). A chunk lives while any
   of its nodes does, so the first chunk of a small source holds exactly
   as many nodes as it can parse into (`nodeBound`): create parses one
   fragment per run, and 4-node chunks for 3-node runs raised its peak
   by a third. Later chunks hold 1,024 nodes. `Check` and `Scan` give
   the visitor nodes allocated one by one; nodes built in code have no
   slab.
5. **Sibling links.** A node links its children (`first`) and its
   siblings (`next`, `prev`; the first child's `prev` is the last
   child), so it holds no slice of them and appending, the last child
   and the sibling before any child are O(1). `FirstChild`,
   `LastChild`, `NextSibling` and `PrevSibling` are new; `Children` and
   `Elements` now build a slice on each call, and the hot read-only
   callers range over `ChildrenSeq` instead. `InsertAt(i)` and
   `InsertFragment(i)` walk to the i-th child, O(i); their callers pass
   0 or an index they just counted, once per change. Three placement
   helpers that searched a parent's child slice from its end (`fits`,
   `prevElement` in the edit core, opc's `insertAfterLast`) walk
   siblings instead: with slices built per call, create had become
   quadratic. opc's `NextID` cache now holds while the relationship
   `Add` added is still the root's last element (it is the highest id),
   instead of comparing the child count, which is no longer O(1).
6. **Smaller nodes.** (a) An element records where the local part of
   its name starts (`setName`), so `Prefix`, `Local` and the checks
   built on them (`Is`, docx `IsW`) slice instead of searching the name:
   the CPU profile of docx read at 20,000 paragraphs had 1.2 s in name
   splitting.
   (b) A node is 64 bytes on 64-bit platforms (`TestNodeSize`; it was
   112 before step 2 and 96 before this): four links, the source, the
   rare part (`x`), the span, and packed element fields. The name is
   the `nameLen` bytes after the start tag's "<" (or `x.name`: renamed,
   built, or over 65,534 bytes), the local part's offset a `uint16` (or
   `x.local`), the start tag's end a `uint16` length from the node's
   start (or `x.tagEnd`), and the six booleans one `flags` byte. The
   whitespace before the start tag's > and the start of the end tag are
   no longer stored: they are read back from the source when a tag is
   rebuilt (`trailAt`, `endTagAt`; a renamed element sets `fEndLost`).
   `Local` and `Prefix` stay inlinable. A parsed Word body holds 65.5
   bytes per node (98.2 before).

## Placeholders and the block operations (P5 block, 2026-11-13)

### Placeholders

- `model.Placeholders(p)` is the one definition of a paragraph's
  placeholders (contract section 7), over its current content (not inside
  `w:del` or `w:moveFrom`), in document order: an object of a run
  (`w:drawing`, `w:pict`, `w:object`, `w:contentPart`, `w:sym`, note
  references and marks, `w:pgNum` and the date parts, separators, an
  `mc:AlternateContent` or an unknown element inside `w:r`); a wrapper
  at paragraph level, or inside a hyperlink, smart tag, custom XML, bidi
  wrapper or insertion (`w:fldSimple`, `w:sdt`, `w:ruby`,
  `mc:AlternateContent`, an unknown element); and a complex field, the
  sibling runs from its begin `w:fldChar` to its end. Equations are not
  placeholders (`CurrentEquations`). A complex field that does not begin
  and end among one container's children, or shares its first or last
  run with other content, is no placeholder and `blocked` says "field":
  an operation that would have to keep it whole refuses the paragraph.
- Read never loses text for this: the projection keeps the rendering it
  had and adds `data-ref` (`<img>`, the unavailable-image span, `<sup
  data-note>`, `<span data-field>`); a wrapper whose text it shows is a
  `<span data-ref data-kind>` around that text; only content the
  projection showed as nothing (a chart, a shape, an OLE object without a
  picture, a symbol) becomes an empty `<opaque data-ref data-kind>`.
  Paragraphs without an address (headers, footers, notes) number
  equations and placeholders across their part: `equation:N`,
  `opaque:N` (before, header equations were numbered per paragraph,
  which was ambiguous for a header-wide replacement).
- HTML input: any element whose `data-ref` is a placeholder address is
  that placeholder (an `AtomOpaque`), whatever its tag; its other
  attributes and its content are derived. Its text content must equal
  the content's (`model.PlaceholderText`: the `w:t` and tab text read
  shows; not compared for kinds read shows without text), so an edit to
  a field result or a content control's text is refused, never dropped.
  `<opaque>` without `data-ref` is refused: new content cannot be
  written as a placeholder.
- build: a placeholder atom moves the original nodes where the HTML puts
  it (never a copy; twice is refused); an object of a run goes into a
  new run carrying a copy of its run's `w:rPr`. Moved content is left
  out of `Content.Groups`, so no revision wraps it.
- Read parity: `tools/oracle compare` takes the additions out of the Go
  markup (`withoutPlaceholders`) before comparing; the rest is
  docxdriver's byte for byte.

### The delete field

- `ops.accountFor` is the omission rule of every op that replaces
  content with html: each equation and placeholder of the replaced
  content is sent back (its `data-ref`) or listed in `delete`; a delete
  entry naming nothing there is `not_found` at `delete[i]`, one also sent
  back `invalid_operation`, and content neither sent nor listed is
  `operation_failed` at `html`, the addresses in the message. The
  addresses are a paragraph's (`P/equation:N`, `P/opaque:N`) for
  paragraph.replace and story-wide (`equation:N`, `opaque:N`) for
  header.set and footer.set. `Env.Deleted` carries what an op deleted
  for the result's `deleted` (the apply side's merge is the integration
  owner's).

### paragraph.replace

- The html is parsed with the paragraph rules (one `<p>`/`<hN>` or bare
  inline content) but without docxdriver's one-link rule, so a
  paragraph's markup as read shows it, links and all, is accepted
  (`build.ParagraphAtoms`). The target's refusals come first: others'
  tracked changes (content, `w:rPrChange`), comment ranges, a field that
  is no one placeholder (docxdriver's blockers, less what placeholders
  now keep).
- Kept equations and placeholders stay where they are; an object inside
  a run is first isolated into a run of its own (`SplitAt`, `w:rPr`
  copied). The rest of the live content goes as the author's `w:del`
  (a simple field deleted in track mode is first rewritten as complex
  field runs); each new segment of html goes in a `w:ins` just before the
  kept piece that follows it, the last after the old content, so
  reject-all gives the input back and direct mode (accepting the ledger)
  gives the html. Moving kept pieces out of document order is direct
  only (track refuses: a move cannot be tracked here); a kept piece
  inside a hyperlink cannot be moved.
- W4 fixed: bare text takes the direct formatting every text run of the
  paragraph shares (`build.SharedFormat`, docxdriver's
  with_shared_run_format, from the runs' own `w:rPr`), in both modes. A
  `<p>`'s CSS merges into `w:pPr` attribute by attribute, so an echo of
  read's CSS changes nothing (an exact line height read shows in points
  equals an at-least one).
- Track mode records whole-content revisions, not docxdriver's word
  diff (listed in write-ops.tsv).

### Equations

- equation.replace and equation.delete resolve `P/equation:N` among the
  paragraph's current equations (the paragraph alone for its only one,
  several `ambiguous`), editable only standing directly in the
  paragraph. replace converts with `math.LaTeXToOMMLWith` and the
  replaced equation's `math.RunProperties`, keeps the target's form
  unless `display` is set, refuses display lines on an inline target,
  and records old-out (`w:del`) new-in (`w:ins`).

### Lists, headers and footers, sections

- Lists in build: an HTML list is a numbering definition of its own
  (`build.NewList`: docxdriver's create template, placed where
  CT_Numbering puts it), the numbering part found or added
  (`build.Numbering`, `edit.Workspace.AddPart`); a list item of several
  paragraphs is refused (docxdriver wrote numId 0 and a bookmark marker,
  a hack not carried over).
- list.set appends definitions only; a start, or a tracked change joining
  an instance nothing refers to yet, clones the instance. Membership is
  a tracked paragraph property change.
- Story handles (`ops/chrome.go`): `section` resolves `section:N`,
  `section:final` and `section:<paraId>`; a new story is a new part
  (`PartName`), its relationship and its reference after the section's
  references. header.set and footer.set use one layout in both modes
  (docxdriver's tracked_story: old paragraphs' content and marks deleted
  but the last, new paragraphs inserted before it, the last new
  content joined into it with `AdoptProperties`); direct accepts it, so
  others' pending revisions in the story are never settled. A story with
  a table or content control, or html keeping a placeholder, is
  replaced outright in direct mode and refused in track mode. In track
  mode new fields are complex field runs (`build.Options.ComplexFields`)
  and old simple fields are rewritten as such before their deletion, so
  neither mode leaves an empty `w:fldSimple` for Word to recompute.
  header.delete and footer.delete remove the reference directly; tracked,
  they delete the story's content and keep the story (EmptiesStory), in
  the first section only (headers.rs:557-575: a later section would
  inherit the previous one's story, which Word cannot track; the OpDef
  track guards say so).
- section.set_boundary is direct only. Adding a boundary at X copies the
  properties of the section it splits into `section:X`; removing one
  merges into the next section, which takes the removed section's story
  references of kinds it lacks; the removed address goes in
  `Env.Deleted`.

### P5 memory work (2026-11-13)

The owner's target: peak memory in the low hundreds of MB at 20,000
paragraphs. Same documents as above (document.xml 8.6 MB). Peak live
heap is the largest of the commands' checkpoints (deterministic: a full
collection at each, `laws.MeasureMemory`); RSS and time are the `docx`
CLI's (`/usr/bin/time -l`, Apple M3 Pro, the machine shared with other
agents, so times are within about 10%).

| command | peak live, before | after | RSS, before | after | time, before | after |
| --- | --- | --- | --- | --- | --- | --- |
| create | 150 MB | 105 MB | 424 MB | 292 MB | 2.00 s | 2.01 s |
| read | 100 MB | 94 MB | 363 MB | 276 MB | 0.96 s | 1.22 s |
| find | 109 MB | 103 MB | 189 MB | 167 MB | 0.33 s | 0.39 s |
| apply paragraph.format | 240 MB | 152 MB | 545 MB | 279 MB | 2.85 s | 3.00 s |
| apply paragraph.insert | 240 MB | 152 MB | 552 MB | 285 MB | 3.21 s | 3.08 s |
| apply paragraph.delete | 240 MB | 154 MB | 614 MB | 288 MB | 3.13 s | 2.99 s |

Allocated per apply: 2.55 GB -> 2.06 GB; per create: 2.57 GB -> 2.07 GB.

What changed:

- **The integrity view is built as a part is read** (`tree.Scan`, a
  visitor over the tokenizer that keeps no tree), so a part's tree and
  its view are never held together; a part the scan refuses goes through
  `tree.Parse` to name the fault exactly as before (`FuzzScanView`).
- **The view is columnar**: elements in blocks in document order,
  parent, first child and next sibling by index, attributes in one array
  and character data in one array of runs per part (a subtree's text is
  a slice of it), names interned, no comment or PI children. 56 bytes an
  element, from 104 plus three slices; at the gate the output view went
  from 110 MB to about 40 MB.
- **The gate holds no copies**: UTF-8 parts are not cloned before
  parsing, a kept zip entry drops its growth slack, and apply compares
  paragraphs before and after by SHA-256, not by a copy of their bytes.
- **Tree attributes keep their spacing as offsets** into the source (64
  bytes, from 96; built ones allocate no spacing slices), and a detached
  element's namespace hints, with the extra part that held them, are
  freed on insertion.
- **create drops each lowered paragraph once placed**, so the lowered
  input and the tree are not held whole at once.
- **Allocation**: the edit core's namespace scope reads declarations
  only (`tree.Node.Declarations`, cached), `LookupNamespace` keeps no
  map, `ParseFragment` shares its caller's hint map, and the checks'
  `visible` yields instead of building a list.
- **GC**: the docx CLI runs at GOGC 50 unless `GOGC` is set
  (`cmd/docx/main.go`). Measured at 20,000 paragraphs, three runs each
  of the same build: apply RSS 346 -> 289 MB for 2.84 -> 2.97 s, read 328
  -> 279 MB for 1.04 -> 1.08 s; create gained nothing (its RSS is set by
  the HTML front end's short peak) and lost about 8%. A memory limit
  (GOMEMLIMIT 150 MiB) cut RSS further but cost apply 75% more time, and
  GOGC 25 cost about 10%; neither is used.
- **GC (pptx)**: the pptx CLI also runs at GOGC 50 unless `GOGC` is set
  (`cmd/pptx/main.go`), because memory outranks a few percent of time
  for tools that run on servers and beside many parallel agents.
  Measured on a Sprite (8 vCPU x86) at tree-flat a195016, pptx read of
  a 5,000-shape slide, median of 5: GOGC 100 0.25 s, 63 MB RSS, 10
  collections; GOGC 50 0.26 s, 53 MB, 21 collections. A memory limit
  without a GC percent (GOGC off, GOMEMLIMIT 384 MiB) doubled RSS to
  126 MB.

Left: the parsed tree is about 9 times its part (78 MB for 8.6 MB: 112
bytes a node, text nodes included), and apply holds it beside the gate's
output view; create's tree is larger still (its fragments each keep a
source, and every hyperlink adds a relationship element), and the HTML
front end peaks at about 147 MB live while it lowers 2.7 MB of input (the
HTML tree, its events and the items at once). create allocates about
100 KB a paragraph, a third of it in the HTML front end.

## xlsx (P0, 2026-11-14)

The third CLI, `xlsx`, edits Excel workbooks on the same engine. This
section condenses the xlsx design; the agent surface it implies was
drafted in `docs/xlsx-contract-draft.md` and is merged into
`docs/contract.md` (2026-11-14). P0 was groundwork; P1 Track A1
(below, "Read") made `read` and `find` work, and P2 ("Write (P2)")
`apply` and `create`.

> **Unverified.** Every Excel behaviour below is from knowledge, not a
> test. Each one the design depends on is an experiment, E1 to E13
> (fixtures in `testdata/xlsx/experiments`, the maintainer runs them in
> Excel), and the code that relies on one merges only after its result
> is recorded in the table at the end of this section.

### Engine rule

- A worksheet's `sheetData` and `sharedStrings.xml` are never parsed
  into a tree. A parsed worksheet is about 5 to 6 times its XML (a cell
  is about 3 nodes, 65 B a node, for about 35 B of XML), so they are
  read with the streaming tokenizer (`tree.Scan`, memory in proportion
  to depth) and written as splices. Small parts (workbook.xml and its
  rels, styles.xml, tables, comments, drawings, charts) use `Part.Tree()`
  and the edit core as they are. archtest enforces it, with an allow
  list.
- Parts still load whole (opc has no streaming part reader): peak memory
  is the part size, the budget gates it and `opc.DefaultLimits` bounds
  it.

### Splice

A new shared package, `internal/ooxml/splice`, is a patch over a part's
original string: ordered, non-overlapping whole-element span
replacements, with `WriteTo(io.Writer)`, `Len` and `Empty`. A
replacement equal to its source span is dropped, so an identity write is
a no-op. It is well formed by construction (spans come from `Scan`, new
bytes from one escaped writer) and fuzzed against `tree.Check`. It is
the only mutator besides the edit core, with its own archtest entry.

Shared-package changes it needs, one commit each, docx and pptx
byte-identical:

1. `opc.Part.SetPatch(*splice.Patch)`: the part is changed when the
   patch is not empty, and the writer streams `WriteTo` into flate with
   its CRC and sizes. Peak is the original plus the edits.
2. `opc.Part.Forget()` drops an unmodified part's cached string.
3. `plan.WriteSet.DeclareSpliced(part, spans)`: Verify checks a spliced
   part by comparing the bytes outside the spans, with no tree, before
   `verifyPart`'s `ParseString` (which would otherwise parse the
   original worksheet).
4. archtest: `xlsx` joins the formats (done in P0), `internal/xlsx/edit`
   joins the part writers, and a rule keeps `Part.Tree()` off worksheet
   and shared-strings parts under `internal/xlsx`.

A worksheet is indexed by one `Scan`: an int32 offset and row number per
`<row>`, shared-formula masters, array and data-table refs, merges, and
the spans around `sheetData`, in memory O(rows plus special formulas).
Cell edits go to a per-sheet overlay that one pass at seal turns into
the patch; a structural op flushes the overlay and runs its own pass.
Head and tail elements an op edits (`dimension`, `cols`, `mergeCells`)
are spliced whole, placed by `schema.ChildOrder("x:CT_Worksheet")`.

Shared strings are read by one `Scan` with an int32 offset per `<si>`,
decoded on demand. New strings are written inline
(`t="inlineStr"`), so the shared-strings part is never touched and stays
a compressed copy; its `count` goes stale (E3, E4).

Styles are a tree: `range.format` interns an xf (finds an identical
`cellXfs` entry or appends one, and likewise fonts, fills and numFmts,
custom ids from 164); it never reorders or collects. Builtin numFmt ids
come from ECMA-376 Part 1 §18.8.30 with provenance and a corpus check.

### Formulas

`internal/xlsx/formula` is a pure package with no engine dependency:

- a lossless lexer (tokens concatenate back to the input), fuzzed:
  strings, errors, numbers (so `1E5` is not a ref), refs with optional
  workbook, sheet or 3-D prefix and `$` flags, functions (an identifier
  followed by `(`, so `LOG10(` is not a ref), names, structured refs,
  operators and the rest;
- a depth-limited precedence validator, gated on zero false refusals
  over every corpus formula;
- a rewriter, `Shift` and `Rename`, that changes only ref tokens that
  resolve to the affected sheet and leaves every other byte verbatim. An
  insert moves coordinates at or after the point (a spanning range
  grows); a delete turns a ref wholly inside into `#REF!` and clips a
  partial overlap; a shift past row 1,048,576 or column XFD refuses.
  Shared formulas whose master mentions the sheet are unshared first.

References are rewritten in three closed tiers, one test per entry:
rewritten (cell formulas, defined names, dimension, merges, cols,
hyperlinks, conditional formatting and data validation, autoFilter,
selection and panes, breaks, comment refs, table refs on row shifts,
drawing anchors, chart `c:f`, pivot cache sources and locations);
refused when affected (a cut pivot, array formula or data table, a
column shift across a table, x14 extension refs at or after the shift,
external-workbook refs to the sheet); never touched (everything else).
The `_xlfn.` table comes from corpus extraction plus Microsoft's
published list, with provenance.

### Recalculation policy

The tool cannot recalculate. Leaving stale `<v>` with no flag is
silently wrong, and removing dependents' `<v>` needs a dependency graph
(INDIRECT, OFFSET, names, volatile functions); both are rejected. So a
save that changes a value, a formula or the structure:

1. sets `fullCalcOnLoad="1"` (Ensure `calcPr` in `x:CT_Workbook` order);
2. writes new formula cells as `<f>` with no `<v>` and no `t` (E1);
3. deletes `calcChain.xml`, its relationship and its override whenever
   a formula cell is added, removed or moved (E2: a chain entry naming a
   non-formula cell is believed to prompt a repair);
4. reports the `stale-values` hazard, and `read` shows `values_stale`.

A no-op plan writes nothing. This is the second stated exception to the
contract's byte-for-byte rule (draft §9).

### Integrity

- **Schema.** `internal/ooxml/schema` tabulates sml (`sml.xsd`, prefix
  `x`) like wml, with an untabulated list and a content model; done in
  P0.
- **ctypes.** Excel is an Office application of `ctypesgen`; the
  workbook, worksheet, shared strings, styles, theme, calcChain, table,
  comments, drawing and chartsheet types are extracted from the corpus.
- **Gate** (`internal/xlsx/integrity`), on docx's rule: only introduced
  findings refuse, as `invalid_output` naming the op. Package (content
  types and rels of added or removed parts, workbook r:ids, calcChain
  removal), workbook (sheet names unique ignoring case, valid, at most
  31 characters; sheetId and r:id unique; a visible sheet; defined
  names valid and lexing), worksheet scoped to the spliced rows and
  their neighbours (rows and cells ascending and matching `r`, style and
  shared-string indexes in range, no orphaned shared child, array refs
  intact, merges not overlapping, head and tail order), styles (counts
  and ids in range), and `schema.CheckAttr` on every attribute written.
  `TestGateMatchesFullCheck` holds the scoped gate to a full streaming
  check.
- **Laws** in `laws.Suite` (units `x:row` and `x:c`): identity, inverse,
  untargeted, atomicity and sequencing, for cell value, formula, range,
  number format, merge, row height, column width, defined name and sheet
  name; undo pairs row.insert/row.delete and a rename there and back.
  Defects (a malformed other sheet, styles, shared strings or calcChain)
  never block; only an op's own target is `unreadable_target`.

### Budgets

N is the touched sheet's XML, T the shared strings:

- read of a range: live heap at most N + T + S + 8 B a row + 4 B a
  `<si>` plus twice the output (S the shared strings' text; the cell
  buffer and the encoded result are both live at the end);
- apply of one `cell.set`: at most 1.3 N + T with `SetPatch`;
- `TestMemoryBudget` (200,000 rows by 10 columns) and `laws.Scales`
  (25,000 and 100,000 rows) in `internal/xlsx/apply`, both in
  `make scaling`; measured first, then fixed with about 20% margin.

### Read (P1 Track A1, 2026-11-14)

`internal/xlsx/sheet` streams a worksheet in one `tree.ScanString` pass
(an 8 B a row index, array membership resolved during the pass, shared
masters resolved at each cell's end); the shared strings keep a 4 B
offset per item and decode on demand. `internal/xlsx/read` builds the
summary, `read` and `find` on it; `internal/xlsx/model` holds the agent
shapes. Measured:

- 1M cells: read 0.75 s, peak live 63.7 MB against an 87.3 MB budget
  (`TestMemoryBudget`), 679 MB allocated; `TestScaling` ratios 3.9
  bytes and 4.0 allocations (read), 3.6 and 3.9 (find), in
  `make scaling`. `BenchmarkScan` is 0.47 s and 223 MB a 1M cells, of
  which the tokenizer alone is 0.25 s and 215 MB (its garbage, not live
  heap: a follow-up for `internal/ooxml/tree`).
- Opening a 1,024-sheet workbook (openpyxl `bigfoot.xlsx`) fell from
  72 ms to 6 ms once the sheets' relationships were mapped once
  (`TestScalingOpenManySheets`); shared-string lookups are bounded to
  their item (they were quadratic).
- Output bounds: a text over 32,767 characters or a formula over 8,192
  is left out with a note, and one read's or find's output is capped at
  64 MiB (apache-poi `poc-shared-strings.xlsx` would print about 10 GB);
  it reads in 0.18 s at 12 MB.
- Corpus: 1,488 workbooks, 585,547 cells in 4.5 s, no panic or hang;
  one limit trip; the summary's invalid_document cases (42) and cells'
  unreadable_target cases (19) are all broken files. A cross-check of
  1.82M cells against an independent reader found only explained
  classes: shared-formula children written out, cells without `r`,
  prefix normalisation, empty `inlineStr`, oracle bugs, `_xHHHH_`
  escapes, Strict `t="d"`, and a zip with a duplicate member name (opc
  keeps the first entry with a note, as for docx and pptx).

### Write (P2, 2026-11-14)

The write core is 43 chunks on frozen signatures (pure kernels, thin
op glue, one owner file each; every chunk diff touched only its file),
then a verification pass that owns the design. What it settled:

- **Gate.** Per sheet, not per spliced row: the parts a plan changed
  (a changed table brings its sheet) plus the package, workbook, styles
  and shared-strings count (`integrity.CheckScoped`). The output is
  checked first and the input only when the output has a finding in
  scope, so a clean output costs one check; narrowing the input check to
  the parts with findings would be unsound (a table's duplicate-id and
  -name findings depend on which tables the scope reads).
  `TestGateMatchesFullCheckCorpus` holds the scoped check to `Check` on
  all 1,484 corpus workbooks.
- **Recalculation.** As above; the policy runs only when a part
  changed, so a value set and set back writes nothing. `--recalc`
  splices LibreOffice's values and gates again with the scope widened to
  every sheet the values landed on (a formula reading a changed cell on
  a sheet no op touched).
- **Memory** (`TestMemoryBudget`, one `cell.set` on 200,000 x 10, N =
  68.7 MB): 2,246 MB allocated in five whole-sheet tokenizer passes fell
  to 1,011 MB (14.7 N; budget 17.7 N): the gate's input check is skipped
  for a clean output, `splice.NewWellFormed` lets `Patch.Check` stop
  after the last span over an original the edit layer has already
  scanned, and the changed range is read back only to its last row
  (`read.CellsRequest.CellsOnly`). Peak live is 82.8 MB (1.21 N, at the
  seal) within 1.3 N + T. About 740 MB of what is left is
  `tree.ScanString`'s per-node garbage over the two passes that remain
  (the edit index of the input, the gate on the output): a tokenizer
  that reuses scan nodes is the follow-up. `shift.SheetData` (a row
  insert near the top of 1M cells, N = 35 MB) holds 37.2 MB (1.06 N) and
  allocates 72 MB (2.06 N); budgets 1.3 N + 1 and 2.5 N.
- **Scaling** (`make scaling`, 1,000 and 4,000 units, ratios bytes and
  allocations): `cell.set` 3.92 and 4.00, `row.insert` 3.98 and 4.00,
  `cell.set` among 250 and 1,000 sheets 3.09 and 3.64 (the workbook's
  relationships are mapped once in the edit layer and the integrity
  check, as read already did).
- **`range.format` cap** (100,000 cells, a 10-column grid): 1,846 MB
  allocated (3,400 MB before the style escaper was built once and
  `WithFormat` asked once per base style), peak live 54.8 MB, 4.9 s
  on a loaded machine. What is left is linear: about 17 KB a cell of
  `edit.Sheet.parseCell` (a `sheet.Scan` of the one cell, done three
  times a cell). The cap stands.
- **Corpus** (`TestApplyCorpus`, 1,484 workbooks, the first sheet of
  each): `cell.set` 1,430 ok, 15 refused (`unreadable_target`: a
  chartsheet first, rows out of order, styles without cellXfs);
  `range.format` 1,386 ok, 59 refused (36 have no styles part: making
  one is a follow-up); `row.insert` at row 1, 962 ok, 469
  `operation_failed`, nearly all v1's closed list (drawings, VML and
  comments, pivots, external links, x14 and protected ranges);
  `column.delete` with `sheet.rename`, 887 ok. Nothing internal or
  `invalid_output`. Before V's fixes it found new rows of an empty
  sheetData written in map order (166 files, all refused by the gate),
  renames that lost the root's namespace scope (51), cells stored
  without `r` refused (7), whole-row sqrefs and corpus defects as
  internal errors, and a deleted selection refusing a delete (105).
- **Formulas.** A moved reference is written in Excel's form, an
  unmoved one keeps its spelling. `Display` shows a formula a rewrite
  would change but that does not validate as stored (fuzzing joined
  tokens of malformed ones), and is not injective on `SINGLE`:
  `SINGLE(-F5:F7)` and `SINGLE((-F5:F7))` both show `@(-F5:F7)`, and
  `Store` writes the second, Excel's form.
- **Verified end to end** (`internal/xlsx/apply/e2e_chunk_test.go`):
  every op through `apply.Run` with the real gate, untouched parts byte
  for byte, the recalculation policy, the read round trip of changed
  units, dry run equal to the run, determinism, atomicity at every save
  stage, identity writes, sequencing and aliases, and `--recalc` with
  LibreOffice 26.2.4.2. Excel itself is unconfirmed (E1 to E13).

### Verification

The corpus is `XLSX_CORPUS` (gitignored), fetched at pinned commits by
`eval/corpus/fetch-xlsx.sh` with its manifest of source and licence
(`eval/corpus/xlsx-sources.tsv`); EPPlus samples are excluded
(non-commercial licence). Excel is the only check for "opens without
repair": `make excel-smoke` (`tools/excel-smoke`) opens a source and its
outputs, judged relative to the source; it force-quits Excel and the
maintainer approves every run. `make conformance` gains xlsx outputs
(the Open XML SDK validator already opens .xlsx). An independent naive
reader under `eval/` is compared with `xlsx read` over the corpus.
LibreOffice headless is at most a second opinion on recalculation,
never a gate.

### Phases

- **P0 groundwork, no behaviour:** the contract draft, this section,
  archtest and Makefile, the `cmd/xlsx` skeleton, sml schema tables,
  ctypes, the corpus script, excel-smoke and the E1 to E8 fixtures.
- **P1, four parallel tracks:** read (`internal/xlsx/{a1,model,sheet,read}`),
  formula, engine (splice and the opc, plan and archtest changes) and
  integrity (the full streaming check).
- **P2 write core:** workspace, overlay, op table with every v1 op
  pending, apply on `plan.Begin`/`FinishSealed`, the recalculation
  policy and the scoped gate; `cell.set`, `range.set`, `range.clear`,
  `text.replace` and `create`.
- **P3 op families in parallel:** structure, sheets, format, names.
- **P4 hardening:** memory, the full corpus sweep, an excel-smoke
  campaign, the Pi extension and bench tasks.
- **v1.1:** sheet delete, move and duplicate; tables; comments;
  hyperlinks; dynamic arrays; display text.

From P2 on every phase is also green on `make conformance` and
excel-smoke.

### Excel experiments (E1 to E13)

Fixtures: `testdata/xlsx/experiments`, written by its Go generator; its
README says what to look for in each. Results are recorded here, with
the Excel version, before the code that relies on them merges.

**First run, 2026-11-14: LibreOffice 26.2.4.2 headless (CSV): Excel
unconfirmed.** Excel is not installed on the maintainer's machine, so
the owner chose LibreOffice for now: each fixture was converted headless
to CSV. A headless conversion shows no dialogs, so every repair-prompt
question (E1 to E5, E8) stays open until Excel is available. The
E1 fixtures hold no cached results, and LibreOffice computes formulas
only then: its xlsx import (`recalcFormulaCells` in
sc/source/filter/oox/workbookfragment.cxx) hard-recalculates when the
file has no calculated formula results or the user's setting says
always, and otherwise keeps the cached values, ignoring
`fullCalcOnLoad` (LibreOffice bug 144819). E9 shows it: an edited input
with stale cached dependents loads with the stale values, flag or not.
So LibreOffice at default settings, like pandas and calamine, shows a
stale value until Excel saves the file. The design does not change: E6
confirms writing `_xlfn.`, and E7 supports refusing spill functions in
v1.

**Second run, 2026-11-14 (E9 to E13): LibreOffice 26.2.4.2 headless
(CSV, and a round trip to xlsx): Excel unconfirmed.**

| # | Question | Fixture | Result |
| --- | --- | --- | --- |
| E1 | Does a formula cell with no `<v>` open cleanly, and is it computed, with and without `fullCalcOnLoad`? | `e1-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. Both variants load; A3 shows 5. Repair prompt and Excel's recalculation open |
| E2 | Does a calcChain entry naming a non-formula cell prompt a repair, and does a workbook with the calcChain removed open cleanly? | `e2-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. Stale and removed both load; A3 shows 5. Repair prompt open |
| E3 | Do `inlineStr` cells open cleanly and survive a save? | `e3-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. Shared, Inline, `  padded  ` (spaces kept), the tab, and `_x0041_` shown literally. Repair prompt and save open |
| E4 | Does a shared-strings `count` lower than the real count prompt a repair? | `e4-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. one, two, three all shown. Repair prompt open |
| E5 | What does Excel do with a shared-formula child whose master is missing? | `e5-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. Loads; the orphaned child B2 is empty (its stored 6 is dropped); A1 2, B1 4, A2 3. Repair prompt open |
| E6 | Is a newer function written without `_xlfn.` computed or shown as `#NAME?`? | `e6-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. A3 `CONCAT` and A4 `TEXTJOIN` without `_xlfn.` give `#NAME?`; A5 `_xlfn.CONCAT` gives `abcd`: write the prefix |
| E7 | What does a spill function written as a plain formula (no array, no metadata) show? | `e7-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. A1 `_xlfn.SEQUENCE(3)` shows 1, no spill; C1 `SEQUENCE(3)` gives `#NAME?`: supports refusing spill functions in v1 |
| E8 | Does the blank template `create` will use open cleanly? | `e8-*` | LibreOffice 26.2.4.2 headless (CSV): Excel unconfirmed. Loads cleanly. Repair prompt open |
| E9 | After an input changes, does the reader show the stale cached dependents or recompute them, with and without `fullCalcOnLoad`? | `e9-*` | LibreOffice 26.2.4.2 headless: Excel unconfirmed. Both variants show A1 7 but A3 5 and A4 50, the stale cached values: `fullCalcOnLoad` is ignored |
| E10 | Does a table whose `tableColumn` name differs from its header cell prompt a repair? | `e10-*` | LibreOffice 26.2.4.2 headless: Excel unconfirmed. Both load with the cells as written; the round trip drops the table part in both, so LibreOffice says nothing here. Repair prompt open |
| E11 | Do empty containers (`mergeCells`, `hyperlinks`, `conditionalFormatting` with an empty `sqref`, `dataValidations`, `definedNames`, `tableParts`) prompt a repair? | `e11-*` | LibreOffice 26.2.4.2 headless: Excel unconfirmed. All six load; the round trip writes none of them. Repair prompt open |
| E12 | Does a row whose `spans` understates its cells lose cells? | `e12-*` | LibreOffice 26.2.4.2 headless: Excel unconfirmed. All five cells shown |
| E13 | What happens to a `Print_Area` name holding `Sheet1!#REF!`? | `e13-*` | LibreOffice 26.2.4.2 headless: Excel unconfirmed. Loads; the round trip keeps the name, lowercased to `sheet1!#ref!`. Repair prompt open |


## create: tables, lists, images and read's markup (2026-11-14)

- `build.Blocks` is document HTML as paragraphs and tables (cells hold
  paragraphs, lists and nested tables); `build.Paragraphs`, which the
  operations use, still refuses table structure and images.
- Tables (`build.PlaceTable`): HTML's table grid (each cell at the first
  free column of its row, a rowspan covering the columns below) written as
  table.insert writes a table (NewTable's grid of 2400-twip columns and
  automatic width; a plain grid is byte-identical): colspan as
  `w:gridSpan`, rowspan as a `w:vMerge` restart over continuation cells
  (which carry the cell's style and an empty paragraph), a short row as
  `w:gridAfter`, at most 63 grid columns (Word's). The skeleton is parsed
  whole: CT_Tbl and CT_Row content is not tabulated for one-at-a-time
  placement. The style read writes (view's tableCSS) is stored by its
  inverse (`build/table_css.go`); row properties are ordered by
  `build.RowPropertyOrder` (CT_TrPrBase is a choice; the table ops keep
  the same list in `ops/table_props.go`, to be shared). A cell, the body
  or a section that would end in a table ends with an empty paragraph.
- Lists: `build.NewListIn` reads the numbering part once per lowering;
  before, each list rescanned every definition (create was quadratic in
  lists: 38.7 s at 2,000 lists, now 1.26 s).
- Images: `<img src="data:image/png|jpeg|gif;base64,...">`, size in CSS
  pixels (9525 EMU, read's unit), the image's own (`image/*`
  DecodeConfig, 96 dpi) when not given, aspect kept from one side. One
  media part per distinct image (`Workspace.AddPartFrom`, related from
  the part that shows it; `RelateFrom` for another part), drawing ids
  counted across the document. read's `src="assets/iN.png"` is refused at
  `html<img>` pointing at `read --kind assets`; there is no `--assets`
  flag (decided 2026-11-14).
- read's markup: `<section>` attributes are accepted on `<section>` and
  ignored, so `create(read(doc))` keeps the new document's page, not the
  source's (the plan's decision: derived). A PAGE or NUMPAGES placeholder
  is a new field, an equation's data-ref is dropped, any other
  placeholder is refused at `html`.
- Laws (`internal/docx/create/laws_test.go`): read(create(html)) shows
  each family as written, and read's markup sent back to create reads the
  same, paragraph addresses aside. Scaling: `TestScaling` in
  internal/docx/create (tables, lists, images), in `make scaling`.
