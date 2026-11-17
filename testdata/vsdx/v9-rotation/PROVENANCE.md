# V9 rotation and flips

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

| Shape | Cells | Expected |
|---|---|---|
| s1 | PinX 2, PinY 9, 1 x 0.5, LocPin 0/0, Angle 1.570796326794897 | box [90,90,72,36] rot 270 |
| s2 | PinX 4, PinY 9, 1 x 0.5, LocPin 0.5/0.25, FlipX 1 | box [252,126,72,36] flip_h |
| s3 | PinX 6, PinY 9, 1 x 0.5, LocPin 0.5/0.25, Angle -0.5235987755982988 | box [396,126,72,36] rot 30 |
