package create

import (
	"fmt"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Template returns the blank drawing's bytes, authored in code like
// xlsx's template, member for member equal to the golden members in
// testdata/vsdx/create/blank (designer-authored), with the content and
// relationship types from ctypes (S0) and the core properties' times
// from now (created and modified). Members are deflated with the fixed
// zip time, in a fixed order, so equal inputs give equal bytes.
// Chunk CT1.
func Template(now time.Time) ([]byte, error) {
	stamp := now.UTC().Format(time.RFC3339)
	page, err := emit.PageXML(emit.Page{ID: 0, Name: "Page-1", Width: 8.5, Height: 11, Orientation: 0, RelID: "rId1"})
	if err != nil {
		return nil, fmt.Errorf("create: blank page: %w", err)
	}
	return opc.BuildZip(
		opc.ZipMember{Name: "[Content_Types].xml", Body: contentTypesXML},
		opc.ZipMember{Name: "_rels/.rels", Body: packageRelsXML},
		opc.ZipMember{Name: "docProps/app.xml", Body: appXML},
		opc.ZipMember{Name: "docProps/core.xml", Body: coreXML(stamp)},
		opc.ZipMember{Name: "visio/_rels/document.xml.rels", Body: documentRelsXML},
		opc.ZipMember{Name: "visio/document.xml", Body: documentXML},
		opc.ZipMember{Name: "visio/pages/_rels/pages.xml.rels", Body: pagesRelsXML},
		opc.ZipMember{Name: "visio/pages/page1.xml", Body: string(emit.PageContentsXML())},
		opc.ZipMember{Name: "visio/pages/pages.xml", Body: pagesXML(page)},
		opc.ZipMember{Name: "visio/windows.xml", Body: windowsXML},
	)
}

// coreXML returns the core properties member with created and
// modified stamped from stamp (already formatted UTC RFC3339).
func coreXML(stamp string) string {
	return opcDecl + "<cp:coreProperties xmlns:cp=\"http://schemas.openxmlformats.org/package/2006/metadata/core-properties\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\" xmlns:dcterms=\"http://purl.org/dc/terms/\" xmlns:dcmitype=\"http://purl.org/dc/dcmitype/\" xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">" +
		"<dcterms:created xsi:type=\"dcterms:W3CDTF\">" + stamp + "</dcterms:created>" +
		"<dcterms:modified xsi:type=\"dcterms:W3CDTF\">" + stamp + "</dcterms:modified></cp:coreProperties>\n"
}

// pagesXML returns the pages member: the Pages wrapper around the
// blank Page element from emit.
func pagesXML(page []byte) string {
	return visioDecl + "<" + vsd.ElemPages + " xmlns='" + vsd.NSMain2012 + "' xmlns:r='" + vsd.NSRel + "' xml:space='preserve'>" + string(page) + "</" + vsd.ElemPages + ">\n"
}

const opcDecl = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n"

const visioDecl = "<?xml version='1.0' encoding='utf-8' ?>\n"

const contentTypesXML = opcDecl + "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
	"<Default Extension=\"rels\" ContentType=\"" + ctypes.CTPackageRelationships + "\"/>" +
	"<Default Extension=\"xml\" ContentType=\"" + ctypes.CTApplicationXml + "\"/>" +
	"<Override PartName=\"/visio/document.xml\" ContentType=\"" + ctypes.CTMsVisioDrawingMain + "\"/>" +
	"<Override PartName=\"/visio/pages/pages.xml\" ContentType=\"" + ctypes.CTMsVisioPages + "\"/>" +
	"<Override PartName=\"/visio/pages/page1.xml\" ContentType=\"" + ctypes.CTMsVisioPage + "\"/>" +
	"<Override PartName=\"/visio/windows.xml\" ContentType=\"" + ctypes.CTMsVisioWindows + "\"/>" +
	"<Override PartName=\"/docProps/core.xml\" ContentType=\"" + ctypes.CTPackageCoreProperties + "\"/>" +
	"<Override PartName=\"/docProps/app.xml\" ContentType=\"" + ctypes.CTExtendedProperties + "\"/></Types>\n"

const packageRelsXML = opcDecl + "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
	"<Relationship Id=\"rId1\" Type=\"" + ctypes.RelDocument + "\" Target=\"visio/document.xml\"/>" +
	"<Relationship Id=\"rId2\" Type=\"" + ctypes.RelMetadataCoreProperties + "\" Target=\"docProps/core.xml\"/>" +
	"<Relationship Id=\"rId3\" Type=\"" + ctypes.RelExtendedProperties + "\" Target=\"docProps/app.xml\"/></Relationships>\n"

const appXML = opcDecl + "<Properties xmlns=\"http://schemas.openxmlformats.org/officeDocument/2006/extended-properties\" xmlns:vt=\"http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes\"><Application>documentdriver</Application></Properties>\n"

const documentRelsXML = opcDecl + "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
	"<Relationship Id=\"rId1\" Type=\"" + ctypes.RelPages + "\" Target=\"pages/pages.xml\"/>" +
	"<Relationship Id=\"rId2\" Type=\"" + ctypes.RelWindows + "\" Target=\"windows.xml\"/></Relationships>\n"

const pagesRelsXML = opcDecl + "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
	"<Relationship Id=\"rId1\" Type=\"" + ctypes.RelPage + "\" Target=\"page1.xml\"/></Relationships>\n"

const documentXML = visioDecl + "<" + vsd.ElemVisioDocument + " xmlns='" + vsd.NSMain2012 + "' xmlns:r='" + vsd.NSRel + "' xml:space='preserve'>" +
	"<DocumentSettings TopPage='0' DefaultTextStyle='3' DefaultLineStyle='3' DefaultFillStyle='3' DefaultGuideStyle='4'><GlueSettings>9</GlueSettings><SnapSettings>65847</SnapSettings><SnapExtensions>34</SnapExtensions><SnapAngles/><DynamicGridEnabled>1</DynamicGridEnabled><ProtectStyles>0</ProtectStyles><ProtectShapes>0</ProtectShapes><ProtectMasters>0</ProtectMasters><ProtectBkgnds>0</ProtectBkgnds></DocumentSettings>" +
	"<FaceNames><FaceName NameU='Calibri'/></FaceNames>" +
	"<StyleSheets><StyleSheet ID='0' NameU='No Style' IsCustomNameU='1' Name='No Style' IsCustomName='1'><Cell N='EnableLineProps' V='1'/><Cell N='EnableFillProps' V='1'/><Cell N='EnableTextProps' V='1'/><Cell N='LineWeight' V='0.01041666666666667'/><Cell N='LineColor' V='0'/><Cell N='LinePattern' V='1'/><Cell N='Rounding' V='0'/><Cell N='FillForegnd' V='1'/><Cell N='FillBkgnd' V='0'/><Cell N='FillPattern' V='1'/><Cell N='LeftMargin' V='0'/><Cell N='RightMargin' V='0'/><Cell N='TopMargin' V='0'/><Cell N='BottomMargin' V='0'/><Cell N='VerticalAlign' V='1'/><Cell N='LangID' V='en-US'/><Cell N='PrintPageOrientation' V='1'/><Section N='Character'><Row IX='0'><Cell N='Font' V='Calibri'/><Cell N='Color' V='0'/><Cell N='Style' V='0'/><Cell N='Case' V='0'/><Cell N='Pos' V='0'/><Cell N='FontScale' V='1'/><Cell N='Size' V='0.1666666666666667'/><Cell N='Strikethru' V='0'/></Row></Section><Section N='Paragraph'><Row IX='0'><Cell N='IndFirst' V='0'/><Cell N='IndLeft' V='0'/><Cell N='IndRight' V='0'/><Cell N='SpLine' V='-1.2'/><Cell N='SpBefore' V='0'/><Cell N='SpAfter' V='0'/><Cell N='HorzAlign' V='1'/><Cell N='Bullet' V='0'/><Cell N='BulletStr' V=''/></Row></Section></StyleSheet>" +
	"<StyleSheet ID='1' NameU='Text Only' IsCustomNameU='1' Name='Text Only' IsCustomName='1' LineStyle='3' FillStyle='3' TextStyle='3'><Cell N='LinePattern' V='0'/><Cell N='FillPattern' V='0'/><Cell N='LeftMargin' V='0'/><Cell N='RightMargin' V='0'/><Cell N='TopMargin' V='0'/><Cell N='BottomMargin' V='0'/></StyleSheet>" +
	"<StyleSheet ID='2' NameU='None' IsCustomNameU='1' Name='None' IsCustomName='1' LineStyle='3' FillStyle='3' TextStyle='3'><Cell N='LinePattern' V='0'/><Cell N='FillPattern' V='0'/></StyleSheet>" +
	"<StyleSheet ID='3' NameU='Normal' IsCustomNameU='1' Name='Normal' IsCustomName='1' LineStyle='6' FillStyle='6' TextStyle='6'><Cell N='LeftMargin' V='0.05555555555555555' U='PT'/><Cell N='RightMargin' V='0.05555555555555555' U='PT'/><Cell N='TopMargin' V='0.05555555555555555' U='PT'/><Cell N='BottomMargin' V='0.05555555555555555' U='PT'/></StyleSheet>" +
	"<StyleSheet ID='4' NameU='Guide' IsCustomNameU='1' Name='Guide' IsCustomName='1' LineStyle='3' FillStyle='3' TextStyle='3'><Cell N='LineWeight' V='0' U='PT'/><Cell N='LineColor' V='#7f7f7f'/><Cell N='LinePattern' V='23'/><Cell N='FillPattern' V='0'/></StyleSheet>" +
	"<StyleSheet ID='6' NameU='Theme' IsCustomNameU='1' Name='Theme' IsCustomName='1' LineStyle='0' FillStyle='0' TextStyle='0'/></StyleSheets>" +
	"<DocumentSheet NameU='TheDoc' IsCustomNameU='1' Name='TheDoc' IsCustomName='1' LineStyle='0' FillStyle='0' TextStyle='0'><Cell N='OutputFormat' V='0'/><Cell N='LockPreview' V='0'/><Cell N='DocLangID' V='en-US'/></DocumentSheet></" + vsd.ElemVisioDocument + ">\n"

const windowsXML = visioDecl + "<Windows ClientWidth='1280' ClientHeight='800' xmlns='" + vsd.NSMain2012 + "' xmlns:r='" + vsd.NSRel + "' xml:space='preserve'>" +
	"<Window ID='0' WindowType='Drawing' WindowState='1073741824' WindowLeft='0' WindowTop='0' WindowWidth='1280' WindowHeight='800' ContainerType='Page' Page='0' ViewScale='-1' ViewCenterX='4.25' ViewCenterY='5.5'><ShowRulers>1</ShowRulers><ShowGrid>1</ShowGrid><ShowPageBreaks>0</ShowPageBreaks><ShowGuides>1</ShowGuides><ShowConnectionPoints>1</ShowConnectionPoints><GlueSettings>9</GlueSettings><SnapSettings>65847</SnapSettings><SnapExtensions>34</SnapExtensions><SnapAngles/><DynamicGridEnabled>1</DynamicGridEnabled><TabSplitterPos>0.5</TabSplitterPos></Window></Windows>\n"
