# V8 pages, background, layers

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

pages.xml order: page0 "Page-1" (BackPage 3; Layer rows IX 0 "Flowchart"
Visible 1 Print 1, IX 1 "Connector" Visible 0 Print 1), page1 "Page-2"
(empty), page3 "Background-1" (Background 1; one shape s1 "Footer").
page0 s1 (PinX 2, PinY 9, 1 x 0.5) has LayerMember "0;1".

Expected: summary pages page0, page1, page3 in that order; page0
background_page "page3"; page3 background true; page0 layers
[page0/layer:Flowchart index 0 visible print, page0/layer:Connector
index 1 not visible, print]; page0/s1 layers [page0/layer:Flowchart,
page0/layer:Connector]; objects counts 1, 0, 1.
