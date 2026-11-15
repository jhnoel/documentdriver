package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

// TestClassify checks Word's alert phrases classify as before.
func TestClassify(t *testing.T) {
	for text, want := range map[string]string{
		"Word found unreadable content in x.docx":            officeapp.StatusRepair,
		"Word experienced an error trying to open the file.": officeapp.StatusOpenError,
		"Grant File Access":                                  officeapp.StatusAccessDialog,
		"Document1":                                          "",
	} {
		if got := classify([]string{text}); got != want {
			t.Errorf("classify(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestSmokeConfig checks word-smoke's smoke wiring: tool, noun,
// extension and Word spec.
func TestSmokeConfig(t *testing.T) {
	if smoke.Tool != "word-smoke" || smoke.Noun != "documents" {
		t.Errorf("smoke tool/noun = %q/%q, want word-smoke/documents", smoke.Tool, smoke.Noun)
	}
	if !slices.Equal(smoke.Exts, []string{".docx"}) {
		t.Errorf("smoke extensions = %v, want [.docx]", smoke.Exts)
	}
	if smoke.App.AppName != "Microsoft Word" || smoke.App.AppPath != "/Applications/Microsoft Word.app" || smoke.App.QuitSettle != time.Second {
		t.Errorf("smoke spec = %+v, want Microsoft Word with a one-second settle", smoke.App)
	}
	if !strings.HasPrefix(smoke.Usage, "usage: word-smoke ") {
		t.Errorf("smoke usage does not name word-smoke: %q", smoke.Usage)
	}
}
