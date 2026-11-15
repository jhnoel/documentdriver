package testdeck

// quirks replaces members of the minimal deck in the quirky fixture. Each
// stays a plausible part of its kind while exercising the tokenizer.
var quirks = map[string]string{
	// BOM, CRLF line ends, single-quoted declaration, comments, a PI,
	// mc:AlternateContent, references, unusual whitespace inside tags,
	// pair-form empty elements and a very long attribute value.
	"ppt/slides/slide1.xml": "\xEF\xBB\xBF<?xml version='1.0' encoding='UTF-8' standalone='yes'?>\r\n" +
		"<!-- quirky fixture -->\r\n" +
		"<p:sld xmlns:a='http://schemas.openxmlformats.org/drawingml/2006/main'\r\n" +
		"       xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\"\r\n" +
		"       xmlns:p='http://schemas.openxmlformats.org/presentationml/2006/main'\r\n" +
		"       xmlns:mc=\"http://schemas.openxmlformats.org/markup-compatibility/2006\"\r\n" +
		"       xmlns:a14=\"http://schemas.microsoft.com/office/drawing/2010/main\"\r\n" +
		"       mc:Ignorable=\"a14\" >\r\n" +
		"  <p:cSld>\r\n" +
		"    <p:spTree>\r\n" +
		"      <p:nvGrpSpPr><p:cNvPr id='1' name=''/><p:cNvGrpSpPr></p:cNvGrpSpPr><p:nvPr /></p:nvGrpSpPr>\r\n" +
		"      <p:grpSpPr><a:xfrm><a:off x=\"0\" y=\"0\"/><a:ext cx=\"0\" cy=\"0\"/><a:chOff x=\"0\" y=\"0\"/><a:chExt cx=\"0\" cy=\"0\"/></a:xfrm></p:grpSpPr>\r\n" +
		"      <?pptxdriver note?>\r\n" +
		"      <p:sp>\r\n" +
		"        <p:nvSpPr><p:cNvPr id=\"2\"\tname = 'A &amp; B &#x2192; &#8594; &lt;C&gt;' descr=\"line&#10;two\"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>\r\n" +
		"        <p:spPr><a:xfrm rot=\"5400000\" flipH='1'><a:off x='914400'   y=\"1828800\"/><a:ext\r\n" +
		"          cx=\"2286000\"\r\n" +
		"          cy='1143000'\r\n" +
		"        /></a:xfrm><a:prstGeom prst=\"rect\"><a:avLst></a:avLst></a:prstGeom></p:spPr>\r\n" +
		"        <p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang=\"de-DE\"/><a:t xml:space=\"preserve\">  Grüße &amp; ünïcödé — 日本語 \U0001F600  </a:t></a:r></a:p></p:txBody>\r\n" +
		"      </p:sp>\r\n" +
		"      <mc:AlternateContent>\r\n" +
		"        <mc:Choice Requires=\"a14\">\r\n" +
		"          <p:sp><p:nvSpPr><p:cNvPr id=\"3\" name=\"Choice\"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:prstGeom prst=\"ellipse\"><a:avLst/></a:prstGeom></p:spPr></p:sp>\r\n" +
		"        </mc:Choice>\r\n" +
		"        <mc:Fallback>\r\n" +
		"          <p:sp><p:nvSpPr><p:cNvPr id=\"3\" name=\"Fallback\"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:prstGeom prst=\"rect\"><a:avLst/></a:prstGeom></p:spPr></p:sp>\r\n" +
		"        </mc:Fallback>\r\n" +
		"      </mc:AlternateContent>\r\n" +
		"      <p:pic><p:nvPicPr><p:cNvPr id=\"5\" name=\"Logo\"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed=\"rId2\"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr/></p:pic>\r\n" +
		"    </p:spTree>\r\n" +
		"  </p:cSld>\r\n" +
		"  <p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr>\r\n" +
		"</p:sld >\r\n",

	// Default namespace redeclared on a child, prefix redeclared to another
	// URI, and an attribute far longer than any buffer.
	"ppt/slideLayouts/slideLayout1.xml": `<?xml version="1.0" encoding="UTF-8" standalone="no"?>` + "\n" +
		`<sldLayout xmlns="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" type="blank">` +
		`<cSld name="` + longName + `"><spTree><nvGrpSpPr><cNvPr id="1" name=""/><cNvGrpSpPr/><nvPr/></nvGrpSpPr>` +
		`<grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></grpSpPr>` +
		`<extLst xmlns:a="urn:not-drawingml"><a:ext uri="{00000000-0000-0000-0000-000000000000}"><x xmlns="urn:other-default"><y/></x></a:ext></extLst>` +
		`</spTree></cSld><clrMapOvr><a:masterClrMapping/></clrMapOvr></sldLayout>`,
}

var longName = func() string {
	b := make([]byte, 0, 1<<17)
	for len(b) < 1<<17 {
		b = append(b, "Layout &amp; name "...)
	}
	return string(b)
}()

// customXML is a custom XML part with CDATA, a DOCTYPE-free prolog and
// trailing misc.
const customXML = "<?xml version=\"1.0\"?>\n" +
	"<data xmlns=\"urn:fixture\"><![CDATA[<raw> & ]] stuff]]><v n='1'>x</v></data>\n" +
	"<!-- trailing -->\n"
