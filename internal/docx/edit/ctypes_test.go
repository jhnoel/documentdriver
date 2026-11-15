package edit

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// TestWriteTypesHaveProvenance checks that every content and relationship
// type the docx write side creates parts with comes from at least two
// Word-authored files (ctypesgen's rule), with a named source.
func TestWriteTypesHaveProvenance(t *testing.T) {
	want := map[string]bool{
		ctypes.CTWordprocessingmlHeader: true, ctypes.CTWordprocessingmlFooter: true,
		ctypes.RelHeader: true, ctypes.RelFooter: true,
	}
	for _, k := range commentKinds {
		want[k.ct], want[k.rel] = true, true
	}
	for _, e := range append(ctypes.ContentTypes(), ctypes.RelTypes()...) {
		if !want[e.Value] {
			continue
		}
		delete(want, e.Value)
		if e.Word < 2 || e.Source == "" || e.SHA256 == "" {
			t.Errorf("%s: %d Word files, source %q", e.Name, e.Word, e.Source)
		}
	}
	for v := range want {
		t.Errorf("%s is not in the table", v)
	}
}
