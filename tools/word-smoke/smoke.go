package main

import (
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

const usage = `usage: word-smoke [--timeout 30] [--json] [--workdir dir] [--source in.docx] files...

Open each .docx in Microsoft Word and report whether Word asked to repair
it. With --source, the source opens first and every other file is judged
relative to it: an output passes when it opens clean, or when the source
did not and the output adds no new alert. Without --source each file must
open clean. FORCE-QUITS Word: run only with the maintainer's agreement,
never while Word is open (the tool refuses to start then).

Statuses: clean, repair (Word offered to recover unreadable content),
open-error (Word could not open the file), access-dialog (the file is
outside Word's granted folder), dialog (an alert blocks Word, text
unreadable), not-listed, crashed, error.
`

// classify reads Word's alert texts.
func classify(texts []string) string {
	for _, t := range texts {
		l := strings.ToLower(t)
		switch {
		case strings.Contains(l, "grant file access") || strings.Contains(l, "additional permissions are required"):
			return officeapp.StatusAccessDialog
		case strings.Contains(l, "unreadable content") || strings.Contains(l, "recover the contents") || strings.Contains(l, "repair"):
			return officeapp.StatusRepair
		case strings.Contains(l, "experienced an error") || strings.Contains(l, "can't open") || strings.Contains(l, "cannot open"):
			return officeapp.StatusOpenError
		}
	}
	return ""
}

// smoke is word-smoke's smoke: its name, noun, document extension,
// classifier, Word spec and usage.
var smoke = officeapp.Smoke{
	Tool:     "word-smoke",
	Noun:     "documents",
	Exts:     []string{".docx"},
	Classify: classify,
	App:      officeapp.Spec{AppName: "Microsoft Word", ProcName: "Microsoft Word", AppPath: "/Applications/Microsoft Word.app", QuitSettle: time.Second},
	Usage:    usage,
}
