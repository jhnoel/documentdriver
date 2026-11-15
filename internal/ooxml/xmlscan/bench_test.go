package xmlscan_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// grid is a sheetData of 200,000 rows x 10 number cells in the shape
// Excel writes (about 35 bytes a cell), built here because internal/ooxml
// may not import the xlsx test helpers.
var grid = sync.OnceValue(func() string {
	const rows, cols = 200000, 10
	var b strings.Builder
	b.Grow(rows * cols * 36)
	b.WriteString("<sheetData>")
	for r := 1; r <= rows; r++ {
		b.WriteString(`<row r="`)
		b.WriteString(strconv.Itoa(r))
		b.WriteString(`">`)
		for c := 0; c < cols; c++ {
			b.WriteString(`<c r="`)
			b.WriteByte(byte('A' + c))
			b.WriteString(strconv.Itoa(r))
			b.WriteString(`"><v>`)
			b.WriteString(strconv.Itoa(r*1000 + c))
			b.WriteString(`</v></c>`)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString("</sheetData>")
	return b.String()
})

// BenchmarkNext lexes the grid and reads every cell's r, the work of a
// streaming kernel's pass.
func BenchmarkNext(b *testing.B) {
	src := grid()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		cells := 0
		for i := 0; i < len(src); {
			t, err := xmlscan.Next(src, i)
			if err != nil {
				b.Fatal(err)
			}
			if t.IsElement() && t.Local(src) == "c" {
				if _, ok := xmlscan.FindAttr(src, t, "r"); ok {
					cells++
				}
			}
			i = t.End
		}
		if cells != 2000000 {
			b.Fatalf("%d cells", cells)
		}
	}
}

// BenchmarkLexNodeBaseline is the same pass with the edit layer's lexer
// (internal/xlsx/edit/lex.go lexNode and attrValue at efc3994, copied
// below), the code xmlscan replaces.
func BenchmarkLexNodeBaseline(b *testing.B) {
	src := grid()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		cells := 0
		for i := 0; i < len(src); {
			n, ok := baseLexNode(src, i)
			if !ok {
				b.Fatal("lex")
			}
			if n.kind == baseStart && baseLocal(n.name) == "c" {
				if baseHasAttr(src[n.start:n.end], "r") {
					cells++
				}
			}
			i = n.end
		}
		if cells != 2000000 {
			b.Fatalf("%d cells", cells)
		}
	}
}

const (
	baseText = iota
	baseStart
	baseEmpty
	baseEnd
	baseOther
)

type baseNode struct {
	kind, start, end int
	name             string
}

func baseLocal(n string) string {
	if i := strings.IndexByte(n, ':'); i >= 0 {
		return n[i+1:]
	}
	return n
}

func baseSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func baseUntil(src string, i int, end string) (baseNode, bool) {
	j := strings.Index(src[i:], end)
	if j < 0 {
		return baseNode{}, false
	}
	return baseNode{kind: baseOther, start: i, end: i + j + len(end)}, true
}

func baseLexNode(src string, i int) (baseNode, bool) {
	if src[i] != '<' {
		j := strings.IndexByte(src[i:], '<')
		if j < 0 {
			return baseNode{kind: baseText, start: i, end: len(src)}, true
		}
		return baseNode{kind: baseText, start: i, end: i + j}, true
	}
	rest := src[i:]
	switch {
	case strings.HasPrefix(rest, "<!--"):
		return baseUntil(src, i, "-->")
	case strings.HasPrefix(rest, "<![CDATA["):
		return baseUntil(src, i, "]]>")
	case strings.HasPrefix(rest, "<?"):
		return baseUntil(src, i, "?>")
	case strings.HasPrefix(rest, "<!"):
		return baseUntil(src, i, ">")
	case strings.HasPrefix(rest, "</"):
		j := strings.IndexByte(rest, '>')
		if j < 0 {
			return baseNode{}, false
		}
		return baseNode{kind: baseEnd, start: i, end: i + j + 1, name: strings.TrimSpace(rest[2:j])}, true
	}
	k := 1
	for k < len(rest) && !baseSpace(rest[k]) && rest[k] != '>' && rest[k] != '/' {
		k++
	}
	name := rest[1:k]
	for k < len(rest) {
		switch c := rest[k]; c {
		case '"', '\'':
			q := strings.IndexByte(rest[k+1:], c)
			if q < 0 {
				return baseNode{}, false
			}
			k += q + 2
		case '>':
			return baseNode{kind: baseStart, start: i, end: i + k + 1, name: name}, true
		case '/':
			if k+1 < len(rest) && rest[k+1] == '>' {
				return baseNode{kind: baseEmpty, start: i, end: i + k + 2, name: name}, true
			}
			k++
		default:
			k++
		}
	}
	return baseNode{}, false
}

// baseHasAttr is the edit layer's tagAttrs walk, stopping at name.
func baseHasAttr(tag, name string) bool {
	k := 1
	for k < len(tag) && !baseSpace(tag[k]) && tag[k] != '>' && tag[k] != '/' {
		k++
	}
	for k < len(tag) {
		for k < len(tag) && baseSpace(tag[k]) {
			k++
		}
		if k >= len(tag) || tag[k] == '>' || tag[k] == '/' {
			return false
		}
		s := k
		for k < len(tag) && tag[k] != '=' && !baseSpace(tag[k]) {
			k++
		}
		n := tag[s:k]
		for k < len(tag) && (baseSpace(tag[k]) || tag[k] == '=') {
			k++
		}
		if k >= len(tag) {
			return false
		}
		q := tag[k]
		e := strings.IndexByte(tag[k+1:], q)
		if e < 0 {
			return false
		}
		if n == name {
			return true
		}
		k += e + 2
	}
	return false
}
