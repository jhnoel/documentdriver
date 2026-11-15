package create

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/read"
)

// The round-trip laws of create, per family:
//
//   - read(create(html)) reads back the content html asks for (each
//     family's test names what it must show);
//   - create(read(doc)) round-trips: the markup read shows for a created
//     document, sent to create, makes a document that reads the same,
//     addresses aside (paragraph ids are fresh in a new document).

// markupOf is read's markup of a document.
func markupOf(t *testing.T, doc []byte) string {
	t.Helper()
	r, err := read.Read(doc, "document", "", read.Options{Equation: math.OMMLToLaTeX})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := r.Object()["markup"].(string)
	if !ok {
		t.Fatal("read has no markup")
	}
	return m
}

// paraIDs are the paragraph addresses read writes (an id, and the
// paragraph part of a data-ref), which differ between two documents of
// the same content.
var paraIDs = regexp.MustCompile(` id="[0-9A-F]{8}"|(data-ref=")[0-9A-F]{8}/`)

// sameContent is markup without paragraph addresses.
func sameContent(m string) string { return paraIDs.ReplaceAllString(m, "$1") }

// created creates a document from in, failing the test on a refusal or a
// fault the created-file gate finds.
func created(t *testing.T, in string) []byte {
	t.Helper()
	out, err := Build(in)
	if err != nil {
		t.Fatalf("create %q: %v", short(in), err)
	}
	if err := integrity.GateCreated(out); err != nil {
		t.Fatalf("gate: %v", err)
	}
	return out
}

// roundTrip checks both laws on in and returns read's markup of
// create(in): every want is shown, and the markup sent back to create
// reads the same.
func roundTrip(t *testing.T, in string, want ...string) string {
	t.Helper()
	m := markupOf(t, created(t, in))
	for _, w := range want {
		if !strings.Contains(m, w) {
			t.Errorf("read(create(html)) has no %s:\n%s", w, m)
		}
	}
	again := markupOf(t, created(t, m))
	if sameContent(again) != sameContent(m) {
		t.Errorf("create(read(doc)) reads differently:\nfirst  %s\nsecond %s", m, again)
	}
	return m
}

func short(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
