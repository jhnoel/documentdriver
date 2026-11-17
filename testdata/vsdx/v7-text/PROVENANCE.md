# V7 text

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

page0 s1 (PinX 3, PinY 8, 3 x 2, styles 3 -> 6 -> 0: StyleSheet 0 gives
Calibri 12 pt, Style 0, HorzAlign 1). Its own rows:
Character 0 Size 0.1666666666666667 (= the inherited 12 pt), 1 Style 1
and Color #FF0000, 2 Font Arial and Size 0.1388888888888889 (10 pt);
Paragraph 0 HorzAlign 0, 1 IndFirst -0.25, IndLeft 0.25, Bullet 1,
BulletStr U+2022; Field 0 PAGENUMBER() cached "3".
Text (escapes as in Go): `<pp IX='0'/><cp IX='0'/>Title<cp IX='1'/> red\n<pp IX='1'/><cp IX='2'/>item\u2028two\tend <fld IX='0'>3</fld>\n`.

Expected (decision f: own cells only; a cell equal to what it inherits
shows nothing):
- Body: [{PP 0, [{CP 0 "Title"}, {CP 1 " red"}]}, {PP 1, [{CP 2
  "item"}, {Br}, {CP 2 "two\tend "}, {CP 2, Field 0, "3"}]}],
  Terminated true.
- Formats: Char {0: {}, 1: {Bold "on", Color "#FF0000"}, 2: {Size
  "10", Font "Arial"}}; Para {0: {Align "left"}, 1: {Bullet true,
  BulletChar "\u2022", MarginLeft "18pt", Indent "-18pt"}}.
- text: "Title red\nitem\ntwo\tend 3".
- html: see ../golden/v7-page0.html (one line, the tab written as a
  tab character).
