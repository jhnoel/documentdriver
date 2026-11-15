package htmlx

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLargeInputsAreLinear guards against quadratic paths: long text split
// by comments, many attributes, many paragraphs and deep-but-capped
// nesting each parse within timeBudget (quadratic behaviour takes minutes).
func TestLargeInputsAreLinear(t *testing.T) {
	for name, in := range map[string]string{
		"comments":   "<p>" + strings.Repeat("a<!---->", 200000) + "</p>",
		"attributes": "<p" + attrs(50000) + ">x</p>",
		"paragraphs": strings.Repeat("<p>x", 200000),
		"nesting":    strings.Repeat("<span>", MaxDepth-1) + strings.Repeat("x", 100000),
		"cells":      "<table>" + strings.Repeat("<tr><td>x", 50000) + "</table>",
	} {
		start := time.Now()
		if _, err := ParseHTML(in, Options{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if d := time.Since(start); d > timeBudget {
			t.Errorf("%s: %v", name, d)
		}
	}
}

func attrs(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(" x")
		b.WriteString(strings.Repeat("y", i%7))
		b.WriteByte(byte('a' + i%26))
		b.WriteString(strconv.Itoa(i))
	}
	return b.String()
}
