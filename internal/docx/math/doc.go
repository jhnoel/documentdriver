// Package math converts equations between Office Math (OMML) and LaTeX,
// directly in both directions. LaTeX is the only equation format of the
// docx tool: reads write it, HTML input and the replace_equation plan
// field take it.
//
// # API
//
//   - [OMMLToLaTeX] writes an m:oMath or m:oMathPara in the canonical
//     LaTeX of the grammar below, says whether it is display math, and
//     lists every [Loss]: OMML it holds that the grammar cannot carry.
//   - [LaTeXToOMML] compiles LaTeX to an m:oMath (inline) or m:oMathPara
//     (display) fragment declaring xmlns:m.
//   - [LaTeXToOMMLWith] does the same with [Options]: a w:rPr every run
//     carries, which [RunProperties] picks from the equation a new one
//     replaces (run formatting is not part of the LaTeX surface).
//   - [CanonicalLaTeX] rewrites LaTeX in canonical form.
//   - [SplitDelimiters] and [Delimit] handle the $...$ (inline) and
//     $$...$$ (display) delimiters; \(...\) and \[...\] are read too.
//   - Malformed LaTeX is an error wrapping [ErrInvalidLaTeX], never text
//     in the document; an unknown command's error lists
//     [SupportedCommands].
//
// # Laws
//
// OMML to LaTeX to OMML keeps the equation: what OMMLToLaTeX reads, the
// OMML LaTeXToOMML writes from its output reads back the same, and the
// LaTeX parses to the same equation (the tests check every equation of
// the goldens and the docx corpus). LaTeX to OMML to LaTeX is canonical:
// CanonicalLaTeX of canonical LaTeX is itself. Both are fuzzed.
//
// # Grammar
//
// The Markdown, MathJax and KaTeX dialect, with a few docx extensions
// (\docx...) for what standard LaTeX cannot say. Spaces in the source are
// ignored outside \text. Each OMML construct has one canonical form; the
// parser also reads the listed alternatives.
//
//	OMML                          LaTeX
//	m:oMathPara, several m:oMath  display lines separated by \\ at the top level
//	m:brk (m:alnAt), w:br in m:r  \\ inside \begin{split}...\end{split}; m:alnAt is \\\docxalnat{n}
//	m:aln                         & inside split
//	m:eqArr                       \begin{aligned} rows \\ ... \end{aligned} (also gathered);
//	                              an & in a row is an alignment mark, written &
//	m:d { around an m:eqArr       \begin{cases}...\end{cases}
//	m:m                           \begin{matrix} a & b \\ c & d \end{matrix};
//	                              inside m:d: pmatrix (), bmatrix [], Bmatrix {}, vmatrix ||, Vmatrix ‖‖
//	m:f bar, noBar, skw, lin      \frac (also \dfrac \tfrac \cfrac), \genfrac{}{}{0pt}{}, \sfrac, \docxlinfrac
//	m:d ( ) around a noBar m:f    \binom (also \dbinom \tbinom; \genfrac with delimiters)
//	m:sSub, m:sSup, m:sSubSup     x_{a}, x^{b}, x_{a}^{b}; a prime superscript is x'
//	m:sPre                        \prescript{sup}{sub}{base}
//	m:nary                        \sum \prod \coprod \int \iint \iiint \oint \oiint \oiiint \bigcup
//	                              \bigcap \bigvee \bigwedge \bigodot \bigoplus \bigotimes \biguplus
//	                              \bigsqcup, other characters \mathop{c}; m:limLoc is \limits or
//	                              \nolimits; limits _{...}^{...}; the operand is the {group} after it
//	                              (unbraced: up to + - = < > , ; or a relation at its level)
//	m:rad                         \sqrt{x}, \sqrt[n]{x}
//	m:d                           \left( a \middle| b \right); . is no delimiter; \{ \| \langle
//	                              \lceil \lfloor \llbracket \backslash and the closing forms by name
//	m:acc                         \hat \check \tilde \acute \grave \dot \ddot \dddot \breve \bar
//	                              (U+0305) \vec \mathring \overleftarrow \overleftrightarrow; also
//	                              \widehat \widetilde \overrightarrow; others \docxaccent{c}{x}
//	m:bar                         \overline{x}, \underline{x}
//	m:groupChr                    \overbrace \underbrace \overparen \underparen \overbracket
//	                              \underbracket, others \docxovergroup{c}{x} \docxundergroup{c}{x};
//	                              \underbrace{x}_{n} is m:limLow over it, \overbrace{x}^{n} m:limUpp
//	m:limLow, m:limUpp            \underset{lim}{x}, \overset{lim}{x} (also \stackrel);
//	                              \lim \max \min \sup \inf \det \gcd \Pr \limsup \liminf take
//	                              _{...} and ^{...} as limits
//	m:func                        \sin{x}, \sin^{2}{x}, \lim_{n}{a}, \operatorname{f}{x}: a name
//	                              followed by a {group}; other names \docxfunc{name}{x}
//	m:phant                       \phantom, \vphantom, \hphantom, \smash;
//	                              others \docxphantom[show,zeroWid,zeroAsc,zeroDesc,transp]{x}
//	m:borderBox                   \boxed, \cancel, \bcancel, \xcancel;
//	                              others \docxboxed[hideTop,...,strikeTLBR]{x}
//	m:box                         a {group} standing alone; with properties \docxbox[opEmu,noBreak,diff]{x}
//	m:r                           characters; m:sty p, i, b, bi other than the character's default
//	                              (italic letters, upright others) is \mathrm \mathit \mathbf
//	                              \boldsymbol (also \bm \mathup); m:scr is \mathbb \mathcal (also
//	                              \mathscr) \mathfrak \mathsf \mathtt; an upright run that is an
//	                              operator name is \sin, \lim, ...; m:nor is \text{...} (also
//	                              \textrm \textnormal \mbox); m:lit is \docxlit{...}
//	characters                    Greek and symbol commands (\alpha, \leq, \infty, ...), raw
//	                              Unicode, \unicode{xHEX} for any other (tab, line feed and carriage
//	                              return included); \{ \} \# \$ \% \& \_ for those characters
//	spaces                        "\ " U+0020, \, U+2009, \: U+205F, \; U+2005, \enspace U+2002,
//	                              \quad U+2003, \qquad two of them, ~ U+00A0; \! is read and dropped
//	                              (Office Math has no negative space)
//
// \displaystyle and the other style switches are read and ignored. \\ is
// an error outside display lines, split and the matrix, aligned and cases
// environments; & is an error outside them.
//
// # What LaTeX does not carry
//
// [OMMLToLaTeX] reports, never drops silently. Content: tracked
// revisions inside an equation (m:delText, w:ins), a hidden n-ary limit
// or radical degree that has content, a line break or alignment point
// inside a structure, comment and bookmark anchors and other WordprocessingML
// elements inside an equation, m:groupChr with a non-standard m:vertJc,
// an m:oMath inside an m:oMath, and unknown elements. Formatting: w:rPr
// (fonts, size, colour, ...) and m:ctrlPr, m:naryPr/m:grow, m:dPr/m:grow
// and m:shp, matrix and equation-array spacing and justification
// (m:mPr, m:eqArrPr), m:oMathParaPr/m:jc, and the style of an alignment
// &. The docx read surface lists only the content losses
// ([LossContent]); run formatting is not part of the LaTeX surface (LaTeX
// carries the semantic styles: \mathrm, \mathbf, \boldsymbol, the
// alphabets), an equation an edit leaves unchanged keeps its OMML, and a
// replacement takes the replaced equation's run formatting
// ([RunProperties], [Options]). Escape proposed for a later version: \docxraw{...} carrying the OMML
// of such a piece verbatim, so an equation could round-trip whole.
//
// # Defaults
//
// Where an OMML property is absent, the defaults follow Microsoft's
// omml2mathml.xsl as shipped with Word for Mac 16.115 (the pinned
// ECMA-376 schema declares no defaults for these elements; its
// properties are optional elements with a required val): m:bar and
// m:groupChr sit below unless m:pos is top; m:groupChr's character is
// U+23DF; m:d is ( ) with separator |; a phantom shows its contents unless
// m:show is off; m:nor and the other on/off properties are on when
// present without a value and off only for false, off or 0; and in an
// m:eqArr row an & in the text is an alignment mark. No Word-authored
// file in the corpus omits these properties, so the corpus neither
// confirms nor contradicts them.
//
// # Untrusted input
//
// Nothing here panics on input. The OMML reader recurses over trees the
// tokenizer built (nesting at most tree.MaxDepth) and stops below that
// depth; the LaTeX parser refuses nesting beyond 256 and characters XML
// cannot carry.
package math
