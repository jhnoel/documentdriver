package integrity

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// entry is one zip entry; a directory entry has no data. The data is
// held as a string, the one copy of an entry the checks keep: the
// parsed parts' views refer into it.
type entry struct {
	name string
	data string
}

// archive is a package read once: its entries, whether it is a readable
// zip at all, and the container faults reading it found, in order.
type archive struct {
	entries []entry
	ok      bool
	faults  []Finding
}

// unzip reads a package's entries within the limits (readEntries), for
// every check of it to share.
func unzip(data []byte, l opc.Limits) *archive {
	var f Findings
	entries, ok := readEntries(data, l, &f)
	a := &archive{entries: entries, ok: ok}
	for _, inst := range f.instances {
		a.faults = append(a.faults, inst.Finding)
	}
	return a
}

// errLimit marks an entry whose reading exceeded a limit.
var errLimit = errors.New("exceeds a safety limit")

// readEntries reads every zip entry within the limits. Container faults
// are findings, not errors; it returns false when the bytes are not a
// readable zip archive at all.
func readEntries(data []byte, l opc.Limits, findings *Findings) ([]entry, bool) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !(errors.Is(err, zip.ErrInsecurePath) && zr != nil) {
		findings.Add("", "package: not a readable zip archive")
		return nil, false
	}
	if len(zr.File) > l.MaxEntries {
		findings.Add("", "package: exceeds a safety limit (too many entries)")
		return nil, false
	}
	r := &reader{limits: l}
	entries := make([]entry, 0, len(zr.File))
	for _, f := range zr.File {
		name := entryName(f)
		if f.Flags&0x1 != 0 {
			findings.Add(name, "package: entry is encrypted")
			continue
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			// docxdriver meant to report this, but its zip reader (deflate
			// only) refused such entries first, so it reported "entry
			// cannot be read".
			findings.Add(name, "package: entry is neither stored nor deflated")
			continue
		}
		if strings.HasSuffix(name, "/") {
			if _, err := f.DataOffset(); err != nil {
				findings.Add(name, "package: entry cannot be read")
				continue
			}
			entries = append(entries, entry{name: name})
			continue
		}
		b, err := r.read(f)
		switch {
		case errors.Is(err, errLimit):
			findings.Add(name, "package: entry exceeds a safety limit")
			continue
		case err != nil:
			findings.Add(name, "package: entry cannot be read")
			continue
		}
		entries = append(entries, entry{name: name, data: b})
	}
	return entries, true
}

// entryName decodes an entry name as docxdriver's zip reader does: UTF-8
// when the entry says so (or the name is ASCII), code page 437 otherwise.
func entryName(f *zip.File) string {
	const utf8Flag = 0x800
	if f.Flags&utf8Flag != 0 || ascii.Valid(f.Name) {
		if !utf8.ValidString(f.Name) {
			return strings.ToValidUTF8(f.Name, "�")
		}
		return f.Name
	}
	var b strings.Builder
	for i := 0; i < len(f.Name); i++ {
		c := f.Name[i]
		if c < 0x80 {
			b.WriteByte(c)
			continue
		}
		b.WriteRune(cp437[c-0x80])
	}
	return b.String()
}

// cp437 maps bytes 0x80-0xFF of code page 437 to Unicode.
var cp437 = []rune("ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜ¢£¥₧ƒáíóúñÑªº¿⌐¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■ ")

// reader decompresses entries within the limits, counting the bytes
// actually produced and consumed (never the sizes headers declare). It
// keeps one buffered reader and one decompressor, reset for each entry.
type reader struct {
	limits opc.Limits
	used   int64
	br     *bufio.Reader
	fr     io.ReadCloser
}

// initialCap bounds the buffer allocated up front from an entry's declared
// size, which is attacker data.
const initialCap = 1 << 20

// read decompresses and returns zip entry f within the limits.
func (r *reader) read(f *zip.File) (string, error) {
	raw, err := f.OpenRaw()
	if err != nil {
		return "", err
	}
	if r.br == nil {
		r.br = bufio.NewReader(raw)
	} else {
		r.br.Reset(raw)
	}
	cr := &countingReader{r: r.br}
	var src io.Reader = cr
	if f.Method == zip.Deflate {
		if r.fr == nil {
			r.fr = flate.NewReader(cr)
		} else if err := r.fr.(flate.Resetter).Reset(cr, nil); err != nil {
			return "", err
		}
		src = r.fr
	}
	out := make([]byte, 0, min(f.UncompressedSize64, initialCap)+1)
	for {
		if len(out) == cap(out) {
			out = append(out, 0)[:len(out)]
		}
		k, err := src.Read(out[len(out):cap(out)])
		out = out[:len(out)+k]
		r.used += int64(k)
		n := int64(len(out))
		switch {
		case n > r.limits.MaxPartSize, r.used > r.limits.MaxTotalSize:
			return "", errLimit
		case n >= r.limits.RatioMinSize && n > r.limits.MaxRatio*max(cr.n, 1):
			return "", errLimit
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	// docxdriver's reader checked the CRC only; an entry shorter or longer
	// than its header declares is corrupt too.
	if uint64(len(out)) != f.UncompressedSize64 || crc32.ChecksumIEEE(out) != f.CRC32 {
		return "", zip.ErrChecksum
	}
	// The entries are kept for the whole check, as strings of their exact
	// size (without the buffer's growth slack).
	return string(out), nil
}

// countingReader counts the compressed bytes the decompressor consumed.
type countingReader struct {
	r *bufio.Reader
	n int64
}

// Read reads from the counting reader into p.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ReadByte reads one byte, counting it on success.
func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}
