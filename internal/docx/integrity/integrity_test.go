package integrity

import (
	"archive/zip"
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
)

// The tests in this file are ported from docxdriver 0.5.2's
// crates/docxdriver-core/tests/integrity.rs. Tests there that commit an
// edit through the plan engine check the engine as much as the gate; until
// the docx operations exist (P5) they are kept below as skipped tests that
// name what to assert, with every integrity assertion on their inputs
// already made here.

const (
	testW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	testW14 = "http://schemas.microsoft.com/office/word/2010/wordml"
)

type part struct{ name, content string }

func zipParts(t testing.TB, parts []part) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, p.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func packageParts(w, body, extraAttrs string) []part {
	return []part{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="` + w + `" xmlns:w14="` + testW14 + `"` +
			extraAttrs + `><w:body>` + body + `</w:body></w:document>`},
	}
}

// testPackage is the Rust tests' package(): a minimal .docx around body.
func testPackage(t testing.TB, body, extraAttrs string) []byte {
	return zipParts(t, packageParts(testW, body, extraAttrs))
}

func messages(data []byte) []string { return Check(data).Messages() }

const paragraph = `<w:p w14:paraId="11111111"><w:r><w:t>Rent is due in thirty days.</w:t></w:r></w:p>`

func TestCleanPackageHasNoFindings(t *testing.T) {
	if got := messages(testPackage(t, paragraph, "")); len(got) != 0 {
		t.Fatalf("findings on a clean package: %q", got)
	}
}

func TestGateRefusesAnIntroducedFaultAndNamesIt(t *testing.T) {
	input := testPackage(t, paragraph, "")
	broken := testPackage(t, `<w:p w14:paraId="11111111"><w:r><w:fldChar w:fldCharType="begin"/><w:t>x</w:t></w:r></w:p>`, "")
	err := Gate(input, broken)
	var failure *GateFailure
	if !asGateFailure(err, &failure) {
		t.Fatalf("Gate = %v, want a *GateFailure", err)
	}
	want := []Finding{{"word/document.xml", "integrity: fldChar begin without an end"}}
	if !slices.Equal(failure.Introduced, want) {
		t.Fatalf("introduced %q, want %q", failure.Introduced, want)
	}
	if !strings.Contains(failure.Summary(), "nothing was written") {
		t.Fatalf("summary %q", failure.Summary())
	}
	if err := Gate(input, input); err != nil {
		t.Fatalf("Gate(input, input) = %v", err)
	}
}

func asGateFailure(err error, out **GateFailure) bool {
	f, ok := err.(*GateFailure) //nolint:errorlint // Gate returns the concrete type unwrapped
	*out = f
	return ok
}

// TestFaultsAlreadyInTheSource is the integrity half of docxdriver's
// faults_already_in_the_source_do_not_block_edits: the faults are found,
// and the gate lets an output that keeps them through.
func TestFaultsAlreadyInTheSource(t *testing.T) {
	// Unbalanced bookmark, an unknown w: element, and a field code without
	// significant-space preservation: all pre-existing, all left alone.
	body := `<w:p w14:paraId="11111111"><w:bookmarkStart w:id="1" w:name="open"/><w:pPr><w:bogus/></w:pPr>` +
		`<w:r><w:t>Rent is due in thirty days.</w:t></w:r></w:p>` +
		`<w:p w14:paraId="22222222"><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> PAGE </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`
	input := testPackage(t, body, "")
	before := messages(input)
	for _, want := range []string{"bookmarkStart without a bookmarkEnd", "w:bogus is not allowed"} {
		if !slices.ContainsFunc(before, func(m string) bool { return strings.Contains(m, want) }) {
			t.Errorf("%q not among %q", want, before)
		}
	}
	edited := testPackage(t, strings.Replace(body, "thirty", "sixty", 1), "")
	if err := Gate(input, edited); err != nil {
		t.Fatalf("pre-existing faults blocked the edit: %v", err)
	}
	if got := messages(edited); !slices.Equal(got, before) {
		t.Fatalf("after %q, before %q", got, before)
	}
}

func TestPackageAndXMLFaultsAreReported(t *testing.T) {
	parts := packageParts(testW, paragraph, "")
	for i := range parts {
		if parts[i].name == "word/document.xml" {
			parts[i].content = strings.Replace(parts[i].content, `<?xml version="1.0" encoding="UTF-8"?>`,
				`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE w:document>`, 1)
		}
	}
	parts = append(parts, part{"word/media/orphan.bin", "x"})
	found := messages(zipParts(t, parts))
	for _, want := range []string{"xml: contains a DTD", "integrity: part has no content type"} {
		if !slices.Contains(found, want) {
			t.Errorf("%q not among %q", want, found)
		}
	}
	if got := messages([]byte("not a zip")); !slices.Equal(got, []string{"package: not a readable zip archive"}) {
		t.Errorf("not a zip: %q", got)
	}
}

func TestStoryRules(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`<w:p><w:r><w:t> lead</w:t></w:r></w:p>`, `integrity: run text has leading or trailing space without xml:space="preserve"`},
		{`<w:p><w:r><w:delText>x</w:delText></w:r></w:p>`, "integrity: w:delText outside a deletion"},
		{`<w:p><w:r><w:instrText>PAGE</w:instrText></w:r></w:p>`, "integrity: instrText outside a field code"},
		{`<w:tbl><w:tblPr/><w:tblGrid/></w:tbl>`, "integrity: table has no rows"},
		{`<w:p><w:bookmarkStart w:id="1" w:name="a"/><w:bookmarkEnd w:id="1"/><w:bookmarkStart w:id="2" w:name="a"/><w:bookmarkEnd w:id="2"/></w:p>`, "integrity: duplicate bookmark name"},
		{`<w:p w14:paraId="90000000"/>`, "integrity: w14:paraId is not eight hex digits below 80000000"},
		{`<w:p><w:pPr><w:jc w:val="justify"/></w:pPr></w:p>`, "schema: w:jc/@w:val is not a valid ST_Jc"},
		{`<w:sectPr/><w:p/>`, "schema: w:p is out of order in w:body"},
	} {
		if found := messages(testPackage(t, c.body, "")); !slices.Contains(found, c.want) {
			t.Errorf("%s: %q not among %q", c.body, c.want, found)
		}
	}
}

func TestSingleInstanceRelationshipsCountAcrossStrictAndTransitional(t *testing.T) {
	parts := append(packageParts(testW, paragraph, ""),
		part{"word/settings.xml", `<?xml version="1.0" encoding="UTF-8"?><w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`},
		part{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://purl.oclc.org/ooxml/officeDocument/relationships/settings" Target="settings.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>` +
			`</Relationships>`})
	found := messages(zipParts(t, parts))
	if !slices.ContainsFunc(found, func(m string) bool { return strings.Contains(m, "more than one settings relationship") }) {
		t.Fatalf("%q", found)
	}
}

// TestRunPropertiesAheadOfOfficeTextEffects is the integrity half of
// run_properties_stay_ahead_of_office_text_effects.
func TestRunPropertiesAheadOfOfficeTextEffects(t *testing.T) {
	w14 := ` xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14"`
	body := func(rPr string) string {
		return `<w:p w14:paraId="11111111"><w:r><w:rPr>` + rPr + `</w:rPr><w:t>Glowing words</w:t></w:r></w:p>`
	}
	glow := `<w14:glow w14:rad="63500"><w14:srgbClr w14:val="00B0F0"/></w14:glow>`
	misplaced := messages(testPackage(t, body(glow+`<w:b/>`), w14))
	if !slices.Contains(misplaced, "schema: w:b follows an Office extension element in w:rPr") {
		t.Errorf("misplaced: %q", misplaced)
	}
	// What the engine must write: the run properties first, then the glow.
	if got := messages(testPackage(t, body(`<w:b/><w:color w:val="AABBCC"/>`+glow), w14)); len(got) != 0 {
		t.Errorf("well-placed: %q", got)
	}
}

// TestSettlingTrackedCellsLeavesNoFault is the integrity half of
// settling_tracked_cells: a table whose only row was rejected becomes an
// empty paragraph, which has no fault.
func TestSettlingTrackedCellsLeavesNoFault(t *testing.T) {
	if got := messages(testPackage(t, `<w:p/>`, "")); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

// TestAcceptedDeletionInput is the integrity half of
// accepting_a_deletion_keeps_anchors_and_never_cuts_a_field: the input's
// findings, which the accepted output must keep exactly.
func TestAcceptedDeletionInput(t *testing.T) {
	body := `<w:p w14:paraId="11111111"><w:del w:id="1" w:author="a" w:date="2020-01-01T00:00:00Z">` +
		`<w:r><w:delText xml:space="preserve">Gone </w:delText></w:r><w:bookmarkStart w:id="5" w:name="mark"/>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:delInstrText xml:space="preserve"> PAGE </w:delInstrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:commentReference w:id="0"/></w:r></w:del>` +
		`<w:r><w:t>7</w:t></w:r><w:bookmarkEnd w:id="5"/><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`
	want := []string{"integrity: commentReference names no w:comment"}
	if got := messages(testPackage(t, body, "")); !slices.Equal(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

// Tests of docxdriver's integrity.rs that commit edits through the plan
// engine. Each names the integrity assertion to make on the engine's
// output once the operation exists.
func TestEngineDependentGateTests(t *testing.T) {
	for _, name := range []string{
		"faults_already_in_the_source_do_not_block_edits: ReplaceText output keeps exactly the input's findings",
		"literal_line_breaks_in_attributes_survive_a_rewrite: rewritten w:conformance reads \"a b\"",
		"justify_is_written_as_the_schema_value: FormatParagraph justify writes w:jc both",
		"editing_a_run_with_several_leaves_keeps_their_order: ReplaceText direct and tracked",
		"tracked_deletion_marks_field_codes_deleted_and_rejection_restores_them: DeleteParagraphs, RevisionSettle",
		"tracked_deletion_leaves_text_box_stories_alone: DeleteParagraphs",
		"accepting_a_deletion_keeps_anchors_and_never_cuts_a_field: RevisionSettle accept, messages unchanged",
		"settling_tracked_cells: RevisionSettle reject and accept on cellIns",
		"deleting_an_equation_keeps_comment_anchors: DeleteEquation",
		"strict_documents_are_refused: ReplaceText on Strict is refused",
		"run_properties_stay_ahead_of_office_text_effects: FormatText output has no finding",
	} {
		t.Run(name[:strings.IndexByte(name, ':')], func(t *testing.T) {
			t.Skip("needs the docx plan engine (P5):" + name[strings.IndexByte(name, ':')+1:])
		})
	}
}
