package create

// Scope is what create builds and what it refuses, for `docx help create`
// (the command's help is its introduction followed by Scope). TestScope
// builds every family it lists and checks every refusal path it lists.
const Scope = `Builds: paragraphs, headings (h1 to h6, or <p class> naming a style of
the new document: Normal, Title, Quote, Heading1 to Heading6), paragraph
CSS (text-align, margins, text-indent, line-height), inline content
(formatted runs, links, line breaks, tabs, equations, page-number and
page-count fields), lists (<ol start type>, <ul>, nested), sections
(<section/>, <section first-page/>) with their headers and footers
(<header>, <header first>, <header even>, likewise <footer>), and
tables (<table> <tr> <td> <th>, colspan, rowspan, nested tables, and
the style read writes on a table, row or cell: width, table-layout,
margin-left/right as alignment, background-color, vertical-align,
height, break-inside, border-spacing, padding-*, border-*), and images
(<img src="data:image/png|jpeg|gif;base64,..." width height alt>, in
CSS pixels; with neither size the image's own, with one its aspect). A
top-level <equation display="block"> is a paragraph of its own.

read's markup of a whole document is accepted unchanged: its <section>
wrappers and derived attributes (ids, markers, data-section, the page
attributes, data-ref on an equation or a page field) are ignored. An
image read shows is an asset address (src="assets/i1.png"): put the
asset's data_url from docx read --kind assets in src.

Refused, with nothing written (invalid_operation; path names what is
refused):
  html<img>                an image whose src is not a data: URL of a
                           PNG, JPEG or GIF (read's asset address
                           among them), or whose bytes are not that image
  html<footnote>, html<endnote>
                           notes
  html<comment-start>      comment markers
  html@class               a class that names no style of the new document
  html@style               a table, row or cell style read does not write
  html                     a placeholder read wrote (data-ref) for
                           content HTML cannot express; and the rest:
                           revision markup (<ins>, <del>,
                           data-break-ins, data-break-del, <format pending>),
                           list markers (data-num), a class on <h1>..<h6>,
                           a list item of several paragraphs, text or an
                           inline equation outside a paragraph, a field
                           other than page-number and page-count, a link
                           in a link, table structure out of place or
                           wider than 63 grid columns, HTML that does not
                           parse (strict HTML5: docx help html), and no
                           paragraph at all.`
