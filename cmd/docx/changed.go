package main

import (
	"github.com/jhnoel/documentdriver/internal/docx/apply"
	"github.com/jhnoel/documentdriver/internal/docx/read"
)

// changedUnits builds the edit result's changed and deleted lists
// (docs/contract.md, section 4) by reading the document before and after
// the plan through the read side, as pptx does: nothing comes from what
// the operations reported. One entry per unit, in read's shape and under
// read's key for a unit's own address, id:
//
//   - a new or changed paragraph, as {id, html}: its markup in the
//     document read;
//   - a table whose structure or format changed, as read's tables entry;
//   - a section whose properties changed, as read's sections entry;
//   - a new or changed comment thread, as read's comments entry.
//
// Deleted are the paragraphs and comments that are gone. Tables and
// sections are addressed by position, so one that was removed shows as a
// change of those after it.
//
// With idsOnly each entry is the unit's address alone. A plan that
// changed nothing has no output model and no units. before are the
// input's units, read from apply's model of it (Options.Input); the
// output's model and the paragraphs' markup are apply's too.
func changedUnits(before read.Object, res *apply.Result, idsOnly bool) (changed []any, deleted []string) {
	changed, deleted = []any{}, append([]string{}, res.Deleted...)
	if res.Output == nil {
		return changed, deleted
	}
	for _, id := range res.Changed {
		if idsOnly {
			changed = append(changed, id)
			continue
		}
		unit := read.Object{"id": id}
		if html, ok := res.Markup[id]; ok {
			unit["html"] = html
		}
		changed = append(changed, unit)
	}
	now := read.Units(res.Output)
	for _, kind := range []string{"tables", "sections", "comments"} {
		was := byID(before[kind])
		is := byID(now[kind])
		list, _ := now[kind].([]any)
		for _, u := range list {
			unit, ok := u.(read.Object)
			if !ok {
				continue
			}
			id, _ := unit["id"].(string)
			if old, had := was[id]; had && same(old, unit) {
				continue
			}
			if idsOnly {
				changed = append(changed, id)
			} else {
				changed = append(changed, unit)
			}
		}
		if kind == "comments" {
			gone, _ := before[kind].([]any)
			for _, u := range gone {
				if unit, ok := u.(read.Object); ok {
					if id, _ := unit["id"].(string); is[id] == nil {
						deleted = append(deleted, id)
					}
				}
			}
		}
	}
	return changed, deleted
}

// byID indexes a read list's entries by their address.
func byID(list any) map[string]read.Object {
	out := map[string]read.Object{}
	items, _ := list.([]any)
	for _, u := range items {
		if unit, ok := u.(read.Object); ok {
			if id, ok := unit["id"].(string); ok {
				out[id] = unit
			}
		}
	}
	return out
}

// same reports whether two read entries are equal as read prints them.
func same(a, b read.Object) bool {
	x, err1 := read.Encode(a, false)
	y, err2 := read.Encode(b, false)
	return err1 == nil && err2 == nil && x == y
}
