package tree

// A node's children are a linked list: first is the first child; each
// child's next is the one after it (nil for the last), and its prev the
// one before it, except that the first child's prev is the last child.
// So the last child, the one before any child and appending are all
// O(1), and a node holds no slice of its children.

// lastChild returns n's last child, or nil.
func (n *Node) lastChild() *Node {
	if n.first == nil {
		return nil
	}
	return n.first.prev
}

// prevSibling returns the child of n's parent before n, or nil.
func (n *Node) prevSibling() *Node {
	if n.parent == nil || n.parent.first == n {
		return nil
	}
	return n.prev
}

// link inserts the detached node c into n's children before ref, a child
// of n, or at the end when ref is nil.
func (n *Node) link(c, ref *Node) {
	c.parent = n
	switch {
	case n.first == nil:
		n.first, c.prev, c.next = c, c, nil
	case ref == nil:
		last := n.first.prev
		last.next, c.prev, c.next = c, last, nil
		n.first.prev = c
	case ref == n.first:
		c.next, c.prev = ref, ref.prev
		ref.prev = c
		n.first = c
	default:
		p := ref.prev
		p.next, c.prev, c.next = c, p, ref
		ref.prev = c
	}
}

// unlink detaches c, a child of n.
func (n *Node) unlink(c *Node) {
	if c == n.first {
		n.first = c.next
		if n.first != nil {
			n.first.prev = c.prev
		}
	} else {
		c.prev.next = c.next
		if c.next != nil {
			c.next.prev = c.prev
		} else {
			n.first.prev = c.prev
		}
	}
	c.parent, c.prev, c.next = nil, nil, nil
}

// FirstChild returns n's first child, or nil.
func (n *Node) FirstChild() *Node { return n.first }

// LastChild returns n's last child, or nil.
func (n *Node) LastChild() *Node { return n.lastChild() }

// NextSibling returns the child of n's parent after n, or nil.
func (n *Node) NextSibling() *Node { return n.next }

// PrevSibling returns the child of n's parent before n, or nil.
func (n *Node) PrevSibling() *Node { return n.prevSibling() }
