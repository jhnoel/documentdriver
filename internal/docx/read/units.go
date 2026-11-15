package read

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// Units returns the units of a document read with --kind document in
// the markup view lists besides paragraphs, as Read shows them: "tables",
// "sections" and, when there are threads, "comments". The edit result
// reads its changed units with it from the models apply already holds,
// rather than reading the whole document twice.
func Units(st *model.Document) Object {
	value := Object{
		"tables":   view.AgentTableInventory(st.XML),
		"sections": view.PruneEmpty(sectionInventory(st, false)),
	}
	if threads, msg := commentThreads(st); msg == "" && len(threads) > 0 {
		value["comments"] = threads
	}
	out := Object{}
	for k, v := range value {
		if v = withoutNulls(v); v != nil {
			out[k] = v
		}
	}
	return out
}
