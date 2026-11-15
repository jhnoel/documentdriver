use crate::html::{parse::parse_dialect, input, Atom, BrKind, Fmt, FmtPending, Item, VertAlign, WrapId};
use serde_json::{json, Value};
use xmloxide::html5::{parse_html5_full_with_options, Html5ParseOptions};
use xmloxide::tree::{Document, NodeId, NodeKind};

include!("chrome_elem.rs");

fn opt(v: &Option<String>) -> Value { match v { None => Value::Null, Some(s) => json!(s) } }
fn wrap(w: &WrapId) -> Value { match w { None => Value::Null, Some(id) => json!({"id": opt(id)}) } }
fn pending(p: &Option<FmtPending>) -> Value { match p { None => Value::Null, Some(p) => json!({"id": opt(&p.id), "author": opt(&p.author)}) } }
fn fmt(f: &Fmt) -> Value {
    json!({"resets": f.resets, "b": f.bold, "i": f.italic, "u": f.underline, "s": f.strike,
        "va": match f.vert_align { None => Value::Null, Some(VertAlign::Superscript) => json!("sup"), Some(VertAlign::Subscript) => json!("sub") },
        "hl": f.highlight.map(|h| json!(h.as_str())).unwrap_or(Value::Null),
        "color": f.color.map(|c| json!(c)).unwrap_or(Value::Null),
        "size": f.font_size.map(|c| json!(c)).unwrap_or(Value::Null),
        "font": f.font_family.map(|c| json!(c.as_str())).unwrap_or(Value::Null)})
}
fn item(it: &Item) -> Value {
    match it {
        Item::ParaOpen(p) => json!({"k": "open", "tag": p.tag, "class": opt(&p.class), "bi": wrap(&p.break_ins), "bd": wrap(&p.break_del),
            "num": opt(&p.num), "css": opt(&p.css),
            "list": p.list.as_ref().map(|l| json!({"cont": l.continuation, "id": l.id, "level": l.level, "format": l.format, "start": l.start})).unwrap_or(Value::Null)}),
        Item::ParaClose => json!({"k": "close"}),
        Item::Struct(s) => json!({"k": "struct", "s": s}),
        Item::Atom(a) => match a {
            Atom::Char { ch, fmt: f, fmt_pending, ins, del, link, field } => json!({"k": "char", "ch": *ch as u32, "fmt": fmt(f), "fp": pending(fmt_pending), "ins": wrap(ins), "del": wrap(del), "link": opt(link), "field": opt(field)}),
            Atom::Br { kind, ins, del, link, field } => json!({"k": "br", "kind": match kind { BrKind::Line => "line", BrKind::Page => "page", BrKind::Column => "column" }, "ins": wrap(ins), "del": wrap(del), "link": opt(link), "field": opt(field)}),
            Atom::Tab { ins, del, link, field } => json!({"k": "tab", "ins": wrap(ins), "del": wrap(del), "link": opt(link), "field": opt(field)}),
            Atom::Milestone { start, id } => json!({"k": "milestone", "start": start, "id": id}),
            Atom::Note { endnote, id, inner, ins, del } => json!({"k": "note", "endnote": endnote, "id": opt(id), "inner": inner, "ins": wrap(ins), "del": wrap(del)}),
            Atom::Equation { text, display, ins, del } => json!({"k": "equation", "text": text, "display": display, "ins": wrap(ins), "del": wrap(del)}),
            Atom::Image { src, rid, width, height, alt, ins, del, link, field } => json!({"k": "image", "src": src, "rid": rid,
                "w": width.map(|v| json!(v)).unwrap_or(Value::Null), "h": height.map(|v| json!(v)).unwrap_or(Value::Null), "alt": opt(alt),
                "ins": wrap(ins), "del": wrap(del), "link": opt(link), "field": opt(field)}),
        },
    }
}
fn items(r: Result<Vec<Item>, String>) -> Value {
    match r { Ok(v) => json!({"ok": v.iter().map(item).collect::<Vec<_>>()}), Err(e) => json!({"err": e}) }
}

// The create-section split of build.rs parse_create_sections, recording
// each piece's parse instead of compiling it.
fn sections(html: &str) -> Value {
    let mut rest = html.trim_start();
    let mut out: Vec<Value> = Vec::new();
    let (mut title, mut slots, mut body) = (false, Vec::<Value>::new(), String::new());
    let mut saw_marker = false;
    let mut flush = |title: &mut bool, slots: &mut Vec<Value>, body: &mut String, out: &mut Vec<Value>| {
        out.push(json!({"title": *title, "slots": std::mem::take(slots), "body": items(parse_dialect(body)), "blank": body.trim().is_empty()}));
        *title = false; body.clear();
    };
    loop {
        rest = rest.trim_start();
        if rest.starts_with("<section") {
            let Some(end) = rest.find("/>") else { return json!({"err": "malformed <section/> in create html"}) };
            let tag = &rest[..end + 2];
            let t = tag.contains("first-page");
            rest = rest[end + 2..].trim_start();
            if !body.is_empty() || !slots.is_empty() || saw_marker || title { flush(&mut title, &mut slots, &mut body, &mut out); }
            title = t; saw_marker = true; continue;
        }
        if rest.starts_with("<header") || rest.starts_with("<footer") {
            let scope = if rest.starts_with("<header") { "hdr" } else { "ftr" };
            let tag_name = if scope == "hdr" { "header" } else { "footer" };
            let (kind, blank, inner, consumed) = match parse_leading_chrome_element(rest, tag_name) { Ok(v) => v, Err(e) => return json!({"err": e}) };
            rest = rest[consumed..].trim_start();
            slots.push(json!({"scope": scope, "kind": kind, "items": if blank { json!({"ok": []}) } else { items(parse_dialect(inner)) }}));
            continue;
        }
        if rest.is_empty() { break; }
        let next = rest.find("<section").unwrap_or(rest.len());
        let chunk = rest[..next].trim();
        if !chunk.is_empty() { if !body.is_empty() { body.push('\n'); } body.push_str(chunk); }
        if next == rest.len() { break; }
        rest = rest[next..].trim_start_matches('\n');
    }
    flush(&mut title, &mut slots, &mut body, &mut out);
    if out.len() == 1 && out[0]["blank"] == json!(true) && !out[0]["slots"].as_array().unwrap().is_empty() {
        out[0]["body"] = items(parse_dialect("<p></p>"));
        out[0]["blank"] = json!(false);
    }
    json!({"ok": out})
}

// The parsed tree in the Go golden dump form: comments and the doctype
// dropped, adjacent text merged, MathML elements marked.
fn dump(doc: &Document, n: NodeId, depth: usize, out: &mut String) {
    let mut text = String::new();
    let ind = "  ".repeat(depth);
    let flush = |text: &mut String, out: &mut String| { if !text.is_empty() { out.push_str(&format!("{ind}{}\n", q(text))); text.clear(); } };
    for c in doc.children(n) {
        match &doc.node(c).kind {
            NodeKind::Text { content } | NodeKind::CData { content } => text.push_str(content),
            NodeKind::Element { name, namespace, attributes, .. } => {
                flush(&mut text, out);
                let p = match namespace.as_deref() { Some("http://www.w3.org/1998/Math/MathML") => "math ", Some(_) => "svg ", None => "" };
                out.push_str(&format!("{ind}<{p}{name}>\n"));
                for a in attributes { out.push_str(&format!("{ind}  {}={}\n", a.name, q(&a.value))); }
                dump(doc, c, depth + 1, out);
            }
            _ => {}
        }
    }
    flush(&mut text, out);
}

fn tree(input: &str) -> Value {
    let input = input.replace("<section/>", "<section></section>").replace("<section first-page/>", "<section first-page></section>")
        .replace("<header/>", "<header></header>").replace("<footer/>", "<footer></footer>");
    let input = regex_lite::Regex::new(r"<(image|comment-start|comment-end|header|footer|section|page-number|page-count)\b([^<>]*?)/>").unwrap().replace_all(&input, "<$1$2></$1>").to_string();
    let full = input.trim_start().to_ascii_lowercase();
    let document = full.starts_with("<!doctype") || full.starts_with("<html");
    let parsed = parse_html5_full_with_options(&input, &Html5ParseOptions { scripting: false, fragment_context: if document { None } else { Some("body".into()) } });
    if let Some(e) = parsed.errors.first() { return json!({"err": format!("{e:?}")}); }
    let root = parsed.document.root_element().unwrap();
    let mut out = String::new();
    dump(&parsed.document, root, 0, &mut out);
    json!({"ok": out})
}

pub fn run() {
    let mut buf = String::new();
    std::io::Read::read_to_string(&mut std::io::stdin(), &mut buf).unwrap();
    let cases: Vec<Value> = serde_json::from_str(&buf).unwrap();
    let mut results = Vec::new();
    for c in cases {
        let s = c["input"].as_str().unwrap();
        let frag = items(input::normalize(s, false).and_then(|d| parse_dialect(&d)));
        let doc = match input::normalize(s, true) { Ok(d) => sections(&d), Err(e) => json!({"err": e}) };
        let canon = match input::canonical_mathml(s) { Ok(m) => json!({"ok": m}), Err(e) => json!({"err": e}) };
        results.push(json!({"input": s, "source": c["source"], "tree": tree(s), "frag": frag, "doc": doc, "canon": canon}));
    }
    println!("{}", serde_json::to_string_pretty(&results).unwrap());
}
fn q(s: &str) -> String { format!("\"{}\"", s.replace('\\', "\\\\").replace('"', "\\\"").replace('\n', "\\n")) }
