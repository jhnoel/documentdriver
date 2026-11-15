package tree_test

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// maxBytesPerNode is the most heap a parsed tree may hold per node of
// wordBody, on 64-bit platforms, beyond the source it was parsed from:
// the measured number with a small margin. Lower it as the tree shrinks;
// never raise it without a design note (docs/design.md, Scaling and
// memory budget). 32-bit platforms (wasip1) are not gated: their
// pointers are half the size.
const maxBytesPerNode = 70

// wordBody is a document.xml of n paragraphs as docx create writes them:
// a paragraph id, runs with and without properties, a hyperlink.
func wordBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\r\n" +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="w14"><w:body>`)
	for i := range n {
		fmt.Fprintf(&b, `<w:p w14:paraId="%08X"><w:r><w:t xml:space="preserve">Paragraph %d has </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>bold</w:t></w:r>`+
			`<w:r><w:t xml:space="preserve">, </w:t></w:r><w:r><w:rPr><w:i/></w:rPr><w:t>italic &amp; more</w:t></w:r><w:hyperlink r:id="rId%d"><w:r><w:t>a link</w:t></w:r></w:hyperlink></w:p>`, i, i, i)
	}
	b.WriteString(`<w:sectPr/></w:body></w:document>`)
	return []byte(b.String())
}

// treeBytes parses src and returns the live heap the tree holds beyond
// src, and its node count.
func treeBytes(t testing.TB, b []byte) (heap uint64, nodes int) {
	t.Helper()
	src := string(b)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	before := ms.HeapAlloc
	d, err := tree.ParseString(src)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&ms)
	heap = ms.HeapAlloc - min(before, ms.HeapAlloc)
	d.Node().Walk(func(*tree.Node) bool { nodes++; return true })
	runtime.KeepAlive(d)
	return heap, nodes
}

// TestBytesPerNode is the tree size gate: a parsed Word body holds at
// most maxBytesPerNode of heap per node on 64-bit platforms.
func TestBytesPerNode(t *testing.T) {
	if laws.RaceEnabled {
		t.Skip("memory is not measured under the race detector")
	}
	src := wordBody(2000)
	heap, nodes := treeBytes(t, src)
	per := float64(heap) / float64(nodes)
	t.Logf("%d nodes, %d bytes of tree (source %d): %.1f bytes per node", nodes, heap, len(src), per)
	if strconv.IntSize == 64 && per > maxBytesPerNode {
		t.Errorf("tree holds %.1f bytes per node, over the budget of %d", per, maxBytesPerNode)
	}
}

// BenchmarkBytesPerNode reports the tree's heap per node of a Word body
// (bytes/node) and the parse's allocations.
func BenchmarkBytesPerNode(b *testing.B) {
	src := wordBody(2000)
	heap, nodes := treeBytes(b, src)
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := tree.Parse(src); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(heap)/float64(nodes), "bytes/node")
}
