# Shared fixture members

Members several hand-written vsdx fixtures use unchanged, copied into
each fixture directory by the fixture's own files (vsdxtest.Fixture
reads `common/` first, then the fixture directory, which may replace a
member). Written by hand for documentdriver from the structure of the
Visio-authored corpus files (single-quoted attributes, `xml:space`,
StyleSheet 0 holding line, fill, text and PrintPageOrientation cells
with a Character and a Paragraph row, colour indexes, Colors from IX
24, FaceName by NameU); not Visio-authored.

- `document.xml`: style sheets 0 "No Style" (LineWeight 0.75 pt,
  LineColor 0 black, FillForegnd 1 white, Character row 0 Calibri 12 pt,
  Paragraph row 0 centred, PrintPageOrientation 1), 1 "Text Only" (no
  line, no fill), 3 "Normal" (parents 6), 6 "Theme" (LineColor 24
  #7F7F7F, FillForegnd 25 #5B9BD5, parents 0).
- `app.xml`: Application "documentdriver fixture", so ctypes extraction
  never counts a fixture as Visio-authored.
