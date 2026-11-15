package view

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// facts is the normalised property set of one view: slide-level keys
// under "", object properties under their ids.
type facts map[string]map[string]string

func (f facts) set(id, k, v string) {
	if f[id] == nil {
		f[id] = map[string]string{}
	}
	f[id][k] = v
}

func (f facts) unset(id, k string) { delete(f[id], k) }

// diff lists the differences between two fact sets.
func diff(a, b facts) []string {
	var out []string
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	var sorted []string
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		keys := map[string]bool{}
		for k := range a[id] {
			keys[k] = true
		}
		for k := range b[id] {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			va, oka := a[id][k]
			vb, okb := b[id][k]
			if va != vb || oka != okb {
				out = append(out, fmt.Sprintf("%s.%s: json=%q(%v) svg=%q(%v)", id, k, va, oka, vb, okb))
			}
		}
	}
	return out
}

// canon re-marshals JSON so that key order and spacing do not matter.
func canon(raw []byte) string {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return "!" + err.Error()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func canonV(v any) string {
	b, _ := json.Marshal(v)
	return canon(b)
}

func numStr(v any) string {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
}

func ptsStr(v any) string {
	arr, _ := v.([]any)
	var s []string
	for _, p := range arr {
		s = append(s, ptStr(p))
	}
	return strings.Join(s, ";")
}

func ptStr(v any) string {
	arr, _ := v.([]any)
	var s []string
	for _, x := range arr {
		s = append(s, numStr(x))
	}
	return strings.Join(s, " ")
}

// colorFromJSON rebuilds the compact colour form (model.ColorString) from
// the JSON colour object.
func colorFromJSON(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprint(v)
	}
	var parts []string
	hex, _ := m["hex"].(string)
	mods, _ := m["mods"].([]any)
	switch {
	case m["scheme"] != nil:
		parts = append(parts, m["scheme"].(string))
	case m["rgb"] != nil:
		parts = append(parts, "srgb:"+m["rgb"].(string))
	case m["sys"] != nil:
		parts = append(parts, "sys:"+m["sys"].(string))
	case m["preset"] != nil:
		parts = append(parts, "prst:"+m["preset"].(string))
	case m["hsl"] != nil:
		parts = append(parts, "hsl:"+strings.ReplaceAll(ptStr(m["hsl"]), " ", ","))
	case m["scrgb"] != nil:
		parts = append(parts, "scrgb:"+strings.ReplaceAll(ptStr(m["scrgb"]), " ", ","))
	default:
		if t, ok := m["type"]; ok {
			return "type:" + t.(string)
		}
		return hex
	}
	for _, md := range mods {
		switch x := md.(type) {
		case string:
			parts = append(parts, x)
		case map[string]any:
			for k, val := range x {
				parts = append(parts, k+"="+numStr(val))
			}
		}
	}
	if hex == "" {
		hex = "#?"
	}
	parts = append(parts, hex)
	return strings.Join(parts, " ")
}

// fromJSON parses the JSON view into facts.
func fromJSON(b []byte) (facts, error) {
	var root map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&root); err != nil {
		return nil, err
	}
	f := facts{}
	f.set("", "slide", root["slide"].(string))
	f.set("", "units", root["units"].(string))
	f.set("", "size", ptStr(root["size"]))
	for k, v := range root["colors"].(map[string]any) {
		f.set("", "color."+k, v.(string))
	}
	if fonts, ok := root["fonts"].(map[string]any); ok {
		for k, v := range fonts {
			f.set("", "font."+strings.TrimPrefix(k, "+"), v.(string))
		}
	}
	f.set("", "hazards", canonV(root["hazards"]))
	f.set("", "notes", canonV(root["notes"]))
	objs, _ := root["objects"].([]any)
	for _, ov := range objs {
		o := ov.(map[string]any)
		id := o["id"].(string)
		kind := o["kind"].(string)
		for k, v := range o {
			switch k {
			case "id":
			case "kind", "type", "name", "descr", "title", "parent", "geom", "first", "from_glue", "to_glue", "image", "html", "svg", "fit":
				f.set(id, k, v.(string))
			case "path":
				// One default path is a string; several, or modes, an
				// array (the SVG's data-paths).
				if p, ok := v.(string); ok {
					f.set(id, k, p)
				} else {
					f.set(id, "paths", canonV(v))
				}
			case "custom":
				f.set(id, k, canonV(v))
			case "crop", "fill_rect":
				f.set(id, k, ptStr(v))
			case "media", "svg_media", "locks":
				f.set(id, k, canonV(v))
			case "hidden", "tx_box":
				f.set(id, k, "1")
			case "z":
				f.set(id, k, numStr(v))
			case "ph":
				m := v.(map[string]any)
				t, _ := m["type"].(string)
				i, _ := m["idx"].(string)
				f.set(id, k, t+":"+i)
			case "adj":
				// Key order matters: re-read in document order.
			case "box", "from", "to":
				f.set(id, k, ptStr(v))
			case "rot":
				f.set(id, k, numStr(v))
			case "flip_h", "flip_v":
				fl := ""
				if o["flip_h"] == true {
					fl += "h"
				}
				if o["flip_v"] == true {
					fl += "v"
				}
				f.set(id, "flip", fl)
			case "elbows", "sites":
				f.set(id, k, ptsStr(v))
			case "fill":
				if kind == "connector" || kind == "picture" {
					continue
				}
				switch x := v.(type) {
				case string:
					f.set(id, "fill", x)
				default:
					f.set(id, "fill", colorFromJSON(x))
				}
			case "line":
				if s, ok := v.(string); ok {
					f.set(id, "line", s)
					continue
				}
				for lk, lv := range v.(map[string]any) {
					switch lk {
					case "color":
						f.set(id, "line.color", colorFromJSON(lv))
					default:
						f.set(id, "line."+lk, numStr(lv))
					}
				}
			case "text":
				for tk, tv := range v.(map[string]any) {
					switch tk {
					case "insets":
						f.set(id, "text.insets", ptStr(tv))
					case "anchor", "wrap":
						f.set(id, "text."+tk, tv.(string))
					default:
						f.set(id, "text."+tk, canonV(tv))
					}
				}
			case "origins", "props", "omitted":
				f.set(id, k, canonV(v))
			case "xml":
				f.set(id, k, v.(string))
			default:
				f.set(id, "unknown."+k, canonV(v))
			}
		}
	}
	// adj in written order, from the raw text.
	if err := adjFromJSON(b, f); err != nil {
		return nil, err
	}
	return f, nil
}

// adjFromJSON reads adj objects keeping key order.
func adjFromJSON(b []byte, f facts) error {
	var root struct {
		Objects []json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return err
	}
	for _, raw := range root.Objects {
		var o struct {
			ID  string          `json:"id"`
			Adj json.RawMessage `json:"adj"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		if o.Adj == nil {
			continue
		}
		d := json.NewDecoder(bytes.NewReader(o.Adj))
		d.UseNumber()
		if _, err := d.Token(); err != nil { // {
			return err
		}
		var parts []string
		for d.More() {
			k, _ := d.Token()
			v, _ := d.Token()
			parts = append(parts, k.(string)+"="+numStr(v))
		}
		f.set(o.ID, "adj", strings.Join(parts, ";"))
	}
	return nil
}

// fromSVG parses the SVG view into facts, with the lossless tree parser.
func fromSVG(b []byte) (facts, error) {
	doc, err := tree.Parse(b)
	if err != nil {
		return nil, err
	}
	f := facts{}
	root := doc.Root()
	a := func(n *tree.Node, k string) string { v, _ := n.Attr(k); return v }
	has := func(n *tree.Node, k string) bool { _, ok := n.Attr(k); return ok }
	f.set("", "slide", a(root, "data-slide"))
	f.set("", "units", a(root, "data-units"))
	vb := strings.Fields(a(root, "viewBox"))
	f.set("", "size", strings.Join(vb[2:], " "))
	f.set("", "hazards", canon([]byte(a(root, "data-hazards"))))
	f.set("", "notes", canon([]byte(a(root, "data-notes"))))
	colors := map[string]string{}
	for _, c := range root.Elements() {
		if c.Name() != "style" {
			continue
		}
		css := c.Text()
		css = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(css), ":root {"), "}"))
		for _, decl := range strings.Split(css, "; ") {
			k, v, ok := strings.Cut(strings.TrimSpace(decl), ": ")
			if !ok {
				continue
			}
			k = strings.TrimPrefix(k, "--")
			if strings.HasPrefix(v, "'") {
				f.set("", "font."+k, strings.ReplaceAll(strings.Trim(v, "'"), `\'`, "'"))
			} else {
				colors[k] = v
				f.set("", "color."+k, v)
			}
		}
	}
	paintOf := func(n *tree.Node, attr, data string) (string, bool) {
		if d, ok := n.Attr(data); ok {
			if d == "?" {
				return "", false
			}
			if !strings.Contains(d, " ") && !strings.HasPrefix(d, "#") {
				return "type:" + d, true // raw fill type
			}
			return d, true
		}
		v, ok := n.Attr(attr)
		if !ok {
			return "", false
		}
		if v == "none" {
			return "none", true
		}
		if s, ok := strings.CutPrefix(v, "var(--"); ok {
			s = strings.TrimSuffix(s, ")")
			h := colors[s]
			if h == "" {
				h = "#?"
			}
			return s + " " + h, true
		}
		return v, true
	}
	var visit func(n *tree.Node, parent string)
	visit = func(n *tree.Node, parent string) {
		for _, e := range n.Elements() {
			id, isObj := e.Attr("data-kind")
			_ = id
			if !isObj {
				continue
			}
			svgObject(f, e, parent, a, has, paintOf)
			if e.Name() == "g" {
				visit(e, a(e, "id"))
			}
		}
	}
	visit(root, "")
	return f, nil
}

type attrFn func(*tree.Node, string) string
type hasFn func(*tree.Node, string) bool
type paintFn func(*tree.Node, string, string) (string, bool)

// transformOf reads "rotate(r cx cy)" and "matrix(a 0 0 d e f)".
func transformOf(t string) (rot string, flip string) {
	for _, part := range strings.SplitAfter(t, ")") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "rotate("):
			rot = strings.Fields(strings.TrimSuffix(strings.TrimPrefix(part, "rotate("), ")"))[0]
		case strings.HasPrefix(part, "matrix("):
			m := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(part, "matrix("), ")"))
			if m[0] == "-1" {
				flip += "h"
			}
			if m[3] == "-1" {
				flip += "v"
			}
		}
	}
	return
}

func svgObject(f facts, e *tree.Node, parent string, a attrFn, has hasFn, paintOf paintFn) {
	id := a(e, "id")
	kind := a(e, "data-kind")
	f.set(id, "kind", kind)
	// The JSON key foo_bar is the attribute data-foo-bar.
	for _, k := range []string{"type", "name", "descr", "title", "geom", "first", "from_glue", "to_glue", "image", "parent", "z", "svg", "fit", "crop", "fill_rect"} {
		if attr := "data-" + strings.ReplaceAll(k, "_", "-"); has(e, attr) {
			f.set(id, k, a(e, attr))
		}
	}
	for k, attr := range map[string]string{"media": "data-media", "svg_media": "data-svg-media", "locks": "data-locks"} {
		if has(e, attr) {
			f.set(id, k, canon([]byte(a(e, attr))))
		}
	}
	if !has(e, "data-parent") && parent != "" {
		f.set(id, "parent", parent)
	}
	if has(e, "data-hidden") {
		f.set(id, "hidden", "1")
	}
	if has(e, "data-tx-box") {
		f.set(id, "tx_box", "1")
	}
	if has(e, "data-ph") {
		f.set(id, "ph", a(e, "data-ph"))
	}
	if has(e, "data-adj") {
		f.set(id, "adj", a(e, "data-adj"))
	}
	for _, k := range []string{"origins", "props", "omitted"} {
		if has(e, "data-"+k) {
			f.set(id, k, canon([]byte(a(e, "data-"+k))))
		}
	}
	if has(e, "data-box") {
		f.set(id, "box", a(e, "data-box"))
	}
	if has(e, "data-rot") {
		f.set(id, "rot", a(e, "data-rot"))
	}
	if has(e, "data-flip") {
		f.set(id, "flip", a(e, "data-flip"))
	}
	stroke := func(n *tree.Node) {
		if a(n, "stroke") == "none" && !has(n, "data-stroke") {
			f.set(id, "line", "none")
			return
		}
		if c, ok := paintOf(n, "stroke", "data-stroke"); ok {
			f.set(id, "line.color", c)
		}
		if has(n, "stroke-width") {
			f.set(id, "line.w", a(n, "stroke-width"))
		}
		for _, k := range []string{"dash", "cap", "head", "tail"} {
			if has(n, "data-"+k) {
				f.set(id, "line."+k, a(n, "data-"+k))
			}
		}
	}
	fill := func(n *tree.Node) {
		if c, ok := paintOf(n, "fill", "data-fill"); ok {
			f.set(id, "fill", c)
		}
	}
	switch e.Name() {
	case "line", "polyline", "path":
		connFacts(f, id, e, a)
		stroke(e)
		return
	}
	// A g element.
	if kind == "group" {
		fill(e)
	}
	if kind == "connector" {
		stroke(e)
	}
	var sites []string
	for _, c := range e.Elements() {
		if _, isObj := c.Attr("data-kind"); isObj {
			continue
		}
		geomEl := c
		rotSrc := c
		if c.Name() == "g" {
			// Custom path: <g transform><svg x y w h><path/></svg></g>.
			inner := c.Elements()
			if len(inner) == 0 {
				continue
			}
			if kind == "picture" {
				continue // the picture's image; its rect sibling carries the box and outline
			}
			geomEl = inner[0]
		}
		switch geomEl.Name() {
		case "rect", "image", "ellipse", "svg", "line", "polyline", "path":
			if has(geomEl, "data-nobox") {
				if kind != "picture" {
					fill(geomEl)
				}
				stroke(geomEl)
				continue
			}
			if geomEl.Name() == "image" {
				continue // its rect sibling carries the outline
			}
			if has(geomEl, "data-path") {
				f.set(id, "path", a(geomEl, "data-path"))
			}
			customFacts(f, id, geomEl, a, has)
			switch {
			case has(geomEl, "data-box"):
				f.set(id, "box", a(geomEl, "data-box"))
			default:
				f.set(id, "box", a(geomEl, "x")+" "+a(geomEl, "y")+" "+a(geomEl, "width")+" "+a(geomEl, "height"))
			}
			r, fl := transformOf(a(rotSrc, "transform"))
			paintEl := geomEl
			if geomEl.Name() == "svg" {
				p := geomEl.Elements()[0]
				f.set(id, "path", a(p, "d"))
				customFacts(f, id, p, a, has)
				_, fl = transformOf(a(p, "transform"))
				paintEl = p
			}
			switch geomEl.Name() {
			case "line", "polyline", "path":
				connFacts(f, id, geomEl, a)
				r, fl = a(geomEl, "data-rot"), a(geomEl, "data-flip")
			}
			if r != "" {
				f.set(id, "rot", r)
			}
			if fl != "" {
				f.set(id, "flip", fl)
			}
			if kind != "picture" {
				fill(paintEl)
			}
			stroke(paintEl)
		case "circle":
			sites = append(sites, a(c, "cx")+" "+a(c, "cy"))
		case "foreignObject":
			var html bytes.Buffer
			for _, ch := range c.Children() {
				html.Write(ch.Bytes())
			}
			f.set(id, "html", html.String())
			for _, k := range []string{"anchor", "wrap", "insets"} {
				if has(c, "data-"+k) {
					f.set(id, "text."+k, a(c, "data-"+k))
				}
			}
			if has(c, "data-body") {
				var m map[string]any
				if err := json.Unmarshal([]byte(a(c, "data-body")), &m); err != nil {
					f.set(id, "text.body", "unparseable: "+err.Error())
				}
				for k, v := range m {
					f.set(id, "text."+k, canonV(v))
				}
			}
		case "desc":
			f.set(id, "xml", c.Text())
		}
	}
	if len(sites) > 0 {
		f.set(id, "sites", strings.Join(sites, ";"))
	}
}

// customFacts reads a custom geometry's data-paths (which replaces the
// path's d as the JSON view's "path" array) and data-custom.
func customFacts(f facts, id string, e *tree.Node, a attrFn, has func(*tree.Node, string) bool) {
	if has(e, "data-paths") {
		f.unset(id, "path")
		f.set(id, "paths", canon([]byte(a(e, "data-paths"))))
	}
	if has(e, "data-custom") {
		f.set(id, "custom", canon([]byte(a(e, "data-custom"))))
	}
}

// connFacts reads endpoints and elbows from a connector drawing element.
func connFacts(f facts, id string, e *tree.Node, a attrFn) {
	switch e.Name() {
	case "line":
		f.set(id, "from", a(e, "x1")+" "+a(e, "y1"))
		f.set(id, "to", a(e, "x2")+" "+a(e, "y2"))
	case "polyline":
		pts := strings.Fields(a(e, "points"))
		conv := func(s string) string { return strings.ReplaceAll(s, ",", " ") }
		f.set(id, "from", conv(pts[0]))
		f.set(id, "to", conv(pts[len(pts)-1]))
		var el []string
		for _, p := range pts[1 : len(pts)-1] {
			el = append(el, conv(p))
		}
		if len(el) > 0 {
			f.set(id, "elbows", strings.Join(el, ";"))
		}
	case "path":
		f.set(id, "from", a(e, "data-from"))
		f.set(id, "to", a(e, "data-to"))
	}
}
