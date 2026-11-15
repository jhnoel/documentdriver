package view

import (
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// maxListDepth bounds the nesting of rendered HTML lists. Word lists have
// nine levels; a deeper run of ever-increasing levels renders its deeper
// items inline at the limit instead of recursing further.
const maxListDepth = 256

// itoa returns i in decimal.
func itoa(i int) string { return strconv.Itoa(i) }

// escapeText escapes &, < and > (docxdriver's escape_text).
func escapeText(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeAttr also escapes '"' (docxdriver's escape_attr).
func escapeAttr(s string) string {
	if !strings.ContainsAny(s, "&<>\"") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// body is rendered block content: the blocks joined by newlines and their
// spans. A filtered render (blockFilter) leaves the blocks it skips
// empty: skipped[i] is set and the block spans no HTML.
type body struct {
	html    string
	blocks  []Block
	skipped []bool
	// nums are, in agent mode, the span of each block's data-number
	// attribute relative to the block, or -1, -1 (renderParagraphGroup).
	nums [][2]int
}

// blockFilter selects the top-level body blocks a render emits: the
// effective block children (a w:p or w:tbl) it holds. A nil filter emits
// every block. A skipped block still counts as rendered for the state
// that runs across the document (the data-table numbering, paragraph
// grouping and list grouping), so every block emitted is byte for byte
// what an unfiltered render gives; a list holding a selected block is
// emitted whole.
type blockFilter map[*tree.Node]bool

// effectiveStoryChildren returns container's children named one of names,
// looking through content controls, custom XML and alternate content (its
// effective branch), but never into paragraphs, tables or properties. It
// recurses only over a tree the tokenizer built.
func effectiveStoryChildren(container *tree.Node, names ...string) []*tree.Node {
	var out []*tree.Node
	var collect func(*tree.Node)
	collect = func(n *tree.Node) {
		for c := range n.ChildrenSeq() {
			switch {
			case isAnyW(c, names):
				out = append(out, c)
			case model.IsLocal(c, "AlternateContent"):
				if b := model.AlternateContentBranch(c); b != nil {
					collect(b)
				}
			case model.IsW(c, "sdt"):
				if sc := model.ChildW(c, "sdtContent"); sc != nil {
					collect(sc)
				}
			case model.IsW(c, "sdtContent"), model.IsW(c, "customXml"):
				collect(c)
			}
		}
	}
	collect(container)
	return out
}

// isAnyW reports whether n is a W element named one of names.
func isAnyW(n *tree.Node, names []string) bool {
	for _, name := range names {
		if model.IsW(n, name) {
			return true
		}
	}
	return false
}

// effectiveBlockChildren returns container's paragraph and table block children.
func effectiveBlockChildren(container *tree.Node) []*tree.Node {
	return effectiveStoryChildren(container, "p", "tbl")
}

// renderBlocks renders the block children of a container (a body, header,
// footer, note or cell).
func (c *partCtx) renderBlocks(container *tree.Node) body {
	return c.renderBlockList(effectiveBlockChildren(container), nil)
}

// renderBlockList renders body blocks in order. tableN, when not nil,
// numbers top-level tables document-wide (data-table).
func (c *partCtx) renderBlockList(children []*tree.Node, tableN *int) body {
	return c.renderFiltered(children, tableN, nil)
}

// blockItem is one block of a block list before it renders: a table or
// a paragraph group, its data-table number, and whether it is emitted.
type blockItem struct {
	nodes  []*tree.Node // the paragraph group; nil for a table
	table  *tree.Node
	number int
	emit   bool
}

// renderFiltered renders the body blocks of children that keep passes
// (all of them when keep is nil), and an empty, skipped block for each
// other one. It lays the blocks out first, numbering tables and grouping
// paragraphs as a full render does, widens the selection to whole lists,
// then renders what is selected.
func (c *partCtx) renderFiltered(children []*tree.Node, tableN *int, keep blockFilter) body {
	v := c.r.view
	items := make([]blockItem, 0, len(children))
	for i := 0; i < len(children); {
		n := children[i]
		switch {
		case model.IsW(n, "tbl") && v != Markup && tableSettledAway(n, v):
			if tableN != nil {
				*tableN++
			}
			i++
			continue
		case model.IsW(n, "tbl"):
			it := blockItem{table: n, emit: keep == nil || keep[n]}
			if tableN != nil {
				it.number = *tableN
				*tableN++
			}
			items = append(items, it)
			i++
		case v == Original && model.RejectedInsertionVanishes(n):
			i++
			continue
		default:
			group := []*tree.Node{n}
			for v != Markup && breakHidden(group[len(group)-1], v) && i+len(group) < len(children) && model.IsW(children[i+len(group)], "p") {
				group = append(group, children[i+len(group)])
			}
			i += len(group)
			it := blockItem{nodes: group, emit: keep == nil}
			for _, p := range group {
				it.emit = it.emit || keep[p]
			}
			items = append(items, it)
		}
	}
	if keep != nil {
		c.widenToLists(items)
	}
	var html strings.Builder
	blocks := make([]Block, 0, len(items))
	var skipped []bool
	if keep != nil {
		skipped = make([]bool, 0, len(items))
	}
	var nums [][2]int
	for _, it := range items {
		var paras []*tree.Node
		var text strings.Builder
		num := [2]int{-1, -1}
		if it.table != nil {
			if it.emit {
				number := it.number
				n := &number
				if tableN == nil {
					n = nil
				}
				c.renderTable(it.table, &text, &paras, n)
			}
		} else {
			paras = it.nodes
			if it.emit {
				ns, ne := c.renderParagraphGroup(it.nodes, &text)
				if c.r.agent {
					num = [2]int{ns, ne}
				}
			}
		}
		if html.Len() > 0 {
			html.WriteByte('\n')
		}
		start := html.Len()
		html.WriteString(text.String())
		blocks = append(blocks, Block{Start: start, End: html.Len(), Paragraphs: paras})
		if keep != nil {
			skipped = append(skipped, !it.emit)
		}
		if c.r.agent {
			nums = append(nums, num)
		}
	}
	return c.groupLists(body{html: html.String(), blocks: blocks, skipped: skipped, nums: nums})
}

// widenToLists selects every block of a run of numbered paragraph blocks
// (the lists groupLists builds) when it selects one of them.
func (c *partCtx) widenToLists(items []blockItem) {
	isList := func(it blockItem) bool {
		if it.table != nil {
			return false
		}
		_, ok := c.lists[it.nodes[len(it.nodes)-1]]
		return ok
	}
	for i := 0; i < len(items); {
		if !isList(items[i]) {
			i++
			continue
		}
		begin, emit := i, false
		for i < len(items) && isList(items[i]) {
			emit = emit || items[i].emit
			i++
		}
		if emit {
			for j := begin; j < i; j++ {
				items[j].emit = true
			}
		}
	}
}

// lead returns a block's last paragraph, or nil when it has none.
func lead(b Block) *tree.Node {
	if len(b.Paragraphs) == 0 {
		return nil
	}
	return b.Paragraphs[len(b.Paragraphs)-1]
}

// listBlock reports whether a block is a numbered paragraph block.
func (c *partCtx) listBlock(in body, b Block) bool {
	l := lead(b)
	if l == nil {
		return false
	}
	_, ok := c.lists[l]
	return ok && !strings.HasPrefix(in.html[b.Start:b.End], "<table")
}

// groupLists groups runs of numbered paragraph blocks into nested HTML
// lists; a list is one block carrying every paragraph inside it.
func (c *partCtx) groupLists(in body) body {
	var html strings.Builder
	var blocks []Block
	var skipped []bool
	for i := 0; i < len(in.blocks); {
		if html.Len() > 0 {
			html.WriteByte('\n')
		}
		start := html.Len()
		var paras []*tree.Node
		skip := in.skipped != nil && in.skipped[i]
		if c.listBlock(in, in.blocks[i]) {
			begin := i
			for i < len(in.blocks) && c.listBlock(in, in.blocks[i]) {
				i++
			}
			if !skip {
				cursor := begin
				for cursor < i {
					level := c.lists[lead(in.blocks[cursor])].Level
					c.renderList(in, &cursor, i, level, &html, 0)
				}
			}
			for _, b := range in.blocks[begin:i] {
				paras = append(paras, b.Paragraphs...)
			}
		} else {
			b := in.blocks[i]
			html.WriteString(in.html[b.Start:b.End])
			paras = append(paras, b.Paragraphs...)
			i++
		}
		blocks = append(blocks, Block{Start: start, End: html.Len(), Paragraphs: paras})
		if in.skipped != nil {
			skipped = append(skipped, skip)
		}
	}
	return body{html: html.String(), blocks: blocks, skipped: skipped}
}

// renderList writes the list at level starting at cursor into out, advancing cursor.
func (c *partCtx) renderList(in body, cursor *int, end int, level uint32, out *strings.Builder, depth int) {
	first := c.lists[lead(in.blocks[*cursor])]
	tag := "ol"
	if first.Format == "bullet" {
		tag = "ul"
	}
	kind := "1"
	switch first.Format {
	case "lowerLetter":
		kind = "a"
	case "upperLetter":
		kind = "A"
	case "lowerRoman":
		kind = "i"
	case "upperRoman":
		kind = "I"
	}
	out.WriteString("<" + tag + ` data-list="` + escapeAttr(first.ID) + `" data-level="` + strconv.FormatUint(uint64(level), 10) + `"`)
	if tag == "ol" {
		out.WriteString(` start="` + strconv.FormatInt(first.Value, 10) + `" type="` + kind + `"`)
		if kind == "1" && first.Format != "decimal" {
			out.WriteString(` data-format="` + escapeAttr(first.Format) + `"`)
		}
	}
	out.WriteByte('>')
	identity := first.ID
	for *cursor < end {
		b := in.blocks[*cursor]
		info := c.lists[lead(b)]
		if info.Level != level || info.ID != identity {
			break
		}
		out.WriteString(`<li data-marker="` + escapeAttr(c.markers[lead(b)]) + `">`)
		if in.nums != nil && in.nums[*cursor][0] >= 0 {
			// The number repeats the item's marker: agent markup drops it.
			n := in.nums[*cursor]
			out.WriteString(in.html[b.Start : b.Start+n[0]])
			out.WriteString(in.html[b.Start+n[1] : b.End])
		} else {
			out.WriteString(in.html[b.Start:b.End])
		}
		*cursor++
		for *cursor < end {
			next := c.lists[lead(in.blocks[*cursor])]
			if next.Continuation && next.Level == level && next.ID == identity {
				nb := in.blocks[*cursor]
				out.WriteString(in.html[nb.Start:nb.End])
				*cursor++
				continue
			}
			if next.Level <= level {
				break
			}
			if depth >= maxListDepth {
				nb := in.blocks[*cursor]
				out.WriteString(in.html[nb.Start:nb.End])
				*cursor++
				continue
			}
			c.renderList(in, cursor, end, next.Level, out, depth+1)
		}
		out.WriteString("</li>")
	}
	out.WriteString("</" + tag + ">")
}

// breakHidden reports whether the paragraph's terminating break is hidden
// in view v: an inserted break from the original, a deleted one from the
// final.
func breakHidden(p *tree.Node, v View) bool {
	switch v {
	case Final:
		return model.ParagraphMarkDel(p) != nil
	case Original:
		return model.ParagraphMarkIns(p) != nil
	}
	return false
}

// paragraphTag is h1..h6 for the styles Heading1..Heading6, else p.
func paragraphTag(p *tree.Node, original bool) string {
	ppr := model.PropertyBag(p, "pPr", original)
	if ppr == nil {
		return "p"
	}
	ps := model.ChildW(ppr, "pStyle")
	if ps == nil {
		return "p"
	}
	style, ok := model.Attr(ps, "val")
	if !ok {
		return "p"
	}
	rest, ok := strings.CutPrefix(style, "Heading")
	if !ok {
		return "p"
	}
	n, ok := model.ParseU32(rest)
	if !ok || n < 1 || n > 6 {
		return "p"
	}
	return "h" + strconv.FormatUint(uint64(n), 10)
}

// renderParagraphGroup writes a paragraph group. In agent mode it
// returns the span of the data-number attribute of a p element in out,
// which a list item drops (AgentHTML), else -1, -1.
func (c *partCtx) renderParagraphGroup(group []*tree.Node, out *strings.Builder) (numStart, numEnd int) {
	v := c.r.view
	agent := c.r.agent
	original := v == Original
	ld := group[len(group)-1]
	tag := paragraphTag(ld, original)
	out.WriteString("<" + tag)
	if id, ok := c.paraIDs[ld]; ok {
		out.WriteString(` id="` + escapeAttr(id.ID) + `"`)
		if id.Ord >= 0 { // another story's paragraph has no body position
			if agent {
				c.sink.omitted = true
			} else {
				out.WriteString(` data-ord="` + itoa(id.Ord) + `"`)
			}
		}
	}
	class, hasClass := c.r.styles.paragraphStyle(ld, original)
	if hasClass {
		if agent && c.r.styles.hasDef && class == c.r.styles.def {
			c.sink.omitted = true
		} else {
			out.WriteString(` data-style="` + escapeAttr(class) + `"`)
			if agent {
				if dec, ok := c.r.decoded(class); ok {
					c.sink.classes[dec] = true
				}
			}
		}
	}
	numStart, numEnd = -1, -1
	if m := c.markers[ld]; m != "" {
		if agent && tag == "p" {
			numStart = out.Len()
		}
		out.WriteString(` data-number="` + escapeAttr(m) + `"`)
		if numStart >= 0 {
			numEnd = out.Len()
		}
	}
	if v == Markup {
		if m := model.ParagraphMarkIns(ld); m != nil {
			out.WriteString(` data-break-ins="` + escapeAttr(model.AttrOr(m, "id")) + `"`)
		}
		if m := model.ParagraphMarkDel(ld); m != nil {
			out.WriteString(` data-break-del="` + escapeAttr(model.AttrOr(m, "id")) + `"`)
		}
	}
	inherited := c.commonRunCSS(group)
	decls := c.r.styles.paragraphDecls(ld, original)
	inherited.declare(decls)
	if css := cssString(decls); css != "" {
		if agent {
			out.WriteString(c.agentStyle(css, decls, class, hasClass))
		} else {
			out.WriteString(` style="` + escapeAttr(css) + `"`)
		}
	}
	out.WriteByte('>')
	e := &emitter{out: out, c: c, view: v, inherited: inherited, fieldInstr: fieldInstructions(group)}
	for _, p := range group {
		if len(group) > 1 {
			if id, ok := c.paraIDs[p]; ok {
				out.WriteString(`<span data-paragraph="` + escapeAttr(id.ID) + `">`)
			}
		}
		if len(group) > 1 && e.fieldDepth > 0 {
			out.WriteString(`<span data-field="` + escapeAttr(e.fieldActive) + `"` + c.editable() + `>`)
		}
		e.para, e.eqIndex, e.phIndex = p, nil, nil
		e.content(p, nil, nil)
		if len(group) > 1 {
			e.finish()
			if e.fieldDepth > 0 {
				out.WriteString("</span>")
			}
			out.WriteString("</span>")
		}
	}
	e.finish()
	if e.fieldDepth > 0 {
		e.fieldDepth = 0
		if len(group) == 1 {
			out.WriteString("</span>")
		}
	}
	out.WriteString("</" + tag + ">")
	return numStart, numEnd
}

// editable is a field's contenteditable attribute, which agent markup
// leaves out.
func (c *partCtx) editable() string {
	if c.r.agent {
		c.sink.omitted = true
		return ""
	}
	return ` contenteditable="false"`
}

// commonRunCSS is the inheritable run formatting every run of the group
// shares, which the paragraph carries so its bare text inherits it. A run or
// hyperlink counts for the paragraph it is nearest to: one walk per
// paragraph that does not descend into a nested paragraph (a text box's)
// collects the paragraph's runs and whether it has a hyperlink.
func (c *partCtx) commonRunCSS(group []*tree.Node) inheritFmt {
	original := c.r.view == Original
	hasLink := false
	runs := c.runBuf[:0]
	for _, p := range group {
		p.Walk(func(n *tree.Node) bool {
			switch {
			case n == p:
			case model.IsW(n, "p"):
				return false
			case model.IsW(n, "hyperlink"):
				hasLink = true
			case model.IsW(n, "r"):
				runs = append(runs, c.r.styles.runFormat(n, original))
			}
			return true
		})
	}
	c.runBuf = runs[:0]
	if len(runs) == 0 {
		return inheritFmt{}
	}
	common := inheritable(runs[0], hasLink)
	for _, f := range runs[1:] {
		common.intersect(inheritable(f, hasLink))
	}
	return common
}

// fieldInstructions maps each complex field's begin fldChar in the group to
// its trimmed instruction text (the w:instrText met before its separate or
// end); nested fields attach text to the innermost open field.
func fieldInstructions(group []*tree.Node) map[*tree.Node]string {
	type open struct {
		begin *tree.Node
		instr strings.Builder
		code  bool
	}
	out := map[*tree.Node]string{}
	var stack []*open
	for _, p := range group {
		p.Walk(func(n *tree.Node) bool {
			if n == p {
				return true
			}
			switch {
			case model.IsW(n, "fldChar"):
				switch model.AttrOr(n, "fldCharType") {
				case "begin":
					stack = append(stack, &open{begin: n, code: true})
				case "separate":
					if len(stack) > 0 {
						stack[len(stack)-1].code = false
					}
				case "end":
					if len(stack) > 0 {
						top := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						out[top.begin] = rustTrim(top.instr.String())
					}
				}
			case model.IsW(n, "instrText"):
				if len(stack) > 0 && stack[len(stack)-1].code {
					stack[len(stack)-1].instr.WriteString(model.TextContent(n))
				}
			}
			return true
		})
	}
	for _, s := range slices.Backward(stack) {
		out[s.begin] = rustTrim(s.instr.String())
	}
	return out
}

// cellInfo is a cell's span facts from its w:tcPr.
type cellInfo struct {
	node          *tree.Node
	gridStart     uint32
	colspan       uint32
	restart, cont bool
}

// cellInfos returns row's cells with grid column, span and vertical-merge facts.
func cellInfos(row *tree.Node) []cellInfo {
	var cells []cellInfo
	var grid uint32
	for _, cell := range effectiveStoryChildren(row, "tc") {
		ci := cellInfo{node: cell, gridStart: grid, colspan: 1}
		if tcpr := model.ChildW(cell, "tcPr"); tcpr != nil {
			for prop := range tcpr.ChildrenSeq() {
				switch {
				case model.IsW(prop, "gridSpan"):
					ci.colspan = 1
					if v, ok := model.ParseU32(model.AttrOr(prop, "val")); ok && v >= 1 {
						ci.colspan = v
					}
				case model.IsW(prop, "vMerge"):
					if model.AttrOr(prop, "val") == "restart" && hasAttr(prop, "val") {
						ci.restart = true
					} else {
						ci.cont = true
					}
				}
			}
		}
		cells = append(cells, ci)
		grid += ci.colspan
	}
	return cells
}

// rowspanOf returns the rowspan of the cell at gridStart in row ri.
func rowspanOf(rows [][]cellInfo, ri int, gridStart uint32) uint32 {
	span := uint32(1)
	for _, row := range rows[ri+1:] {
		cont := false
		for _, cell := range row {
			if cell.gridStart == gridStart && cell.cont {
				cont = true
				break
			}
		}
		if !cont {
			break
		}
		span++
	}
	return span
}

// tableSettledAway reports whether settling v's revisions removes every
// row of the table.
func tableSettledAway(t *tree.Node, v View) bool {
	rows := childrenW(t, "tr")
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if !model.RowGoes(r, v == Final) {
			return false
		}
	}
	return true
}

// renderTable writes table t as HTML, appending its paragraphs to paras.
func (c *partCtx) renderTable(t *tree.Node, out *strings.Builder, paras *[]*tree.Node, tableN *int) {
	v := c.r.view
	original := v == Original
	var rows [][]cellInfo
	for _, r := range effectiveStoryChildren(t, "tr") {
		rows = append(rows, cellInfos(r))
	}
	rowNodes := childrenW(t, "tr")
	if tableN != nil {
		out.WriteString(`<table data-table="` + itoa(*tableN) + `"` + c.tableAttrs(t, "tblPr", original) + ">")
		*tableN++
	} else {
		out.WriteString("<table" + c.tableAttrs(t, "tblPr", original) + ">")
	}
	covered := map[uint32]uint32{}
	out.WriteString("<tbody>")
	for ri, row := range rows {
		var rowNode *tree.Node
		if ri < len(rowNodes) {
			rowNode = rowNodes[ri]
		}
		if v != Markup && rowNode != nil && model.RowGoes(rowNode, v == Final) {
			continue
		}
		out.WriteString("\n<tr")
		if rowNode != nil {
			out.WriteString(c.tableAttrs(rowNode, "trPr", original))
		}
		out.WriteByte('>')
		type run struct{ col, left uint32 }
		var pending []run
		for _, cell := range row {
			if cell.cont && covered[cell.gridStart] > 0 {
				continue
			}
			out.WriteString("\n<td" + c.tableAttrs(cell.node, "tcPr", original))
			if cell.colspan > 1 {
				out.WriteString(` colspan="` + strconv.FormatUint(uint64(cell.colspan), 10) + `"`)
			}
			if cell.restart {
				if span := rowspanOf(rows, ri, cell.gridStart); span > 1 {
					out.WriteString(` rowspan="` + strconv.FormatUint(uint64(span), 10) + `"`)
					pending = append(pending, run{cell.gridStart, span - 1})
				}
			}
			out.WriteByte('>')
			rendered := c.renderBlockList(effectiveBlockChildren(cell.node), nil)
			for _, b := range rendered.blocks {
				*paras = append(*paras, b.Paragraphs...)
			}
			out.WriteString("\n" + rendered.html + "\n</td>")
		}
		for col, left := range covered {
			if left <= 1 {
				delete(covered, col)
			} else {
				covered[col] = left - 1
			}
		}
		for _, p := range pending {
			covered[p.col] = p.left
		}
		out.WriteString("\n</tr>")
	}
	out.WriteString("\n</tbody></table>")
}

// tableAttrs is a table, row or cell's address (its id, as the contract
// has an element's address) and supported CSS.
func (c *partCtx) tableAttrs(n *tree.Node, tag string, original bool) string {
	var b strings.Builder
	if h, ok := c.tables[n]; ok {
		b.WriteString(` id="` + h + `"`)
	}
	if css := tableCSS(n, tag, original); css != "" {
		b.WriteString(` style="` + escapeAttr(css) + `"`)
	}
	return b.String()
}
