package main

import "testing"

// TestAccVisio checks that a Visio-authored file counts in the Visio
// column, so Word+PowerPoint+Excel+Visio == Files holds for Visio
// evidence (the invariant ctypes_test checks on the table), and that an
// external relationship seen only in Visio files stays out of the Word
// and PowerPoint view.
func TestAccVisio(t *testing.T) {
	var a acc
	a.add("visio", Source{Label: "vsdx-repos", Path: "a.vsdx"}, true)
	a.add("word", Source{Label: "docx-lo", Path: "b.docx"}, false)
	a.add(officeApps["Microsoft Visio"], Source{Label: "vsdx-repos", Path: "c.vsdx"}, false)
	if a.files != 3 || a.visio != 2 || a.word != 1 || a.ppt != 0 || a.excel != 0 {
		t.Errorf("counts files %d word %d ppt %d excel %d visio %d, want 3 1 0 0 2", a.files, a.word, a.ppt, a.excel, a.visio)
	}
	if a.word+a.ppt+a.excel+a.visio != a.files {
		t.Error("family columns do not sum to files")
	}
	if a.external != 1 || a.externalVisio != 1 || a.externalExcel != 0 {
		t.Errorf("external %d visio %d excel %d, want 1 1 0", a.external, a.externalVisio, a.externalExcel)
	}
	if a.src.Path != "a.vsdx" {
		t.Errorf("source %q, want the first file", a.src.Path)
	}
}
