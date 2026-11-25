package text

// Renumber returns b and f with the Character and Paragraph rows
// numbered as Visio writes them: in text order from 0, a new row
// where the format changes from the run (or paragraph) before, so
// each row covers one contiguous stretch and no row is used twice.
// A run before any cp marker (CP -1) and a paragraph before any pp
// marker (PP -1) take row 0, as Visio reads them.
//
// LibreOffice's Visio import (libvisio VSDXMLParserBase::readText)
// applies a shape's rows by character counts in row order, not by the
// markers, so a row used for two stretches, or rows out of order in
// the text, put formatting on the wrong characters; every Visio-
// authored text of the corpus is numbered this way
// (docs/vsdx-design.md, Text runs). Field and break runs are runs
// like any other. b is not changed; the returned body shares no
// slices with it.
func Renumber(b Body, f Formats) (Body, Formats) {
	out := Body{Terminated: b.Terminated, Paras: make([]Para, len(b.Paras))}
	var nf Formats
	nextCP, nextPP := 0, 0
	var lastChar CharFormat
	var lastPara ParaFormat
	for i, p := range b.Paras {
		pf := paraFormat(f, p.PP)
		if i == 0 || pf != lastPara {
			if nf.Para == nil {
				nf.Para = map[int]ParaFormat{}
			}
			nf.Para[nextPP] = pf
			nextPP++
			lastPara = pf
		}
		np := Para{PP: nextPP - 1, Runs: make([]Run, len(p.Runs))}
		for j, r := range p.Runs {
			cf := charFormat(f, r.CP)
			if nextCP == 0 || cf != lastChar {
				if nf.Char == nil {
					nf.Char = map[int]CharFormat{}
				}
				nf.Char[nextCP] = cf
				nextCP++
				lastChar = cf
			}
			r.CP = nextCP - 1
			np.Runs[j] = r
		}
		out.Paras[i] = np
	}
	return out, nf
}
