package opc

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ErrChangedOnDisk is returned by Save when the file was modified by
// someone else after the package was opened.
var ErrChangedOnDisk = errors.New("opc: file changed on disk since it was opened")

// pending collects the members whose bytes changed and checks that every
// changed XML part is well formed. It reports whether anything changed.
func (p *Package) pending() (map[*member]payload, bool, error) { return p.collect(true) }

// collect is pending, checking the changed XML parts only when check is
// set. A patched part is checked by its patch (splice.Patch.Check: one
// scan of the original), never materialised.
func (p *Package) collect(check bool) (map[*member]payload, bool, error) {
	out := map[*member]payload{}
	changed := false
	for _, m := range p.members {
		if m.deleted {
			if m.zf != nil {
				changed = true
			}
			continue
		}
		ch, b, err := m.changed()
		if err != nil {
			return nil, false, err
		}
		if !ch {
			continue
		}
		changed = true
		out[m] = b
		if check && p.IsXML(m.name) {
			var err error
			if b.patch != nil {
				err = b.patch.Check()
			} else {
				err = tree.Check(b.data)
			}
			if err != nil {
				return nil, false, fmt.Errorf("opc: %s is not well formed after editing: %w", m.name, err)
			}
		}
	}
	return out, changed, nil
}

// IsXML reports whether a part holds XML, by content type or extension.
func (p *Package) IsXML(name string) bool {
	ext := strings.ToLower(extension(name))
	if ext == "xml" || ext == "rels" {
		return true
	}
	if ct, err := p.ContentTypes(); err == nil {
		if t, ok := ct.Lookup(name); ok {
			t = strings.ToLower(t)
			if i := strings.IndexByte(t, ';'); i >= 0 {
				t = t[:i]
			}
			return t == ctypes.CTApplicationXml || t == "text/xml" || strings.HasSuffix(t, "+xml")
		}
	}
	return false
}

// Check re-tokenises every modified XML part and reports the first that is
// not well formed.
func (p *Package) Check() error {
	_, _, err := p.pending()
	return err
}

// ErrReopen wraps a failure to reopen the package after Save renamed the
// new file into place. The file on disk holds the saved changes; only the
// in-memory package is unusable.
var ErrReopen = errors.New("opc: saved, but reopening failed")

// Save writes the package back to the file it was opened from. If nothing
// changed it writes nothing at all. Otherwise it checks every modified XML
// part, writes a new zip to a temporary file in the same directory (copying
// untouched members' compressed bytes, recompressing only modified ones),
// syncs it and renames it over the original. On any error before the
// rename the original file is untouched and the temporary file is removed.
// After the rename the package is reopened from the new file; if that
// fails the error wraps ErrReopen, the file is saved and the package is
// closed.
func (p *Package) Save() error {
	saved, err := p.saveInPlace()
	if err != nil || !saved {
		return err
	}
	return p.reopen()
}

// SaveAndClose saves like Save and then closes the package instead of
// reopening it. A nil error means the changes, if any, are on disk; a
// non-nil error means the file on disk is unchanged. Nothing that can fail
// runs after the rename, so the two cannot be confused.
func (p *Package) SaveAndClose() error {
	_, err := p.saveInPlace()
	cerr := p.Close()
	if err != nil {
		return err
	}
	_ = cerr // the file was saved (or untouched); closing the old reader cannot undo that
	return nil
}

// saveInPlace writes the changes over the original file. It reports
// whether it wrote anything.
func (p *Package) saveInPlace() (bool, error) {
	if p.info == nil {
		return false, ErrInMemory
	}
	changes, changed, err := p.pending()
	if err != nil || !changed {
		return false, err
	}
	if err := p.checkUnchangedOnDisk(); err != nil {
		return false, err
	}
	if err := p.write(p.path, changes); err != nil {
		return false, err
	}
	return true, nil
}

// SaveAs writes the package to dst. With no changes the original file's
// bytes are copied verbatim.
func (p *Package) SaveAs(dst string) error {
	if p.info == nil {
		return ErrInMemory
	}
	if same, err := samePath(p.path, dst); err != nil {
		return err
	} else if same {
		return p.Save()
	}
	changes, changed, err := p.pending()
	if err != nil {
		return err
	}
	if !changed {
		return atomicWrite(dst, p.path, p.info.Mode().Perm(), func(w io.Writer) error {
			_, err := io.Copy(w, io.NewSectionReader(p.file, 0, p.info.Size()))
			return err
		})
	}
	return p.write(dst, changes)
}

// samePath reports whether paths a and b name the same file, or false when b is absent.
func samePath(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

// checkUnchangedOnDisk returns ErrChangedOnDisk when the package's file changed since opening.
func (p *Package) checkUnchangedOnDisk() error {
	info, err := os.Stat(p.path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, p.info) || info.Size() != p.info.Size() || !info.ModTime().Equal(p.info.ModTime()) {
		return ErrChangedOnDisk
	}
	return nil
}

// reopen reopens the package's file, replacing the package, or returns ErrReopen on failure.
func (p *Package) reopen() error {
	np, err := OpenWithLimits(p.path, p.budget.limits)
	if err != nil {
		p.Close()
		return fmt.Errorf("%w: %w", ErrReopen, err)
	}
	p.Close()
	*p = *np
	return nil
}

// write builds the new zip into dst atomically.
func (p *Package) write(dst string, changes map[*member]payload) error {
	return atomicWrite(dst, p.path, p.info.Mode().Perm(), func(w io.Writer) error { return p.writeZip(w, changes) })
}

// WriteZip writes the package as it stands (its pending changes included)
// to w as a zip, as Save would write it, without touching any file:
// untouched members are copied as their original raw entries (the same
// compressed bytes, CRC and header, never decompressed), so a damaged
// member nothing read stays as it was and fails nothing. It is how a
// caller reads the working copy through a reader that opens zips.
func (p *Package) WriteZip(w io.Writer) error {
	changes, _, err := p.pending()
	if err != nil {
		return err
	}
	return p.writeZip(w, changes)
}

// writeZip writes the zip of the members with changes applied.
func (p *Package) writeZip(w io.Writer, changes map[*member]payload) error {
	zw := zip.NewWriter(w)
	byZip := make(map[*zip.File]*member, len(p.members))
	for _, m := range p.members {
		if m.zf != nil {
			byZip[m.zf] = m
		}
	}
	put := func(m *member) error {
		var err error
		if pl, ok := changes[m]; ok {
			if pl.patch != nil {
				err = writePatch(zw, p.headerFor(m), pl.patch)
			} else {
				err = writeMember(zw, p.headerFor(m), pl.data)
			}
		} else {
			err = copyMember(zw, m.zf)
		}
		if err != nil {
			return fmt.Errorf("opc: writing %s: %w", m.name, err)
		}
		return nil
	}
	// Original members in zip order (directory entries included), then
	// added parts.
	for _, zf := range p.zr.File {
		m, ok := byZip[zf]
		switch {
		case !ok:
			if err := copyMember(zw, zf); err != nil {
				return fmt.Errorf("opc: copying %s: %w", zf.Name, err)
			}
		case !m.deleted:
			if err := put(m); err != nil {
				return err
			}
		}
	}
	for _, m := range p.members {
		if m.zf == nil && !m.deleted {
			if err := put(m); err != nil {
				return err
			}
		}
	}
	if err := zw.SetComment(p.zr.Comment); err != nil {
		return err
	}
	return zw.Close()
}

// atomicWrite writes a temporary file next to dst, syncs it and renames it
// over dst. The temporary file is removed on failure. Each stage passes
// through fault, which tests use to inject failures; src is the path the
// package was opened from, so that a hook scoped to it sees its saves to
// other files (snapshots) too.
func atomicWrite(dst, src string, perm os.FileMode, fill func(io.Writer) error) (err error) {
	at := faultScope{dst, src}
	dir, base := filepath.Split(dst)
	if dir == "" {
		dir = "."
	}
	if err = fault(at, StageCreate, ""); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name()) //nolint:errcheck // already failing: best-effort removal of the temp file
		}
	}()
	if err = fill(faultWriter{tmp, at}); err != nil {
		return err
	}
	if err = fault(at, StageChmod, tmp.Name()); err != nil {
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if err = fault(at, StageSync, tmp.Name()); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = fault(at, StageClose, tmp.Name()); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = fault(at, StageRename, tmp.Name()); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	// The file is in place: nothing after this point may report failure.
	if fault(at, StageDirSync, dst) == nil {
		if d, derr := os.Open(dir); derr == nil { //nolint:gosec // G304: the path is the caller's argument (library API), never a document-supplied name
			d.Sync() //nolint:errcheck // best effort: persist the rename
			d.Close()
		}
	}
	return nil
}

// headerFor returns the zip header for a modified or added member. A
// modified member keeps its original header (name, method, timestamps,
// attributes, extra fields other than zip64); an added member copies the
// header of [Content_Types].xml, so it matches what the producing
// application wrote.
func (p *Package) headerFor(m *member) zip.FileHeader {
	var fh zip.FileHeader
	if m.zf != nil {
		fh = m.zf.FileHeader
	} else {
		fh = zip.FileHeader{Method: zip.Deflate}
		if ct, ok := p.byName[PartKey(ContentTypesName)]; ok && ct.zf != nil {
			fh = ct.zf.FileHeader
			fh.Comment = ""
			fh.Extra = nil
		}
		fh.Name = strings.TrimPrefix(m.name, "/")
		if !ascii.Valid(fh.Name) && utf8.ValidString(fh.Name) {
			fh.Flags |= 0x800
		}
	}
	fh.Extra = stripZip64(fh.Extra)
	return fh
}

// writeMember compresses data with the header's method and writes it with
// sizes and CRC in the local header (no data descriptor).
func writeMember(zw *zip.Writer, fh zip.FileHeader, data []byte) error {
	var body []byte
	switch fh.Method {
	case zip.Store:
		body = data
	case zip.Deflate:
		var buf bytes.Buffer
		fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		if err := fw.Close(); err != nil {
			return err
		}
		body = buf.Bytes()
	default:
		return fmt.Errorf("unsupported compression method %d", fh.Method)
	}
	fh.Flags &^= 0x8
	fh.CRC32 = crc32.ChecksumIEEE(data)
	fh.CompressedSize64 = uint64(len(body))
	fh.UncompressedSize64 = uint64(len(data))
	w, err := zw.CreateRaw(&fh)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// writePatch is writeMember of a patch's result, streamed: the result is
// never held whole, and neither is its compressed form. A first pass
// compresses into a counter for the CRC and the compressed size the
// local header needs; the second compresses again into the zip. Deflate
// is deterministic, so both passes make the same bytes, and the entry is
// byte for byte the one writeMember writes for the same content. Memory
// over time: the second pass costs a compression, not a copy of the
// member.
func writePatch(zw *zip.Writer, fh zip.FileHeader, p *splice.Patch) error {
	crc := crc32.NewIEEE()
	n := uint64(p.Len()) //nolint:gosec // G115: a length is never negative
	fh.Flags &^= 0x8
	fh.UncompressedSize64 = n
	fh.CompressedSize64 = n
	switch fh.Method {
	case zip.Store:
		if _, err := p.WriteTo(crcWriter{crc, io.Discard}); err != nil {
			return err
		}
	case zip.Deflate:
		var size countWriter
		if err := deflateTo(&size, p, crc); err != nil {
			return err
		}
		fh.CompressedSize64 = uint64(size) //nolint:gosec // G115: a count of bytes is never negative
	default:
		return fmt.Errorf("unsupported compression method %d", fh.Method)
	}
	fh.CRC32 = crc.Sum32()
	w, err := zw.CreateRaw(&fh)
	if err != nil {
		return err
	}
	if fh.Method == zip.Store {
		_, err = p.WriteTo(w)
		return err
	}
	var cw countWriter
	if err := deflateTo(io.MultiWriter(w, &cw), p, nil); err != nil {
		return err
	}
	if uint64(cw) != fh.CompressedSize64 { //nolint:gosec // G115: a count of bytes is never negative
		return fmt.Errorf("deflate gave %d bytes, then %d", fh.CompressedSize64, cw)
	}
	return nil
}

// deflateTo writes p's result deflated, as writeMember compresses, to w,
// summing the uncompressed bytes into crc when it is not nil.
func deflateTo(w io.Writer, p *splice.Patch, crc hash.Hash32) error {
	fw, err := flate.NewWriter(w, flate.DefaultCompression)
	if err != nil {
		return err
	}
	var in io.Writer = fw
	if crc != nil {
		in = crcWriter{crc, fw}
	}
	if _, err := p.WriteTo(in); err != nil {
		return err
	}
	return fw.Close()
}

// countWriter counts the bytes written to it.
type countWriter int64

// Write adds len(b) to the count and reports all bytes written.
func (c *countWriter) Write(b []byte) (int, error) {
	*c += countWriter(len(b))
	return len(b), nil
}

// crcWriter writes to w and sums what it writes. It takes only bytes
// (unlike io.MultiWriter, whose WriteString copies each string whole),
// so a patch streams the original through a small buffer.
type crcWriter struct {
	crc hash.Hash32
	w   io.Writer
}

// Write adds b to the checksum and writes it to the underlying writer.
func (c crcWriter) Write(b []byte) (int, error) {
	c.crc.Write(b) //nolint:errcheck // hash writes never fail
	return c.w.Write(b)
}

// copyMember copies an untouched member's compressed bytes. It uses
// zip.Writer.Copy, except when the central header carries a zip64 extra
// field, which the writer would duplicate; then the same raw copy is done
// with that field removed.
func copyMember(zw *zip.Writer, zf *zip.File) error {
	stripped := stripZip64(zf.Extra)
	if len(stripped) == len(zf.Extra) {
		return zw.Copy(zf)
	}
	r, err := zf.OpenRaw()
	if err != nil {
		return err
	}
	fh := zf.FileHeader
	fh.Extra = stripped
	w, err := zw.CreateRaw(&fh)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// stripZip64 returns extra without zip64 extended-information fields
// (header id 0x0001); the writer adds its own when sizes require it.
func stripZip64(extra []byte) []byte {
	var out []byte
	found := false
	for b := extra; len(b) >= 4; {
		id := binary.LittleEndian.Uint16(b)
		size := int(binary.LittleEndian.Uint16(b[2:]))
		if 4+size > len(b) {
			if found {
				out = append(out, b...)
			}
			break
		}
		if id == 0x0001 {
			if !found {
				out = append([]byte(nil), extra[:len(extra)-len(b)]...)
				found = true
			}
		} else if found {
			out = append(out, b[:4+size]...)
		}
		b = b[4+size:]
	}
	if !found {
		return extra
	}
	return out
}
