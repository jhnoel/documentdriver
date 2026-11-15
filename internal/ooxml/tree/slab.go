package tree

import "strings"

// A parse allocates its nodes from a slab: fixed chunks of nodes, each
// one allocation, never grown, moved or reused. A node is never taken
// back, even when it is removed from its tree: the edit core's registry,
// its record of created nodes and SourceOf provenance keep removed nodes,
// so their slots must stay theirs. A chunk lives while any of its nodes
// is referenced.
//
// Each parse has its own slab, as it has its own source: a fragment
// (ParseFragment, so each copy the edit core makes and each
// mc:AlternateContent branch it parses) and each re-parse of a part.
// Nodes built in code (NewElement, NewText, NewRaw's) are allocated one
// by one and have no source.

// The first chunk of a small source holds as many nodes as it can have
// (nodeBound), so that a small fragment wastes almost nothing: the edit
// core parses one per copy, and create one per run, and a chunk keeps
// all its nodes alive while one of them is. Later chunks, and the first
// of a large source, hold slabMax nodes.
const (
	slabMax   = 1024
	slabSmall = 16 << 10 // bytes: a source this large or smaller is bounded first
)

// slab hands out the nodes of one parse.
type slab struct {
	chunk []Node // the current chunk; its unused tail is free
	next  int    // size of the chunk after this one
}

// newSlab returns the slab for parsing src.
func newSlab(src string) slab {
	if len(src) > slabSmall {
		return slab{next: slabMax}
	}
	return slab{next: min(nodeBound(src), slabMax)}
}

// nodeBound is at least the number of nodes src parses into, when it
// parses: each node but text starts with a "<" that does not start an
// end tag, and each text node starts the source or follows a ">".
func nodeBound(src string) int {
	n := strings.Count(src, "<") - strings.Count(src, "</")
	if src != "" && src[0] != '<' {
		n++
	}
	for i := 0; i < len(src); {
		j := strings.IndexByte(src[i:], '>')
		if j < 0 {
			break
		}
		i += j + 1
		if i < len(src) && src[i] != '<' {
			n++
		}
	}
	return n
}

// newNode returns a zeroed node from s.
func (s *slab) newNode() *Node {
	if len(s.chunk) == 0 {
		s.chunk = make([]Node, max(s.next, 1))
		s.next = slabMax
	}
	n := &s.chunk[0]
	s.chunk = s.chunk[1:]
	return n
}
