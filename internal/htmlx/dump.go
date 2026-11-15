package htmlx

import (
	"fmt"
	"strings"
)

// Dump renders a tree as an indented outline (elements, attributes and
// quoted text), the form the docx golden files record.
func Dump(n *Node) string {
	var b strings.Builder
	var walk func(*Node, int)
	walk = func(n *Node, d int) {
		for _, c := range n.Kids {
			b.WriteString(strings.Repeat("  ", d))
			if c.Tag == "" {
				b.WriteString(quote(c.Text) + "\n")
				continue
			}
			fmt.Fprintf(&b, "<%s>\n", c.Tag)
			for _, a := range c.Attrs {
				fmt.Fprintf(&b, "%s  %s=%s\n", strings.Repeat("  ", d), a.Name, quote(a.Value))
			}
			walk(c, d+1)
		}
	}
	walk(n, 0)
	return b.String()
}

// quote wraps s in double quotes, escaping only backslash, quote and
// newline, so dumps compare across implementations.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}
