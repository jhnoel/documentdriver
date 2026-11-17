# V15 foreign shapes and a guide

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

page0: s1 Type Foreign with ForeignData ForeignType Bitmap,
CompressionType PNG, Rel to visio/media/image1.png (a 1 x 1 PNG written
by the generator) through page1.xml.rels; s2 Type Foreign with
ForeignType Object and no Rel (an OLE object without its embedding); s3
Type Guide (vertical guide at x 1 in, an InfiniteLine row).
Expected: s1 kind picture, s2 kind other, s3 kind other with type
"Guide"; all read only; boxes s1 [108,108,72,72], s2 [252,108,72,72].
