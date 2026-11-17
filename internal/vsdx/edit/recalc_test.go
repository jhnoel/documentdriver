package edit

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// customHead is the start of a corpus-like custom properties part.
const customHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
	`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">`

// withCustom builds V1 with a docProps/custom.xml member holding body;
// rel and override say whether the package relationship and the
// content-type Override are written too.
func withCustom(t *testing.T, body string, rel, override bool) string {
	t.Helper()
	raw, err := laws.Parts(vsdxtest.Fixture(t, "v1-minimal"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for n, b := range raw {
		parts[strings.TrimPrefix(n, "/")] = b
	}
	parts["docProps/custom.xml"] = []byte(body)
	if override {
		ct := string(parts["[Content_Types].xml"])
		parts["[Content_Types].xml"] = []byte(strings.Replace(ct, "</Types>",
			`<Override PartName="/docProps/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.custom-properties+xml"/></Types>`, 1))
	}
	if rel {
		r := string(parts["_rels/.rels"])
		parts["_rels/.rels"] = []byte(strings.Replace(r, "</Relationships>",
			`<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/custom-properties" Target="docProps/custom.xml"/></Relationships>`, 1))
	}
	names := make([]string, 0, len(parts))
	for n := range parts {
		if n != "[Content_Types].xml" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	members := []opc.ZipMember{{Name: "[Content_Types].xml", Body: string(parts["[Content_Types].xml"])}}
	for _, n := range names {
		members = append(members, opc.ZipMember{Name: n, Body: string(parts[n])})
	}
	b, err := opc.BuildZip(members...)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "custom.vsdx")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// customBytes returns the working copy's docProps/custom.xml.
func customBytes(t *testing.T, w *Workspace) string {
	t.Helper()
	pt, err := w.Package().Part(customPart)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSetRecalcDocumentNew checks the absent-part case: the part, its
// Override and its package relationship are added, Verify accepts the
// declared write set, and a second call changes nothing.
func TestSetRecalcDocumentNew(t *testing.T) {
	w := mustBegin(t, vsdxtest.Fixture(t, "v1-minimal"))
	changed, err := w.SetRecalcDocument()
	if err != nil || !changed {
		t.Fatalf("SetRecalcDocument() = %v, %v; want true, nil", changed, err)
	}
	want := customHead + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="RecalcDocument"><vt:bool>true</vt:bool></property></Properties>`
	if got := customBytes(t, w); got != want {
		t.Errorf("custom.xml =\n%s\nwant\n%s", got, want)
	}
	if fixed, err := w.ensureCustomLinks(); err != nil || fixed {
		t.Errorf("relationship or Override missing after the add (fixed %v, %v)", fixed, err)
	}
	if err := w.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if changed, err := w.SetRecalcDocument(); err != nil || changed {
		t.Errorf("second SetRecalcDocument() = %v, %v; want false, nil", changed, err)
	}
}

// TestSetRecalcDocumentExisting checks the existing-part cases: a
// property is added with the next pid before </Properties>, a false
// value is turned true in place, a true one changes nothing, and every
// other byte is kept.
func TestSetRecalcDocumentExisting(t *testing.T) {
	other := `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="_VPID_ALTERNATENAMES"><vt:lpwstr></vt:lpwstr></property>` +
		`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="3" name="BuildNumberCreated"><vt:i4>1075593861</vt:i4></property>`
	for _, tc := range []struct {
		name, in, want string
		changed        bool
	}{
		{"absent", customHead + other + `</Properties>`,
			customHead + other + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="4" name="RecalcDocument"><vt:bool>true</vt:bool></property></Properties>`, true},
		{"false", customHead + other + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="7" name="RecalcDocument"><vt:bool>false</vt:bool></property></Properties>`,
			customHead + other + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="7" name="RecalcDocument"><vt:bool>true</vt:bool></property></Properties>`, true},
		{"true", customHead + other + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="7" name="RecalcDocument"><vt:bool>true</vt:bool></property></Properties>`,
			customHead + other + `<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="7" name="RecalcDocument"><vt:bool>true</vt:bool></property></Properties>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := mustBegin(t, withCustom(t, tc.in, true, true))
			changed, err := w.SetRecalcDocument()
			if err != nil || changed != tc.changed {
				t.Fatalf("SetRecalcDocument() = %v, %v; want %v, nil", changed, err, tc.changed)
			}
			if got := customBytes(t, w); got != tc.want {
				t.Errorf("custom.xml =\n%s\nwant\n%s", got, tc.want)
			}
			if err := w.Verify(); err != nil {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

// TestSetRecalcDocumentLinks checks that an existing part without its
// relationship or Override gets them.
func TestSetRecalcDocumentLinks(t *testing.T) {
	in := customHead + `</Properties>`
	w := mustBegin(t, withCustom(t, in, false, false))
	changed, err := w.SetRecalcDocument()
	if err != nil || !changed {
		t.Fatalf("SetRecalcDocument() = %v, %v; want true, nil", changed, err)
	}
	if fixed, err := w.ensureCustomLinks(); err != nil || fixed {
		t.Errorf("links still missing (fixed %v, %v)", fixed, err)
	}
	if err := w.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// TestSetRecalcDocumentUnreadable checks that a malformed custom.xml
// is left as it is and reported, never rebuilt.
func TestSetRecalcDocumentUnreadable(t *testing.T) {
	for _, in := range []string{`<Properties><property name="x">`, `<NotProperties/>`} {
		w := mustBegin(t, withCustom(t, in, true, true))
		changed, err := w.SetRecalcDocument()
		if !errors.Is(err, ErrCustomUnreadable) || changed {
			t.Errorf("%q: SetRecalcDocument() = %v, %v; want false, ErrCustomUnreadable", in, changed, err)
		}
		if got := customBytes(t, w); got != in {
			t.Errorf("%q: part changed to %q", in, got)
		}
	}
}
