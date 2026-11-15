package tree

import (
	"bytes"
	"testing"
)

// benchSlide is the test slide repeated inside one root, about 100 shapes.
func benchSlide() []byte {
	var b bytes.Buffer
	b.WriteString(`<r xmlns:a="` + nsA + `" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`)
	for range 100 {
		i := bytes.Index([]byte(slide), []byte("<p:sld"))
		b.WriteString(slide[i:])
	}
	b.WriteString(`</r>`)
	return b.Bytes()
}

func BenchmarkParse(b *testing.B) {
	in := benchSlide()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for b.Loop() {
		if _, err := Parse(in); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckChars benchmarks the tokenizer's whole-source character
// scan, the one hot loop that keeps tree's private isXMLChar (chars.go):
// a scan with multibyte runs and no allocation, which xmlscan's bool
// ValidChars cannot replace (it cannot name the first bad byte).
func BenchmarkCheckChars(b *testing.B) {
	in := string(bytes.Repeat([]byte("café αβ text "), 4096))
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for b.Loop() {
		if off := checkChars(in); off >= 0 {
			b.Fatalf("bad byte at %d", off)
		}
	}
}

func BenchmarkFindAll(b *testing.B) {
	doc, err := Parse(benchSlide())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if len(doc.Root().FindAll(nsA, "off")) == 0 {
			b.Fatal("none found")
		}
	}
}
