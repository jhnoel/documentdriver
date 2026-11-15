package opc

import (
	"bytes"
	"fmt"
	"io"
)

// Sealed is a package's bytes as a save writes them, built once in
// memory: every modified XML part serialised and checked for
// well-formedness, recompressed, and the zip assembled (untouched members
// copied as their raw entries). A caller reads the bytes (its checks and
// its result) and then writes those same bytes with CommitSealed, so what
// it checked is what is saved.
type Sealed struct {
	data    []byte
	changed bool
}

// Bytes returns the sealed package. The slice is the Sealed's; callers
// must not modify it.
func (s *Sealed) Bytes() []byte { return s.data }

// Changed reports whether the sealed package differs from the file the
// package was opened from.
func (s *Sealed) Changed() bool { return s.changed }

// Seal serialises the package as it stands into memory, with the checks
// Save makes before it writes (every modified XML part well formed). With
// no changes the bytes are the original file's.
func (p *Package) Seal() (*Sealed, error) { return p.seal(true) }

// SealUnchecked is Seal without the well-formedness check of the modified
// XML parts, for a caller that parses every changed part of the sealed
// bytes itself and refuses one that is not well formed (the docx commit
// gate), so that each part is tokenised once.
func (p *Package) SealUnchecked() (*Sealed, error) { return p.seal(false) }

// seal seals the package, checking modified XML parts only when check is true.
func (p *Package) seal(check bool) (*Sealed, error) {
	if p.info == nil {
		return nil, ErrInMemory
	}
	changes, changed, err := p.collect(check)
	if err != nil {
		return nil, err
	}
	if !changed {
		b := make([]byte, p.info.Size())
		if _, err := io.ReadFull(io.NewSectionReader(p.file, 0, p.info.Size()), b); err != nil {
			return nil, fmt.Errorf("opc: reading %s: %w", p.path, err)
		}
		return &Sealed{data: b}, nil
	}
	// The sealed package is about the original's size: one buffer that
	// size, not a doubling series of copies of it.
	var buf bytes.Buffer
	buf.Grow(int(p.info.Size()) + 64<<10)
	if err := p.writeZip(&buf, changes); err != nil {
		return nil, err
	}
	return &Sealed{data: buf.Bytes(), changed: true}, nil
}

// CommitSealed writes s, sealed from p, to dst ("" or the package's own
// file: in place), atomically as Save and SaveAs do, and closes the
// package. In place, an unchanged package writes nothing, and a file
// someone else modified since it was opened is refused with
// ErrChangedOnDisk. The package must not have changed since s was
// sealed. A nil error means the bytes are on disk; any error means the
// target is untouched.
func (p *Package) CommitSealed(s *Sealed, dst string) error {
	if p.info == nil {
		return ErrInMemory
	}
	defer p.Close()
	inPlace := dst == ""
	if !inPlace {
		same, err := samePath(p.path, dst)
		if err != nil {
			return err
		}
		inPlace = same
	}
	if inPlace {
		if !s.changed {
			return nil
		}
		if err := p.checkUnchangedOnDisk(); err != nil {
			return err
		}
		dst = p.path
	}
	return atomicWrite(dst, p.path, p.info.Mode().Perm(), func(w io.Writer) error {
		_, err := w.Write(s.data)
		return err
	})
}

// DropContents releases every part's cached content (original string,
// tree, replacement bytes, patch) and the parsed content types and
// relationships, for a caller that has sealed the package and now only
// checks the sealed bytes and commits them (CommitSealed needs nothing
// else): a large part's original is not held beside the sealed bytes and
// the checks of them. After it the package must only be committed or
// closed; reading or editing a part again is an error.
func (p *Package) DropContents() {
	for _, m := range p.members {
		m.orig, m.loaded, m.doc, m.data, m.patch = "", false, nil, nil, nil
		m.dropped = true
	}
	p.ct, p.rels = nil, nil
}
