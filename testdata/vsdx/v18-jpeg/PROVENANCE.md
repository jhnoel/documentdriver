# V18 a Visio-authored JPEG picture with a crop

Derived (2026-11-18) from `studentenassistenten/overzichtfoto's.vsdx`
in Structural-Mechanics-CEG/mechanics-figures-source at
627bbced015c3da624df588bdd630f436d680b00, application "Microsoft
Visio", licensed CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/):
© Structural Mechanics, Civil Engineering and Geosciences, TU Delft.
Changes: only page 1's shape 5 (`Type='Foreign'`, `ForeignData
ForeignType='Bitmap' CompressionType='JPEG' CompressionLevel='0.05'`)
and its media part `visio/media/image3.jpeg` are kept, byte for byte,
with its page relationship rId3; the page start, pages.xml, document
and package parts are this repository's V15 fixture with the JPEG
content-type Default `<Default Extension="jpeg" ContentType="image/jpeg"/>`
as the source stores it. The source's other shapes (BMP pictures of
25 and 44 MB) are left out.

Expected: page0/s5 kind picture, image `/visio/media/image3.jpeg`,
media image/jpeg 229,833 bytes, sha256 d023efcc…207a, px [1142, 1600];
crop [2.27, 9.6, 4.03, 23.52] from ImgWidth `Width*1.0672273209766`,
ImgOffsetX `ImgWidth*-0.022676637990729`, ImgHeight
`Height*1.4952396791266`, ImgOffsetY `ImgHeight*-0.23519818232179`.
