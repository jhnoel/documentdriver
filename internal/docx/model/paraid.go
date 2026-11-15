package model

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// IsStoredParaID reports whether a stored w14:paraId value is a
// paragraph's address as it stands: exactly eight hexadecimal digits,
// whatever its value (ECMA-376 limits it to below 80000000, but a value
// outside that range still names the paragraph, and rewriting it would
// break the w15:paraId and paraIdParent references to it).
func IsStoredParaID(v string) bool {
	if len(v) != 8 {
		return false
	}
	for i := range len(v) {
		if !isHex(v[i]) {
			return false
		}
	}
	return true
}

// IsValidParaID reports whether v is a w14:paraId the engine may mint:
// eight hexadecimal digits with a value above 00000000 and below
// 80000000.
func IsValidParaID(v string) bool {
	if len(v) != 8 {
		return false
	}
	for i := range len(v) {
		if !isHex(v[i]) {
			return false
		}
	}
	n, err := strconv.ParseUint(v, 16, 32)
	return err == nil && n > 0 && n < 0x8000_0000
}

// isHex reports whether byte c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// FormatParaID is the canonical address form: eight uppercase hex digits.
func FormatParaID(v uint32) string { return fmt.Sprintf("%08X", v) }

// ErrParaIDSpace is returned when every valid paragraph id is taken.
var ErrParaIDSpace = errors.New("paragraph ID space exhausted: no unused w14:paraId remains")

// ParaIDAllocator allocates paragraph ids deterministically against a
// part-local set of occupied values.
type ParaIDAllocator struct {
	occupied map[uint32]bool
}

// NewParaIDAllocator returns an allocator with nothing occupied.
func NewParaIDAllocator() *ParaIDAllocator {
	return &ParaIDAllocator{occupied: map[uint32]bool{}}
}

// Reserve marks v occupied; it reports false when v already was.
func (a *ParaIDAllocator) Reserve(v uint32) bool {
	if a.occupied[v] {
		return false
	}
	a.occupied[v] = true
	return true
}

// Allocate returns the first unused value derived from seed: candidates
// come from SHA-256 of the seed and a probe counter, mapped into
// 1..0x7FFFFFFF, and a collision probes forward.
func (a *ParaIDAllocator) Allocate(seed []byte) (uint32, error) {
	for probe := range uint64(0x7FFF_FFFF) {
		c := paraIDCandidate(seed, probe)
		if a.Reserve(c) {
			return c, nil
		}
	}
	return 0, ErrParaIDSpace
}

// paraIDCandidate derives a paragraph id from seed and probe via SHA-256 in the valid range.
func paraIDCandidate(seed []byte, probe uint64) uint32 {
	h := sha256.New()
	h.Write(seed)
	var p [8]byte
	binary.LittleEndian.PutUint64(p[:], probe)
	h.Write(p[:])
	d := h.Sum(nil)
	return binary.BigEndian.Uint32(d[:4])%0x7FFF_FFFF + 1
}

// paragraphIDSeed is the seed material of one paragraph: the input package
// hash, the part name and the paragraph's structural path.
func paragraphIDSeed(source [32]byte, part, path string) []byte {
	h := sha256.New()
	h.Write(source[:])
	h.Write([]byte("|"))
	h.Write([]byte(part))
	h.Write([]byte("|"))
	h.Write([]byte(path))
	return h.Sum(nil)
}

// ParaID is a paragraph's resolved identity.
type ParaID struct {
	// ID is the address: the stored eight hex digits in upper case, with
	// "~n" for the n-th paragraph sharing them (n from 2), or a
	// provisional id.
	ID  string
	Ord int // 1-based story position, 0 for a nested paragraph
}

// ParaIDIndex is the paragraph-identity resolution of one part.
type ParaIDIndex struct {
	IDs map[*tree.Node]ParaID
	// Repaired holds the paragraphs without a stored w14:paraId: they get
	// a provisional id, which a save writes.
	Repaired map[*tree.Node]bool
	// Unstable holds the paragraphs whose stored w14:paraId is not eight
	// hexadecimal digits: they get a provisional id that is never written
	// (the stored value is not rewritten), so their address changes when
	// the file is saved.
	Unstable map[*tree.Node]bool
	// Repairs counts the paragraphs that need a repair (both kinds).
	Repairs int
	// AllocationErr is set when the id space ran out.
	AllocationErr error
}

// ParaIDValue returns the 32-bit value of an address's stored digits
// (an "~n" suffix ignored), and false for an address that has none.
func ParaIDValue(id string) (uint32, bool) {
	if len(id) < 8 || !IsStoredParaID(id[:8]) {
		return 0, false
	}
	n, err := strconv.ParseUint(id[:8], 16, 32)
	return uint32(n), err == nil
}

// ResolveParagraphIDs resolves every paragraph's identity in the part,
// without changing it. A stored w14:paraId of eight hex digits is the
// paragraph's address whatever its value; a later paragraph sharing it is
// "<id>~2", "<id>~3", .... Every other paragraph (no stored id, or one
// that is not eight hex digits) gets a deterministic provisional id from
// the input package hash, the part name and its structural path
// ("story/<ord>" or "nested/<n>"), never one a stored id holds.
func (x *XML) ResolveParagraphIDs(part string, source [32]byte) *ParaIDIndex {
	idx := &ParaIDIndex{IDs: map[*tree.Node]ParaID{}, Repaired: map[*tree.Node]bool{}, Unstable: map[*tree.Node]bool{}}
	alloc := NewParaIDAllocator()
	all := x.AllParagraphs()
	scope := x.Body()
	if scope == nil {
		scope = x.Root()
	}
	if scope == nil {
		scope = x.Doc.Node()
	}
	ord := map[*tree.Node]int{}
	for _, p := range ParagraphsIn(scope) {
		ord[p.Node] = p.Index
	}
	seen := map[string]int{}
	for _, n := range all {
		v, ok := Attr(n, "paraId")
		if !ok || !IsStoredParaID(v) {
			continue
		}
		parsed, err := strconv.ParseUint(v, 16, 32)
		if err != nil {
			continue
		}
		alloc.Reserve(uint32(parsed))
		id := FormatParaID(uint32(parsed))
		seen[id]++
		if k := seen[id]; k > 1 {
			id += "~" + strconv.Itoa(k)
		}
		idx.IDs[n] = ParaID{ID: id, Ord: ord[n]}
	}
	nested := 0
	for _, n := range all {
		if _, ok := idx.IDs[n]; ok {
			continue
		}
		var path string
		if o, ok := ord[n]; ok {
			path = "story/" + strconv.Itoa(o)
		} else {
			nested++
			path = "nested/" + strconv.Itoa(nested)
		}
		v, err := alloc.Allocate(paragraphIDSeed(source, part, path))
		if err != nil {
			idx.AllocationErr = err
			return idx
		}
		idx.IDs[n] = ParaID{ID: FormatParaID(v), Ord: ord[n]}
		if _, stored := Attr(n, "paraId"); stored {
			idx.Unstable[n] = true
		} else {
			idx.Repaired[n] = true
		}
		idx.Repairs++
	}
	return idx
}
