package apply

import (
	"regexp"
	"testing"
)

var listID = regexp.MustCompile(`data-list=\\"[0-9]+\\"`)

// TestListSetLaws runs list.set's change-mode law on the law inputs and
// write fixtures: a new list over two paragraphs, a bullet list with a
// start, leaving lists, and joining the document's first list instance.
func TestListSetLaws(t *testing.T) {
	eachInput(t, deleteInputs(t), func(t *testing.T, in string, _ int) {
		ids := bodyParagraphIDs(t, in)
		if len(ids) < 2 {
			return
		}
		at := `"at":["` + ids[0] + `","` + ids[1] + `"]`
		for name, ops := range map[string]string{
			"new":    `[{"op":"list.set",` + at + `,"format":"decimal"}]`,
			"bullet": `[{"op":"list.set",` + at + `,"format":"bullet","level":1,"start":3}]`,
			"remove": `[{"op":"list.set",` + at + `,"remove":true}]`,
			"join":   `[{"op":"list.set",` + at + `,"num_id":1}]`,
		} {
			s := chromeSuite("list.set "+name, ops)
			if name == "join" {
				// Track mode joins an instance nothing refers to yet
				// through a clone of its own (OpDef: direct equals
				// accepted track up to the relabelling of new list ids).
				s.Project = func(path string) (string, error) {
					p, err := projectNoSource(path)
					return listID.ReplaceAllString(p, `data-list=\"N\"`), err
				}
			}
			s.Check(t, in)
		}
	})
}
