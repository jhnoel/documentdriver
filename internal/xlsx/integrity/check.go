package integrity

import (
	"errors"
	"runtime"
	"slices"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Check checks the workbook in data and returns what it found. It never
// fails: a package, part or sheet that cannot be read is a finding, and
// the rest is checked as far as it can be. It reads the package with
// opc.DefaultLimits.
func Check(data []byte) *Findings { return CheckWithLimits(data, opc.DefaultLimits()) }

// CheckWithLimits is Check with explicit reading limits.
func CheckWithLimits(data []byte, l opc.Limits) *Findings {
	f := &Findings{}
	pkg, err := opc.OpenBytes(data, l)
	if err != nil {
		f.add(PackageUnreadable, "", "", err.Error())
		return f
	}
	defer pkg.Close() //nolint:errcheck // an in-memory package holds nothing to release
	CheckPackageInto(f, pkg)
	return f
}

// CheckPackage checks an open package.
func CheckPackage(pkg *opc.Package) *Findings {
	f := &Findings{}
	CheckPackageInto(f, pkg)
	return f
}

// CheckPackageInto adds the findings of pkg to f.
func CheckPackageInto(f *Findings, pkg *opc.Package) {
	(&checker{f: f, pkg: pkg}).run()
}

// openChecked opens data with the default reading limits, returning its
// findings and a package the caller closes: ok is false when the package
// cannot be opened, and the findings hold the PackageUnreadable finding.
func openChecked(data []byte) (pkg *opc.Package, f *Findings, ok bool) {
	f = &Findings{}
	pkg, err := opc.OpenBytes(data, opc.DefaultLimits())
	if err != nil {
		f.add(PackageUnreadable, "", "", err.Error())
		return nil, f, false
	}
	return pkg, f, true
}

// checkMarked is CheckWithLimits with a checkpoint (see checker.mark).
func checkMarked(data []byte, mark func(string)) *Findings {
	pkg, f, ok := openChecked(data)
	if !ok {
		return f
	}
	defer pkg.Close() //nolint:errcheck // an in-memory package holds nothing to release
	(&checker{f: f, pkg: pkg, mark: mark}).run()
	return f
}

type checker struct {
	f   *Findings
	pkg *opc.Package
	ct  *opc.ContentTypes
	// mark, when not nil, is called where the working set is largest (a
	// worksheet scanned, its state still held): the memory test's checkpoint.
	mark func(string)
	// scope, when not nil, holds the part keys of the worksheets (and
	// their tables) checked; the rest are skipped (CheckScoped).
	scope map[string]bool
}

// sheetInfo is a sheet of the workbook.
type sheetInfo struct {
	name   string
	part   string
	isCell bool // a worksheet, not a chart or dialog sheet
}

// pendingHeaders holds a sheet's header cells until the shared strings
// they name are read.
type pendingHeaders struct {
	sheet  string
	tables []tableInfo
	hdr    map[[2]int]headerCell
}

// run checks the package and records its findings in c.f.
func (c *checker) run() {
	var err error
	if c.ct, err = c.pkg.ContentTypes(); err != nil {
		c.f.add(PackageContentTypes, "", "", err.Error())
		c.ct = nil
	}
	root, err := c.pkg.Rels("/")
	if err != nil {
		c.f.add(PackageRels, opc.PackageRelsName, "", err.Error())
		return
	}
	var wb string
	for _, r := range root.All() {
		if !ctypes.Is(r.Type, ctypes.RelOfficeDocument) || r.External() {
			continue
		}
		if wb, err = root.TargetPart(r); err != nil {
			continue
		}
		break
	}
	if wb == "" {
		c.f.add(PackageNoWorkbook, "", "", "no officeDocument relationship")
		return
	}
	if !c.pkg.Has(wb) {
		c.f.add(PackagePartMissing, "", wb, "workbook part "+wb+" is missing")
		return
	}
	c.expectType(wb, ctypes.CTSpreadsheetmlSheetMain, ctypes.CTSpreadsheetmlTemplateMain,
		ctypes.CTMsExcelSheetMacroEnabledMain, ctypes.CTMsExcelTemplateMacroEnabledMain)
	rels, err := c.pkg.Rels(wb)
	if err != nil {
		c.f.add(PackageRels, opc.RelsName(wb), "", err.Error())
		rels = nil
	}
	sheets := c.checkWorkbook(wb, rels)

	// Shared strings and styles, by the workbook's relationships.
	nsst, nxf := 0, -1
	if rels != nil {
		if p := c.target(rels, ctypes.RelSharedStrings, wb); p != "" {
			c.expectType(p, ctypes.CTSpreadsheetmlSharedStrings)
			nsst = -1
			if src, ok := c.text(p); ok {
				nsst, _ = scanSST(c.f, p, src, nil)
			}
		}
		if p := c.target(rels, ctypes.RelStyles, wb); p != "" {
			c.expectType(p, ctypes.CTSpreadsheetmlStyles)
			nxf = c.checkStyles(p).cellXfs
		}
	}

	ids, names := map[string]bool{}, map[string]bool{}
	var pending []pendingHeaders
	done := map[string]bool{}
	for _, sh := range sheets {
		if !sh.isCell || sh.part == "" || done[sh.part] {
			continue
		}
		done[sh.part] = true
		if c.scope != nil && !c.scope[opc.PartKey(sh.part)] && !c.tableInScope(sh.part) {
			continue
		}
		c.expectType(sh.part, ctypes.CTSpreadsheetmlWorksheet)
		var tables []tableInfo
		wants := map[int][][2]int{}
		if srels, err := c.pkg.Rels(sh.part); err != nil {
			c.f.add(PackageRels, opc.RelsName(sh.part), "", err.Error())
		} else {
			for _, r := range srels.All() {
				if !ctypes.Is(r.Type, ctypes.RelTable) || r.External() {
					continue
				}
				tp, err := srels.TargetPart(r)
				if err != nil || !c.present(tp, sh.part) {
					continue
				}
				c.expectType(tp, ctypes.CTSpreadsheetmlTable)
				if t, ok := c.readTable(tp, ids, names); ok {
					tables = append(tables, t)
					if t.headers == 1 {
						wants[t.ref.r1] = append(wants[t.ref.r1], [2]int{t.ref.c1, t.ref.c2})
					}
				}
			}
		}
		src, ok := c.text(sh.part)
		if !ok {
			continue
		}
		s := newSheetScan(c.f, sh.part, sh.name, nxf, nsst, wants)
		if err := tree.ScanString(src, s); err != nil {
			c.f.add(XMLMalformed, sh.part, "", err.Error())
		}
		if c.mark != nil {
			c.mark("sheet scanned")
		}
		if len(tables) > 0 {
			pending = append(pending, pendingHeaders{sh.name, tables, s.hdr})
		}
		runtime.KeepAlive(s)
	}
	c.resolveHeaders(rels, wb, pending)
	if rels != nil {
		c.checkCalcChain(rels, wb)
	}
	c.f.note("defined-name and cell formulas are not lexed (pending: the formula package)")
}

// resolveHeaders reads the shared strings of the header cells that name
// one, and compares the headers with their tables.
func (c *checker) resolveHeaders(rels *opc.Rels, wb string, pending []pendingHeaders) {
	if len(pending) == 0 {
		return
	}
	want := map[int]struct{}{}
	for _, p := range pending {
		for _, h := range p.hdr {
			if h.t == "s" {
				if n, ok := sml.ParseIndex(h.text); ok {
					want[n] = struct{}{}
				}
			}
		}
	}
	text := map[int]string{}
	if len(want) > 0 && rels != nil {
		if p := c.target(rels, ctypes.RelSharedStrings, wb); p != "" {
			if src, ok := c.text(p); ok {
				// The findings of this second read were made by the first.
				_, text = scanSST(&Findings{}, p, src, want)
			}
		}
	}
	for _, p := range pending {
		c.checkHeaders(p.sheet, p.tables, p.hdr, text)
	}
}

// target returns the part the first relationship of type rel of source
// names, "" when there is none or the part is missing (a finding).
func (c *checker) target(rels *opc.Rels, rel, source string) string {
	for _, r := range rels.All() {
		if !ctypes.Is(r.Type, rel) || r.External() {
			continue
		}
		p, err := rels.TargetPart(r)
		if err != nil {
			continue
		}
		if c.present(p, source) {
			return p
		}
		return ""
	}
	return ""
}

// present reports whether part p exists, adding a finding when not.
func (c *checker) present(p, source string) bool {
	if c.pkg.Has(p) {
		return true
	}
	c.f.add(PackagePartMissing, source, p, "relationship from "+source+" targets the missing part "+p)
	return false
}

// expectType checks that part has a content type and one of want.
func (c *checker) expectType(part string, want ...string) {
	if c.ct == nil {
		return
	}
	got, ok := c.ct.Lookup(part)
	switch {
	case !ok:
		c.f.add(PackageNoType, part, "", part+" has no content type")
	case !slices.Contains(want, got):
		c.f.add(PackageTypeMismatch, part, got, part+" has content type "+got)
	}
}

// text returns a part's text, adding a finding when it cannot be read.
func (c *checker) text(name string) (string, bool) {
	pt, err := c.pkg.Part(name)
	if err != nil {
		c.f.add(PackagePartMissing, name, "", err.Error())
		return "", false
	}
	s, ok, err := pt.OriginalString()
	if err != nil || !ok {
		msg := "part has no stored bytes"
		if err != nil {
			msg = err.Error()
		}
		c.f.add(PackagePartRead, name, "", msg)
		return "", false
	}
	return s, true
}

// tree parses a small part, adding a finding when it cannot be read or is
// not well formed. Worksheets and shared strings never come here.
func (c *checker) tree(name string) *tree.Document {
	if _, ok := c.text(name); !ok {
		return nil
	}
	pt, err := c.pkg.Part(name)
	if err != nil {
		return nil
	}
	doc, err := pt.Tree()
	if err != nil {
		var se *tree.SyntaxError
		msg := err.Error()
		if errors.As(err, &se) {
			msg = se.Msg
		}
		c.f.add(XMLMalformed, name, "", msg)
		return nil
	}
	return doc
}

// checkCalcChain checks that the calcChain part and its relationship go
// together: a chain whose relationship was removed, or the reverse, is
// what an edit that drops the chain half-way leaves.
func (c *checker) checkCalcChain(rels *opc.Rels, wb string) {
	const chain = "/xl/calcChain.xml"
	var rel bool
	for _, r := range rels.All() {
		if !ctypes.Is(r.Type, ctypes.RelCalcChain) {
			continue
		}
		rel = true
		if p, err := rels.TargetPart(r); err == nil && !c.pkg.Has(p) {
			c.f.add(PackageCalcChain, p, "rel-without-part", "calcChain relationship targets the missing part "+p)
		}
	}
	if !rel && c.pkg.Has(chain) {
		c.f.add(PackageCalcChain, chain, "part-without-rel", "calcChain part has no relationship from "+wb)
	}
	if c.ct != nil && c.pkg.Has(chain) {
		if _, ok := c.ct.Lookup(chain); !ok {
			c.f.add(PackageNoType, chain, "", chain+" has no content type")
		}
	}
}

// tableInScope reports whether a table part of the sheet part is in the
// scope: a changed table is checked against its sheet's header cells,
// so its sheet is read too.
func (c *checker) tableInScope(part string) bool {
	srels, err := c.pkg.Rels(part)
	if err != nil {
		return false
	}
	for _, r := range srels.All() {
		if !ctypes.Is(r.Type, ctypes.RelTable) || r.External() {
			continue
		}
		if tp, err := srels.TargetPart(r); err == nil && c.scope[opc.PartKey(tp)] {
			return true
		}
	}
	return false
}
