# V13 scaled drawing

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

page0 "Plan": PageScale 1 (U IN), DrawingScale 120 (U FT: 1 in = 10 ft),
DrawingScaleType 3. s1 PinX 2, PinY 9, 1 x 0.5.
Expected: a page note "drawing scale 1 in = 10 ft: lengths are stored
drawing units" (VE11 open); s1's box as stored, unscaled
[108,126,72,36]; P2 geometry writes on this page are refused.
