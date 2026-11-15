package main

// valuesHelp is `xlsx help values`: the cell value forms of the draft
// contract's §7a, as read shows them and plans will take them.
func valuesHelp() string {
	return `Cell values are JSON, the same in read output and in plans:

  number      1200, 0.5, -3e-7: the shortest form that reads back to the
              same double. NaN and infinities do not occur.
  text        "Item". Text that starts with = is {"text":"=literal"}.
  boolean     true, false
  error       {"error":"#N/A"}, one of Excel's error values
  date        {"date":"2024-03-01"}, {"date":"2024-03-01T13:30:00"}
  time        {"time":"13:30:00"}

Dates and times come from the cell's number format and the workbook's
date system (1900 or 1904). A duration format such as [h]:mm, a serial
below 61 in the 1900 system (before 1900-03-01, where Excel's calendar
is wrong) and a serial no date can show stay plain numbers, with a note.

In read's cells view a formula cell is {"f":"=B2+C2","v":2400}; v is the
value the file stored and is left out when it stored none. The values
view shows v alone, the formulas view f alone ("=B2+C2"). Formulas are
shown as Excel shows them; a cell that shares a formula with others
shows its own formula written out.

A dynamic-array formula fills (spills into) the cells of its range: its
own cell shows the formula, each other cell {"v":3,"spill":"Budget!A1"},
naming the formula's cell. A legacy array formula (Ctrl+Shift+Enter) is
shown the same way with "array":"cse" on its own cell and on the others;
a data table with "array":"data_table". Rich text is shown as its plain
text.`
}
