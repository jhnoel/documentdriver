// Package ttf reads TrueType fonts (the glyf outline flavour of
// OpenType): the character map, advance widths and glyph outlines,
// enough to lay text out and fill its glyphs. It reads only the tables
// it needs, checks every offset against the font's bytes, and never
// panics on a malformed font: a lookup that cannot be satisfied
// returns glyph 0 (the missing glyph), a zero advance or an error.
// Hinting, kerning and OpenType layout are not applied.
package ttf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Font is a parsed TrueType font: the offsets of the tables it reads.
type Font struct {
	data        []byte
	unitsPerEm  int
	numGlyphs   int
	locaLong    bool
	numHMetrics int
	ascent      int
	descent     int
	cmap        []byte // the chosen cmap subtable
	cmapFormat  int
	hmtx        []byte
	loca        []byte
	glyf        []byte
}

// ErrFormat reports a font this package cannot read.
var ErrFormat = errors.New("ttf: not a TrueType font it can read")

// MaxComponentDepth bounds composite glyph nesting.
const MaxComponentDepth = 8

// MaxPoints bounds the points of one glyph, components included.
const MaxPoints = 1 << 16

// u16 reads a big-endian uint16 at off of b, and false out of range.
func u16(b []byte, off int) (int, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, false
	}
	return int(binary.BigEndian.Uint16(b[off:])), true
}

// i16 reads a big-endian int16 at off of b.
func i16(b []byte, off int) (int, bool) {
	v, ok := u16(b, off)
	return int(int16(uint16(v))), ok //nolint:gosec // G115: a 16-bit field reinterpreted as signed, by the format
}

// u32 reads a big-endian uint32 at off of b.
func u32(b []byte, off int) (int, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return int(binary.BigEndian.Uint32(b[off:])), true
}

// Parse reads the table directory and the head, maxp, hhea, hmtx,
// cmap, loca and glyf tables of a TrueType font. It keeps data, which
// the caller must not change.
func Parse(data []byte) (*Font, error) {
	tag, ok := u32(data, 0)
	if !ok || (tag != 0x00010000 && tag != 0x74727565) { // 1.0 or "true"
		return nil, fmt.Errorf("%w: not a TrueType outline font", ErrFormat)
	}
	n, _ := u16(data, 4)
	tables := map[string][]byte{}
	for i := range n {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil, fmt.Errorf("%w: table directory past the end", ErrFormat)
		}
		name := string(data[rec : rec+4])
		off, _ := u32(data, rec+8)
		length, _ := u32(data, rec+12)
		if off+length > len(data) || off+length < off {
			return nil, fmt.Errorf("%w: table %s past the end", ErrFormat, name)
		}
		tables[name] = data[off : off+length]
	}
	f := &Font{data: data}
	for _, need := range []string{"head", "maxp", "hhea", "hmtx", "cmap", "loca", "glyf"} {
		if _, ok := tables[need]; !ok {
			return nil, fmt.Errorf("%w: no %s table", ErrFormat, need)
		}
	}
	head := tables["head"]
	f.unitsPerEm, _ = u16(head, 18)
	locFmt, ok := i16(head, 50)
	if !ok || f.unitsPerEm < 16 || f.unitsPerEm > 16384 {
		return nil, fmt.Errorf("%w: bad head table", ErrFormat)
	}
	f.locaLong = locFmt == 1
	f.numGlyphs, ok = u16(tables["maxp"], 4)
	if !ok {
		return nil, fmt.Errorf("%w: bad maxp table", ErrFormat)
	}
	hhea := tables["hhea"]
	f.ascent, _ = i16(hhea, 4)
	f.descent, _ = i16(hhea, 6)
	f.numHMetrics, ok = u16(hhea, 34)
	if !ok || f.numHMetrics == 0 || 4*f.numHMetrics > len(tables["hmtx"]) {
		return nil, fmt.Errorf("%w: bad hhea or hmtx table", ErrFormat)
	}
	f.hmtx, f.loca, f.glyf = tables["hmtx"], tables["loca"], tables["glyf"]
	if err := f.chooseCmap(tables["cmap"]); err != nil {
		return nil, err
	}
	return f, nil
}

// chooseCmap picks the Unicode subtable of cmap: format 12 (full
// repertoire) when there is one, else format 4 (BMP).
func (f *Font) chooseCmap(cmap []byte) error {
	n, ok := u16(cmap, 2)
	if !ok {
		return fmt.Errorf("%w: bad cmap table", ErrFormat)
	}
	best := -1
	for i := range n {
		rec := 4 + 8*i
		pid, ok1 := u16(cmap, rec)
		eid, ok2 := u16(cmap, rec+2)
		off, ok3 := u32(cmap, rec+4)
		if !ok1 || !ok2 || !ok3 {
			break
		}
		unicode := pid == 0 || (pid == 3 && (eid == 1 || eid == 10))
		if !unicode || off >= len(cmap) {
			continue
		}
		format, _ := u16(cmap, off)
		if (format == 12 && best != 12) || (format == 4 && best == -1) {
			best = format
			f.cmap, f.cmapFormat = cmap[off:], format
		}
	}
	if best == -1 {
		return fmt.Errorf("%w: no Unicode cmap of format 4 or 12", ErrFormat)
	}
	return nil
}

// UnitsPerEm returns the font's design units per em.
func (f *Font) UnitsPerEm() int { return f.unitsPerEm }

// Ascent returns the hhea ascender in design units.
func (f *Font) Ascent() int { return f.ascent }

// Descent returns the hhea descender in design units (negative below
// the baseline).
func (f *Font) Descent() int { return f.descent }

// Index returns the glyph of r, 0 when the font has none.
func (f *Font) Index(r rune) int {
	if r < 0 {
		return 0
	}
	c := int(r)
	switch f.cmapFormat {
	case 4:
		return f.index4(c)
	case 12:
		return f.index12(c)
	}
	return 0
}

// index4 looks c up in a format 4 subtable.
func (f *Font) index4(c int) int {
	t := f.cmap
	if c > 0xFFFF {
		return 0
	}
	segX2, ok := u16(t, 6)
	if !ok {
		return 0
	}
	ends, starts, deltas, ranges := 14, 16+segX2, 16+2*segX2, 16+3*segX2
	lo, hi := 0, segX2/2
	for lo < hi {
		mid := (lo + hi) / 2
		end, ok := u16(t, ends+2*mid)
		if !ok {
			return 0
		}
		if end < c {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	seg := lo
	start, ok := u16(t, starts+2*seg)
	if !ok || c < start {
		return 0
	}
	delta, _ := u16(t, deltas+2*seg)
	ro, ok := u16(t, ranges+2*seg)
	if !ok {
		return 0
	}
	if ro == 0 {
		return (c + delta) & 0xFFFF
	}
	g, ok := u16(t, ranges+2*seg+ro+2*(c-start))
	if !ok || g == 0 {
		return 0
	}
	return (g + delta) & 0xFFFF
}

// index12 looks c up in a format 12 subtable.
func (f *Font) index12(c int) int {
	t := f.cmap
	n, ok := u32(t, 12)
	if !ok || 16+12*n > len(t) || n < 0 {
		return 0
	}
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		end, _ := u32(t, 16+12*mid+4)
		if end < c {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= n {
		return 0
	}
	start, _ := u32(t, 16+12*lo)
	first, _ := u32(t, 16+12*lo+8)
	if c < start {
		return 0
	}
	g := first + c - start
	if g >= f.numGlyphs {
		return 0
	}
	return g
}

// Advance returns the advance width of glyph g in design units.
func (f *Font) Advance(g int) int {
	if g < 0 || g >= f.numGlyphs {
		return 0
	}
	if g >= f.numHMetrics {
		g = f.numHMetrics - 1
	}
	v, _ := u16(f.hmtx, 4*g)
	return v
}

// Point is an outline point in design units, y up; On is false for a
// quadratic control point.
type Point struct {
	X, Y float64
	On   bool
}

// Contour is one closed contour of a glyph.
type Contour []Point

// glyphData returns the glyf bytes of glyph g (empty for a glyph with
// no outline).
func (f *Font) glyphData(g int) ([]byte, error) {
	if g < 0 || g >= f.numGlyphs {
		return nil, fmt.Errorf("ttf: glyph %d out of range", g)
	}
	var a, b int
	var ok1, ok2 bool
	if f.locaLong {
		a, ok1 = u32(f.loca, 4*g)
		b, ok2 = u32(f.loca, 4*g+4)
	} else {
		a, ok1 = u16(f.loca, 2*g)
		b, ok2 = u16(f.loca, 2*g+2)
		a, b = 2*a, 2*b
	}
	if !ok1 || !ok2 || a > b || b > len(f.glyf) {
		return nil, fmt.Errorf("ttf: glyph %d: bad loca entry", g)
	}
	return f.glyf[a:b], nil
}

// Outline returns the contours of glyph g in design units, components
// of a composite glyph placed by their offsets and transforms.
func (f *Font) Outline(g int) ([]Contour, error) {
	var out []Contour
	points := 0
	if err := f.outline(g, [6]float64{1, 0, 0, 1, 0, 0}, 0, &out, &points); err != nil {
		return nil, err
	}
	return out, nil
}

// outline appends glyph g's contours, transformed by m (a, b, c, d,
// e, f: x' = a x + c y + e, y' = b x + d y + f), to out.
func (f *Font) outline(g int, m [6]float64, depth int, out *[]Contour, points *int) error {
	if depth > MaxComponentDepth {
		return fmt.Errorf("ttf: glyph %d: components nested past %d", g, MaxComponentDepth)
	}
	b, err := f.glyphData(g)
	if err != nil || len(b) == 0 {
		return err
	}
	nc, ok := i16(b, 0)
	if !ok {
		return fmt.Errorf("ttf: glyph %d: short header", g)
	}
	if nc < 0 {
		return f.composite(g, b, m, depth, out, points)
	}
	return f.simple(g, b, nc, m, out, points)
}

// simple decodes a simple glyph of nc contours.
func (f *Font) simple(g int, b []byte, nc int, m [6]float64, out *[]Contour, points *int) error {
	bad := fmt.Errorf("ttf: glyph %d: malformed outline", g)
	ends := make([]int, nc)
	for i := range nc {
		e, ok := u16(b, 10+2*i)
		if !ok || (i > 0 && e < ends[i-1]) {
			return bad
		}
		ends[i] = e
	}
	if nc == 0 {
		return nil
	}
	np := ends[nc-1] + 1
	*points += np
	if *points > MaxPoints {
		return fmt.Errorf("ttf: glyph %d: more than %d points", g, MaxPoints)
	}
	insLen, ok := u16(b, 10+2*nc)
	if !ok {
		return bad
	}
	p := 12 + 2*nc + insLen
	flags := make([]byte, np)
	for i := 0; i < np; {
		if p >= len(b) {
			return bad
		}
		fl := b[p]
		p++
		flags[i] = fl
		i++
		if fl&8 != 0 {
			if p >= len(b) {
				return bad
			}
			rep := int(b[p])
			p++
			for ; rep > 0 && i < np; rep-- {
				flags[i] = fl
				i++
			}
		}
	}
	xs := make([]int, np)
	ys := make([]int, np)
	for axis, short, same := 0, byte(2), byte(16); axis < 2; axis, short, same = axis+1, 4, 32 {
		v := 0
		for i := range np {
			fl := flags[i]
			switch {
			case fl&short != 0:
				if p >= len(b) {
					return bad
				}
				d := int(b[p])
				p++
				if fl&same == 0 {
					d = -d
				}
				v += d
			case fl&same == 0:
				d, ok := i16(b, p)
				if !ok {
					return bad
				}
				p += 2
				v += d
			}
			if axis == 0 {
				xs[i] = v
			} else {
				ys[i] = v
			}
		}
	}
	start := 0
	for _, e := range ends {
		c := make(Contour, 0, e-start+1)
		for i := start; i <= e; i++ {
			x, y := float64(xs[i]), float64(ys[i])
			c = append(c, Point{X: m[0]*x + m[2]*y + m[4], Y: m[1]*x + m[3]*y + m[5], On: flags[i]&1 != 0})
		}
		if len(c) > 0 {
			*out = append(*out, c)
		}
		start = e + 1
	}
	return nil
}

// composite decodes a composite glyph's components.
func (f *Font) composite(g int, b []byte, m [6]float64, depth int, out *[]Contour, points *int) error {
	bad := fmt.Errorf("ttf: glyph %d: malformed composite", g)
	p := 10
	for {
		flags, ok1 := u16(b, p)
		comp, ok2 := u16(b, p+2)
		if !ok1 || !ok2 {
			return bad
		}
		p += 4
		var dx, dy int
		if flags&1 != 0 { // ARG_1_AND_2_ARE_WORDS
			var ok3, ok4 bool
			dx, ok3 = i16(b, p)
			dy, ok4 = i16(b, p+2)
			if !ok3 || !ok4 {
				return bad
			}
			p += 4
		} else {
			if p+2 > len(b) {
				return bad
			}
			dx, dy = int(int8(b[p])), int(int8(b[p+1])) //nolint:gosec // G115: signed bytes by the format
			p += 2
		}
		if flags&2 == 0 { // point matching, not offsets: not applied
			dx, dy = 0, 0
		}
		a, bb, c, d := 1.0, 0.0, 0.0, 1.0
		f2dot14 := func(off int) (float64, bool) {
			v, ok := i16(b, off)
			return float64(v) / 16384, ok
		}
		switch {
		case flags&8 != 0: // WE_HAVE_A_SCALE
			s, ok := f2dot14(p)
			if !ok {
				return bad
			}
			a, d = s, s
			p += 2
		case flags&0x40 != 0: // X_AND_Y_SCALE
			sx, ok1 := f2dot14(p)
			sy, ok2 := f2dot14(p + 2)
			if !ok1 || !ok2 {
				return bad
			}
			a, d = sx, sy
			p += 4
		case flags&0x80 != 0: // TWO_BY_TWO
			v0, ok1 := f2dot14(p)
			v1, ok2 := f2dot14(p + 2)
			v2, ok3 := f2dot14(p + 4)
			v3, ok4 := f2dot14(p + 6)
			if !ok1 || !ok2 || !ok3 || !ok4 {
				return bad
			}
			a, bb, c, d = v0, v1, v2, v3
			p += 8
		}
		// The component's map, then the parent's.
		cm := [6]float64{
			m[0]*a + m[2]*bb, m[1]*a + m[3]*bb,
			m[0]*c + m[2]*d, m[1]*c + m[3]*d,
			m[0]*float64(dx) + m[2]*float64(dy) + m[4], m[1]*float64(dx) + m[3]*float64(dy) + m[5],
		}
		if err := f.outline(comp, cm, depth+1, out, points); err != nil {
			return err
		}
		if flags&0x20 == 0 { // MORE_COMPONENTS
			return nil
		}
	}
}
