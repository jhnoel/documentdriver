# V17 orientation

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

StyleSheet 0 (common) stores PrintPageOrientation 1.

| Page | Size | Local PrintPageOrientation | Expected orientation / print_orientation |
|---|---|---|---|
| page0 "Tall" | 8.5 x 11 | 1 | portrait / portrait (local) |
| page1 "Wide" | 11 x 8.5 | 2 | landscape / landscape (local) |
| page2 "Wide-inherited" | 11 x 8.5 | none | landscape / portrait (style: they disagree) |
| page3 "Square" | 8 x 8 | none | (absent) / portrait (style) |

page0 s1: PinX 2, PinY V 5.5 F ThePage!PageHeight*0.5, 1 x 0.5 (box
[108,378,72,36]).

P2 and create (decision 6): page.set orientation landscape on page0 ->
size [792,612], local PrintPageOrientation 2, s1's PinY stored value
unchanged (5.5; shown y 216 - 18 = 198: box [108,198,72,36]) and a
stale-values hazard naming page0/s1 PinY; on page2 -> no size change,
local PrintPageOrientation 2 written; on page3 -> no size change, local
2 written; on page1 -> nothing written (a no-op).
