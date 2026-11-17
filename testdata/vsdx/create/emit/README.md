# emit goldens (designer-authored)

Exact outputs of the emit kernels for the c2 and c3 create plans
(inches, y up, page height 11 in). Element forms follow the corpus:
the rect's RelMoveTo/RelLineTo unit square (460 corpus RelLineTo rows),
the master-less glued connector of apache-poi 60973.vsdx shape 146
(cells, formulas, NoFill 1, MoveTo/LineTo), Connect rows with FromPart
9/12 and ToPart 100+k-1. The connection-point order (X1 top, X2 right,
X3 bottom, X4 left) is ours.

- rect.xml: emit.ShapeXML(Shape{ID 1, Geom "rect", PinX 2, PinY 9.5,
  Width 2, Height 1, LocPinX 1, LocPinY 0.5, Style 3}).
- ellipse.xml: the same with ID 2, Geom "ellipse", PinX 6.
- connector.xml, connects.txt (one Connect per line):
  emit.ConnectorXML(3, End{3, 9.5, glued to 1 point 2}, End{5, 9.5,
  glued to 2 point 4}).
- page-wide.xml: emit.PageXML(Page{ID 1, "Wide", 17, 11, Orientation 2,
  RelID "rId2"}). The PageXML of Page{0, "Page-1", 8.5, 11, 0, "rId1"}
  is the Page element of ../blank/visio/pages/pages.xml.
