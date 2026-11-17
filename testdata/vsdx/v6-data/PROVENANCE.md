# V6 shape data

Written by hand for documentdriver (2026-11-17) after the structure of
the Visio-authored corpus files; not Visio-authored. Members: these plus
`../common/document.xml` (as visio/document.xml unless this directory
has its own) and `../common/app.xml`. Page height 11 in unless said;
lengths in inches, views in points y down.

master2 "Item" (shape 5, 1 x 0.5) defines Property rows, in order:
Cost (Type 7, V 0), Owner (Type 0, V "", U STR), Done (Type 3, V 0,
U BOOL), Due (Type 5, V 2024-03-01T00:00:00, U DATE), Size (Type 2,
V 0.5, U MM), Hidden (Type 0, Invisible 1, V h), Linked (Type 0,
DataLinked 1, V L), Gone (Type 0, V g), MasterOnly (Type 0, V m).
page0 s1 is an instance (PinX 2, PinY 9) whose own rows set Cost 12.5,
Owner "Ada", Done 1, delete Gone (Del 1), and add NewRow (Type 2, V 42).

Type codes: corpus Property rows show 0 string (STR), 1 fixed list and
4 variable list (STR), 2 number, 5 date (U DATE, ISO text), 7
currency; 3 (boolean) and 6 (duration) do not occur (VE9).

Expected props of page0/s1, in order: Cost {currency, 12.5, local};
Owner {string, "Ada", local}; Done {boolean, true, local}; Due {date,
{"date":"2024-03-01T00:00:00"}, master}; Size {number, 0.5, unit MM,
master}; Hidden {string, "h", hidden, master}; Linked {string, "L",
linked, master}; MasterOnly {string, "m", master}; NewRow {number, 42,
local}. Gone is absent.
