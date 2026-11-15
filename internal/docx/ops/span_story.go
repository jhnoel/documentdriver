package ops

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// textParagraph resolves a paragraph address for a text or bookmark
// operation: a body paragraph (env.paragraph), or else a paragraph of
// another story (headers, footers, footnotes, endnotes, comments) by its
// stored w14:paraId, as read lists bookmark anchors there; "~n" picks the
// n-th of several sharing the id.
func (env *Env) textParagraph(at ParagraphAddress) (part string, p *tree.Node, id string, err error) {
	p, id, err = env.paragraph(at)
	if err == nil {
		return env.WS.DocumentPart(), p, id, nil
	}
	var r *Rejection
	if at.IsAlias() || !errors.As(err, &r) || strings.Contains(r.Msg, "joined") {
		return "", nil, "", err
	}
	want := strings.ToUpper(string(at))
	base, nth := want, 1
	if b, n, ok := strings.Cut(want, "~"); ok {
		k, convErr := strconv.Atoi(n)
		if convErr != nil || k < 2 {
			return "", nil, "", err
		}
		base, nth = b, k
	}
	parts, perr := env.WS.StoryParts()
	if perr != nil {
		return "", nil, "", err
	}
	seen := 0
	for _, name := range parts {
		if name == env.WS.DocumentPart() {
			continue
		}
		doc, terr := env.WS.Tree(name)
		if terr != nil || doc.Root() == nil {
			continue
		}
		var found *tree.Node
		doc.Root().Walk(func(n *tree.Node) bool {
			if found != nil {
				return false
			}
			if model.IsW(n, "p") {
				if v, ok := model.Attr(n, "paraId"); ok && strings.ToUpper(v) == base {
					seen++
					if seen == nth {
						found = n
					}
				}
			}
			return found == nil
		})
		if found != nil {
			return name, found, want, nil
		}
	}
	return "", nil, "", err
}
