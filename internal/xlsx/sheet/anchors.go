package sheet

import (
	"container/heap"
	"sort"
)

// activeSet answers, during the row-major pass, which anchor's ref holds
// a cell. An anchor joins when its cell is read and leaves once the pass
// is below its ref's last row. Excel never lets two array formulas or
// data tables overlap, so the refs active at one row are disjoint in
// columns: the set is kept sorted by first column and a lookup is a
// binary search. Cost: O(log k) per lookup and O(k) per join or leave,
// with k the anchors active at once; memory O(k).
//
// On a malformed sheet whose refs overlap, a cell is given to the active
// anchor with the greatest first column at or before it if that ref holds
// it, else to none; the answer is deterministic.
type activeSet struct {
	anchors []Anchor // Index.anchors, shared
	byCol   []int32  // indexes into anchors, sorted by Ref first column
	ends    endHeap  // the same indexes, a min-heap by Ref last row
}

// join adds anchor i (already appended to anchors).
func (s *activeSet) join(i int) {
	c := s.anchors[i].Ref.From.Col
	k := sort.Search(len(s.byCol), func(j int) bool { return s.anchors[s.byCol[j]].Ref.From.Col > c })
	s.byCol = append(s.byCol, 0)
	copy(s.byCol[k+1:], s.byCol[k:])
	s.byCol[k] = int32(i)                                                   //nolint:gosec // G115: anchors are bounded by the cells of a part under 4 GiB
	heap.Push(&s.ends, endItem{row: s.anchors[i].Ref.To.Row, id: int32(i)}) //nolint:gosec // G115: as above
}

// advance drops the anchors whose refs end above row.
func (s *activeSet) advance(row int) {
	for len(s.ends) > 0 && s.ends[0].row < row {
		it := heap.Pop(&s.ends).(endItem)
		for j, id := range s.byCol {
			if id == it.id {
				s.byCol = append(s.byCol[:j], s.byCol[j+1:]...)
				break
			}
		}
	}
}

// lookup returns the 1-based index of the anchor whose ref holds
// (row, col) when that cell is not the anchor itself, or 0. The caller
// has advanced to row. Only the active anchor with the greatest first
// column at or before col is checked: on a valid sheet it is the only
// candidate.
func (s *activeSet) lookup(row, col int) int {
	k := sort.Search(len(s.byCol), func(j int) bool { return s.anchors[s.byCol[j]].Ref.From.Col > col })
	if k == 0 {
		return 0
	}
	id := s.byCol[k-1]
	a := &s.anchors[id]
	if !a.Ref.Contains(row, col) || a.Row == row && a.Col == col {
		return 0
	}
	return int(id) + 1
}

type endItem struct {
	row int
	id  int32
}

// endHeap is a min-heap of anchors by the last row of their ref.
type endHeap []endItem

// Len returns the number of items in the heap.
func (h endHeap) Len() int { return len(h) }

// Less reports whether the item at i is ordered before the item at j, by row then id.
func (h endHeap) Less(i, j int) bool {
	if h[i].row != h[j].row {
		return h[i].row < h[j].row
	}
	return h[i].id < h[j].id
}

// Swap exchanges the items at i and j in the heap.
func (h endHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push appends x, an endItem, to the heap.
func (h *endHeap) Push(x any) { *h = append(*h, x.(endItem)) }

// Pop removes and returns the heap's last item, the smallest after heap's swap.
func (h *endHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
