# vsdx: design (draft for the Fable advisor and the owner, 2026-11-17)

The fourth CLI, `vsdx`, reads and edits Microsoft Visio drawings
(.vsdx, .vsdm, .vstx, .vstm; stencils .vssx/.vssm read only) on the same
engine as `pptx`, `docx` and `xlsx`. The owner's instruction: "Add a vsdx
tool to documentdriver patterned after pptx tool." The agent surface is
drafted in `docs/vsdx-contract-draft.md`; `docs/contract.md` is not
changed until the owner signs the draft.

Format facts come from the researcher's notes (kept beside the work as
`.worktrees/vsdx-work/vsdx-format-notes.md`, not committed; tags [C]
confirmed in a fetched source, [K] knowledge, UNCONFIRMED). Every format
behaviour below that the notes mark [K] or UNCONFIRMED is an experiment
(VE1 to VE12, Verification) before code that relies on it merges, as
xlsx did with E1 to E13.

Status: P0 skeleton and the shared promotions are on `wt/vsdx-design`
(see "What landed with this design"). Nothing reads a file yet.

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

Counts: REUSE 26, PROMOTE 3 (2 done with this design, 1 for P2), NEW
SHARED 3, NEW vsdx-only 14.

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
| 31 | Visio child order and value tables (P2) | NEW SHARED (S3) | `internal/ooxml/schema` table `vsd` generated from the MS-VSDX XSD | edit-core inserts read generated child order; never hand-written (owner decision 2) |
| 32 | `visio-smoke` (designed, not built) | NEW SHARED tool (later) | `tools/visio-smoke` on `tools/internal/officeapp` conventions | owner ruling 3: no Windows Visio now |
| 33 | Visio vocabulary: namespaces, names, units, style categories, addresses | NEW vsdx-only | `internal/vsdx/vsd` | the only home of Visio literals (archtest rule W1) |
| 34 | Package structure: document, pages, masters, style sheets, fonts, custom props | NEW vsdx-only | `internal/vsdx/doc` | Visio part graph |
| 35 | ShapeSheet index | NEW vsdx-only | `internal/vsdx/sheet` | Cell/Section/Row keys |
| 36 | Inheritance: master shape, style chain, origins | NEW vsdx-only | `internal/vsdx/inherit` | Visio-specific chain |
| 37 | Pin/LocPin/Angle/Flip to the view frame | NEW vsdx-only (on geom) | `internal/vsdx/place` | Visio transform semantics; arithmetic on `geom` |
| 38 | Text mixed content (cp/pp/tp/fld) to HTML | NEW vsdx-only | `internal/vsdx/text` | Visio text model; renders the pptx grammar's subset (law X1) |
| 39 | Read model | NEW vsdx-only | `internal/vsdx/model` | objects, glue graph, props, layers, hazards |
| 40 | read, find, xml results | NEW vsdx-only | `internal/vsdx/read` | contract shapes |
| 41 | Fixture builder | NEW vsdx-only (tests) | `internal/vsdx/vsdxtest` on `opc.BuildZip` | V1 to V17 |
| 42 | ShapeSheet formula lexer, constant check, closed evaluator (P2) | NEW vsdx-only | `internal/vsdx/formula` | decision c; xlsx's `formula` is Excel's grammar |
| 43 | Edit layer (P2) | NEW vsdx-only | `internal/vsdx/edit` | handles on Shape/Cell/Row through the core |
| 44 | Op table and kernels (P2) | NEW vsdx-only | `internal/vsdx/ops`, `internal/vsdx/apply` | as xlsx's layering |
| 45 | Integrity gate (P2) | NEW vsdx-only | `internal/vsdx/integrity` | unique IDs, Connects resolve, one rel/override per part (notes §9) |
| 46 | CLI | NEW vsdx-only | `cmd/vsdx` | the binary |

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
a Visio default. A value stored as `themed` with no cached colour is
omitted with reason `themed value not derived` (theme resolution is not
in v1).

How the agent tells them apart: `--detail full` adds, as pptx does,
`origins` (`{"box":"local","fill":"inherited","html":"master","line":"style"}`)
and `cells`, the shape's own stored cells (`[{"n":"PinX","v":"2.5","f":"..."}]`,
key spelled as the ShapeSheet spells it: `PinX`, `Prop.Cost.Value`,
`Geometry1.X2`, `Char.Size` rows as `Character[0].Size`). Origins:

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
`drawing scale 1 in = 10 ft: lengths are stored drawing units`;
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
(Field row; content is the cached display text). LF ends a paragraph,
U+2028 is a line break (VE7), TAB is a tab.

Mapping to the contract's HTML (the pptx grammar's subset, section 7 of
the draft), each property resolved through (a) for the row's cells:

| Visio | HTML (pptx form) |
|---|---|
| paragraph | `<p>`; bulleted (`Bullet` != 0) `<ul><li>`, `data-char` when `BulletStr` is stored |
| `HorzAlign` 0/1/2/3 | `style="text-align:left|center|right|justify"`; 4 (force) not shown, kept on write |
| `IndLeft`, `IndRight`, `IndFirst` | `margin-left`, `margin-right`, `text-indent` in pt |
| `SpBefore`, `SpAfter` | `margin-top`, `margin-bottom` in pt |
| `SpLine` (negative: a multiple) | `line-height` (`120%` or `14pt`) |
| `Style` bits 1, 2, 4 | `<b>`, `<i>`, `<u>`; a row that clears a bit the style sets: `font-weight:normal` etc. |
| `Strikethru` | `<s>` |
| `Size` (inches) | `font-size` in pt |
| `Color` | `color:#RRGGBB` |
| `Font` (index into FaceNames) | `font-family:'Name'` |
| U+2028 | `<br/>` |
| `fld` | `<opaque data-ref="page0/s5/field:1" data-kind="field" data-text="cached"></opaque>` (contract placeholder; `data-text` derived, ignored on input) |
| `tp`, `Pos`, `Case`, small caps, `DblUnderline`, `Letterspace`, `LangID`, `FontScale`, Asian and complex-script fonts | not shown; kept on write from the run or paragraph replaced (as pptx keeps east Asian fonts) |

Adjacent runs that show the same formatting are merged (pptx's
`canonical`). A shape without its own `Text` shows its master shape's
(origin `master`); `text.set` on it writes local Text and local
Character/Paragraph rows only for what the HTML sets differently from
the inherited rows. `text` (plain, `find`'s haystack) joins paragraphs
with `\n`, a break as `\n`, a field as its cached text.

### (g) Opaque and refused in v1 (closed list)

Read, not editable (kind `picture` or `other`, or shown and marked):
Foreign shapes (images, OLE, ink), guides, master shapes (all of a
master), comments (not shown in v1), hyperlinks, actions, user cells and
geometry (`--detail full` `cells` only), data-graphic callouts, themes.

Refused by P2 ops, `operation_failed` with `detail` naming the construct:

1. a target cell whose formula is outside the allow list (decision c);
2. a shape in a container, list or callout structure (`User.msvStructureType`,
   `User.msvSDContainer...`, a non-empty `Relationships` cell) for
   move, resize, delete;
3. a data-linked shape data row (`DataLinked` 1) for `prop.set` and
   `prop.delete`;
4. any op on a master or a master shape, and any write to a stencil;
5. text and fill on a Foreign shape;
6. glue to a ToCell other than `PinX` and `Connections.Xn`;
7. deleting a page another page uses as its background;
8. geometry writes on a scaled page (until VE11);
9. `page.set` orientation or size when PageWidth or PageHeight has a
   formula outside the allow list.

Everything else no op targets is copied byte for byte, unknown parts and
`v:` extensions included; `.vsdm` VBA copied through.

### Page orientation (owner ruling 1)

Read: the summary's page entries and the page result show `size [w, h]`
(PageWidth, PageHeight) and `orientation`: `"portrait"` or
`"landscape"` from `PrintPageOrientation` (1, 2) when the page sheet (or
the document's default page sheet through its styles) stores it, left
out otherwise (never guessed from the size).

Write (P2): `page.set` with `orientation` and/or `size`. The rule:

1. `orientation` different from the stored one swaps PageWidth and
   PageHeight (when they differ) and sets `PrintPageOrientation` (1
   portrait, 2 landscape). Equal to the stored one: nothing changes.
   `orientation` with `size` sets the size as given and the orientation
   as given (no swap); a size whose shape contradicts the orientation
   (wider than high for portrait) is `invalid_operation`.
2. No shape cell changes: stored pins stay measured from the page's
   lower-left corner, as Visio keeps them (VE10). Because the view is
   y down from the top, every shown `y` moves by the change in height;
   `changed` lists the page and every top-level object (read back), so
   the agent sees the new coordinates.
3. Cells whose formulas reference the page size (`ThePage!PageWidth`,
   `PageHeight`, title blocks, backgrounds): on the page sheet itself,
   the closed evaluator (decision c-R) rewrites its own dependents
   (margins are constants; `DrawingSizeType` auto-size is left to
   Visio); dependents on shapes (`ThePage!...`) are left stale, with
   RecalcDocument set and `stale-values` reported naming them.
   Under c-S all of them are stale.
4. Refused: a page whose PageWidth or PageHeight formula is outside the
   allow list (rule g9).

pptx has no orientation (a deck has one slide size); this is in the
differs list and fixture V17.

### Create (P3)

What `create` can mean: a blank drawing (one page `Page-1`, US Letter or
A4 portrait, no masters, the minimal style sheets Visio writes), with an
optional plan. Without masters `object.add` can only add master-less
shapes (P3 adds `geom: rect|ellipse|line` writing Geometry rows) or
masters copied from another file (`object.add` with `from:
"other.vssx#master:Process"`, the master-copy checklist of notes §7,
the most corruption-prone path per notes §9). Recommendation: defer
`create` to P3 and decide its template only after a real-Visio check is
available (owner decision 5); nothing in P0 to P2 depends on it.

## 5. Phases

Each phase merges on its own with main green (`make ci`), archtest rules
in force, and its own laws.

- **P0 skeleton (this branch + S0, W1, W2, F1, F2):** shared promotions
  (done), packages and signatures (done), `cmd/vsdx` wired (done),
  archtest `formats` (done), vocabulary rule, Makefile (`build` done;
  `REPRO_CMDS`, `FUZZ_TARGETS`, `scaling`, `corpus` with `VSDX_CORPUS`),
  ctypes from Visio-authored files (S0, blocked on owner decision 1),
  the corpus suites against `VSDX_CORPUS` (fetch script and manifest are on wt/vsdx-corpus), fixtures V1 to V17.
- **P1 read/find/xml:** chunks V1, V2, H1, P1, T1 (pure, parallel), then
  D1 to D3, H2, H3, T2, M1 to M4, R1 to R3, C1, L1, X1. Release wiring
  (wheel, release-smoke, RELEASING.md) when P1 merges (W3).
- **P2 apply:** S2 (CSS promotion), S3 (schema table, owner decision 2),
  formula, edit, integrity, op table with all ops pending, apply on
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
- **P3 create** (deferred, decision 5); layers (`layer.add`,
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
  `Part.Forget`): peak is the largest page, not the document.
- **Summary** counts objects with `tree.ScanString` (memory in the
  depth).

Budgets (L1 measures, then fixes with about 20% margin, as xlsx did):
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
5. **Schema validation** (owner decision 2): the unmodified MS-VSDX XSD
   (licence allows unmodified redistribution with notice) under
   `tools/conformance`, run by `uv run --script` with lxml over
   fixtures, corpus inputs and P2 outputs, with a namespace-rewritten
   copy for 2012/main files. Like the OOXML gate it fails only on error
   kinds an output has that its input did not.
6. **Structural lint in Go** (P2 integrity gate, shared with the read
   hazards): unique shape IDs per page across nesting, Connects resolve,
   Master/MasterShape/style IDs resolve, page IDs unique, BackPage
   resolves, one Override per part and one rel per (source, target,
   type), document content type matches the kind. Refuses only what a
   plan introduces (contract section 10).
7. **Cross-reader** (`eval/vsdxnaive`): an independent naive Go reader
   (local cells only, no inheritance) compared with `read` on the corpus
   for local-origin values; Apache POI XDGF or python-vsdx as optional
   second parsers if the owner wants a JVM or uv tool (not planned).
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

### Fixtures (hand-written XML under testdata/vsdx/, F1 and F2)

Each is a directory of zip members plus `PROVENANCE.md` ("written by
hand for documentdriver; not Visio-authored"). They prove what our code
does with the structures the notes describe; they do not prove Visio
writes them that way (that is the corpus's and VE's job).

| # | Content | Proves |
|---|---|---|
| V1 | one page `Page-1` (ID 0, 8.5 x 11 in, orientation 1), one rectangle s1: PinX 2, PinY 9, Width 1, Height 0.5, LocPin 0.5/0.25, Geometry rectangle, Text `<cp IX="0"/>Hello`, FillForegnd `#4472C4`, LineWeight 0.01041666666667 | navigation, summary, y-flip box `[108,126,72,36]`, fill `{"hex":"#4472C4"}`, line w 0.75, html `<p>Hello</p>` |
| V2 | master2 "Process" (one shape ID 5: Width 1, Height 0.75, LocPin formulas, Text `Process`, FillForegnd `#FFFFFF`); page shapes s1 (Master 2, PinX 2 PinY 9 only), s2 (Master 2, PinX 4 PinY 9, own Text `Review`, FillForegnd `#FF0000`, Width 1 F="Inh") | inheritance: s1 box `[108,117,72,54]` origin master, html `<p>Process</p>` origin master; s2 html local, fill local, width inherited |
| V3 | style sheets 0 "No Style", 1 "Normal" (LineWeight 0.01388888888889 = 1 pt, LineColor `#000000`), 3 "Thick" (LineStyle parent 1, LineWeight 0.04166666666667 = 3 pt), 5 and 6 with LineStyle parents pointing at each other; s1 LineStyle 3 (no local line), s2 LineStyle 1, s3 LineStyle 5 | style chain: s1 line w 3 color from 1 (origin style), s2 w 1, s3 line omitted + one note "style cycle 5 -> 6 -> 5" |
| V4 | group s10 (PinX 4, PinY 5, 2 x 2, LocPin 1/1) holding s11 (PinX 0.5, PinY 0.5, 1 x 1) and a nested group s12 (PinX 1.5 PinY 1.5, 1 x 1, Angle 0.5235987755983 (30 deg)) holding s13; group s20 with FlipY 1 holding s21 | composition: s11 box `[216,432,72,72]` parent `page0/s10`; s13 rotated; s21 flip_v true; addresses of nested ids; depth |
| V5 | boxes s1 (PinX 2, PinY 9), s2 (PinX 5, PinY 9) with Connection rows (X 0.5 Y 0.25 / 0 0.125 / 1 0.125 ...); connector s3 (BeginX 2.5 BeginY 9, EndX 4.5 EndY 9) glued begin to s1 PinX, end to s2 Connections.X2; s4 glued end to s9 (absent); s5 point-glued to s1 Connections.X1 with BeginX 3 (off site) | `from_glue "s1"`, `to_glue "s2:2"`, connections graph, `dangling-glue` on s4 (`#9`), `stale-connector` on s5 |
| V6 | shape data on master and instance: rows Cost (Type 2, Value 12.5, U CY), Owner (Type 0 "Ada"), Done (Type 3 per VE9, Value 1), Due (Type 5, date), Hidden (Invisible 1), Linked (DataLinked 1), a master row the instance deletes (Row Del 1), a master-only row | prop types and values, origin master for master-only, deleted row absent, hidden and linked flags, ids `page0/s1/prop:Cost` |
| V7 | text formatting: Character rows 0 (Size 0.1666666666667 = 12 pt), 1 (Style 1, Color `#FF0000`), 2 (Style 0 on a style that sets bold); Paragraph rows 0 (HorzAlign 1), 1 (Bullet 1, BulletStr "•"); text `<pp IX="0"/><cp IX="0"/>Title<cp IX="1"/> red\n<pp IX="1"/>item<U+2028>two\tend <fld IX="0">3</fld>` | the T1/T2 rows on a real page; field placeholder `page0/s1/field:1` |
| V8 | pages 0 (BackPage 3), 1, 3 (Background 1); page 0 Layer rows 0 "Flowchart", 1 "Connector" (Visible 0); s1 LayerMember "0;1" | summary order 0,1,3; background flags; layers and object `layers` |
| V9 | rotation/flip matrix: s1 Angle 1.5707963267949 LocPin 0/0 (box `[90,90,72,36]` rot 270); s2 FlipX; s3 Angle -0.5235987755983 | rot 30 and 270, flips, LocPin off centre |
| V10 | defects: page 2 malformed XML, page 4 Rel to a missing part, a shape with Master 99 (absent), a cell `V="themed"`, an unknown Section, duplicate shape ID 7 twice, a Connect with FromSheet 0 | notes not failures; `page0/s7` and `page0/s7~2`; unreadable_target for `--page page2` only |
| V11 | V1 in the 2011/1/core namespace | same read as V1 |
| V12 | V1 as .vsdm with a `vbaProject.bin` member and macro content type; a .vssx stencil with masters only | kinds; stencil summary has no pages; read only |
| V13 | page with PageScale 1 in, DrawingScale 120 in | the scale note (VE11) |
| V14 | (generated, not committed) `vsdxtest.Generated(n)` | scaling and memory |
| V15 | Foreign image shape (PNG member via page rels), an OLE Foreign, a Guide | kinds picture and other; read only |
| V16 | group nesting 600 deep (generated in the test) | the tokenizer cap: `--page` of it is unreadable_target, the summary still lists the page with a note (as pptx TestDeepGroups); no stack growth |
| V17 | page 0 portrait 8.5 x 11 (PrintPageOrientation 1), page 1 landscape 11 x 8.5 (2), page 2 with no PrintPageOrientation; a shape on page 0 with PinY referencing `ThePage!PageHeight*0.5` | read orientation and size; P2: `page.set orientation landscape` on page 0 swaps size, keeps PinY stored, rewrites or reports the dependent per decision c |

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
- `eval/vsdxnaive` cross-reader comparison.
- P2 `TestApplyCorpus` (each op on the first page of each file).

S0's ctypes extraction runs over the Visio-authored subset
(`docProps/app.xml` Application "Microsoft Visio"), with `min_files` 2
per entry; a type seen in fewer files stays out of the table and the
code that needs it waits (S0 reports which).

### Visio experiments (VE1 to VE12, results recorded before dependent code merges)

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
   XSD; VE list; visio-smoke later.
3. **Glue writes** are the most corruption-prone feature in every other
   implementation (notes §9). They are last in P2's order and refuse
   anything beyond PinX and Connections.Xn.
4. **ctypes and schema need owner decisions** (S0 blocks P1's D1; S3
   blocks P2's inserts).
5. **Inheritance edge cases** (group masters' MasterShape chains, styles
   with Del rows, theme values) are under-specified in the notes; reads
   omit with a note rather than guess.
6. **y-down view** shifts shown y with page height; agents might expect
   Visio's y-up. Mitigation: `cells` shows stored values; help says so.
7. **Grammar drift from pptx**: X1 catches it.

## 10. Open owner decisions

1. **ctypes source.** Approve S0's ctypes extraction from the
   Visio-authored files of the fetched corpus (wt/vsdx-corpus).
   Recommend: yes; S0 blocks P1's D1.
2. **Vendor the MS-VSDX XSD** (unmodified, with its notice) for the P2
   schema table and the conformance check. Recommend: yes, under
   `corpus/ooxml-schemas` beside the ECMA schemas (or committed in
   `tools/conformance` if the licence notice is enough for the owner).
3. **Dependent cached values** (decision c): R (closed same-shape
   evaluator plus RecalcDocument) or S (never evaluate, xlsx-strict).
   Recommend R: without it every resize renders wrong outside Visio.
4. **`stale-values` and `RecalcDocument` as the third exception** to
   untouched bytes (contract draft section 9). Recommend: accept.
5. **create**: defer to P3 until a Visio check exists. Recommend: defer.
