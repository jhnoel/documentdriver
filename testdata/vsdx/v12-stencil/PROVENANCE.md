# V12b stencil

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

A stencil (document content type stencil.main+xml, as the corpus's
testVISIO.vssx) with one master, master2 "Box" (shape 5, 1 x 1, Text
"Box\n"), and no pages part. Expected: Kind Stencil; summary pages []
and masters [master2 "Box", 1 object]; read --master master2 shows s5;
apply refuses (stencils are read only).
