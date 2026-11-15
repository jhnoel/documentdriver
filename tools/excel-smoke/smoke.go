package main

import (
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

const usage = `usage: excel-smoke [--timeout 30] [--json] [--workdir dir] [--source in.xlsx] files...

Open each .xlsx or .xlsm in Microsoft Excel and report whether Excel asked to repair
it. With --source, the source opens first and every other file is judged
relative to it: an output passes when it opens clean, or when the source
did not and the output adds no new alert. Without --source each file must
open clean. FORCE-QUITS Excel: run only with the maintainer's agreement,
never while Excel is open (the tool refuses to start then).

Statuses: clean, repair (Excel offered to recover content),
open-error (Excel could not open the file), access-dialog (the file is
outside Excel's granted folder), dialog (an alert blocks Excel, text
unreadable), not-listed, crashed, error.
`

// classify reads Excel's alert texts. The phrases are Excel's repair
// prompt ("We found a problem with some content in ... Do you want us to
// try to recover as much as we can?"), its repair report ("Excel was able
// to open the file by repairing or removing the unreadable content") and
// its open failures ("Excel cannot open the file ... because the file
// format or file extension is not valid", "We can't open ..."). They are
// from knowledge, not yet from a recorded run: the first maintainer run
// (E1 to E8) confirms them, and a missed prompt shows as dialog or
// not-listed, never as clean.
func classify(texts []string) string {
	for _, t := range texts {
		l := strings.ToLower(strings.ReplaceAll(t, "’", "'"))
		switch {
		case strings.Contains(l, "grant file access") || strings.Contains(l, "additional permissions are required"):
			return officeapp.StatusAccessDialog
		case strings.Contains(l, "problem with some content") || strings.Contains(l, "recover as much as we can") ||
			strings.Contains(l, "unreadable content") || strings.Contains(l, "repair"):
			return officeapp.StatusRepair
		case strings.Contains(l, "cannot open the file") || strings.Contains(l, "can't open") ||
			strings.Contains(l, "file format or file extension is not valid") || strings.Contains(l, "cannot open"):
			return officeapp.StatusOpenError
		}
	}
	return ""
}

// smoke is excel-smoke's smoke: its name, noun, workbook extensions,
// classifier, Excel spec and usage.
var smoke = officeapp.Smoke{
	Tool:     "excel-smoke",
	Noun:     "workbooks",
	Exts:     []string{".xlsx", ".xlsm"}, // workbooks, which Excel opens under their own name (a template would open as a new, unsaved workbook and never be listed)
	Classify: classify,
	App:      officeapp.Spec{AppName: "Microsoft Excel", ProcName: "Microsoft Excel", AppPath: "/Applications/Microsoft Excel.app", QuitSettle: time.Second},
	Usage:    usage,
}
