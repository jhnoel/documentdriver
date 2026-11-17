# read goldens (designer-authored)

Exact outputs of `read.Page(input, "BASE", address, opts)` (cli.Marshal,
compact, plus a newline), and of text rendering:

- v1-page0.json: V1, default view.
- v1-page0-full.json: V1, --detail full (z, origins, cells).
- v1-page0-in.json: V1, --units in.
- v2-page0.json: V2, default view (masters, a group master's
  sub-shapes, colour indexes 2, 4, 24, 25).
- v4-page0.json: V4, default view (nesting, rotation, flips).
- v5-page0.json: V5, default view (glue, connections, glued control,
  hazards with their fixed detail wording).
- v7-page0.html: V7 s1's html.

Layout rules the goldens fix (R2 implements them):
- Page keys in order: base, page, name, units, size, orientation,
  print_orientation, background, background_page, layers, objects,
  connections, hazards, notes; lists always present.
- Object keys in order: id, kind, type (kind other only), name, name_u
  (only when it differs from name), master (only on the shape that
  stores Master), parent, hidden, box (2-D shapes; 1-D only with full),
  rot (when not 0), flip_h, flip_v, from, to, from_glue, to_glue,
  glued, fill, line, html, text, props, layers, sites; with full: z,
  origins (keys sorted), cells (stored order), omitted. Empty or absent
  values are left out.
- Numbers: points through geom.FormatNum; inches through
  vsd.FormatInches; raw cells as stored.
- line: {w, color, dash}; colours {"hex": "#RRGGBB"}.
- Hazard details (approved wording, compared whole; `<glue>` as
  vsd.Glue writes it, `<d>` through geom.FormatNum):
  - dangling end: "<from_glue|to_glue> names shape <n>, which is not on
    the page";
  - dangling non-end Connect: "<FromCell> names shape <n>, which is not
    on the page" ("Controls.Row_1 names shape 99, …");
  - stale point glue: "<from|to> end is <d> pt from site <glue>"
    ("from end is 80.5 pt from site s1:1");
  - stale whole-shape glue: "<from|to> end is <d> pt outside shape
    <glue>" ("to end is 18 pt outside shape s2").
