# V10 defects

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

pages.xml: page0 "Page-1", page2 "Broken" (its part page2.xml is not
well-formed: `</Shape>` missing), page4 "Missing" (Rel r:id rId9, which
pages.xml.rels does not hold), page5 "Twin" and a second page with ID 5
"Twin-2" (addressed page5~2).
page0: two shapes with ID 7 ("First", "Second": page0/s7 and
page0/s7~2), s8 an instance of Master 99 (no masters part), s9 with
FillForegnd V "Themed" F THEMEVAL(), s10 with an unknown Section "Bogus",
and a Connect with FromSheet 0.

Expected: Open succeeds; summary lists page0, page2, page4, page5,
page5~2 with notes at page2 (unreadable) and page4 (no part); read
--page page2 is unreadable_target, every other page reads; page0: s8's
own cells only plus one note "master99: no such master"; s9 fill
omitted, reason "themed value not derived"; s10 read normally (the
unknown section is kept in cells, never shown otherwise); the Connect
with FromSheet 0 is a note, no hazard. Integrity: duplicate-shape-id
(page0 s7), duplicate-page-id (5), missing-part (page4),
unresolved-master (s8), dangling-connect (FromSheet 0).
