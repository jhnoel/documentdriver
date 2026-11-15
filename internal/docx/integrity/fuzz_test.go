package integrity

import (
	"testing"
)

// FuzzCheck runs Check on arbitrary bytes, seeded with the test packages:
// it must return findings, never panic or hang.
func FuzzCheck(f *testing.F) {
	for _, body := range []string{
		paragraph,
		`<w:tbl><w:tblPr/><w:tblGrid><w:gridCol/></w:tblGrid><w:tr><w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`,
		`<w:p><w:r><w:fldChar w:fldCharType="begin"/><w:instrText> PAGE </w:instrText></w:r></w:p>`,
	} {
		f.Add(testPackage(f, body, ""))
	}
	f.Add([]byte("PK\x05\x06" + string(make([]byte, 18))))
	f.Fuzz(func(_ *testing.T, data []byte) {
		Check(data)
	})
}

// FuzzCheckDocument runs Check on a package whose document part is
// arbitrary, so the fuzzer works on the XML and story checks rather than
// the zip container.
func FuzzCheckDocument(f *testing.F) {
	for _, c := range parseCases {
		f.Add([]byte(c.in))
	}
	f.Add([]byte(`<w:document xmlns:w="` + testW + `" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><w:body>` +
		`<w:p><mc:AlternateContent><mc:Choice Requires="w"/><mc:Fallback><w:r/></mc:Fallback></mc:AlternateContent></w:p>` +
		`<w:sdt><w:sdtContent><w:tbl><w:tr><w:tc/></w:tr></w:tbl></w:sdtContent></w:sdt><w:sectPr/></w:body></w:document>`))
	f.Fuzz(func(t *testing.T, doc []byte) {
		parts := packageParts(testW, "", "")
		parts[2].content = string(doc)
		Check(zipParts(t, parts))
	})
}

// FuzzScanView checks that the view parseXML builds as a part is read is
// the one built from the part's tree, and refuses what the tree path
// refuses with the same message.
func FuzzScanView(f *testing.F) {
	for _, c := range parseCases {
		f.Add([]byte(c.in))
	}
	f.Add([]byte(`<w:document xmlns:w="` + testW + `"><w:body><w:p> a <!--c--> b <w:r><w:t>t</w:t></w:r> c</w:p></w:body></w:document>`))
	f.Fuzz(func(t *testing.T, in []byte) {
		got, msg := parseXML(in)
		want, wantMsg := parseXMLTree(in)
		if msg != wantMsg {
			t.Fatalf("message %q, tree path %q", msg, wantMsg)
		}
		if got != nil && want != nil && dumpView(got) != dumpView(want) {
			t.Fatalf("scanned view\n%s\ntree view\n%s", dumpView(got), dumpView(want))
		}
	})
}
