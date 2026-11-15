// Package ids is the one place fresh identifiers and the wall clock come
// from. Operations take a Source from their transaction and never call a
// random or clock API themselves (internal/archtest enforces it), so a
// run with a fixed seed and clock is byte-for-byte reproducible.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Source produces fresh values for the output.
type Source interface {
	// GUID returns a new version-4 GUID in the braced upper-case form
	// PowerPoint writes, such as {1B4E28BA-2FA1-4D3B-A3A4-6A1F4E0C9A11}.
	GUID() string
	// Now returns the time to stamp into the output (revision dates).
	Now() time.Time
}

// Default returns the production source: crypto-random GUIDs and the
// real clock.
func Default() Source { return system{} }

type system struct{}

// GUID returns a fresh crypto-random GUID in braced upper-case form.
func (system) GUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("ids: no system randomness: " + err.Error())
	}
	return format(b)
}

// Now returns the current UTC time.
func (system) Now() time.Time { return time.Now().UTC() }

// Fixed returns a deterministic source: its GUIDs are a pure function of
// seed and call order, and Now always returns clock.
func Fixed(seed int64, clock time.Time) Source {
	return &fixed{seed: seed, clock: clock.UTC()}
}

type fixed struct {
	mu    sync.Mutex
	seed  int64
	n     uint64
	clock time.Time
}

// GUID returns the next deterministic GUID derived from the seed and call order.
func (f *fixed) GUID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in [16]byte
	binary.BigEndian.PutUint64(in[:8], uint64(f.seed)) //nolint:gosec // G115: the seed's bit pattern is hashed; reinterpreting a negative seed is intended
	binary.BigEndian.PutUint64(in[8:], f.n)
	f.n++
	sum := sha256.Sum256(in[:])
	var b [16]byte
	copy(b[:], sum[:16])
	return format(b)
}

// Now returns the fixed clock time.
func (f *fixed) Now() time.Time { return f.clock }

// format renders 16 bytes as a version-4 GUID string.
func format(b [16]byte) string {
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := strings.ToUpper(fmt.Sprintf("%x", b[:]))
	return "{" + h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32] + "}"
}
