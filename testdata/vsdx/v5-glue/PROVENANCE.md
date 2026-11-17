# V5 glue

Written by hand for documentdriver (2026-11-17) after the Connect
forms of the Visio-authored corpus (FromPart 9 and 12 for the ends,
100 + n - 1 for Connections.Xn and Controls.Row_n); not Visio-authored.
Members: these plus `../common/document.xml` and `../common/app.xml`.
Page height 11 in. All lengths in inches.

| Shape | Cells | Connects |
|---|---|---|
| s1 box | PinX 2, PinY 9, 1 x 0.5, LocPin 0.5/0.25; Connection IX 0 (1, 0.25) = Connections.X1, IX 1 (0, 0.25) = X2 | target of s3 begin (PinX) and s5 begin (Connections.X1) |
| s2 box | PinX 5, PinY 9, 1 x 0.5, LocPin 0.5/0.25; Connection IX 0 (0, 0.25) = X1, IX 1 (1, 0.25) = X2 | target of s3 end (Connections.X1) and s6's control (PinX) |
| s3 connector | BeginX 2.5, BeginY 9, EndX 4.5, EndY 9; PinX 3.5, PinY 9, Width 2, Height 0, LocPinX 1, LocPinY 0, Angle 0 | begin -> s1 PinX; end -> s2 Connections.X1 |
| s4 connector | BeginX 5.5, BeginY 9, EndX 7, EndY 9; PinX 6.25, PinY 9, Width 1.5, LocPinX 0.75, Angle 0 | end -> s9 (no such shape) |
| s5 connector | BeginX 3, BeginY 8, EndX 3, EndY 7; PinX 3, PinY 7.5, Width 1, LocPinX 0.5, Angle -1.570796326794897 | begin -> s1 Connections.X1, which is at (2.5, 9): 1.118 in = 80.50 pt away |
| s6 shape | PinX 5, PinY 7, 1 x 0.5; Control row Row_1 | Controls.Row_1 -> s2 PinX |

Expected: s3 from [180,144] to [324,144], from_glue "s1", to_glue
"s2:1", no hazard (s3's begin (2.5, 9) is on s1's right edge, its end on
s2's site X1 (4.5, 9)); s4 to_glue "#9" and dangling-glue; s5 from
[216,216] to [216,288], from_glue "s1:1", stale-connector (80.5 pt);
s6 glued [{"cell":"Controls.Row_1","to":"s2"}], no connector entry.
