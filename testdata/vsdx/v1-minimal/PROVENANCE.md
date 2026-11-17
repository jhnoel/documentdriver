# V1 minimal

Written by hand for documentdriver (2026-11-17), from the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml) and `../common/app.xml`
(as docProps/app.xml).

One page `page0` "Page-1", 8.5 x 11 in, no PrintPageOrientation on the
page sheet (StyleSheet 0 has 1). One shape s1: PinX 2, PinY 9, Width 1,
Height 0.5, LocPin 0.5/0.25 (formulas Width*0.5, Height*0.5), styles 3
(chain 3 -> 6 -> 0), FillForegnd "#4472c4" (lower case as stored), no
local line cells, a local Character row 0 with Style 1 (bold), Text
"Hello" followed by a line feed.

Expected (docs/vsdx-design.md, fixture table): box [108,126,72,36];
fill {"hex":"#4472C4"} origin local; line w 0.75 (StyleSheet 0) and
colour #7F7F7F (StyleSheet 6, index 24) origin style; html
`<p><b>Hello</b></p>`; text "Hello"; orientation "portrait" (size),
print_orientation "portrait" (StyleSheet 0).
