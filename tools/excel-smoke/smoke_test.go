package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// TestClassify checks Excel's alert phrases classify as before.
func TestClassify(t *testing.T) {
	for text, want := range map[string]string{
		"We found a problem with some content in 'x.xlsx'. Do you want us to try to recover as much as we can?": officeapp.StatusRepair,
		"Excel was able to open the file by repairing or removing the unreadable content.":                      officeapp.StatusRepair,
		"Excel cannot open the file 'x.xlsx' because the file format or file extension is not valid.":           officeapp.StatusOpenError,
		"We can’t open 'x.xlsx'.": officeapp.StatusOpenError,
		"Grant File Access":       officeapp.StatusAccessDialog,
		"Book1":                   "",
	} {
		if got := classify([]string{text}); got != want {
			t.Errorf("classify(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestSmokeConfig checks excel-smoke's smoke wiring: tool, noun,
// extensions and Excel spec.
func TestSmokeConfig(t *testing.T) {
	if smoke.Tool != "excel-smoke" || smoke.Noun != "workbooks" {
		t.Errorf("smoke tool/noun = %q/%q, want excel-smoke/workbooks", smoke.Tool, smoke.Noun)
	}
	if !slices.Equal(smoke.Exts, []string{".xlsx", ".xlsm"}) {
		t.Errorf("smoke extensions = %v, want [.xlsx .xlsm]", smoke.Exts)
	}
	if smoke.App.AppName != "Microsoft Excel" || smoke.App.AppPath != "/Applications/Microsoft Excel.app" || smoke.App.QuitSettle != time.Second {
		t.Errorf("smoke spec = %+v, want Microsoft Excel with a one-second settle", smoke.App)
	}
	if !strings.HasPrefix(smoke.Usage, "usage: excel-smoke ") {
		t.Errorf("smoke usage does not name excel-smoke: %q", smoke.Usage)
	}
}
