// Package treeoracle is a frozen copy of package tree as it stood before
// its storage was rebuilt (a *Node per node, children in a slice,
// attributes parsed eagerly). It is imported only by tree's tests: the
// differential fuzz (FuzzDifferential) runs the same parse and edit
// sequences on both and requires the same bytes and the same answers.
// Do not change it except to keep it building; it is the reference.
package treeoracle
