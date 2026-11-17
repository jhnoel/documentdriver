# V2 masters

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files (python-vsdx
`test_master_multiple_child_shapes.vsdx`: a group instance whose
children carry only MasterShape); not Visio-authored. Members: these
plus `../common/document.xml` and `../common/app.xml`.

- master2 "Process" (shape 5): 1 x 0.75 in, LocPin formulas, FillForegnd
  #FFFFFF, Character row 0 Style 2 (italic), Text "Process\n", shape
  data Cost (Type 7, Value 0, U CY), one Connection row, geometry with
  Width*/Height* formulas.
- master3 "Pair" (group shape 5, 2 x 1 in) with children 6 "Left" (Text
  "Left\n") and 7 "Right" (FillForegnd 4, built-in blue), child cells as
  formulas on Sheet.5.
- page0: s1 instance of master2 with PinX 2, PinY 9 only; s2 instance of
  master2 with PinX 4, PinY 9, Width 1 F="Inh", FillForegnd 2 (built-in
  red), Cost 12.5, own Text "Review\n" and no Character rows; s3
  instance of master3 (PinX 6, PinY 6 only) holding s4 (MasterShape 6,
  no cells) and s5 (MasterShape 7, own Text "Right\n").

Expected: see docs/vsdx-design.md fixture table and the H2, M2a, M2b,
M2c and M5 rows of the chunk plan.
