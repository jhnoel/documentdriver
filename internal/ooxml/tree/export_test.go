package tree

// rebuildAll marks every node as changed so that serialisation takes the
// re-emission path everywhere instead of copying spans. A lossless tree must
// still reproduce its input byte for byte.
func rebuildAll(d *Document) {
	d.node.Walk(func(n *Node) bool {
		n.flags |= fDirty
		if n.kind == ElementNode {
			n.flags |= fTagDirty
			n.ownAttrList() // rebuild each attribute from its parts too
		}
		return true
	})
}
