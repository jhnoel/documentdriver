package ops

import (
	"fmt"
	"slices"
)

// sectionBreaks are the section break types the layout ops write.
var sectionBreaks = []string{"nextPage", "continuous", "oddPage", "evenPage", "nextColumn"}

// sectioned is an op with an optional 1-based section number.
type sectioned interface {
	SetFirstPageMode | SetHeader | SetFooter | ClearHeader | ClearFooter
}

// sectionOf returns the section address held by section specs, or nil.
func sectionOf(s any) *SectionAddress {
	switch x := s.(type) {
	case *SetFirstPageMode:
		return x.At
	case *SetHeader:
		return x.At
	case *SetFooter:
		return x.At
	case *ClearHeader:
		return x.At
	case *ClearFooter:
		return x.At
	}
	return nil
}

const sectionMsg = "at must be a section address: section:<paraId> (as read shows it), section:final, section:N (N from 1) or section:$name"

// badSection reports whether a given section address is malformed.
func badSection(at *SectionAddress) bool {
	if at == nil {
		return false
	}
	return !at.Valid()
}

// sectionRules is the one check of the header, footer and first-page ops.
func sectionRules[T sectioned](example string) []Rule {
	return []Rule{rule(sectionMsg, "at",
		func(t *T) bool { return badSection(sectionOf(t)) }, example)}
}

var setHeaderRules = append(sectionRules[SetHeader](`{"op":"header.set","at":"section:0","html":"<p>x</p>"}`),
	deleteRule[SetHeader](false, func(h *SetHeader) []string { return h.Delete },
		`{"op":"header.set","html":"<p>x</p>","delete":["0000000A/equation:1"]}`))

var setFooterRules = append(sectionRules[SetFooter](`{"op":"footer.set","at":"section:0","html":"<p>x</p>"}`),
	deleteRule[SetFooter](false, func(f *SetFooter) []string { return f.Delete },
		`{"op":"footer.set","html":"<p>x</p>","delete":["0000000A/equation:1"]}`))

var setPageMarginsRules = []Rule{
	rule(sectionMsg, "at",
		func(m *SetPageMargins) bool { return badSection(m.At) },
		`{"op":"section.set_margins","at":"section:0","top":72}`),
	rule("section.set_margins requires a margin", "",
		func(m *SetPageMargins) bool {
			return m.Top == nil && m.Right == nil && m.Bottom == nil && m.Left == nil &&
				m.Header == nil && m.Footer == nil && m.Gutter == nil
		}, `{"op":"section.set_margins","at":"section:1"}`),
}

var setSectionBoundaryRules = []Rule{
	rule("removing a boundary cannot set break_type", "break_type",
		func(b *SetSectionBoundary) bool { return b.Remove && b.BreakType != nil },
		`{"op":"section.set_boundary","at":"0000000A","remove":true,"break_type":"nextPage"}`),
	rule("unsupported section break_type", "break_type",
		func(b *SetSectionBoundary) bool {
			return b.BreakType != nil && !slices.Contains(sectionBreaks, *b.BreakType)
		},
		`{"op":"section.set_boundary","at":"0000000A","break_type":"nextpage"}`),
}

// badLength reports whether points are present and outside 1 to 31680.
func badLength(p *Points) bool { return p != nil && (*p <= 0 || *p > 31680) }

var setSectionLayoutRules = []Rule{
	rule(sectionMsg, "at",
		func(l *SetSectionLayout) bool { return badSection(l.At) },
		`{"op":"section.set_layout","at":"1","orientation":"landscape"}`),
	rule("page_size must be letter/legal/a4/a3/a5", "page_size",
		func(l *SetSectionLayout) bool {
			return l.PageSize != nil && !slices.Contains([]string{"letter", "legal", "a4", "a3", "a5"}, *l.PageSize)
		}, `{"op":"section.set_layout","page_size":"A4"}`),
	rule("page_size conflicts with width/height", "page_size",
		func(l *SetSectionLayout) bool { return l.PageSize != nil && (l.Width != nil || l.Height != nil) },
		`{"op":"section.set_layout","page_size":"a4","width":595,"height":842}`),
	ruleX("width and height must be supplied together",
		func(l *SetSectionLayout) (string, string, bool) {
			const msg = "width and height must be supplied together"
			switch {
			case l.Width != nil && l.Height == nil:
				return msg, "height", true
			case l.Width == nil && l.Height != nil:
				return msg, "width", true
			}
			return "", "", false
		}, `{"op":"section.set_layout","width":612}`),
	ruleX("page dimensions must be >0 and <=1584 points",
		func(l *SetSectionLayout) (string, string, bool) {
			const msg = "page dimensions must be >0 and <=1584 points"
			switch {
			case badLength(l.Width):
				return msg, "width", true
			case badLength(l.Height):
				return msg, "height", true
			}
			return "", "", false
		}, `{"op":"section.set_layout","width":612,"height":2000}`),
	rule("orientation must be portrait/landscape", "orientation",
		func(l *SetSectionLayout) bool {
			return l.Orientation != nil && *l.Orientation != "portrait" && *l.Orientation != "landscape"
		}, `{"op":"section.set_layout","orientation":"wide"}`),
	rule("columns must be between 1 and 45", "columns",
		func(l *SetSectionLayout) bool { return l.Columns != nil && (*l.Columns == 0 || *l.Columns > 45) },
		`{"op":"section.set_layout","columns":46}`),
	rule("column_spacing must be between 0 and 1584 points", "column_spacing",
		func(l *SetSectionLayout) bool {
			return l.ColumnSpacing != nil && (*l.ColumnSpacing < 0 || *l.ColumnSpacing > 31680)
		},
		`{"op":"section.set_layout","column_spacing":1600}`),
	ruleX("column_widths requires 1..45 positive widths of at most 1584 points",
		func(l *SetSectionLayout) (string, string, bool) {
			const msg = "column_widths requires 1..45 positive widths of at most 1584 points"
			if l.ColumnWidths == nil {
				return "", "", false
			}
			ws := *l.ColumnWidths
			if len(ws) == 0 || len(ws) > 45 {
				return msg, "column_widths", true
			}
			for i, w := range ws {
				if badLength(&w) {
					return msg, fmt.Sprintf("column_widths[%d]", i), true
				}
			}
			return "", "", false
		}, `{"op":"section.set_layout","column_widths":[200,0]}`),
	rule("columns must match column_widths length", "columns",
		func(l *SetSectionLayout) bool {
			return l.Columns != nil && l.ColumnWidths != nil && int(*l.Columns) != len(*l.ColumnWidths)
		}, `{"op":"section.set_layout","columns":3,"column_widths":[200,200]}`),
	rule("unsupported section break_type", "break_type",
		func(l *SetSectionLayout) bool {
			return l.BreakType != nil && !slices.Contains(sectionBreaks, *l.BreakType)
		},
		`{"op":"section.set_layout","break_type":"page"}`),
	rule("unsupported page_number_format", "page_number_format",
		func(l *SetSectionLayout) bool {
			return l.PageNumberFormat != nil &&
				!slices.Contains([]string{"decimal", "upperRoman", "lowerRoman", "upperLetter", "lowerLetter"}, *l.PageNumberFormat)
		}, `{"op":"section.set_layout","page_number_format":"roman"}`),
	rule("page_number_continue only accepts true; use page_number_start to restart", "page_number_continue",
		func(l *SetSectionLayout) bool { return l.PageNumberContinue != nil && !*l.PageNumberContinue },
		`{"op":"section.set_layout","page_number_continue":false}`),
	rule("page_number_start must be between 1 and 2147483647", "page_number_start",
		func(l *SetSectionLayout) bool { return badID(l.PageNumberStart) },
		`{"op":"section.set_layout","page_number_start":0}`),
	rule("page_number_start conflicts with page_number_continue", "page_number_start",
		func(l *SetSectionLayout) bool { return l.PageNumberStart != nil && isTrue(l.PageNumberContinue) },
		`{"op":"section.set_layout","page_number_start":3,"page_number_continue":true}`),
	rule("section.set_layout requires a property", "",
		func(l *SetSectionLayout) bool {
			return l.PageSize == nil && l.Width == nil && l.Orientation == nil && l.Columns == nil &&
				l.ColumnSpacing == nil && l.ColumnWidths == nil && l.ColumnSeparator == nil &&
				l.BreakType == nil && l.PageNumberFormat == nil && l.PageNumberStart == nil &&
				l.PageNumberContinue == nil
		}, `{"op":"section.set_layout","at":"section:2"}`),
}
