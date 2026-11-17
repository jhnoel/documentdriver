# V12a macro-enabled drawing

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

V1 as a .vsdm: the document part's content type
drawing.macroEnabled.main+xml, a vbaProject.bin member (placeholder
bytes, not a real VBA project) related from the document part with the
vbaProject relationship and a Default for .bin, as the corpus's
diagram_with_macro.vsdm. Expected: Kind Drawing, Macro true; read as V1;
the .bin member is never opened.
