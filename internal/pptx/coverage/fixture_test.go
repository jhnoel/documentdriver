package coverage

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The probe deck: the committed minimal deck (testdata/decks/minimal)
// with its slide, layout, master and presentation replaced by the parts
// below. The slide holds one object of every root kind, each the first
// of its element name in the shape tree, so that a path rooted at p:sp
// lands in the plain shape s2: a shape with a transform and a preset but
// no fill, line or style of its own (so that style references and
// inherited values are visible), and text at every level (so that every
// list-style level is resolved). Placeholders s3 (title) and s4 (body)
// inherit from the layout and master, s6 is a glue target, s7 a group
// with child s8, s9 a picture (a PNG with an SVG blip, as PowerPoint 16
// stores an SVG picture), s10 a graphic frame, s11 an ink content part.
var (
	nsDecl = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

	probeSlide = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld ` + nsDecl + `><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Plain"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="914400" y="914400"/><a:ext cx="1828800" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` + levelParas + `</p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>Title</a:t></a:r></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="4" name="Body"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/>` + levelParas + `</p:txBody></p:sp>` +
		`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="5" name="Link"/><p:cNvCxnSpPr><a:stCxn id="2" idx="3"/><a:endCxn id="6" idx="1"/></p:cNvCxnSpPr><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm><a:off x="2743200" y="1371600"/><a:ext cx="914400" cy="457200"/></a:xfrm><a:prstGeom prst="straightConnector1"><a:avLst/></a:prstGeom></p:spPr></p:cxnSp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="6" name="Target"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="3657600" y="914400"/><a:ext cx="1828800" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:sp>` +
		`<p:grpSp><p:nvGrpSpPr><p:cNvPr id="7" name="Group"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="914400" y="3657600"/><a:ext cx="1828800" cy="914400"/><a:chOff x="0" y="0"/><a:chExt cx="1828800" cy="914400"/></a:xfrm></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="8" name="Child"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:grpFill/></p:spPr></p:sp></p:grpSp>` +
		`<p:pic><p:nvPicPr><p:cNvPr id="9" name="Logo"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rId2"><a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}"><asvg:svgBlip xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main" r:embed="rId3"/></a:ext></a:extLst></a:blip><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="457200" y="457200"/><a:ext cx="457200" cy="457200"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>` +
		`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="10" name="Frame"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="6400800" y="3657600"/><a:ext cx="1828800" cy="914400"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"/></a:graphic></p:graphicFrame>` +
		`<p:contentPart r:id="rId2"/>` +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`

	probeLayout = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout ` + nsDecl + ` type="obj" preserve="1"><p:cSld name="Title and Content"><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title 1"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Content 2"/><p:cNvSpPr/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

	probeMaster = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster ` + nsDecl + `><p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Title Placeholder 1"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="365125"/><a:ext cx="10515600" cy="1325563"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr anchor="ctr"/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Text Placeholder 2"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="1825625"/><a:ext cx="10515600" cy="4351338"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
		`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` +
		`<p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="4400"/></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="2800"/></a:lvl1pPr></p:bodyStyle><p:otherStyle><a:lvl1pPr><a:defRPr sz="1800"/></a:lvl1pPr></p:otherStyle></p:txStyles></p:sldMaster>`

	probePres = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation ` + nsDecl + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/><p:defaultTextStyle><a:lvl1pPr><a:defRPr sz="1800"/></a:lvl1pPr><a:lvl2pPr><a:defRPr sz="1600"/></a:lvl2pPr></p:defaultTextStyle></p:presentation>`
)

// levelParas are nine paragraphs, one at each level, each one run.
var levelParas = func() string {
	var b strings.Builder
	for i := range 9 {
		fmt.Fprintf(&b, `<a:p><a:pPr lvl="%d"/><a:r><a:rPr lang="en-US"/><a:t>Level %d</a:t></a:r></a:p>`, i, i+1)
	}
	return b.String()
}()

// Part names of the probe deck.
const (
	partSlide  = "ppt/slides/slide1.xml"
	partLayout = "ppt/slideLayouts/slideLayout1.xml"
	partMaster = "ppt/slideMasters/slideMaster1.xml"
	partPres   = "ppt/presentation.xml"
	partTheme  = "ppt/theme/theme1.xml"
)

// fixture is the probe deck as editable copies of its XML parts.
type fixture map[string]*el

var (
	baseOnce  sync.Once
	baseParts map[string][]byte // the minimal deck's members
	baseFix   fixture
	deckSeq   atomic.Int64
)

func loadBase(_ testing.TB) {
	baseOnce.Do(func() {
		baseParts = map[string][]byte{}
		dir := filepath.Join(testdeck.Root(), "testdata", "decks", "minimal")
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			b, err := os.ReadFile(p)
			baseParts[filepath.ToSlash(rel)] = b
			return err
		})
		if err != nil {
			panic(err)
		}
		baseParts["ppt/media/image1.png"] = pngStub
		// The picture's SVG: its part, relationship and content type.
		baseParts["ppt/media/image2.svg"] = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
		baseParts["ppt/slides/_rels/slide1.xml.rels"] = bytes.Replace(baseParts["ppt/slides/_rels/slide1.xml.rels"], []byte("</Relationships>"),
			[]byte(`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image2.svg"/></Relationships>`), 1)
		baseParts["[Content_Types].xml"] = bytes.Replace(baseParts["[Content_Types].xml"], []byte(`<Default Extension="xml"`),
			[]byte(`<Default Extension="svg" ContentType="image/svg+xml"/><Default Extension="xml"`), 1)
		baseFix = fixture{
			partSlide:  parseEl(probeSlide),
			partLayout: parseEl(probeLayout),
			partMaster: parseEl(probeMaster),
			partPres:   parseEl(probePres),
			partTheme:  parseEl(string(baseParts[partTheme])),
		}
	})
}

// pngStub is a 1x1 PNG.
var pngStub = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xcf\xc0\xf0\x1f\x00\x05\x00\x01\xff\x89\x99=\x1d\x00\x00\x00\x00IEND\xaeB`\x82")

var (
	probePNGOnce sync.Once
	probePNGPath string
)

// probePNG returns a local PNG file holding the probe picture's bytes.
func probePNG() string {
	probePNGOnce.Do(func() {
		dir, err := os.MkdirTemp("", "coverage-png")
		if err != nil {
			panic(err)
		}
		probePNGPath = filepath.Join(dir, "probe.png")
		if err := os.WriteFile(probePNGPath, pngStub, 0o644); err != nil {
			panic(err)
		}
	})
	return probePNGPath
}

func newFixture(t testing.TB) fixture {
	loadBase(t)
	f := fixture{}
	for k, v := range baseFix {
		f[k] = v.clone()
	}
	return f
}

// write saves the fixture as a deck in dir and returns its path.
func (f fixture) write(t testing.TB, dir string) string {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(baseParts))
	for n := range baseParts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		ci, cj := names[i] == "[Content_Types].xml", names[j] == "[Content_Types].xml"
		if ci != cj {
			return ci
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		data := baseParts[n]
		if e, ok := f[n]; ok {
			data = e.xml()
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, fmt.Sprintf("probe%d.pptx", deckSeq.Add(1)))
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
