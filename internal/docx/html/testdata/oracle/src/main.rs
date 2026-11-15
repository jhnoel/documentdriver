//! Throwaway differential harness: runs docxdriver 0.5.2's HTML input path
//! (input.rs, parse.rs, mathml_omml.rs included by path, read-only) over
//! inputs and prints JSON results for the Go side to compare.
#![allow(dead_code, unused_imports, unused_variables, clippy::all)]
mod api;
mod html {
    include!("html_types.rs");
    pub mod styles { include!("styles.rs"); }
    pub mod surface { include!("surface.rs"); }
    pub mod omml_mathml { pub const MATH_NS: &str = "http://www.w3.org/1998/Math/MathML"; }
    #[path = "DOCXDRIVER/crates/docxdriver-core/src/html/input.rs"]
    pub mod input;
    #[path = "DOCXDRIVER/crates/docxdriver-core/src/html/parse.rs"]
    pub mod parse;
    #[path = "DOCXDRIVER/crates/docxdriver-core/src/html/mathml_omml.rs"]
    pub mod mathml_omml;
}
mod harness;
fn main() { harness::run(); }
