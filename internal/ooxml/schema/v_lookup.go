package schema

// This file wires the generated Visio table (v_table.go, from
// tools/vsdorder) into the schema package's lookups. The table is
// keyed by element name, not complex-type name: ChildOrder,
// Slots and ChildType work on "v:Shape", "v:PageSheet" and so on,
// RootType on the element's local name, and ChildType returns
// "v:<child local name>" for every tabulated child.

// vTables returns the generated Visio tables for the lookups that
// dispatch by table prefix.
func vTables() (map[string][]Slot, map[string]map[string]string, map[string]Simple) {
	return vTypes, vAttrs, vSimple
}

// vRootType returns the type of the global Visio element local and
// whether it is one of the 12 roots.
func vRootType(local string) (string, bool) {
	t, ok := vRoots[local]
	return t, ok
}
