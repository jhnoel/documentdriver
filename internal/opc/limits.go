package opc

import (
	"archive/zip"
	"bufio"
	"compress/flate"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"strings"
)

// ErrLimit is wrapped by every error that reports a package exceeding one
// of its Limits.
var ErrLimit = errors.New("opc: package exceeds a safety limit")

// Limits bound what reading an untrusted package may cost. Every limit is
// enforced on bytes actually read or entries actually present, never on
// the sizes a zip header declares, which are attacker data. A zero field
// takes its value from DefaultLimits.
//
// Untouched members are never decompressed: Save copies their compressed
// bytes with zip.Writer.Copy. A copy costs at most the member's
// compressed bytes, and Open rejects members whose compressed data
// overlaps another's, so all copies together cost at most the input
// file's size.
type Limits struct {
	// MaxPartSize is the most decompressed bytes one part may hold.
	MaxPartSize int64
	// MaxTotalSize is the most decompressed bytes all parts read from one
	// open package may hold together.
	MaxTotalSize int64
	// MaxRatio is the largest ratio of decompressed bytes to compressed
	// bytes consumed that a member may reach once it has produced at
	// least RatioMinSize bytes.
	MaxRatio int64
	// RatioMinSize is the decompressed size below which the ratio is not
	// checked (small, highly repetitive XML parts compress well).
	RatioMinSize int64
	// MaxEntries is the most entries (members and directory entries) the
	// zip may have.
	MaxEntries int
}

// DefaultLimits returns the limits Open applies. They were chosen against
// the corpus (corpus/repos and corpus/web, 1,353 decks, measured
// 2026-11-13) with a wide margin: largest part 4.8 MB (an MP3), largest
// package 7.8 MB decompressed, most entries 529, highest ratio 667:1 (a
// 1.45 MB EMF). Deflate cannot exceed about 1032:1 in one stream, so the
// ratio limit rejects only near-degenerate members; the byte limits are
// the main defence.
func DefaultLimits() Limits {
	return Limits{
		MaxPartSize:  256 << 20,
		MaxTotalSize: 1 << 30,
		MaxRatio:     1000,
		RatioMinSize: 1 << 20,
		MaxEntries:   20000,
	}
}

// withDefaults fills zero fields from DefaultLimits.
func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxPartSize <= 0 {
		l.MaxPartSize = d.MaxPartSize
	}
	if l.MaxTotalSize <= 0 {
		l.MaxTotalSize = d.MaxTotalSize
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = d.MaxRatio
	}
	if l.RatioMinSize <= 0 {
		l.RatioMinSize = d.RatioMinSize
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}
	return l
}

// checkEntries enforces MaxEntries and rejects members whose compressed
// data starts at the same offset as, or overlaps, another member's. Such
// sharing lets a small file expand into many copies on save or read. The
// ranges come from the central directory; reading the local headers to
// find each data offset is one small read per entry. A member whose local
// header cannot be read is skipped: reading or copying it fails anyway.
func checkEntries(zr *zip.Reader, l Limits) error {
	if len(zr.File) > l.MaxEntries {
		return fmt.Errorf("%w: %d zip entries (limit %d)", ErrLimit, len(zr.File), l.MaxEntries)
	}
	type span struct {
		off, end int64
		name     string
	}
	spans := make([]span, 0, len(zr.File))
	for _, f := range zr.File {
		off, err := f.DataOffset()
		if err != nil {
			// A broken local header: reading or copying the member fails
			// the same way later, so it shares nothing.
			continue
		}
		// The declared size is attacker data: a value that cannot be the
		// length of a range starting at off (it would wrap past MaxInt64)
		// cannot describe real data in this file.
		if off < 0 || f.CompressedSize64 > uint64(math.MaxInt64-off) { //nolint:gosec // G115: off >= 0 is checked first, so MaxInt64-off is non-negative
			return fmt.Errorf("%w: zip entry %s declares an impossible compressed size %d", ErrLimit, f.Name, f.CompressedSize64)
		}
		spans = append(spans, span{off, off + int64(f.CompressedSize64), f.Name}) //nolint:gosec // G115: bounded by the check above

	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].off < spans[j].off })
	for i := 1; i < len(spans); i++ {
		a, b := spans[i-1], spans[i]
		if a.off == b.off || a.end > b.off {
			return fmt.Errorf("%w: zip entries %s and %s share compressed data", ErrLimit, a.name, b.name)
		}
	}
	return nil
}

// budget is the decompressed byte allowance left for one open package. It
// also keeps one buffered reader and one decompressor, reset for each
// member, so reading many small parts does not allocate a window each.
type budget struct {
	limits Limits
	used   int64

	br *bufio.Reader
	fr io.ReadCloser
}

// read decompresses a member within the limits. It decompresses from the
// raw stream itself so that it can count the compressed bytes the
// decompressor actually consumed, and checks the CRC-32 and size against
// the central directory as archive/zip does.
func (b *budget) read(f *zip.File) (string, error) {
	// Read into one string builder sized from the declared size, so the
	// part is held once: no doubling buffer and no copy into a string.
	// The declared size is attacker data, so it only sizes the first
	// allocation, bounded by what the limits let this member decompress
	// to; the bytes actually read are what the limits count.
	var sb strings.Builder
	sb.Grow(int(min(f.UncompressedSize64, uint64(b.growLimit(f))))) //nolint:gosec // G115: growLimit is non-negative and at most MaxPartSize
	if err := b.stream(f, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// stream decompresses member f into w within the limits, checking the
// size and CRC-32 as read does, without holding the whole member.
func (b *budget) stream(f *zip.File, w io.Writer) error {
	raw, err := f.OpenRaw()
	if err != nil {
		return err
	}
	if b.br == nil {
		b.br = bufio.NewReader(raw)
	} else {
		b.br.Reset(raw)
	}
	cr := &countReader{r: b.br}
	var src io.Reader
	switch f.Method {
	case zip.Store:
		src = cr
	case zip.Deflate:
		if b.fr == nil {
			b.fr = flate.NewReader(cr)
		} else if err := b.fr.(flate.Resetter).Reset(cr, nil); err != nil {
			return err
		}
		src = b.fr
	default:
		return zip.ErrAlgorithm
	}
	lr := &limitReader{r: src, comp: cr, b: b, crc: crc32.NewIEEE()}
	if _, err := io.Copy(w, lr); err != nil {
		return err
	}
	if lr.n < 0 || uint64(lr.n) != f.UncompressedSize64 { //nolint:gosec // G115: lr.n counts bytes read, so it is never negative (checked)
		return zip.ErrFormat
	}
	if lr.crc.Sum32() != f.CRC32 && f.CRC32 != 0 {
		return zip.ErrChecksum
	}
	return nil
}

// growLimit is the most member f may decompress to within the limits:
// the part and remaining total sizes, and the ratio to its compressed
// size when that applies.
func (b *budget) growLimit(f *zip.File) int64 {
	lim := b.limits
	n := min(lim.MaxPartSize, max(lim.MaxTotalSize-b.used, 0))
	if c := f.CompressedSize64; c <= uint64(math.MaxInt64/max(lim.MaxRatio, 1)) { //nolint:gosec // G115: MaxRatio is positive
		n = min(n, max(lim.RatioMinSize, int64(c)*lim.MaxRatio)) //nolint:gosec // G115: c is bounded just above
	}
	return n
}

// countReader counts bytes consumed through it. It is an io.ByteReader so
// that flate reads exactly the bytes it needs, without buffering ahead.
type countReader struct {
	r *bufio.Reader
	n int64
}

// Read reads into p and counts the bytes read.
func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ReadByte reads one byte and counts it on success.
func (c *countReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

// limitReader enforces the per-part, total and ratio limits while a
// member is decompressed.
type limitReader struct {
	r    io.Reader
	comp *countReader
	b    *budget
	n    int64
	crc  hash.Hash32
}

// Read decompresses into p, enforcing the part, total and ratio limits.
func (l *limitReader) Read(p []byte) (int, error) {
	lim := l.b.limits
	// Never decompress more than one byte past any limit.
	room := min(lim.MaxPartSize-l.n, lim.MaxTotalSize-l.b.used) + 1
	if int64(len(p)) > room {
		p = p[:room]
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	l.b.used += int64(n)
	l.crc.Write(p[:n])
	switch {
	case l.n > lim.MaxPartSize:
		return n, fmt.Errorf("%w: part decompresses to more than %d bytes", ErrLimit, lim.MaxPartSize)
	case l.b.used > lim.MaxTotalSize:
		return n, fmt.Errorf("%w: parts read decompress to more than %d bytes in total", ErrLimit, lim.MaxTotalSize)
	case l.n >= lim.RatioMinSize && l.n > lim.MaxRatio*max(l.comp.n, 1):
		return n, fmt.Errorf("%w: part decompresses at more than %d:1 (%d bytes from %d)", ErrLimit, lim.MaxRatio, l.n, l.comp.n)
	}
	return n, err
}
