# vsdx: design (draft for the Fable advisor and the owner, 2026-11-17)

The fourth CLI, `vsdx`, reads and edits Microsoft Visio drawings
(.vsdx, .vsdm, .vstx, .vstm; stencils .vssx/.vssm read only) on the same
engine as `pptx`, `docx` and `xlsx`. The owner's instruction: "Add a vsdx
tool to documentdriver patterned after pptx tool." The agent surface is
merged into `docs/contract.md` on 2026-11-17 (the draft,
`docs/vsdx-contract-draft.md`, is kept as the record).

Format facts come from the researcher's notes (kept beside the work as
`.worktrees/vsdx-work/vsdx-format-notes.md`, not committed; tags [C]
confirmed in a fetched source, [K] knowledge, UNCONFIRMED). Every format
behaviour below that the notes mark [K] or UNCONFIRMED is an experiment
(VE1 to VE14, Verification). No Visio has been available: none has been
run in Visio, the code that relies on them merged, and `docs/contract.md` labels
each claim that rests on one "Visio unconfirmed" until its result is
recorded here.

Status: P0 skeleton and the shared promotions are on `wt/vsdx-design`
(see "What landed with this design"). Nothing reads a file yet. The
Fable advisor approved the design on 2026-11-17 with amendments; they
are applied here (section 11 lists them). The owner decided all seven
open items on 2026-11-17 (section 10): 1, 3, 4, 6 and 7 approved as
recommended; 2 clarified (the MS-VSDX schema may be used but is never
committed); 5 reversed
(create is designed now, section "Create").

## 1. Principles

- **Pattern after pptx.** Same commands, the same read shape (a list
  view, then one container's objects in paint order with `box`, `rot`,
  `flip_h`, `flip_v`, `from`, `to`, `from_glue`, `to_glue`, `fill`,
  `line`, `html`), the same plan grammar (`object.move`, `object.set`,
  `object.add`, `object.delete`, `object.reorder`, `text.set`,
  `text.replace`), the same hazards (`stale-connector`,
  `dangling-glue`), `--detail sites|full`, `--units`, `--ids`,
  `--region`, `xml <file> <address>`. An agent that knows `pptx` guesses
  `vsdx`. Every difference is forced by the format and listed in the
  contract draft (section 13).
- **Derive, never decide; never compute what Visio computes unless the
  owner says so** (decision c). Visio trusts cached values on open (notes
  §2, [C] Microsoft how-to), and every non-Visio reader (LibreOffice,
  POI) shows cached values only (notes §10.1, tested). So an edit that
  leaves a dependent cached value stale is visibly wrong outside Visio.
  The design is explicit about which cached values an op rewrites and
  which it leaves stale, and reports the stale ones.
- **Reuse.** Everything below the format vocabulary is the shared
  engine. The capability inventory (section 2) is binding for the
  implementers: a "Must not implement" item in a chunk is rejected in
  review.
- **Untrusted input.** `opc.DefaultLimits`, the tokenizer's depth cap,
  iterative walks with explicit depth or cycle limits (group nesting,
  style chains, master chains), notes instead of failures for broken
  parts (contract section 10).

## 2. Capability inventory

Counts: REUSE 26, PROMOTE 4 (2 done with this design; the CSS parser
and the render pipeline as chunks S2 and RN1), NEW SHARED 3, NEW
vsdx-only 17.

| # | Capability | Category | Helper | Justification |
|---|---|---|---|---|
| 1 | Open a zip/OPC package with limits | REUSE | `opc.Open`, `opc.OpenBytes`, `opc.DefaultLimits` | as every format |
| 2 | Part bytes without pinning a tree | REUSE | `opc.Part.OriginalString`, `Part.Forget` | find and the summary visit pages one at a time (xlsx precedent) |
| 3 | Relationships and targets | REUSE | `opc.Package.Rels`, `opc.RelsName`, `opc.PartKey` | document, pages, masters, page images |
| 4 | Content and relationship types | REUSE after S0 | `internal/ooxml/ctypes` constants | archtest forbids literals; the Visio entries need Visio-authored sources (S0) |
| 5 | Parse XML losslessly | REUSE | `tree.ParseString`, `tree.Node` API, `tree.NSStack` | pages, masters, document.xml |
| 6 | Stream XML with memory in the depth | REUSE | `tree.Scan`, `tree.ScanString` | summary object counts |
| 7 | Escape and decode XML and HTML text | REUSE | `xmlscan.EscapeText`, `EscapeAttr`, `DecodeText`, `DecodeAttr` | HTML output, P2 cell values |
| 8 | Exact source bytes of an element | REUSE | `tree.Node.Bytes` | `vsdx xml` (as `pptx xml`) |
| 9 | Command grammar, help, version, exit codes | REUSE | `cli.App`, `cli.Command`, `cli.Flag`, `cli.OutFlag` | contract section 1 |
| 10 | Errors object | REUSE | `cli.Errorf`, `cli.Wrap`, `cli.Errors`, `cli.Code*` | contract section 2 |
| 11 | File hash | REUSE | `cli.Hash`, `cli.CheckBase` | `base` |
| 12 | JSON out | REUSE | `cli.Marshal`, `cli.WriteJSON`, `cli.NonNil`, `cli.NonNilMap` | archtest TestJSONThroughCLI |
| 13 | Plan envelope and decoder (P2) | REUSE | `cli.ReadPlan`, `cli.ParsePlan`, `cli.Decoder`, `cli.DecodeEnum`, `cli.ValidAlias` | one decoder |
| 14 | Op table and generated help (P2) | REUSE | `cli.OpIndex`, `cli.FieldDocs`, `cli.OpsHelp`, `cli.OpHelp`, `cli.FindOpHelp` | never a hand-written op list |
| 15 | snake_case of stored names | REUSE | `cli.Snake` | `name_u`, raw cell keys stay as stored (data) |
| 16 | Transactions, write set, verifier, result, hazards (P2) | REUSE | `plan.Begin`, `plan.Tx`, `plan.WriteSet`, `plan.Hazard`, `plan.MarkNew`, `plan.Result`, `plan.Units` | archtest: only plan saves |
| 17 | Edit core handles and placed inserts (P2) | REUSE | `internal/ooxml/edit` (`Handle`, `Ensure`, `AppendEach`, `Before`, `After`, `SetText`, ...) | tree mutation only in the core |
| 18 | Fresh GUIDs and clock (P2) | REUSE | `ids.Source`, `ids.Fixed`; hidden `--seed`/`--clock` as pptx | `UniqueID` of a new shape or page |
| 19 | HTML5 front end (P2 text.set) | REUSE | `htmlx.ParseHTML` with `Strict` | one HTML policy |
| 20 | ASCII case helpers | REUSE | `ascii.EqualFold`, `ascii.Lower` | unit codes, booleans |
| 21 | Law harness | REUSE | `laws.Suite`, `laws.Scales`, `laws.WithinMemory`, `laws.Kept`, `laws.Untargeted`, `laws.Parts` | read laws in P1, write laws in P2 |
| 22 | Corpus suites | REUSE | `corpustest.Files`, `corpustest.Run`, `corpustest.Shard`, `MaxParallel` | VSDX_CORPUS sweeps |
| 23 | LibreOffice headless | REUSE | `soffice.Find`, `soffice.Run` | render oracle (no exec elsewhere) |
| 24 | New zip bytes for fixtures | REUSE | `opc.BuildZip` | vsdxtest (no zip.NewWriter) |
| 25 | Repo root in tests | REUSE | `internal/repo` | fixtures |
| 26 | Sorted map iteration | REUSE | `slices.Sorted(maps.Keys(m))` | determinism |
| 27 | Points, affine maps, box/rotation/flips, two-decimal number form | PROMOTE (done) | `internal/geom`: `Pt`, `Affine`, `Translate`, `Scale`, `Rotate`, `SinCosDeg`, `NormDeg`, `Box`, `Box.Matrix`, `Decompose(m, w, h, flipV, tol)`, `FormatNum` | was `internal/pptx/derive` (xform.go, units.go); vsdx needs the same decomposition to show a group child's box as pptx does; pptx keeps aliases, behaviour unchanged |
| 28 | Simple Unicode case folding for `find --ignore-case` | PROMOTE (done) | `internal/casefold.Simple` | was `internal/xlsx/model.Fold`; same approved rule for vsdx find |
| 29 | CSS inline-style declarations parser | PROMOTE (P2, chunk S2) | `internal/htmlx.StyleDecls`, from `internal/docx/html.Declarations` (docx keeps a forwarder until its callers move) | text.set parses `style`; three private copies exist (docx view `cssPairs`, docx html `Declarations`, pptx `cssDecls`); vsdx must not add a fourth |
| 30 | Visio content and relationship types | NEW SHARED (S0) | `ctypes` entries via `tools/ctypesgen -extract` with a `vsdx-*` source and Visio in `officeApps` | needs Visio-authored files (owner decision 1) |
| 31 | Visio child order (writes, create) | NEW SHARED (S3) | `internal/ooxml/schema` table `v`, generated by `tools/vsdorder` from the MS-VSDX XSD (fetched into a gitignored cache by `eval/corpus/fetch-vsdx-schema.sh`, pinned by sha256), cross-checked against the 53 Visio-authored corpus files, the corpus winning every disagreement; table and provenance committed, the XSD never | edit-core inserts read a generated child order, never a hand-written one (owner decision 2 as clarified) |
| 32 | `visio-smoke` (designed, not built) | NEW SHARED tool (later) | `tools/visio-smoke` on `tools/internal/officeapp` conventions | owner ruling 3: no Windows Visio now |
| 33 | Visio vocabulary: namespaces, names, units, style categories, addresses | NEW vsdx-only | `internal/vsdx/vsd` | the only home of Visio literals (archtest rule W1) |
| 34 | Package structure: document, pages, masters, style sheets, fonts, custom props | NEW vsdx-only | `internal/vsdx/doc` | Visio part graph |
| 35 | ShapeSheet index | NEW vsdx-only | `internal/vsdx/sheet` | Cell/Section/Row keys |
| 36 | Inheritance: master shape, style chain, origins | NEW vsdx-only | `internal/vsdx/inherit` | Visio-specific chain |
| 37 | Pin/LocPin/Angle/Flip to the view frame | NEW vsdx-only (on geom) | `internal/vsdx/place` | Visio transform semantics; arithmetic on `geom` |
| 38 | Text mixed content (cp/pp/tp/fld) to HTML | NEW vsdx-only | `internal/vsdx/text` | Visio text model; renders the pptx grammar's subset (law X1) |
| 39 | Read model | NEW vsdx-only | `internal/vsdx/model` | objects, glue graph, props, layers, hazards |
| 40 | read, find, xml results | NEW vsdx-only | `internal/vsdx/read` | contract shapes |
| 41 | Fixture builder | NEW vsdx-only (tests) | `internal/vsdx/vsdxtest` on `opc.BuildZip` | builds V1 to V17; the fixture XML and goldens are designer-authored, the implementer writes only the builder |
| 41a | Colour indexes: built-in palette 0 to 23 and the document's Colors | NEW vsdx-only | `vsd.BuiltinColor`, `doc.Document.Color` | 2,347 corpus colour cells are indexes; 0 to 23 are never stored |
| 42 | ShapeSheet formula lexer, constant check, closed evaluator (P2) | NEW vsdx-only | `internal/vsdx/formula` | decision c; xlsx's `formula` is Excel's grammar |
| 43 | Edit layer (P2) | NEW vsdx-only | `internal/vsdx/edit` | handles on Shape/Cell/Row through the core |
| 44 | Op table and kernels (P2) | NEW vsdx-only | `internal/vsdx/ops`, `internal/vsdx/apply` | as xlsx's layering |
| 45 | Integrity gate (P2) | NEW vsdx-only | `internal/vsdx/integrity` | unique IDs, Connects resolve, one rel/override per part (notes §9) |
| 46 | CLI | NEW vsdx-only | `cmd/vsdx` | the binary |
| 47 | XML of created things: master-less shapes, connectors, text, pages | NEW vsdx-only | `internal/vsdx/emit` | pure writers in the corpus's forms; used by the ops and create |
| 48 | create: blank template and run | NEW vsdx-only | `internal/vsdx/create` | as xlsx create |
| 49 | Render pipeline (soffice, PDF cache, rasteriser, marks) | PROMOTE (RN1) | shared `internal/render` from `internal/pptx/render` | vsdx render must not copy pptx's |

Not reused, on purpose: the pptx text package (`internal/pptx/ops/text`).
Its Body model holds DrawingML values (`algn="ctr"`, `u="sng"`, scheme
colours); promoting it would mean a second, format-neutral text model
and a rewrite of 5,000 lines of pptx. vsdx instead renders the **same
grammar** (tags, attribute names, CSS property names and order, `;`
separators) from its own Visio run model, and law X1 (section 8) checks
under `tools/` that every HTML `vsdx read` writes on the fixtures and the
corpus is accepted by pptx's `text.Parse` (placeholders aside). Revisit
if a fifth format needs the grammar.

## 3. Package layout and architecture rules

```
cmd/vsdx/                 main, read, find, xml (P1); apply, help ops (P2); create (P3)
internal/geom/            shared (promoted): points, affine, box decomposition, number form
internal/casefold/        shared (promoted): find --ignore-case
internal/vsdx/
  vsd/                    vocabulary and addresses (only home of Visio literals)
  doc/                    package structure; page/master contents loading
  sheet/                  ShapeSheet index (Key, Cell, Del)
  inherit/                effective cells, rows, text with origins
  place/                  transforms to the view frame; sites; 1-D ends
  text/                   Text -> Body -> HTML, plain text
  model/                  read model: Page, Object, Connection, Prop, Layer, hazards
  read/                   read/find/xml results
  vsdxtest/               fixtures (tests only)
  formula/  edit/  ops/  apply/  integrity/   (P2)
testdata/vsdx/<fixture>/  hand-written XML members + PROVENANCE.md
```

Dependency order (no cycles): vsd <- sheet <- doc <- inherit <- place,
text <- model <- read <- cmd/vsdx. `place` and `text` import no `doc`
(pure kernels over numbers and nodes).

archtest:

- Done: `vsdx` joins `formats` (internal/vsdx imported only by cmd/vsdx,
  eval/, tools/); `internal/geom` and `internal/casefold` join the shared
  engine.
- W1 adds `TestVsdxVocabulary`: no Visio namespace literal
  (`schemas.microsoft.com/office/visio`) and no address prefix built by
  concatenation (`"page"+`, `"master"+`, `"/s"+`, `"prop:"`, `"layer:"`)
  outside `internal/vsdx/vsd` (as `TestXlsxVocabulary`).
- P2: `internal/vsdx/edit` joins `partWriterPkgs` (page and master
  parts are trees: no splice; see Performance), and `internal/vsdx/ops`
  and what it imports never depend on `internal/vsdx/model` (as pptx).

## 4. Decisions

### (a) What read shows: effective values, origins on request

`read --page` shows each object's **effective** values (pptx shows a
placeholder's inherited box and fill the same way): the cell the shape
stores, else its master shape's, else the style sheet chain of the
cell's category (LineStyle for line cells, FillStyle for fill and
shadow, TextStyle for text block, Character, Paragraph and Tabs), notes
§2 step 1 to 3. A cell no sheet stores is **absent** (left out of the
object; with `--detail full` listed in `omitted` with the reason), never
a Visio default. A value stored as `Themed` (12,058 corpus cells, always
with a capital T; no cached value) is omitted with reason `themed value
not derived` (theme resolution is not in v1). StyleSheet 0 ("No Style")
stores nearly every cell in every corpus document (line, fill, text
block, locks, events, a Character and a Paragraph row, and in 55
documents PrintPageOrientation), so most effective values of a
master-less shape have origin `style`. Only the cells of the style
category table (V1) are looked up in styles; other cells StyleSheet 0
stores (locks, events) are not shown.

How the agent tells them apart: `--detail full` adds, as pptx does,
`origins` (`{"box":"local","fill":"inherited","html":"master","line":"style"}`)
and `cells`, the shape's own stored cells (`[{"n":"PinX","v":"2.5","f":"..."}]`,
key spelled in ShapeSheet reference syntax where Visio has one:
`PinX`, `Prop.Cost.Value`, `Geometry1.X2`, `Connections.X1`,
`Char.Size[1]`, `Para.HorzAlign[1]`; layers, which formulas cannot
name, `Layer[1].Name`; `model.CellKey`, chunk M7). Origins:

| Origin | Meaning |
|---|---|
| `local` | the shape stores the cell with its own value or formula |
| `inherited` | the shape stores the cached value, but its formula is `F="Inh"` (master's or style's) |
| `master` | the shape stores nothing; its master shape does |
| `style` | neither; a style sheet on the chain does |

A property shown from several cells (box: PinX, PinY, Width, Height,
LocPinX, LocPinY, Angle, FlipX, FlipY) takes the "most inherited" origin
of its cells, in the order local < inherited < master < style.

Why: agents need what the drawing looks like; Visio caches inherited
values on instances anyway (notes §2), and a read of local cells only
would show an empty instance for most master shapes. Origins tell the
agent what an edit overrides.

### (b) Units and coordinates

The view frame is pptx's: **points, origin at the page's top-left
corner, y down, rotation in degrees clockwise**, `box [x, y, w, h]` the
unrotated box whose centre is the shape's centre, flips about that
centre, group children in page coordinates with `parent`. Visio stores
inches, y up from the lower-left, radians counter-clockwise about the
pin (notes §3). `place` maps: local point p to parent `Pin + R(Angle) *
diag(fx, fy) * (p - LocPin)`; groups compose; the page map is
`(x, y) -> (72 x, 72 (PageHeight - y))`; `geom.Decompose` gives the box.
1-D shapes show `from` and `to` (BeginX/Y, EndX/Y through the parent
frame), and their box only with `--detail full`, as pptx connectors.

Numbers written to cells are the shortest decimal that reads back to
the same double (`vsd.FormatInches`, strconv 'f' -1). Visio writes its
own 16-to-17-digit spelling (0.1666666666666667 for 12 pt, which is not
the same double as 12/72 = 0.16666666666666666); both are read alike,
and a write keeps a stored value that already shows as the requested
one, so identity writes stay no-ops (decided 2026-11-17, K5b).

`--units in` (and plan `units: "in"`) shows lengths in stored inches,
written as Visio writes numbers (`vsd.FormatInches`): the native-unit
twin of pptx's `--units emu`. Coordinates stay y down in both units;
only the scale changes.

Why y down: every agent-facing view of a drawing (pptx JSON, SVG, any
render, LibreOffice's PNG) is y down, and a `box` an agent copies from
`pptx read` into a vsdx plan lands in the same place. Cost: the shown y
depends on PageHeight, so a page orientation or size change moves every
shown y (stored PinY does not change; see the orientation rule). Stored
values are always visible in `cells`.

Scaled drawings (PageScale != DrawingScale) are **pending VE11**: the
notes cannot confirm whether stored lengths are page or drawing inches.
Until then read shows stored values unscaled and adds a page note
`drawing scale <PageScale> = <DrawingScale>: lengths are stored drawing
units`, each with its stored unit (V13: `1 in = 120 ft`; CC6; the
binary at 7d09f90 wrote "1 in = 10 ft" for every page);
P2 refuses geometry writes on such pages (`operation_failed`).

### (c) Formulas and cached values

Facts (notes §2, §10.1): a Cell has `V` (cached value, internal units),
optional `F` (formula, `Inh`, or `No Formula`), `U` (display unit), `E`
(error). Visio does **not** recalculate on open; it recalculates a cell
marked with the processing instruction `<?NewValue V?>`, and the whole
document when `docProps/custom.xml` has `RecalcDocument` true ([C]).
LibreOffice and POI never recalculate.

Policy (the xlsx precedent made explicit for Visio):

1. **Read** shows cached values only (effective, as (a)); formulas only
   in `--detail full` `cells`. `values_stale: true` in the summary and
   on a page when the document has `RecalcDocument` true.
2. **What an op writes on its target cell:** `V` in internal units
   (`vsd.FormatInches` digits), keeps `U`, and removes a local `F`. It
   writes the cell only when the cell's effective formula is absent,
   `Inh` resolving to a constant, `No Formula`, a constant (a number
   with an optional unit), `THEMEVAL()` or `THEMEGUARD(...)` (a themed
   colour the agent overrides, as typing a colour in Visio does).
   Anything else (`GUARD(...)`, a reference such as `Sheet.10!Width*0.25`
   on a group child, `PAR(PNT(...))` on a glued end) is **refused**,
   `operation_failed`, `detail` naming the cell and its formula. The
   allow list is a table in `formula` (P2), one test row each.
3. **Dependent cached values.** Owner decision 3 chooses between:
   - **(R, recommended)** a closed evaluator rewrites the dependents an
     op's own shape holds whose formulas are in a closed grammar:
     numbers with units, `+ - * /`, parentheses, unary minus, and
     references to the same shape's cells (`Width`, `Height`,
     `LocPinX`, `TxtWidth`, `Geometry1.X1`, ... without `Sheet.N!`),
     evaluated in dependency order with a depth limit. This covers the
     dependents of Width and Height that real masters hold (LocPin,
     Geometry rows, text block, connection points, notes §3 and §4).
     Every other dependent (another shape's formula, a function, a page
     reference) is left as it is, the written cell gets
     `<?NewValue V?>`, the document gets `RecalcDocument` true, and the
     result reports `stale-values` (`new` true) naming the first few
     cells. A resize then renders right in LibreOffice and Visio
     recalculates the rest.
     **R closure (recorded now, binding at P2):**
     - *All or nothing per shape.* For each shape an op changes, R
       collects the shape's dependents of the changed cells (the
       transitive closure over same-shape references). If every one is
       in the closed grammar, R rewrites them all; if any is outside it,
       R rewrites **none** of that shape's dependents and the shape
       falls back to S. A shape is never left half recomputed.
     - *Dependents held in a master.* A dependent the shape does not
       store (its formula lives on the master shape, often as the
       instance's `F="Inh"`) is materialised locally as
       `<Cell N=.. V=.. F="Inh"/>` (the form Visio writes for an
       inherited formula with a recomputed value), or R does not apply
       to it and the shape falls back to S.
     - *Units are a closed list:* the grammar accepts the unit suffixes
       of lengths, angles and plain numbers only (`IN`, `FT`, `MM`,
       `CM`, `M`, `PT`, `DEG`, `RAD`, and none), each with its exact
       factor to internal units; any other unit makes the formula
       outside the grammar.
     - *Acceptance requires a corpus sweep* (chunk FL4): over every
       cell of `VSDX_CORPUS` whose formula is in the closed grammar, the
       evaluator's result on the file's own cached inputs equals the
       cached `V` (within 1e-9 relative). Disagreements are classified
       before R ships; an unexplained one blocks R.
     - *Connector transforms* (a 1-D shape's PinX, PinY, Width, Angle
       from BeginX/Y and EndX/Y: `SQRT`, `ATAN2`) are not in the closed
       grammar; they are one named kernel, `formula.OneD`, with its own
       rows, used by the glue and 1-D move ops.
   - **(S, xlsx-strict)** the tool never evaluates: every changed cell
     with dependents gets `<?NewValue V?>`, `RecalcDocument` is set,
     and `stale-values` is reported. A resize then renders with the old
     geometry everywhere but Visio (tested: libvisio draws cached
     geometry, notes §10.1).
4. **Moves** change PinX/PinY only (2-D) or BeginX/Y and EndX/Y (1-D).
   A move has no same-shape geometric dependents but the text block's
   pin can depend on nothing that moves; connectors glued to the moved
   shape are left as they are and reported as `stale-connector` (as
   pptx `object.move`), with `RecalcDocument` set so Visio reroutes
   them (whether it reroutes every connector is VE4).
5. A plan that changes nothing writes nothing (no RecalcDocument).
   `RecalcDocument` is the third stated exception to "untargeted bytes
   unchanged" (contract draft section 9), with `docProps/custom.xml`
   created when absent (its content type and rel from ctypes).

### (d) Connectors: glue as a graph, and glued writes

Read: a 1-D shape is `kind: "connector"` (pptx: `cxnSp`); `from` and
`to` its ends; `from_glue` and `to_glue` from the page's `Connects`
(`FromCell` BeginX or EndX picks the end; notes §5):

| Glue | ToCell | Shown |
|---|---|---|
| whole shape (dynamic) | `PinX` | `"s12"` |
| connection point n | `Connections.Xn` | `"s12:n"` (n as stored, 1-based) |
| anything else | e.g. `Geometry1.X2`, `AlignLeft` | `"s12@Geometry1.X2"` (read only) |
| missing shape | (ToSheet names none) | `"#12"`, and a `dangling-glue` hazard |

The page result also has `connections`, the graph: one entry per 1-D
shape with at least one glued end, `{"connector","from","to","from_glue","to_glue"}`
with `from` and `to` full shape addresses (`page0/s3`) or absent for a
free end. `stale-connector` (as pptx): a point-glued end farther than
0.5 pt from its site; a whole-shape-glued end farther than 0.5 pt
outside its target's box.

Write (P2): `object.set` with `from_glue` / `to_glue` (`"s12"`,
`"s12:3"`, `"$name"`, `"$name:3"`, `"none"`), as pptx; `object.add` of
a connector master with both. The op writes the two `Connect` rows
(FromPart 9/12, ToPart 3 or 100+n-1, notes §5 [C]/[K]), the trigger
cells `BegTrigger`/`EndTrigger` with `_XFTRIGGER(Sheet.N!EventXFMod)`
and the `_WALKGLUE` formulas when the connector's master has them
(spelling VE5), the ends' cached values at the site (point glue) or at
the target's nearest box edge midpoint toward the other end (whole-shape
glue, the derived fact pptx would use), the connector's own transform
cells and a straight two-point geometry consistent with them (by the
closed evaluator, decision c-R), and RecalcDocument so Visio routes it.
`"none"` removes the Connect row and keeps the end where it is. The
route Visio draws is not reproduced (notes §5); outside Visio the
connector shows straight until Visio saves the file.

### (e) Addresses

| Kind | Form | Stable |
|---|---|---|
| page | `page0`: `page` and the page's stored `ID` (Visio's first page is 0) | yes (ID survives Visio saves; part names do not) |
| master | `master2`: stored master `ID` | yes |
| shape | `page0/s5`, `master2/s5`; `page0/s5~2` for the second shape sharing ID 5. Group children are addressed directly (IDs are unique per page across nesting); `parent` gives the group | yes (`~n` positional) |
| shape data row | `page0/s5/prop:Cost` (the row name, `Prop.Cost` in ShapeSheet formulas) | yes |
| layer | `page0/layer:Connector` (the layer's name; index in `read`) | until renamed |
| connection site | `s12:3` on the connector's own page; `s12` the whole shape | yes |
| field placeholder | `page0/s5/field:1` (1-based in the shape's text) | positional |
| alias | `$name` | for the plan |

pptx's `slide3` is a part name; vsdx's `page0` is the stored page ID,
because Visio renumbers page parts on save and keeps IDs (notes §6).
The number is not a position: `read` lists pages in order.

### (f) Text model

Visio's `Text` is mixed content with run markers (notes §4 [C]): `cp IX`
(Character row), `pp IX` (Paragraph row), `tp IX` (Tabs row), `fld IX`
(Field row; content is the cached display text). A line feed (`\n`)
**terminates** a paragraph: a trailing `\n` adds no empty paragraph
(257 of the 285 page Text elements of the corpus, and 95 of 144 master
ones, end in `\n`), and `Body.Terminated` records it so a P2 write
reproduces it. `\u2028` is a line break inside a paragraph (VE7), `\t`
a tab. Text before any `cp` uses Character row 0, before any `pp`
Paragraph row 0, as Visio does (`CP -1` and `PP -1` in the model mean
row 0).

**Which formatting the HTML shows (matching pptx).** pptx's HTML shows
only an object's own run and paragraph properties, never inherited ones
(`internal/pptx/ops/text`, `Body`: "only the object's own paragraph and
run formatting, never inherited values"). vsdx does the same: a format
holds only the cells stored on the sheet that owns the shown Text (the
shape's own Character and Paragraph rows when it has its own Text; the
master shape's rows when it shows the master's Text). Style sheets'
rows (StyleSheet 0 sets Calibri 12 pt centred on every document) and
master rows under a shape's own Text are not shown, so a plain shape's
text is `<p>Hello</p>`, not wrapped in a 12 pt span. The "off" forms
(`font-weight:normal`, `font-style:normal`, `text-decoration:none`) are
shown only when the owning sheet stores a Style bit cleared that the
next sheet on the chain sets; pptx shows `b="0"` whenever stored, but
Visio stores Style on most rows, so the literal rule would put an off
form on almost every run. This is the one difference from pptx's rule
and is in the differs list. Formats are built by `model.formats`
(chunk M5).

| Visio | HTML (pptx form) |
|---|---|
| paragraph | `<p>`; bulleted (`Bullet` != 0) `<ul><li>`, `data-char` when `BulletStr` is stored |
| `HorzAlign` 0/1/2/3 | `style="text-align:left|center|right|justify"`; 4 (force) not shown, kept on write |
| `IndLeft`, `IndRight`, `IndFirst` | `margin-left`, `margin-right`, `text-indent` in pt |
| `SpBefore`, `SpAfter` | `margin-top`, `margin-bottom` in pt |
| `SpLine` (negative: a multiple) | `line-height` (`120%` or `14pt`) |
| `Style` bits 1, 2, 4 | `<b>`, `<i>`, `<u>` |
| `Strikethru` | `<s>` |
| `Size` (inches) | `font-size` in pt |
| `Color` (`#RRGGBB` or an index, resolved by `doc.Document.Color`) | `style="color:#RRGGBB" data-color="#RRGGBB"` (pptx's canonical form, decided 2026-11-17: data-color is "the colour as written"; for Visio that is always an sRGB value, so the mirror is true; X1 keeps its fixed-point law) |
| `Font` (the face name as stored: `Calibri`, `Arial`; `Themed` omitted) | `font-family:'Calibri'` |
| `\u2028` | `<br/>` |
| `fld` | `<opaque data-ref="page0/s5/field:1" data-kind="field" data-text="cached"></opaque>` (contract placeholder; `data-text` derived, ignored on input) |
| `tp`, `Pos`, `Case`, small caps, `DblUnderline`, `Letterspace`, `LangID`, `FontScale`, Asian and complex-script fonts | not shown; kept on write from the run or paragraph replaced (as pptx keeps east Asian fonts) |

**Fonts.** A `Font` cell stores the face name itself (corpus: Calibri
260, Themed 185, Arial 36, ...), never an index; `FaceName` elements
have `NameU` and no ID. `doc.Document.FaceNames` is the list of
declared faces, for information only.

**Colours.** A colour cell stores `#RRGGBB` or a decimal index (2,347
corpus colour cells outside layers are indexes: 0 1,706, 1 553, 4 56,
15 18, 2 9, 6 4, 255 1). A document's `Colors` element lists
`ColorEntry IX RGB` from IX 24 (54 of 57 documents have one; none has
an IX below 24). Indexes 0 to 23 are Visio's built-in palette, never
stored: `vsd.BuiltinColor`, a designer-owned table whose provenance
(the MS-VSDX ColorEntry/Colors description and libvisio's built-in
palette) chunk V3 records. `doc.Document.Color` resolves a cell value:
hex, then Colors, then the built-in table; an index neither holds (255)
is omitted with a note.

**Field placeholders on input (ruling 2026-11-17).** `field:N` is the
shape's N-th field in text order, as read numbers it. ParseHTML keeps a
placeholder unresolved (`text.Run.Ref`); the operation that writes the
text checks its shape and maps N to the IX of that shape's N-th `fld`.
A font name holding `;` is not shown in the html (the shared CSS
splitter splits on `;`), so read never writes markup apply refuses.

Adjacent runs that show the same formatting are merged (pptx's
`canonical`). A shape without its own `Text` shows its master shape's
(origin `master`); `text.set` on it writes local Text and local
Character/Paragraph rows only for what the HTML sets differently from
the inherited rows. `text.replace` on such a shape writes local Text
only: the master's Text markup (cp, pp, tp, fld) with the match
replaced, and no Character or Paragraph rows, which Visio inherits from
the master by row index, so the text looks the same in Visio. The html
then follows the rule above: the master's rows are under the shape's
own Text and are not shown (V2 s1 `<p><i>Process</i></p>` origin
master becomes `<p>Step</p>` origin local, Character row 0 Style still
2 with origin master). Copying the master's rows locally would change
nothing in the html (a local Style 2 over an inherited 2 shows
nothing) and would store redundant rows; and `text.set` of the html
read then shows writes nothing (the identity write). Designer ruling on
TRP, 2026-11-17. `text` (plain, `find`'s haystack) puts `\n` between
paragraphs, a break as `\n`, a field as its cached text.

### (g) Opaque and refused in v1 (closed list)

Read, not editable (kind `picture` or `other`, or shown and marked):
Foreign shapes (images, OLE, ink), guides, master shapes (all of a
master), comments (not shown in v1), hyperlinks, actions, user cells and
geometry (`--detail full` `cells` only), data-graphic callouts, themes.

Refused by P2 ops (the closed list; rewritten 2026-11-17 to match the
binary after Fable's contract review; "(CCn)" marks a refusal the
binary does not make yet and the chunk that adds it). `operation_failed`
unless given:

1. a target cell whose formula vsdx does not evaluate (decision c),
   `detail` each cell as `Width F=User.DefaultWidth`, `path` the field
   written (`ops[n].at` for object.move) (detail and path: CC10);
2. a container, list or callout (`User.msvStructureType` `Container`,
   `List`, `Callout`) for every write op, and a member of one (a
   `Relationships` cell with `DEPENDSON`) for box, rotation, flips,
   move and delete (CC6);
3. geometry on a page with a drawing scale: box, rot, flips, connector
   ends, object.move, object.add, page.set size (CC6; until VE11);
4. a data-linked shape data row (`DataLinked` 1) for `prop.set` and
   `prop.delete`;
5. any write to a master or a master shape, and any write to a stencil;
6. text and fill on a picture or other read-only kind;
7. glue to a cell other than a shape's pin or a connection point
   (`invalid_operation` at the glue field: the address form is
   checked before the document opens);
8. deleting the last foreground page;
9. deleting a page another page uses as its background;
10. a duplicate page name (page.add, page.rename);
11. a duplicate shape data row (prop.add);
12. object.reorder relative to an object of another parent;
13. a connector whose ends coincide (object.add, object.set, move);
14. `from`, `to` or glue on a 2-D shape (`invalid_operation` when the
    op says it is a shape; `operation_failed` when the target is);
15. object.move of a connector glued at both ends (CC2);
16. whole-shape glue on a master-less connector (CC2; Fable's ruling:
    all 17 corpus shapes with `_WALKGLUE` are Dynamic connector master
    instances, so no Visio-authored file shows the cells on a
    master-less shape), hint: add the connector from the Dynamic
    connector master;
17. deleting a sub-shape a master instance inherits (a shape with
    `MasterShape` inside an instance; V2 page0/s4 is accepted today)
    (CC6);
18. `page.set` orientation or size when PageWidth or PageHeight has a
    formula outside the allow list.

Not refused any more (Fable's review): whole-shape glue on connectors
from a master that stores the dynamic glue cells (CC2, CC3); free ends, point glue and move on Visio-drawn
connectors with the standard 1-D forms (CC1, CC2); object.add of a 1-D
master (CC3); glue to a shape without connection points (whole-shape
glue, CC2).

Everything else no op targets is copied byte for byte, unknown parts and
`v:` extensions included; `.vsdm` VBA copied through.

### Page orientation (owner ruling 1; approved, decision 6)

Corpus facts (verified): `PrintPageOrientation` is the **printer paper
orientation** (1 portrait, 2 landscape), not the page's shape. It is
stored on 34 of the 86 corpus page sheets; 55 of 57 documents store it
in StyleSheet 0 (which page sheets inherit through their style
attributes), and 85 master page sheets carry it. A page's shape is its
size.

**The rule (Fable's proposal, approved by the owner 2026-11-17):**

Read: each page (summary and page result) shows `size [w, h]`
(PageWidth, PageHeight), `orientation` derived from the size
(`"portrait"` when height > width, `"landscape"` when width > height,
absent when square or the size is not derived), and
`print_orientation`, the effective `PrintPageOrientation` of the page
sheet through its style chain (`"portrait"` 1, `"landscape"` 2, absent
when nothing on the chain stores 1 or 2), with its origin under
`--detail full`. The two may disagree (a landscape page printed
portrait); read shows both as stored and derived, never reconciled.

Write (P2): `page.set` with `orientation` and/or `size`:

1. `orientation` equal to the derived one: the size does not change.
   Different: PageWidth and PageHeight are swapped. A square page's size
   never changes.
2. The print orientation follows: after the size is settled, the page
   sheet stores `PrintPageOrientation` equal to the new orientation
   (1 or 2) **when** the effective print orientation is absent or
   differs from it (whether it came from the page sheet or a style);
   when it already agrees nothing is written. A local cell is written
   on the page sheet; StyleSheet 0 is never edited.
3. `size` alone sets PageWidth and PageHeight and then applies rule 2
   for the orientation the new size implies; `size` with `orientation`
   that contradicts it (wider than high with `portrait`) is
   `invalid_operation`.
4. No shape cell changes: stored pins stay measured from the page's
   lower-left corner (VE10). Every shown `y` moves by the change in
   height; `changed` lists the page and every top-level object.
5. Dependents of the page size (`ThePage!PageHeight*0.5` on a shape,
   page-sheet cells): per decision c (R rewrites those in the closed
   grammar on the page sheet; shape dependents on `ThePage!` are other
   sheets, so they stay stale with RecalcDocument and `stale-values`).
6. Objects whose box ends up wholly outside the new page are reported
   as a new hazard `off-page` (`id` the object), not moved.
7. Refused: a page whose PageWidth or PageHeight formula is outside the
   allow list (rule g9).

pptx has no orientation (a deck has one slide size); this is in the
differs list and fixture V17.

### Create (decision 5 reversed: designed now)

**What create means for Visio.** `vsdx create [<plan.json|->] [--dry-run] -o <out>`
writes a new drawing and, given a plan, applies it, exactly as `xlsx
create` (blank workbook plus an optional plan through apply). docx
create's HTML input does not fit: a Visio drawing is not a flow of
paragraphs, and pptx has no create. The plan is the ordinary vsdx plan
(contract draft section 3) without `base`.

**The blank drawing** (golden members committed in
`testdata/vsdx/create/blank`, with a provenance table). Every choice is
grounded in the 53 Visio-authored corpus files (docProps/app.xml
"Microsoft Visio"):

- members every one of the 53 has: `[Content_Types].xml`, `_rels/.rels`,
  `visio/document.xml`, `visio/pages/pages.xml` and its rels, a page,
  `docProps/app.xml`, `docProps/core.xml`, `visio/windows.xml`;
- not written: masters (13 of 53 Visio files have none; the corpus has
  142 master-less 2-D and 12 master-less 1-D shapes, 5 of them glued),
  theme (29 of 53), thumbnail (49 of 53), `docProps/custom.xml` (Visio
  build numbers only; created when a plan needs RecalcDocument);
- `document.xml`: DocumentSettings, FaceNames (Calibri), StyleSheets 0
  No Style, 1 Text Only, 2 None, 3 Normal, 4 Guide, 6 Theme (the IDs
  and names of 53 of 53, Theme in 52) with explicit values and no
  `Themed` cells (there is no theme part), DocumentSheet; no Colors
  element (one Visio file has none);
- one page, `page0` "Page-1", US Letter 8.5 x 11 in (the corpus is
  mostly A4, 43 of 56 pages; the owner may prefer A4; `page.set` changes
  it), PrintPageOrientation inherited from StyleSheet 0 (1, portrait),
  as 52 of 86 corpus pages inherit it;
- `docProps/app.xml` Application "documentdriver" (never claims to be
  Visio); `core.xml` only created and modified, from the transaction's
  clock (`ids.Source`; hidden `--seed`/`--clock`).

**Input and ops.** create runs the plan through apply on the blank
drawing, so every op works the same in create and apply. The ops create
needs first (and P2 therefore builds first): `page.add`, `page.rename`,
`page.set` (decision 6), `object.add`, `object.set`, `text.set`,
`prop.add`, `prop.set`. A new drawing has no masters, so `object.add`
gains two master-less forms, grounded in the corpus's master-less
shapes:

- `geom: "rect"` or `"ellipse"` with `box` (or `pin`), `html`, `name`,
  `fill`, `line`: a 2-D shape with styles 3 (Normal, as the corpus's
  master-less shapes), transform cells with LocPin formulas
  (`Width*0.5`), one Geometry section (rect: RelMoveTo/RelLineTo over
  the unit square, the form of 460 corpus RelLineTo rows; ellipse: one
  Ellipse row), and four connection points (X1 top centre, X2 right
  middle, X3 bottom centre, X4 left middle, with `Width*`/`Height*`
  formulas). The point numbering is ours (these are our shapes).
- `kind: "connector"` with `from`/`to` points and/or `from_glue`/
  `to_glue` **point glue** (`"$a:2"`, `"s3:1"`): a master-less straight
  1-D shape in the exact form of the corpus's glued master-less
  connector (apache-poi 60973.vsdx, shape 146): transform cells with
  `(BeginX+EndX)/2`, `SQRT(...)`, `ATAN2(...)` formulas and values from
  `formula.OneD`; a glued end `F="PAR(PNT(Sheet.N!Connections.Xk,
  Sheet.N!Connections.Yk))"` with the site's value; Geometry NoFill 1,
  MoveTo (0,0), LineTo (Width,0); Connect rows FromPart 9/12, ToPart
  100+k-1. Whole-shape glue (`"$a"`) stays refused on a master-less
  connector (`operation_failed`, path `ops[n].to_glue`, hint: add the
  connector from the Dynamic connector master): every corpus shape with
  `_WALKGLUE` is a master instance (Fable's ruling, CC2). A new drawing
  has no masters, so create offers point glue only.
- `object.add` with `master` on a new drawing is `not_found` (no
  masters). Copying masters from another file is the master-import
  chunk of P3 (notes §7's checklist).

**Pages with orientation**: `page.add` takes `size` and `orientation`
(decision 6's rule; a new page's PrintPageOrientation is written when
the inherited one disagrees with its orientation); `page.set` as
decided. New page IDs are max+1; new parts `visio/pages/page<n>.xml`
with the next free n, one Override and one relationship each (notes
§9's duplicate-part pitfall).

**Text**: `html` goes through `text.ParseHTML` (the grammar read
writes) and `emit.TextXML`: local Character and Paragraph rows only for
what the HTML sets, `cp`/`pp` markers, and a final line feed as Visio
writes (`Body.Terminated`).

**What is unverified and how create is accepted.** Whether Visio opens
a created file without repair, whether it keeps the master-less
connector glued when a shape moves, and whether the windows and
document settings we write are what it expects are all **unverified**
(no Visio). The acceptance checks are: (1) our read round trip: `vsdx
read` of every create output shows exactly what the plan asked for
(boxes, text, glue, props, pages, orientations: the CR1 and CL1 rows);
(2) LibreOffice import: conversion succeeds, every text appears, boxes
agree with read within 1 pt, the connector is drawn between its sites;
(3) the structural lint (integrity gate) finds nothing; (4) laws: the
same plan and clock give the same bytes, `--dry-run` equals the run,
and create with no plan equals the golden blank.

**Dispatch.** Create's kernels (`emit`, `formula.OneD`,
`place.Unplace`, the template, the child-order table, the CSS
promotion, the render promotion) depend only on frozen signatures and
start now, in parallel with P1. The edit layer, the op runs, apply and
the create command need P1's doc and read and come after (chunk plan).

### Render (approved, decision 7)

`vsdx render <file> --page pageN -o out.png [--overlay ids,sites,glue]
[--ids ...] [--scale n]`, as `pptx render`: headless LibreOffice to PDF
(through `internal/soffice`), then the rasteriser, one page, overlays
from the read model. The format-neutral part of `internal/pptx/render`
(finding soffice and the rasteriser, the PDF cache, rasterising a page,
drawing marks) is promoted to a shared `internal/render` first (RN1),
pptx keeping its slide locating; vsdx adds page locating (RN2: which
PDF page a Visio page becomes is measured on V8, background pages
included). LibreOffice draws cached values and ignores Visio-only
behaviour: the render is for checking, not proof.

## 5. Phases

Each phase merges on its own with main green (`make ci`), archtest rules
in force, and its own laws.

- **P0 skeleton (this branch + S0, W1, W2, F1, FX):** shared promotions
  (done), packages and signatures (done), `cmd/vsdx` wired (done),
  archtest `formats` (done), vocabulary rule, Makefile (`build` done;
  `REPRO_CMDS`, `FUZZ_TARGETS`, `scaling`, `corpus` with `VSDX_CORPUS`),
  ctypes from Visio-authored files (S0, blocked on owner decision 1),
  the corpus suites against `VSDX_CORPUS` (fetch script and manifest are on wt/vsdx-corpus), fixtures V1 to V17.
- **P1 read/find/xml:** chunks V1, V2, V3, H1, P1, T1 (pure, parallel),
  then D1 to D4, H2, H3, T2, M1, M2a to M2c, M3 to M7, R1 to R3, C1,
  L1a to L1c, X1, and W4 (every FUZZ_TARGETS line) last. Release wiring
  (wheel, release-smoke, RELEASING.md) when P1 merges (W3).
- **Create (now, in parallel with P1):** S2, S3, K1 to K6, CT1, RN1;
  then after P1's doc and read: E1, G1, O0, A1, the create ops, CR1,
  CL1, RN2 (chunk plan).
- **P2 apply:** the rest of the op list below on the create
  infrastructure (S2, S3, formula, edit, integrity, op table, apply on
  `plan.Begin`/`FinishSealed`, then op families in parallel (outline in
  `.worktrees/vsdx-work/chunks.md`). Op list, smallest useful set first:
  1. `text.set`, `text.replace`;
  2. `prop.set` (value), `prop.add`, `prop.delete`;
  3. `object.move`, `object.set` (box, rot, flips: resize per decision c);
  4. `object.set` fill (`"none"`, `{"hex"}`), line (`w`, `color`, `dash`
     as the stored LinePattern number), `name`;
  5. `object.add` from an existing master (`master: "master2"` or the
     master's `name`), `at` page or group, `box` or `pin`, `html`, `as`;
  6. `object.set` `from_glue`/`to_glue` (connect, disconnect), `from`/`to`;
  7. `object.delete` (removes its Connect rows; glued connectors become
     free, `dangling` never left), `object.reorder` (`to: front|back|<addr>`);
  8. `page.add` (`name`, `as`, `position`), `page.rename`, `page.delete`,
     `page.reorder` (`to`, `position`), `page.set` (`orientation`, `size`).
- **P3:** master import (copying a master from another file), layers (`layer.add`,
  `object.set layers`), `object.duplicate`, `group.create`,
  `group.ungroup`, master import, comments.

## 6. Performance

Hot paths and how they stay linear:

- **A page of thousands of shapes.** One `tree.ParseString` of the page
  (not `Part.Tree`, so nothing is pinned in the package), one walk with
  an explicit stack of group frames, one `sheet.Index` per shape (one
  map entry per stored cell), then per-object derivations that look up
  a fixed set of keys. O(page bytes + cells).
- **Master inheritance.** Each master used by the page is parsed once
  per command and indexed once (`map[shapeID]*Shape`); each style
  sheet's effective cells are memoised per style ID; chains are bounded
  (`inherit.MaxStyleDepth` 32; a master shape has no master). Lookups
  are O(1) per cell. Masters stay parsed for the command (they are
  small and shared); pages do not.
- **Connects.** One map from shape ID to object; O(connects).
- **find** visits pages one at a time (parse, build, match, drop,
  `Part.Forget`): peak is the largest page, not the document. Within a
  page it streams (chunk RM2): one top-level shape parsed, built and
  matched at a time, so the peak is the page bytes plus one shape.
  Measured before RM2 (2026-11-17, Generated(20000), page XML 15.1 MB,
  GOGC 50): find held the whole tree and model, 78 MB live and
  134-137 MB RSS; read default 97 MB live, 179 MB RSS; `--detail full`
  116 MB live, 220-272 MB RSS. The tree is 46 MB (3 times the page
  XML) and `Object.Node` pinned it through marshalling; RM2 releases
  it. RSS1 gates RSS at twice the live budgets, every run.
- **Summary** counts objects with `tree.ScanString` (memory in the
  depth).

Budgets (L1b measures, then fixes with about 20% margin, as xlsx did):
`laws.Scales` for `read --page` on 1,000 and 4,000 shapes and for `find`
on 4 and 16 pages (ratios at most 5.5, in `make scaling`);
`TestMemoryBudget`: a 10,000-shape page (about 30 MB of XML) reads with
peak live heap at most 8 times the page XML plus the masters. No
streaming splice in P2: page parts are trees (as pptx slides); if a
corpus page exceeds 50 MB we revisit with the xlsx splice design.

## 7. What landed with this design (branch wt/vsdx-design)

- `internal/geom`, `internal/casefold` (pure moves; pptx and xlsx tests
  green); AGENTS.md shared-helper rows.
- Skeleton packages under `internal/vsdx/` with the types and
  signatures of P0/P1; bodies return zero values or
  `vsd.ErrNotImplemented`.
- `cmd/vsdx` with `read`, `find`, `xml`, `help html`, `--version`;
  commands refuse with `operation_failed` until C1.
- archtest `formats` includes `vsdx`; Makefile `build`; `.gitignore`.
- Designer-authored fixtures `testdata/vsdx/common`, `v1-minimal`,
  `v2-master`, `v5-glue` (each with PROVENANCE.md and its cells).
- Create, render and write skeleton: `internal/vsdx/{emit,create,ops,edit,apply,integrity}`,
  `formula/oned.go`, `place/unplace.go`, `text/input.go`, `cmd/vsdx`
  `apply`, `create`, `render` (refusing until their chunks land); create
  goldens and plans in `testdata/vsdx/create/{blank,emit,plans}`.

## 8. Verification

The owner requires evidence. There is no Visio on this Mac and, by the
owner's ruling, no Windows Visio for now. **"Opens in real Visio without
repair" is unverified for every output** until `visio-smoke` exists;
every result below says what it proves instead.

### Oracle stack

1. **Laws (P1 read, P2 writes) through `laws.Suite`.** P1: read is
   deterministic (two reads byte-identical), read never writes (file
   hash unchanged), `--ids` and `--region` are filters of the full read
   (same objects, same values), every address `read` prints resolves
   (`xml` succeeds on each object id; `find` hits resolve through
   `--ids`). P2: identity write is a no-op; read after write shows the
   value; untargeted units unchanged (`laws.Untargeted` with units Shape,
   Connect, Page entry, Master entry, StyleSheet); atomicity under
   `opc.SetFaultHookFor`; sequencing; undo pairs (move dx/-dx, rename
   and back, page reorder and back, orientation and back restores size).
2. **Property tests.** `place`: `Box` round-trips through `geom` for
   random transforms (pin, size, angle, flips, nested groups 1 to 4
   deep) against a naive corner-mapping implementation in the test;
   `vsd.FormatInches` round-trips `Number` for random doubles (15
   significant digits); `text`: `Plain(Parse(x))` equals the naive
   concatenation for random marker placements; `Render` of a parsed body
   re-parsed by X1 gives the same plain text.
3. **Cross-grammar law X1** (under `tools/`, which may import both
   formats): every `html` vsdx reads on the fixtures and the corpus,
   `<opaque>` placeholders removed, is accepted by pptx `text.Parse`,
   and `Normalise` of it is itself.
4. **LibreOffice import render (open check, not a validity oracle).**
   `soffice --headless --convert-to svg` (private profile, through
   `internal/soffice`) of each fixture and each P2 output: the
   conversion succeeds; every shape text `read` shows appears in the SVG
   text; positions of unrotated top-level shapes agree with `read`'s box
   within 1 pt (SVG user units mapped from the page size). Tests skip
   without soffice. Proves: what a second, independent reader draws
   agrees with our read and our cached values; that edited files still
   import. Does not prove Visio acceptance (libvisio is lenient, notes
   §10.1).
5. **Schema validation, opt-in** (decision 2 as clarified: use the
   schema, never commit it). `make vsdx-schema` fetches the XSD into the
   gitignored cache; `make vsdx-schema-check` (chunk S3c) validates
   fixtures, corpus inputs and write outputs with an XSD 1.1 validator
   (the schema uses `xsd:alternative` and `xsd:assert`, which xmllint's
   XSD 1.0 rejects; Python `xmlschema` 4.3.2 loads it, run through
   `uv run --script`) and, as the OOXML gate, fails only on error kinds
   an output has that its input did not. Real Visio files do not
   validate (V1, built in Visio's form, gives three errors: `xml:space`
   on the root is not declared, character data in `Text` is not
   allowed, an assertion on a Section fails), so the check is
   differential by necessity. It skips cleanly without the cache and is
   never part of `make check` or `make ci`.
6. **Structural lint in Go** (P2 integrity gate, shared with the read
   hazards): unique shape IDs per page across nesting, Connects resolve,
   Master/MasterShape/style IDs resolve, page IDs unique, BackPage
   resolves, one Override per part and one rel per (source, target,
   type), document content type matches the kind. Refuses only what a
   plan introduces (contract section 10).
7. **Cross-reader**: none is planned in P1 (an independent naive reader
   was dropped: it had no chunk). LibreOffice (item 4) is the
   independent reader; Apache POI XDGF or python-vsdx stay options if
   the owner wants a JVM or uv tool.
8. **Corpus sweeps** (`VSDX_CORPUS`, `corpustest`): read summary and
   every page of every file with no panic, hang or limit trip; counts of
   notes and `invalid_document` (each one inspected and classified, as
   xlsx's 42); P2: each op on the first page of each file, outcome
   counts (ok, refused by the closed list, unreadable_target), never
   `internal` or `invalid_output`.
9. **Fuzz targets** (seeds in `testdata/fuzz`, added to `FUZZ_TARGETS`):
   `FuzzParseAddress` (vsd), `FuzzNumber` (vsd), `FuzzIndex` (sheet),
   `FuzzText` (text Parse/Plain/Render), `FuzzRead` and `FuzzFind` (read,
   on mutated fixtures and members), `FuzzCommandLine` (cmd/vsdx); P2:
   `FuzzParsePlan`, `FuzzFormula` (lexer round trip, evaluator never
   panics), `FuzzApply`.
10. **Scaling and memory** (section 6) in `make scaling`.

### Fixtures (designer-authored XML under testdata/vsdx/; builder F1)

Each is a directory of zip members plus `PROVENANCE.md` ("written by
hand for documentdriver; not Visio-authored"), with shared members in
`testdata/vsdx/common/` (`document.xml`, `app.xml`). **The designer
authors every fixture's XML and every golden output; implementers write
only the builder (F1).** All fixtures V1 to V17 are committed (V14 and V16 are generated by the
tests that need them); each directory's PROVENANCE.md holds its cells
and expected values. Fixtures mirror real structure from the corpus:
single-quoted attributes, `xml:space='preserve'`, StyleSheet 0 holding
line, fill, text and PrintPageOrientation cells with a Character and a
Paragraph row, colour indexes, `Colors` from IX 24, `FaceName` by
`NameU`, Text ending in a line feed, a group master whose instance
children carry only `MasterShape`. They prove what our code
does with the structures the notes describe; they do not prove Visio
writes them that way (that is the corpus's and VE's job).

| # | Content | Proves |
|---|---|---|
| V1 | **designer-authored, committed** (`testdata/vsdx/v1-minimal`, with `common/`): page0 "Page-1" 8.5 x 11 in, no local PrintPageOrientation (StyleSheet 0 has 1); s1 PinX 2, PinY 9, 1 x 0.5, LocPin 0.5/0.25 (formulas), styles 3 -> 6 -> 0, FillForegnd `#4472c4`, no local line cells, local Character row 0 Style 1, Text `<cp IX='0'/>Hello\n` | box `[108,126,72,36]`; fill `{"hex":"#4472C4"}` local; line w 0.75 (StyleSheet 0) color `#7F7F7F` (StyleSheet 6, index 24) origin style; html `<p><b>Hello</b></p>`; text "Hello"; orientation and print_orientation "portrait" |
| V2 | **designer-authored, committed** (`testdata/vsdx/v2-master`): master2 "Process" (shape 5: 1 x 0.75, LocPin formulas, FillForegnd `#FFFFFF`, Character row 0 Style 2, Text `Process\n`, shape data Cost Type 7, a Connection row); master3 "Pair" (group shape 5, 2 x 1, children 6 "Left" with Text `Left\n` and 7 "Right" with FillForegnd 4, cells as `Sheet.5!` formulas); page s1 (Master 2, PinX 2 PinY 9 only), s2 (Master 2, PinX 4 PinY 9, Width F="Inh", FillForegnd 2, Cost 12.5, own Text `Review\n`), s3 (Master 3 group, PinX 6 PinY 6) holding s4 (MasterShape 6, no cells) and s5 (MasterShape 7, own Text `Right\n`) | s1 box `[108,117,72,54]` origin master, html `<p><i>Process</i></p>` origin master, fill `#FFFFFF` master; s2 box `[252,117,72,54]` (width inherited), fill `#FF0000` (built-in 2) local, html `<p>Review</p>`, Cost 12.5 currency; s3 box `[360,324,144,72]`; s4 box `[360,324,72,72]` html `<p>Left</p>` origin master; s5 box `[432,324,72,72]` fill `#0000FF` (built-in 4) origin master, html `<p>Right</p>` local |
| V3 | **committed** (`v3-styles`, own document.xml): styles 1 "Thin" (1 pt, #000000), 2 "None", 3 "Thick" (parent 1, 3 pt), 5/6 a LineStyle cycle; s1 line 3, s2 line 1, s3 line 5, s4 line and fill 2 | s1 w 3 pt colour #000000 (style); s2 w 1 pt; s3 line absent and one cycle note; s4 fill and line none |
| V4 | **committed** (`v4-groups`): s10 group (4,5; 2 x 2) > s11, s12 group (rot 30 ccw) > s13; s20 group FlipY > s21 | boxes s10 [216,360,144,144], s11 [216,432,72,72], s12 [288,360,72,72] rot 330, s13 [306,378,36,36] rot 330, s20 [396,396,72,72] flip_v, s21 [414,396,36,36] flip_v |
| V5 | **designer-authored, committed** (`testdata/vsdx/v5-glue`; the full cell table is its PROVENANCE.md): s1 (PinX 2, PinY 9; sites X1 (1, 0.25), X2 (0, 0.25)), s2 (PinX 5, PinY 9; sites X1 (0, 0.25), X2 (1, 0.25)); connector s3 Begin (2.5, 9) glued to s1 PinX, End (4.5, 9) glued to s2 Connections.X1; s4 End glued to s9 (absent); s5 Begin (3, 8) glued to s1 Connections.X1; s6 with a Control row glued Controls.Row_1 to s2 | s3 from `[180,144]` to `[324,144]`, `from_glue "s1"`, `to_glue "s2:1"`, no hazard; s4 `to_glue "#9"` and dangling-glue; s5 from `[216,216]` to `[216,288]`, `from_glue "s1:1"`, stale-connector (80.5 pt); s6 `glued [{"cell":"Controls.Row_1","to":"s2"}]` |
| V6 | **committed** (`v6-data`): master2 "Item" rows Cost (7), Owner (0), Done (3), Due (5, DATE), Size (2, U MM), Hidden, Linked, Gone, MasterOnly; instance s1 sets Cost 12.5, Owner Ada, Done 1, deletes Gone, adds NewRow (2, 42) | props in master order then NewRow; Gone absent; Due {"date":"2024-03-01T00:00:00"}; Size 0.5 with unit MM; flags and origins per PROVENANCE |
| V7 | **committed** (`v7-text`): own Character rows 0 (12 pt, equal to inherited), 1 (bold, #FF0000), 2 (Arial 10 pt); Paragraph rows 0 (left), 1 (bullet U+2022, indents); a field; Text with \u2028, \t and a final \n | body, formats and text in PROVENANCE; html = `golden/v7-page0.html` (re-derived under decision f) |
| V8 | **committed** (`v8-pages`): page0 (BackPage 3, layers Flowchart and Connector (hidden)), page1, page3 (background); page0 s1 LayerMember "0;1" | summary order 0, 1, 3; background flags; layers and object `layers` |
| V9 | **committed** (`v9-rotation`): rotation/flip matrix: s1 PinX 2, PinY 9, 1 x 0.5, Angle 1.570796326794897, LocPin 0/0 (box `[90,90,72,36]` rot 270); s2 FlipX; s3 Angle -0.5235987755983 | rot 30 and 270, flips, LocPin off centre |
| V10 | **committed** (`v10-defects`): page2 malformed, page4 Rel without a part, two pages ID 5, duplicate shape ID 7, Master 99, FillForegnd Themed, an unknown section, a Connect with FromSheet 0 | notes not failures; `page0/s7`, `page0/s7~2`, `page5~2`; unreadable_target for `--page page2` only; integrity findings per PROVENANCE |
| V11 | **committed** (`v11-ns2011`): V1 in the 2011/1/core namespace | same read as V1 |
| V12 | **committed** (`v12-vsdm`: V1 as .vsdm with a placeholder vbaProject.bin; `v12-stencil`: stencil.main with master2 "Box", no pages) | kinds; stencil summary has no pages; read only |
| V13 | **committed** (`v13-scale`): PageScale 1 in, DrawingScale 120 ft | the scale note (VE11); boxes unscaled |
| V14 | (generated, not committed) `vsdxtest.Generated(n)` | scaling and memory |
| V15 | **committed** (`v15-foreign`): Foreign Bitmap PNG (image via page rels), Foreign Object without embedding, a Guide | kinds picture, other, other; read only |
| V16 | group nesting 600 deep (generated in the test) | the tokenizer cap: `--page` of it is unreadable_target, the summary still lists the page with a note (as pptx TestDeepGroups); no stack growth |
| V17 | **committed** (`v17-orientation`): page 0 8.5 x 11 with local PrintPageOrientation 1; page 1 11 x 8.5 with local 2; page 2 11 x 8.5 with none stored locally (StyleSheet 0 has 1); page 3 8 x 8 (square); a shape on page 0 with PinY V 5.5 F `ThePage!PageHeight*0.5` | read: page0 orientation portrait, print_orientation portrait (local); page1 landscape, landscape (local); page2 landscape, print_orientation portrait (style: the two disagree); page3 no orientation, print_orientation portrait (style). P2 and create: `page.set orientation landscape` on page0 gives size `[792,612]` and local PrintPageOrientation 2, PinY stored unchanged, the `ThePage!` dependent left stale with `stale-values`; on page2 `orientation landscape` changes no size and writes local PrintPageOrientation 2; on page3 it changes nothing but writes 2 |

### Schema cross-check (designer, 2026-11-17)

Run over the cached XSD and the 53 Visio-authored corpus files, the
2011/1/core and 2012/main namespaces taken as one (no corpus file uses
another namespace for a child of a Visio element):

- **Disagreements (2), the corpus wins:** `Trigger` appears as a child
  of 33 `PageSheet` elements (in 32 files) but the XSD admits it only through the
  ShapeSheet `xsd:any` wildcard; `Trigger` itself is not declared, so
  its own children (36 occurrences) have no schema order. S3 places
  Trigger in PageSheet's Cell slot (the corpus shows both orders, 352
  and 515) and takes Trigger's children from the corpus.
- **Refinements (52), the corpus orders what the XSD leaves open:**
  `Shape` Cell < Section < Text < ForeignData (73,587, 1,399, 150, 11,
  1 occurrences); `StyleSheet`, `PageSheet` and `DocumentSheet` Cell <
  Section; `Section` Cell < Row (25,418); `Page` PageSheet < Rel;
  `Master` PageSheet < Icon < Rel; 36 pairs among `DocumentSettings`'
  children (GlueSettings < SnapSettings < ... < ProtectBkgnds, 53 files
  each). `Text` is mixed content: its markers stay unordered.
- The XSD's order for `Shapes` after the ShapeSheet choice (a sequence
  extension) agrees with the corpus (Text < Shapes, 60).

### Corpus (owner ruling 2; fetched on wt/vsdx-corpus, 91c1072)

Layout, as xlsx's: `corpus/vsdx/repos/<source>/<upstream path>`
(gitignored), fetched by `eval/corpus/fetch-vsdx.sh` from the manifest
`eval/corpus/vsdx-sources.tsv` (source, repository, pinned commit,
licence and its evidence) on `wt/vsdx-corpus`; 65 drawings from Apache
POI `test-data/diagram` (Apache-2.0), libvisio `src/test/data`
(MPL-2.0), python-vsdx `tests` (BSD-3-Clause; Visio 2016-authored),
Tika (Apache-2.0), vsdx-js (MIT) and visio2img (Apache-2.0). Excluded
(ruling): Aspose, libvisio-ng (GPL-3.0), Microsoft samples.

Suites read `VSDX_CORPUS` (default `corpus/vsdx` of the main checkout;
`make corpus` passes it through, W2) with `corpustest.Files` over
`.vsdx .vsdm .vstx .vstm .vssx .vssm`, one subtest per corpus-relative
path, sharded and parallel as every corpus suite:

- `internal/vsdx/read` `TestReadCorpus`: the summary and every page and
  master of every file, with no panic, hang or limit trip; counts by
  outcome (ok, notes, unreadable_target pages, invalid_document). The
  8 POI clusterfuzz files that are not valid zips must each give a
  clean `invalid_document` (an error, never a panic): the suite lists
  them by name as expected refusals and fails if any other file gives
  `invalid_document` without a classified reason in the test's table,
  or if one of the 8 is ever read (the table is reviewed, as xlsx's 42).
- `tools/vsdxgrammar` X1 over every page's html.
- `TestCorpusLibreOffice` (skips without soffice; every non-fuzz file).
- P2 `TestApplyCorpus` (each op on the first page of each file).

S0's ctypes extraction runs over the Visio-authored subset
(`docProps/app.xml` Application "Microsoft Visio"), with `min_files` 2
per entry; a type seen in fewer files stays out of the table and the
code that needs it waits (S0 reports which).

### Visio experiments (VE1 to VE21; not yet run, dependent claims labelled Visio unconfirmed)

Fixtures under `testdata/vsdx/experiments`, written by a Go generator,
with a README of what to look for; run by the owner if and when a Visio
becomes available (visio-smoke), and with LibreOffice meanwhile, results
labelled "LibreOffice ...: Visio unconfirmed" as xlsx did.

| # | Question |
|---|---|
| VE1 | Does an instance with only PinX/PinY (no cached Width/Height) open cleanly? |
| VE2 | Does `<?NewValue V?>` on a changed Width make Visio recompute LocPin and geometry? |
| VE3 | Does `RecalcDocument` recompute every cell, and is it removed on save? |
| VE4 | Does `RecalcDocument` reroute every glued dynamic connector? |
| VE5 | Exact `_XFTRIGGER`/`_WALKGLUE` spellings Visio writes and accepts |
| VE6 | Is a file in the 2011/1/core namespace accepted? |
| VE7 | Is U+2028 Visio's soft line break in Text? |
| VE8 | Does a local cell with V and no F override an inherited formula as a constant? |
| VE9 | Shape data Type codes (boolean, date, duration, currency) and V forms |
| VE10 | Does Visio keep stored pins when the orientation is flipped (shapes stay measured from the lower-left)? |
| VE11 | Are lengths on a scaled page stored in page or drawing inches? |
| VE12 | Does a created `docProps/custom.xml` with only RecalcDocument open cleanly? |
| VE13 | Does Visio re-walk whole-shape glue CC2 writes (`_WALKGLUE`, `_XFTRIGGER(Sheet.N!EventXFMod)`, Connect `ToCell PinX ToPart 3`) when the target moves, and route the straight line CC2 stores when RecalcDocument is set (with VE4)? |
| VE14 | Does Visio clamp a stored character Size between 0 and 1 pt to 1 pt (four corpus `GUARD(0)` Size cells cache 1 pt), and is there an upper bound? Until recorded, formula.CellBounds clamps Size to at least 1 pt and text input refuses font-size below 1pt (CC1). |
| VE15 | Which arrowhead does Visio draw for `BeginArrow`/`EndArrow` 4, 5 and 13, and for sizes 0 to 6? Does a local arrow cell on a Dynamic connector instance override the style's themed value? |
| VE16 | Does Visio accept `Background='1'` and `BackPage='<ID>'` as vsdx writes them; does it require background pages after foreground pages; does it accept a background that itself has a background; what does it draw when the two pages differ in size? |
| VE17 | Does Visio show a shadow for `ShdwPattern` 1 with `ShapeShdwType` 1 and local offsets; what do `ShapeShdwShow` 0, 1 and 2 do; which offset applies when `ShapeShdwType` is 0? |
| VE18 | Does Visio open a page picture vsdx added (PNG, JPEG) without repair, and crop by `ImgOffsetX/Y`, `ImgWidth/Height` in the sense the contract states? |
| VE19 | Does Visio draw FillPattern 2 to 8 and 19 to 24 as the Fill dialog names them (contract §3, Pattern fills), at what spacing and line weight, does `FillBkgndTrans` 1 make the background clear, and do the patterns print and export to PDF as vector hatching or as bitmaps? |
| VE20 | Does Visio open and draw a master-less shape whose one Geometry section is `RelMoveTo` then `RelLineTo` or `RelCubBezTo` rows as vsdx writes them (NoFill 1 open, 0 closed), with line ends on the open ones, and does a resize scale the rows? |
| VE21 | Does Visio draw `TextBkgnd` stored as `#RRGGBB` (no formula) as that colour, at `TextBkgndTrans`, over the text and its margins rather than the whole text block; and does it read Character and Paragraph rows written with `F="Inh"` cells as inherited, showing the formatting vsdx's html says? |

### What cannot be verified without real Visio

That any output opens without a repair prompt; that Visio honours
`NewValue` and `RecalcDocument` as documented; connector routing after
glue writes; that our cached values match what Visio would compute
(beyond the closed evaluator's own tests); theme and QuickStyle
appearance; scaled-page units; anything in VE1 to VE12 that LibreOffice
cannot show.

### visio-smoke (designed, not built; owner ruling 3)

`tools/visio-smoke` (later): `make visio-smoke SOURCE=in.vsdx OUT="out.vsdx ..."`
copies the files to a Windows host (SSH, host from `VISIO_SMOKE_HOST`),
runs a PowerShell script that opens each with COM
`Visio.InvisibleApp.Documents.OpenEx` (flags: no repair UI), records
the open result, the repair log and the page and shape counts, saves a
copy, and returns JSON; an output fails when it needs repair or fails to
open where its source did not (as word-smoke). It never runs in `make
ci`; the maintainer approves every run.

## 9. Risks

1. **Cached-value consistency** is the main correctness risk: an op that
   misses a dependent leaves a drawing that looks wrong everywhere but
   Visio. Mitigation: the closed evaluator's dependency walk, the
   allow-list refusal, `stale-values`, LibreOffice render checks.
2. **Visio acceptance is unverified** (no Visio). Mitigation: write only
   structures read from Visio-authored corpus files; structural lint;
   VE list; visio-smoke later.
3. **Glue writes** are the most corruption-prone feature in every other
   implementation (notes §9). They are last in P2's order and refuse
   anything beyond PinX and Connections.Xn.
4. **Child order from a schema Visio's own files contradict in places**:
   the XSD leaves most orders open (`xsd:choice` unbounded in
   ShapeSheet, DocumentSettings, Master) and does not declare
   `Trigger`; S3 orders by the corpus where the XSD is silent or wrong
   and lists each case in the provenance file. A child neither shows in
   a parent cannot be placed and is refused, never guessed.
5. **Inheritance edge cases** (group masters' MasterShape chains, styles
   with Del rows, theme values) are under-specified in the notes; reads
   omit with a note rather than guess.
6. **y-down view** shifts shown y with page height; agents might expect
   Visio's y-up. Mitigation: `cells` shows stored values; help says so.
7. **Grammar drift from pptx**: X1 catches it.

## 10. Owner decisions (2026-11-17)

1. **ctypes source**: approved. S0 extracts the Visio types from the
   Visio-authored corpus files.
2. **MS-VSDX schema**: clarified by the owner: "You can use the schema,
   just don't commit it to the repo." It is fetched by
   `eval/corpus/fetch-vsdx-schema.sh` (`make vsdx-schema`) into the
   gitignored cache (`$VSDX_SCHEMA`, default `corpus/vsdx-schema`),
   pinned by sha256: the Markdown page at
   https://raw.githubusercontent.com/MicrosoftDocs/office-developer-client-docs/f8c5dfed613fafd2fb8c8f9b1e43454f29517831/docs/visio/schema-mapvisio-xml.md
   (sha256 4e6935f7177e41f3ec0b012c48eb3cb13d1b51bf04547a242991c9bd229c9fe0),
   whose one XML block is the XSD (`visio.xsd`, sha256
   29caea2b747dcc560cca0b112de30e3cbefe751ccd0e874618ff7dfb3ba0db76).
   S3 generates the child order from it, cross-checked against the
   corpus; the opt-in schema check is S3c. The script refuses to write
   into a tracked path.
3. **Dependent cached values**: R approved, with the closure rules of
   decision (c).
4. **`stale-values` and `RecalcDocument`** as the third exception:
   approved.
5. **create**: reversed. Designed now ("Create").
6. **Page orientation**: approved as drafted.
7. **`vsdx render`** through LibreOffice: approved ("Render").

## 11. Fable review amendments (2026-11-17)

Corpus figures re-checked by the designer over the 57 non-fuzz files
(scripted with `uv run`; counts by part):

| Claim | Fable | Re-check |
|---|---|---|
| Text ending in LF | 257 of 285 | 257 of 285 page Text elements (95 of 144 in masters) |
| V with 16+ significant digits | 7,346 | 21,124 numeric V values with 16 or more significant digits (3,766 distinct): the 15-digit rule is wrong either way |
| Font cells hold names | Calibri, Themed, Arial | Calibri 260, Themed 185, Arial 36, others; FaceName has NameU and no ID (61 of 61) |
| "Themed" capitalised | 12,058 | 12,058, never lower case |
| Indexed colours | 1,003 | 2,347 colour cells (FillForegnd, FillBkgnd, LineColor, Color, ShdwForegnd, ShdwBkgnd, TextBkgnd, outside Layer rows) hold an index; Colors entries start at IX 24 |
| PrintPageOrientation in StyleSheet 0 | 55 documents | 55 of 57; stored on 34 of 86 page sheets |
| Non-end Connects | 48 Controls.Row_N | 48 (FromCell Controls.Row_N, FromPart 100+N-1); ToCell PinX 26, Connections.Xn 132 |

The counts differ where the scope of the count differs; every
conclusion holds.

| # | Amendment | Status |
|---|---|---|
| 1 | Trailing LF terminates; T1 row 3 and Plain | done (text package doc, `Body.Terminated`, T1 rows) |
| 2 | FormatInches shortest round trip | done (doc comment, V1 rows) |
| 3 | Font by name; FaceNames; D3; F1 | done (`Document.FaceNames []string`, decision f, D3 row, fixtures) |
| 4 | "Themed" capitalised | done (`vsd.ValueThemed`, V10, M2b) |
| 5 | Colour indexes with a built-in 0-23 table | done (`vsd.BuiltinColor`, `doc.Document.Color`, `Document.Colors`; chunks V3, D4 with rows; table provenance is V3's first task) |
| 6 | Orientation held for the owner | done (proposal drafted, decision 6; V1, V17, R1 rows) |
| 7 | V5 consistent cell table; V9 cells | done (V5 committed with PROVENANCE table; s3 glued to s2:1; V9 cells stated) |
| 8 | Designer authors fixtures and goldens; real structure | V1, V2, V5 and `common/` committed; the rest are chunk FX (designer) before the chunks that read them; goldens are designer-authored in R2's spec |
| 9 | Disjointness | done (`read/page.go`, `inherit/styles.go`, `doc/contents.go`, `doc/colors.go` stubs; W4 owns FUZZ_TARGETS; dependency fixes in chunks.md) |
| 10 | Signatures | done (`vsd.FieldID`, `KindField`, `PageID` dup; `ParaFormat.BulletChar`; `model.Site` with N; `linkConnects` with the Connects node, placed shapes and page height; M4 uses math.Hypot; T2 rows) |
| 11 | Missing chunks; split M2 and L1; drop vsdxnaive | done (M5 formats, M6 page sheet and layers, M7 cells and origins, Glued in M4b; M2a-c; L1a-c; vsdxnaive dropped) |
| 12 | R closure | recorded in decision c, binding at P2 (with the FL4 corpus sweep and `formula.OneD`) |
| 13 | Contract §13: find is an addition; render option | done (contract draft; decision 7) |
| 14 | HTML rule for inherited character properties | done (decision f: own formatting only, as pptx; V7 to be re-derived in FX; V1/V2 rows on Visio-shaped fixtures) |
| opt | off-page after page.set; `page0~2`; casefold messages; V4 s13 row | taken (off-page hazard in the orientation rule; `PageID(id, dup)`; casefold_test messages; P1 row 13); H1 presizing hint taken in H1's spec |


## 12. Contract review by Fable for the owner (2026-11-17)

Verdict APPROVE-WITH-AMENDMENTS: the shape is right, but the draft
promised what the binary at 7d09f90 did not do. The designer probed
each item against the binary (9a57372) and the corpus, rewrote the
draft in the Replace / Add-after form with a status line and a merge
plan, rewrote the closed refusal list (decision g), and specified
chunks CC1 to CC14 with rows and a coverage matrix in the chunk plan.
Main does not move until they merge.

Designer rulings, and Fable's answers (2026-11-17; Fable is the
architect for contract questions):

- `from` with `from_glue` is accepted as pptx does; an end off its site
  is stale-connector plus stale-values (CC10). Accepted.
- object.move keeps glued ends where their glue puts them and refuses a
  connector glued at both ends (CC2). Accepted.
- Identity, changed by Fable: a value equal to the effective value read
  shows writes nothing, for objects as well as text, so the identity
  law holds; a different value is stored locally and its origin
  becomes local. Pinning an inherited value is deferred (CC4, CC8,
  CC11, CC14).
- Container members are refused for geometry, move and delete; the
  structure shapes for every write (CC6). Accepted.
- Whole-shape glue and rerouting, changed by Fable: the contract marks
  them "Visio unconfirmed (VE4, VE13)"; whole-shape glue on a
  master-less connector is allowed only if every cell written has a
  form cited from a Visio-authored corpus file. The corpus has none
  (all 17 `_WALKGLUE` shapes are Dynamic connector instances, in
  test4_connectors, test7_with_connector, 60489 and testVISIO_60489;
  none stores WalkPreference locally), so it is refused, with a hint to
  add from the Dynamic connector master (CC2).
- prop.add names, accepted with a condition: non-ASCII names are
  `invalid_operation` at `name` before the document opens; existing
  non-ASCII rows stay readable, settable and deletable (CC8).
- page.add `before` the first page is allowed (CC13). Accepted.
- Sub-shape delete, changed by Fable: deleting a master-inherited
  sub-shape is refused, listed in the contract's §3 refusals and in
  `help object.delete` (CC6, CC7, CC14).
- §9: RecalcDocument and `<?NewValue V?>` carry "Visio unconfirmed"
  until VE2, VE3 and VE12 are recorded, as xlsx's §9 exception did.
- Merge plan: Fable confirms the draft's section order matches its map;
  Fable reviews the merged diff before main moves.

Gate rulings on held chunks (2026-11-17): CC1's four `Char.Size
GUARD(0)` disagreements are a Visio clamp (Size at least 1 pt) that
writes would also hit, so it is modelled (formula.CellBounds, font-size
below 1pt refused) instead of classified, and FL4's table stays empty;
CC5's path changes in OT1's tests and CC13's removal of OP1's
first-page row are confirmed; RM3's budgets (default 84/343 MB, full
123/551 MB) are the designer's; CC6's container skip in text.replace's
walks stays (the skip replaces the "note" the row first asked for,
since apply results carry no notes).

## 13. Line ends, background pages, shadows and pictures (2026-11-18)

The architect's amendment (line ends, background page assignment,
shadows, pictures by file path) is merged into `docs/contract.md`. What
each write stores, and its grounding:

- **Line ends**: `BeginArrow`, `EndArrow`, `BeginArrowSize`,
  `EndArrowSize` on the shape sheet (MS-VSDX `BeginArrow_Type` and
  siblings, visio.xsd ShapeSheet cell alternatives, lines 172-176);
  values as the corpus stores them (0, 4, 5, 13; sizes 0 to 3).
- **Shadows**: `ShdwPattern`, `ShdwForegnd`, `ShdwForegndTrans`,
  `ShapeShdwType`, `ShapeShdwOffsetX`/`Y`, the cells and values of the
  Visio-authored Tika `testVISIO.vsdx` shadowed shapes.
- **Background pages**: the `Page` element's `Background` and
  `BackPage` attributes (visio.xsd `Page_Type`, lines 4671-4672); no
  Visio-authored sample (B1), so every behavioural claim is VE16.
- **Pictures**: `Shape Type='Foreign'` with `ImgOffsetX`, `ImgOffsetY`,
  `ImgWidth`, `ImgHeight` cells in the formula forms `ImgWidth*k`,
  `Width*k` and a `ForeignData ForeignType='Bitmap' CompressionType=`
  element with one `Rel r:id` (visio.xsd `ForeignData_Type`, lines
  4298-4312; `Rel_Type`, line 4313); the page relationship of type
  `http://schemas.openxmlformats.org/officeDocument/2006/relationships/image`
  with a relative target, and a content-type `Default` per extension
  (OPC, ECMA-376 Part 2). The JPEG form is the Visio-authored sample's
  (fixture V18, derived from `overzichtfoto's.vsdx`, CC BY 4.0:
  `CompressionType='JPEG'`, `imageN.jpeg`, `image/jpeg`); PNG takes the
  same form with `CompressionType='PNG'`, `imageN.png`, `image/png`.
- **No fill, no line, no style reference**: an added picture stores
  `LinePattern` 0 and `FillPattern` 0 (visio.xsd `LinePattern_Type`
  line 621, `FillPattern_Type` line 698, cells of `ShapeSheet_Type`
  line 150) and omits `LineStyle`, `FillStyle` and `TextStyle`
  (optional attributes of `ShapeSheet_Type`, lines 508-510), so it
  depends on no style sheet ID. It carries no Connection section:
  nothing in the schema requires one on a Foreign shape.
- **Owner guidance (2026-11-18)**: the MS-VSDX schema and spec are the
  authority for what vsdx writes; corpus samples confirm the schema
  and do not replace it.
- **Owner ruling (2026-11-18)**: picture writes are grounded in the
  MS-VSDX schema and spec and the one Visio-authored JPEG sample, not in
  two Visio-authored samples per format. This supersedes the
  amendment's precondition PRE-D. Visio's acceptance is VE18. Checks:
  `tools/vsdxpicture` validates PNG and JPEG adds, crop, replace and
  delete against the XSD (the differential check; the one kind it
  reports, `r:id` on `ForeignData/Rel`, is the schema's own `Rel_Type`
  gap that every drawing's `Page/Rel` shows too) and renders a picture
  through LibreOffice.
- **Corpus**: the researcher's Visio-authored files were not added as
  sources: the CEG drawing holds 70 MB of BMP media and times out the
  corpus test's LibreOffice step, and the Aspose drawing moves the
  pinned CC1 corpus tables; the fetch script gained single-file source
  kinds for later use.

## 14. Pattern fills and paths (2026-11-24, owner sign-off pending)

For patent-style line art (37 CFR 1.84: black and white, hatching in
place of colour), `fill` gains patterns and `object.add` gains
`geom: "polyline"` and `"curve"` (contract §3, Pattern fills and
Paths). What each write stores, and its grounding:

- **Patterns**: `FillPattern` 2 to 24, `FillForegnd`, `FillBkgnd`,
  `FillBkgndTrans` (visio.xsd cell alternatives lines 180-186; cell
  types lines 691-736; MS-VSDX FillPattern: 0 none, 1 solid, 2 to 24
  patterns, 25 to 40 gradients). The name table follows the order of
  the Visio Fill dialog's pattern list as published by a Visio MVP
  (bVisual, "Using Custom Fill Patterns from a List", 2021: position
  in the list is the FillPattern value), and LibreOffice's importer
  (libvisio `VSDContentCollector::_fillAndShadowProperties`, read as
  a summary, not verbatim) maps them to hatches: 2 single 45 deg,
  3 double 0 deg, 4 double 45 deg, 5 single 315 deg, 6 single 0 deg,
  7 single 90 deg at 0.1 in; 8 to 12, 17, 18 and 24 triple at 0.05 in;
  13 to 16 and 19 to 22 single at 0.05 in; 23 double at 0.05 in; a
  `FillBkgndTrans` of 1 leaves the background out. The render test
  (`TestRenderPatternsAndPaths` in `cmd/vsdx`) confirms that drawing
  for every named pattern, the rising and falling slant of the
  diagonals, and the clear and white backgrounds. The vsdx corpus was not on the
  machine this was written on, so it was not searched for patterns;
  Visio's drawing is VE19. The libvisio table also shows what is
  lost in LibreOffice: Visio's stipples (8 to 12, the `dots` name)
  become a dense triple hatch, and patterns of the same direction
  differ only in spacing.
- **Density**: no ShapeSheet cell scales a fill pattern, so density
  is the choice between a pattern and its `_fine` sibling (the
  dialog's "narrow" and "small" variants). `cross_hatch_fine` (24)
  is the one LibreOffice draws differently from its name.
- **Paths**: one Geometry section of `RelMoveTo`, `RelLineTo` and
  `RelCubBezTo` rows (visio.xsd lines 3507-3517 and 3767-3783),
  relative to Width and Height as the rect form's `RelLineTo` rows
  (the form of 460 corpus rows), so the cells hold plain values and a
  resize scales them. Bézier rows over `NURBSTo`: `NURBSTo` needs the
  `NURBS()` formula in its E cell, which libvisio parses but which
  is a formula vsdx would write and Visio would have to evaluate;
  `RelCubBezTo` is plain cells. Over `SplineStart`/`SplineKnot`:
  libvisio turns splines into NURBS anyway. The curve is the uniform
  Catmull-Rom spline, the simplest smooth curve through the points
  whose segments are cubic Béziers exactly, so the stored curve is
  the drawn curve in both renderers; `internal/vsdx/path.CurveSegs`
  is its one definition, used to write and to recognise.
- **Read**: `path.Parse` recognises only the form vsdx writes (one
  section, plain cells, curve controls equal to the spline's), so
  `geom`, `closed` and `points` are a derived fact, never a guess at
  someone else's geometry. Rect and ellipse are not shown: read never
  showed them, and a master instance's geometry is not derived, so
  showing them on master-less shapes only would be uneven.
- **Line ends** on an open path use the connector cells
  (`BeginArrow`, `EndArrow`, sizes) and refusals; they are allowed on
  a 2-D shape only when `path.Parse` says it is an open path.
- **Solid over a pattern**: object.set `{"hex"}` used to compare only
  the foreground colour, so on a pattern fill of that colour it wrote
  nothing (consistent with the old read, which showed any non-zero
  pattern as `{"hex"}`). Now that read shows patterns, it compares
  the pattern too and writes FillPattern 1.
- **Pattern read of existing drawings**: a shape whose effective
  FillPattern is 2 to 24 used to read as `{"hex"}` of its foreground;
  it now reads as a pattern fill. This changes what `read` shows for
  such drawings (an added key `pattern` and `bg` on `fill`), which the
  owner signs off with the contract change.

## 15. The drawing view: read --format svg and render without LibreOffice (2026-11-25, owner sign-off pending)

A patent-figure kit builds figures through vsdx inside Open WebUI Code
Mode, which has no LibreOffice, so `render` could not show an agent its
own figure. `read --format svg` and `render --renderer svg` draw the
page from the read model instead (contract §1, vsdx). The layers:

- **Model** (`internal/vsdx/model`, `Options.Draw`, built only for the
  drawing): `Object.Draw` holds every effective Geometry section
  (`inherit.Rows`, so a master instance draws its master's rows) as
  paths in the view, from each cell's cached V, never an evaluated
  formula: MoveTo, LineTo and the Rel forms, RelCubBezTo,
  RelQuadBezTo, ArcTo (a circle through the ends and the bow point),
  EllipticalArcTo (mapped to a circle by the axis angle and ratio),
  Ellipse (four Béziers through conjugate diameters); PolylineTo,
  NURBSTo and splines as a line to their end point, with a note. A
  row's type comes from the stored row element of one of its cells.
  The text block is the effective TxtPinX to TxtAngle (when no sheet
  stores them, Visio's default: the shape's box) composed with the
  shape's transform, unmirrored when the shape is flipped, with the
  effective margins, VerticalAlign and each Character and Paragraph
  row's effective font, size, colour, style bits and HorzAlign (the
  HTML read shows carries only what the shape stores, so the drawing
  cannot use it).
- **Scene** (`internal/render`, shared engine): fills, solid strokes,
  text lines and images, in points. `WriteSVG` writes it and
  `Rasterise` draws it (signed-area accumulation, non-zero, after
  font-rs; strokes as same-wound quads plus round joins), so the SVG
  and the PNG draw the same marks; the rasteriser reads the scene,
  not the SVG text, and needs no SVG parser.
- **View** (`internal/vsdx/view`): model to scene. Hidden layers are
  skipped; background pages are drawn under the page (up to 8).
  Limits are `view.Limits`, shown in `help read`.

Grounding and choices:

- **No dash arrays, markers, clip paths or SVG patterns.** MuPDF
  1.27.2 (`mutool draw`, the library behind PyMuPDF), on a test SVG:
  `stroke-dasharray` ignored (solid line), `marker-end` ignored (no
  arrowhead), `clip-path` ignored (lines drawn past the clip), and a
  `fill="url(#pattern)"` (also with a `none` fallback) drawn solid
  black. Text with tspans, font-weight, font-style and text-anchor
  and `xlink:href` data-URI PNGs work. So dashes are segments,
  arrowheads filled paths, hatches lines clipped here (`render.Hatch`,
  non-zero, aligned to the page origin so neighbouring shapes line
  up) and dots small squares. The contract asked for `<pattern>`s;
  this is the documented reason not to use them. MuPDF also anchors
  each tspan of a centred line on its own, so a line of several
  styles places each span at an x from the width tables; one style
  keeps the viewer's own anchoring.
- **Patterns** follow §14's table: 0.1 in (7.2 pt) spacing for 2 to 7,
  0.05 in for the rest, lines 0.5 pt; `dots` (8) as 1 pt dots 3.6 pt
  apart in offset rows, where LibreOffice draws a triple hatch. 9 to
  18, which the contract does not name, are dots of decreasing
  density, with a note.
- **Dashes** are libvisio's LinePattern table
  (`VSDContentCollector::_lineProperties`, read verbatim): pattern 2
  is 6 on, 3 off, 3 is 1 on, 3 off, ... 23, multiples of the line
  weight (at least 0.5 pt here, so hairline dashes stay visible).
- **Arrowheads** are libvisio's markers 4 (triangle,
  `m10 0-10 20h20z`) and 5 (stealth, `m10 0-10 20q10,-5 20,0z`) at its
  width for a weight of w inches, `0.1/(w*w+1) + 2.54w` inches (at
  least 0.05 in), as long as wide; libvisio ignores BeginArrowSize and
  EndArrowSize, and so does the view (a limit: Visio applies them).
  The line stops inside the head (0.9 of its length for triangle, 0.75
  for stealth) so a thick line does not show past the tip.
- **Text widths** are the advance widths of printable ASCII from the
  OFL fonts LibreOffice 26.2 bundles: Carlito (Calibri metrics),
  Liberation Sans (Arial) and Liberation Serif (Times New Roman),
  regular and bold (`metrics_table.go`, read from hmtx and cmap once;
  Liberation Sans matches the published Helvetica widths, `A` 667). The
  repository had no font metrics before. A face is classed by name;
  others measure as Arial. Wrapping is greedy at spaces, a word wider
  than the line is cut between characters, line height is 1.2 times
  the largest size (or SpLine's percentage), the baseline 0.8 of the
  size into the line. The PNG drew text with the overlays' 7x13
  glyphs scaled to each advance until §17 replaced them with the
  embedded fonts' outlines.
- **Renderer choice**: `--renderer auto` keeps LibreOffice where it is
  installed (existing callers get the same picture) and falls back to
  `svg` where it is not (Code Mode gets a picture, not an error); the
  result's `renderer` says which drew it, and `libreoffice` still
  refuses without LibreOffice. A `DD_SOFFICE` that names no file counts
  as not installed for `auto`. No dependency was added: the standard
  library has no vector rasteriser, so `internal/render` has one
  (about 400 lines), and decodes pictures with `image/png`,
  `image/jpeg` and `image/gif`.
- **Bounds on untrusted input**: 4096 drawn geometry rows per object,
  256 pieces per curve, 20,000 hatch lines or dots and 10^8 hatch
  tests per area, 64 MiB per embedded picture, 40 million raster
  pixels, 4096 text lines per block; each cut is a note.

Evidence (2026-11-25): `TestRenderPatternsAndPaths` runs its checks
(pattern ink, hatch slant, clear and white backgrounds, curve sample
points) for both renderers; `TestRenderSVGMatchesLibreOffice` compares
the svg renderer with LibreOffice on that page and on a figure-like
page at scale 2 (2 pt tolerance, measured unmatched ink at most 2.2%,
bound 4%; the derivation is in the test); `TestReadSVGStructure`
checks the SVG without any renderer; `FuzzPageSVG` and
`TestSceneProperty` check well-formed output and no panics. Dropping
pattern fills fails the structure, comparison and paths tests;
dropping arrowheads fails the structure test (the pixel comparison is
too coarse for them, which is why they are counted). LibreOffice's own
import draws a Character row that stores no cells (vsdx writes
`<Row IX='0'/>` for the plain run after a bold one) with the previous
row's style, so it shows "Controller 104 ..." all bold and carries an
italic into later shapes; the drawing view and `read` show the file's
values. Visio's drawing of either is unverified (no Visio here).

## 16. Text blocks and text runs (2026-11-25, owner sign-off pending)

Asked for by the patent-figure kit, which placed labels with blank
paragraphs, separate white rectangles and invisible label shapes
because the engine had no text block fields and its multi-run text
drew wrongly in LibreOffice. Contract: §3, Text blocks and Text runs.

### Text runs: what was wrong, and whose bug

The engine numbered Character and Paragraph rows by format, not by
position: a format used twice shared one row (`<cp IX='1'/>Bold<cp
IX='0'/> plain <cp IX='2'/>it<cp IX='0'/> plain`), the format with no
cells was row 0 written as an empty `<Row IX='0'/>`, and paragraph
rows could appear out of order in the text (`<pp IX='1'/>…<pp
IX='0'/>`). Visio never writes that: over the vsdx corpus, 59
Visio-authored texts with more than one `cp`, none reuses a row, none
is out of order and no Character row is empty; Visio's rows carry the
full cell set (126 rows with 17 cells).

libvisio (VSDXMLParserBase::readText, readCharIX; the same code from
0.1.8 to master) reads text by character counts: each row's count is
the number of characters after its marker, and the rows are applied
in IX order. A reused row sums its stretches and lands them together,
rows out of order format the wrong characters, an empty row element is
skipped (readCharIX returns on an empty element), and the first row it
does read becomes the shape's default style (the bold leak: a plain
row 0 that is empty makes bold row 1 the default).

The fix (`text.Renumber`, `emit.CanonicalTextXML`, used by object.add
and text.set) writes Visio's form: rows in text order from 0, a new row
where the format changes, each marker once, every row with the same
cells, an inherited value as `V=<inherited> F="Inh"` (Visio's form for
an inherited cell; read ignores it as formatting, so `html` round-trips;
`formatsCells` already treats F="Inh" as not the owner's). The inherited
value is the cell below the shape at that IX (master row, style chain),
else the style chain's row 0 (a style sheet stores only row 0); Style,
Strikethru and Bullet default to 0. text.replace still writes inside
the existing rows and is unchanged.

That fixes the markup, but LibreOffice 26.2.4.2 still draws a shape's
whole text in its last row's format: converting to ODG gives one span
style for the whole text. The same happens with Visio's own rows and
Text transplanted into a shape (apache-poi 60973.vsdx page3 shape 137:
three sizes, drawn all at the last one's 3 pt). So that part is a
LibreOffice bug, documented in `help render`; the engine does not work
around it (e.g. by splitting labels into shapes). Before the fix the
four cases of `TestTextRunsVisioForm` drew wrong in LibreOffice in four
different ways; after it they draw in one style each, as Visio's own
text does there.

Changed expectation: `TestTextSetInheritedItalic` (row CC11) asserted
"no local Character row" when every value is inherited. The row is now
written with F="Inh" cells only; html and the italic are unchanged.

### Text block fields

`text_block {valign, margins, background}` on object.add and
object.set, grouped like `shadow` and pptx `text {anchor, insets}`
(margins in pptx's inset order). Cells and forms are the corpus's:
`VerticalAlign` 0/1/2; margins in inches with `U="PT"` (56 of the
corpus's LeftMargin cells); `TextBkgnd` 0, `#RRGGBB` or a colour index
plus one (as libvisio reads it), `TextBkgndTrans` a fraction. vsdx
writes `#RRGGBB` without Visio's `RGB(…)+1` formula, which the Formulas
rule would then refuse to overwrite (VE21).

`read` shows `text_block` only when the shape or its master stores one
of its cells. Showing it on every object with text (the style's
values) cost a full read of Generated(5000) 13.7 MB more allocation
(123.4 MB against a 123.0 MB budget) and BuildPage(30000) 2.7 MB more
peak (121.4 MB against 120.0 MB); with the stored-only rule both are at
the baseline (110.4 MB, 118.7 MB). The model keeps the block on every
object with text only for the drawing view and `--detail full` (the
law suite reads effective values from it).

The drawing view lays text out in the block (TxtWidth and the rest, or
the shape's box) with its margins and valign, and fills the background
behind the laid-out lines grown by the margins, at 1 - transparency.
LibreOffice draws valign and margins the same way; it draws the
background as a character highlight behind the letters, opaque.

Polygons: a closed polyline or curve already took `html`; its text
block is its box, as Visio's default formulas give, so a diamond holds
its own label (`TestTextBlockPolygon`, `TestReadSVGTextBlock`).

Tests: `TestTextBlockWriteRead`, `TestTextBlockValign`,
`TestTextBlockUnits`, `TestTextBlockPlanErrors`,
`TestTextBlockPolygon`, `TestTextRunsVisioForm`, `TestRenumberProperty`,
`TestTextBackground`, `TestReadSVGTextBlock`, the `text_block` law
suite (18 targets on V1-V17: identity, inverse, atomicity, sequence)
and the text block page in `TestRenderSVGMatchesLibreOffice`. Each new
test was seen to fail with the code it checks removed.

## 17. Outline fonts in the Go renderer (2026-11-25, owner decision pending on embedding)

Code Mode has no LibreOffice, so `--renderer svg` (and `auto`) is the
picture its agents check. The 7x13 bitmap font drew a box for every
character outside printable ASCII: `•`, `₀`, `→`, `…`, Greek, primes.

The renderer now fills glyph outlines from embedded fonts
(`internal/render/fonts`, unmodified, with `OFL.txt`,
`LICENSE-DejaVu.txt` and `PROVENANCE.md`): Carlito, Liberation Sans and
Liberation Serif, each in four styles (the faces the width tables came
from, so ASCII widths are unchanged: `TestEmbeddedFonts` compares every
printable character with the old tables), and DejaVu Sans as the
fallback (`⇒` and `′` are not in Carlito or Liberation). A run is drawn
in its face's family, weight and slant; a character that face lacks
comes from the other families in the same style, then DejaVu Sans; one
no font has is left out with a note naming it, never a box. Widths for
layout come from the same fonts (italic now from the italic faces).
Glyphs are unhinted and not kerned.

The reader is `internal/render/ttf`, standard library only: the table
directory, head, maxp, hhea, hmtx, cmap formats 4 and 12, loca and glyf
with composite glyphs (offsets, scales, 2x2 transforms; point matching
is not applied), every read bounds-checked, components nested at most 8
deep and at most 65536 points per glyph. Over 8401 glyphs of three of
the fonts its contours, point counts, coordinate sums and advances
equal fontTools'. `golang.org/x/image` (opentype, vector) would have
done this but breaks the standard-library rule (`make norequire`); the
rasteriser already had the anti-aliased non-zero fill the glyphs need.

Cost: the fonts are 6.4 MB, embedded in the vsdx binary (and so in
each wheel). They are third-party binaries committed to the repository,
which AGENTS.md's data rules do not cover (they allow small authored
fixtures): that is the owner's call. Options if 6.4 MB is too much:
drop Liberation Serif (1.5 MB; Times-like faces would draw as Arial) or
the italic faces (2.1 MB; italic drawn upright). Subsetting is not an
option without renaming Carlito (its Reserved Font Name binds modified
versions).

Tests: `TestGlyphCoverage` (each of about 270 characters, ASCII,
Latin-1, Greek, arrows, sub- and superscript digits, punctuation and
maths signs, in 3 families by 4 styles, must ink the picture and differ
from a character no font has; on the bitmap font it fails 1944 times),
`TestMissingGlyphNote`, `TestEmbeddedFonts`, `TestParse` (values from
fontTools), `TestParseTruncated`, `FuzzParse`. The LibreOffice
comparisons stay within bound (figure 0.02% and 0.52% unmatched, text
block 0.39% and 0.00%, paths unchanged).

## 18. Placing the text block (2026-11-25, owner sign-off pending)

Patent figures put a label outside its shape's geometry: a region's
title on its border, an arrow's label beside its line, a numeral at a
leader's end. `text_block.box` `[x, y, w, h]` and `text_block.angle`
place it, in page view points like `box`, `from`, `to` and `points`,
so an agent derives a label's place from what `read` shows without
frame arithmetic, and the same form works for 2-D and 1-D shapes. The
peer's suggestion was the shape's local space; page space was chosen
because 1-D shapes' local frames run along the line (Visio's `Width` is
the length) and a rotated shape's local frame is turned, both of which
an agent would have to invert.

`place.TextFrame`, `place.TextBox` and `place.TextCellsFor` hold the
geometry once for read, the drawing view and the writer: the frame is
the drawing's (the block's own space through `TxtLocPin`, `TxtAngle`,
`TxtPin`, the shape's transform and its parents', a flipped shape's
text reflected so it is not mirrored), and `TextCellsFor` its inverse
pinned at the block centre (`TestTextCellsForProperty`: 5000 random
shapes, angles, flips and group frames round-trip to 1e-6).

Cells, as Visio stores them (corpus survey): moved 2-D text keeps plain
`TxtPinX`/`TxtPinY` values, `TxtLocPinX`/`TxtLocPinY` with
`TxtWidth*0.5`/`TxtHeight*0.5` (the most common form: 173 TxtLocPinX cells), and
Dynamic connectors with moved text store `Controls.TextPosition` `X`
and `Y` with `XDyn`, `YDyn` and the pins as F="Inh" caches (every
sampled connector with a local TextPosition row). The writer stores all seven cells the first time it places a
block: an absent `TxtPinX` is `Width*0.5` to Visio but 0 to
LibreOffice (an arrow label drew at the line's start until it was
written). The edit layer gained `SetRowCells` for the named row.

LibreOffice 26.2 draws placed and turned blocks on rectangles,
master-less connectors, open paths and a Visio Dynamic connector whose
text was moved, as the Go renderer does (placement page: 0.00% and
0.04% unmatched). A white background on a region title clears the
border in both: LibreOffice behind the letters only, the Go renderer
over the text and its margins.

Tests: `TestTextBoxPlacements` (the three placements and an angle,
cell values derived by hand from the geometry, identity, angle alone),
`TestTextBoxPlanErrors`, `TestTextBoxDynamicConnector` (corpus:
TextPosition and caches, identity, the TEXTHEIGHT refusal),
`TestReadSVGTextPlacement`, `TestTextCellsForProperty`,
`TestTextBoxDefault`, the law suite's box (16 inverse read-backs fail
when the box is dropped) and the placement page in the LibreOffice
comparison. Each was seen to fail with its code removed.

Packaging (owner approved embedding every font, 2026-11-25): each wheel
carries `LICENSE`, `fonts/OFL.txt`, `fonts/LICENSE-DejaVu.txt` and
`fonts/PROVENANCE.md` in `.dist-info/licenses`, each a `License-File`,
under `License-Expression: Apache-2.0 AND OFL-1.1 AND Bitstream-Vera AND
LicenseRef-Arev` (SPDX has no identifier for the Arev licence; PEP 639
allows `LicenseRef-`). `verifyWheel` (every release) and
`make release-smoke` refuse a wheel missing any of them. A binary
copied without the wheel prints the notices and licence texts with
`vsdx help fonts`.
