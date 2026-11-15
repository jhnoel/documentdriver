//! Reads JSON lines {"text": "<plan json>"} and prints one JSON line each:
//! {"ok":true,"canonical":..,"preview_key":..} or {"ok":false,"diagnostic":..}.
use std::io::{BufRead, Write};
fn main() {
    let stdin = std::io::stdin();
    let mut out = std::io::stdout().lock();
    for line in stdin.lock().lines() {
        let line = line.unwrap();
        if line.trim().is_empty() { continue; }
        let case: serde_json::Value = serde_json::from_str(&line).unwrap();
        let text = case["text"].as_str().unwrap();
        let res = match docxdriver_core::parse_plan_json(text) {
            Ok(plan) => {
                let canonical = plan.canonical_json().unwrap();
                serde_json::json!({"ok": true, "canonical": canonical, "preview_key": plan.preview_key().0})
            }
            Err(d) => serde_json::json!({"ok": false, "diagnostic": d}),
        };
        writeln!(out, "{}", res).unwrap();
    }
}
