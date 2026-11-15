package opc

import (
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// benchDeck is the deck the benchmarks read: $OPC_BENCH_DECK, or the
// minimal fixture.
func benchDeck(b *testing.B) string {
	if d := os.Getenv("OPC_BENCH_DECK"); d != "" {
		return d
	}
	return testdeck.Minimal(b)
}

// BenchmarkOpen opens a package (central directory and the entry checks).
func BenchmarkOpen(b *testing.B) {
	path := benchDeck(b)
	b.ReportAllocs()
	for b.Loop() {
		p, err := Open(path)
		if err != nil {
			b.Fatal(err)
		}
		p.Close()
	}
}

// BenchmarkOpenReadAll opens a package and decompresses every part.
func BenchmarkOpenReadAll(b *testing.B) {
	path := benchDeck(b)
	b.ReportAllocs()
	for b.Loop() {
		p, err := Open(path)
		if err != nil {
			b.Fatal(err)
		}
		for _, name := range p.Names() {
			pt, err := p.Part(name)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := pt.Original(); err != nil {
				b.Fatal(err)
			}
		}
		p.Close()
	}
}
