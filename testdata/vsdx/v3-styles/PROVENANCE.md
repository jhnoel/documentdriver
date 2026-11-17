# V3 style chains

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

Own visio/document.xml: StyleSheet 0 as common; 1 "Thin" (parents 0;
LineWeight 0.01388888888888889 = 1 pt, LineColor #000000); 2 "None"
(LinePattern 0, FillPattern 0); 3 "Thick" (LineStyle parent 1,
LineWeight 0.04166666666666666 = 3 pt); 5 "LoopA" and 6 "LoopB" whose
LineStyle parents point at each other.

| Shape | Styles (line, fill, text) | Expected |
|---|---|---|
| s1 | 3, 0, 0 | LineWeight 0.04166666666666666 (style 3), LineColor #000000 (style 1 through 3), line w 3 pt |
| s2 | 1, 0, 0 | LineWeight 0.01388888888888889, line w 1 pt |
| s3 | 5, 0, 0 | line cells absent; one note "style cycle 5 -> 6 -> 5" for the page, however many cells |
| s4 | 2, 2, 0 | fill none, line none |

All four: PinX/PinY (2,9) (4,9) (6,9) (2,7), 1 x 0.5, LocPin 0.5/0.25,
no local line or fill cells; fill FillForegnd 1 (white, StyleSheet 0)
for s1 to s3.
