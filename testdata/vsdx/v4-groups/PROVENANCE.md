# V4 groups

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

| Shape | Cells | Expected box (pt) |
|---|---|---|
| s10 group | PinX 4, PinY 5, 2 x 2, LocPin 1/1 | [216,360,144,144] |
| s11 in s10 | PinX 0.5, PinY 0.5, 1 x 1, LocPin 0.5/0.5, Text "A\n" | [216,432,72,72], parent page0/s10 |
| s12 group in s10 | PinX 1.5, PinY 1.5, 1 x 1, LocPin 0.5/0.5, Angle 0.5235987755987756 | [288,360,72,72] rot 330 |
| s13 in s12 | PinX 0.5, PinY 0.5, 0.5 x 0.5, LocPin 0.25/0.25, Text "B\n" | [306,378,36,36] rot 330, parent page0/s12 |
| s20 group | PinX 6, PinY 5, 1 x 1, LocPin 0.5/0.5, FlipY 1 | [396,396,72,72] flip_v |
| s21 in s20 | PinX 0.5, PinY 0.25, 0.5 x 0.5, LocPin 0.25/0.25 | [414,396,36,36] flip_v |

Paint order: s10, s11, s12, s13, s20, s21.
