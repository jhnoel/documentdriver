# create: the blank drawing (golden)

Designer-authored (2026-11-17): the exact members `create.Template`
writes for the clock 2000-01-01T00:00:00Z (the default fixed clock of
`--seed`/`--clock`). Not Visio-authored. Every choice is grounded in the
53 Visio-authored corpus files (docProps/app.xml "Microsoft Visio"):

| Member | Corpus | Choice |
|---|---|---|
| [Content_Types].xml, _rels/.rels, visio/document.xml, pages.xml + rels, a page | 53 of 53 | written |
| docProps/app.xml, core.xml | 53 of 53 | written; Application "documentdriver" (never claims to be Visio); core holds only created and modified from the clock |
| visio/windows.xml | 53 of 53 (MS-VSDX does not list it among the document part's relationships) | written, one Drawing window on page 0 |
| docProps/custom.xml | 53 of 53 (Visio build numbers) | not written: nothing to say; a plan that needs RecalcDocument creates it |
| masters | 40 of 53 (13 files have none; 142 master-less 2-D and 12 master-less 1-D shapes in the corpus) | none |
| theme | 29 of 53 | none |
| thumbnail | 49 of 53 | none |
| document.xml children | DocumentSettings, Colors, FaceNames, StyleSheets, DocumentSheet in 47; one file has no Colors | DocumentSettings, FaceNames, StyleSheets, DocumentSheet |
| StyleSheets | 0 No Style, 1 Text Only, 2 None, 3 Normal, 4 Guide in 53 of 53; 6 Theme in 52 | the same IDs and names, with explicit values (no Themed cells: there is no theme part) |
| page size | corpus mostly A4 (43 of 56 pages) | US Letter 8.5 x 11 portrait; PrintPageOrientation inherited from StyleSheet 0 (1), as on 52 of 86 corpus pages |

StyleSheet 0 is `../../common/document.xml`'s. Whether Visio opens this
file without repair is unverified (no Visio); the acceptance checks are
our read round trip and LibreOffice import.
