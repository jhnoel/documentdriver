package math

// symbolNames maps characters to the LaTeX command written for them; the
// parser reads each command back as its character. Every character has
// one command, so writing and reading are inverse.
var symbolNames = map[rune]string{
	// Greek.
	'\u03b1': "alpha", '\u03b2': "beta", '\u03b3': "gamma", '\u03b4': "delta", '\u03f5': "epsilon",
	'\u03b5': "varepsilon", '\u03b6': "zeta", '\u03b7': "eta", '\u03b8': "theta", '\u03d1': "vartheta",
	'\u03b9': "iota", '\u03ba': "kappa", '\u03bb': "lambda", '\u03bc': "mu", '\u03bd': "nu",
	'\u03be': "xi", '\u03c0': "pi", '\u03d6': "varpi", '\u03c1': "rho", '\u03f1': "varrho",
	'\u03c3': "sigma", '\u03c2': "varsigma", '\u03c4': "tau", '\u03c5': "upsilon", '\u03d5': "phi",
	'\u03c6': "varphi", '\u03c7': "chi", '\u03c8': "psi", '\u03c9': "omega",
	'\u0393': "Gamma", '\u0394': "Delta", '\u0398': "Theta", '\u039b': "Lambda", '\u039e': "Xi",
	'\u03a0': "Pi", '\u03a3': "Sigma", '\u03a5': "Upsilon", '\u03a6': "Phi", '\u03a8': "Psi",
	'\u03a9': "Omega",
	// Binary operators.
	'\u00b1': "pm", '\u2213': "mp", '\u00d7': "times", '\u00f7': "div", '\u22c5': "cdot",
	'\u2218': "circ", '\u2219': "bullet", '\u2295': "oplus", '\u2296': "ominus", '\u2297': "otimes",
	'\u2298': "oslash", '\u2299': "odot", '\u222a': "cup", '\u2229': "cap", '\u2228': "lor",
	'\u2227': "land", '\u2216': "setminus", '\u22c6': "star", '\u2020': "dagger", '\u2021': "ddagger",
	'\u2294': "sqcup", '\u2293': "sqcap", '\u228e': "uplus", '\u2240': "wr", '\u2210': "amalg",
	// Relations.
	'\u2264': "leq", '\u2265': "geq", '\u2260': "neq", '\u2248': "approx", '\u2261': "equiv",
	'\u223c': "sim", '\u2243': "simeq", '\u2245': "cong", '\u221d': "propto", '\u226a': "ll",
	'\u226b': "gg", '\u2282': "subset", '\u2283': "supset", '\u2286': "subseteq", '\u2287': "supseteq",
	'\u2208': "in", '\u2209': "notin", '\u220b': "ni", '\u22a5': "perp", '\u2225': "parallel",
	'\u2223': "mid", '\u22a2': "vdash", '\u22a3': "dashv", '\u227a': "prec", '\u227b': "succ",
	'\u2aaf': "preceq", '\u2ab0': "succeq", '\u2250': "doteq", '\u2192': "to", '\u2190': "leftarrow",
	'\u2194': "leftrightarrow", '\u21d2': "Rightarrow", '\u21d0': "Leftarrow", '\u21d4': "Leftrightarrow",
	'\u21a6': "mapsto", '\u27f6': "longrightarrow", '\u27f5': "longleftarrow", '\u27f9': "Longrightarrow",
	'\u27f8': "Longleftarrow", '\u27fa': "iff", '\u2191': "uparrow", '\u2193': "downarrow",
	'\u21d1': "Uparrow", '\u21d3': "Downarrow", '\u2197': "nearrow", '\u2198': "searrow",
	'\u2199': "swarrow", '\u2196': "nwarrow", '\u21aa': "hookrightarrow", '\u21a9': "hookleftarrow",
	'\u21cc': "rightleftharpoons", '\u2202': "partial",
	// Other symbols.
	'\u221e': "infty", '\u2207': "nabla", '\u2200': "forall", '\u2203': "exists", '\u2204': "nexists",
	'\u2205': "emptyset", '\u00ac': "neg", '\u2113': "ell", '\u210f': "hbar", '\u211c': "Re",
	'\u2111': "Im", '\u2135': "aleph", '\u2118': "wp", '\u2032': "prime", '\u2026': "ldots",
	'\u22ef': "cdots", '\u22ee': "vdots", '\u22f1': "ddots", '\u2220': "angle", '\u25b3': "triangle",
	'\u2234': "therefore", '\u2235': "because", '\u221a': "surd",
	'\u27e8': "langle", '\u27e9': "rangle", '\u2308': "lceil", '\u2309': "rceil", '\u230a': "lfloor",
	'\u230b': "rfloor", '\u27e6': "llbracket", '\u27e7': "rrbracket", '\u2016': "|", '\\': "backslash",
	'\u00b0': "degree",
	// Characters special to LaTeX.
	'{': "{", '}': "}", '#': "#", '$': "$", '%': "%", '_': "_",
}

// symbolAliases are commands the parser also reads, for the characters
// above or for characters with no command of their own.
var symbolAliases = map[string]rune{
	"le": '\u2264', "ge": '\u2265', "ne": '\u2260', "leqslant": '\u2264', "geqslant": '\u2265',
	"rightarrow": '\u2192', "gets": '\u2190', "implies": '\u27f9', "lnot": '\u00ac',
	"wedge": '\u2227', "vee": '\u2228', "dots": '\u2026', "cdotp": '\u22c5', "colon": ':',
	"vert": '|', "Vert": '\u2016', "lbrace": '{', "rbrace": '}', "lbrack": '[', "rbrack": ']',
	"lvert": '|', "rvert": '|', "lVert": '\u2016', "rVert": '\u2016', "ast": '*', "lt": '<', "gt": '>', "&": '&',
}

// spaceCommands map spacing characters to commands; "~" is the no-break
// space. \qquad is two em spaces.
var spaceCommands = map[rune]string{
	' ': `\ `, '\u2009': `\,`, '\u205f': `\:`, '\u2005': `\;`, '\u2003': `\quad`, '\u2002': `\enspace`,
	'\u00a0': "~",
}

// spaceAliases are the other spacing commands the parser reads.
var spaceAliases = map[string]string{
	",": "\u2009", ":": "\u205f", ";": "\u2005", " ": " ", "quad": "\u2003", "qquad": "\u2003\u2003",
	"enspace": "\u2002", "thinspace": "\u2009", "medspace": "\u205f", "thickspace": "\u2005",
	"!": "", "negthinspace": "",
}

// naryNames are the large operators; other n-ary characters are written
// \mathop{c}.
var naryNames = map[string]string{
	"\u2211": "sum", "\u220f": "prod", "\u2210": "coprod", "\u222b": "int", "\u222c": "iint",
	"\u222d": "iiint", "\u222e": "oint", "\u222f": "oiint", "\u2230": "oiiint", "\u22c3": "bigcup",
	"\u22c2": "bigcap", "\u22c1": "bigvee", "\u22c0": "bigwedge", "\u2a00": "bigodot",
	"\u2a01": "bigoplus", "\u2a02": "bigotimes", "\u2a04": "biguplus", "\u2a06": "bigsqcup",
}

// accentNames are the accents; other accent characters are written
// \docxaccent{c}{x}.
var accentNames = map[string]string{
	"\u0302": "hat", "\u030c": "check", "\u0303": "tilde", "\u0301": "acute", "\u0300": "grave",
	"\u0307": "dot", "\u0308": "ddot", "\u20db": "dddot", "\u0306": "breve", "\u0305": "bar",
	"\u20d7": "vec", "\u030a": "mathring", "\u20d6": "overleftarrow", "\u20e1": "overleftrightarrow",
}

// accentAliases are other accent commands the parser reads.
var accentAliases = map[string]string{
	"widehat": "\u0302", "widetilde": "\u0303", "overrightarrow": "\u20d7", "widecheck": "\u030c",
}

// groupNames are the grouping characters with their position.
var groupNames = map[string]struct {
	chr string
	top bool
}{
	"overbrace": {"\u23de", true}, "underbrace": {"\u23df", false},
	"overparen": {"\u23dc", true}, "underparen": {"\u23dd", false},
	"overbracket": {"\u23b4", true}, "underbracket": {"\u23b5", false},
}

// operatorNames are written \name for an upright run of that name. The
// limit names take limits below and above (\lim_{x} is m:limLow).
var operatorNames = map[string]bool{
	"sin": true, "cos": true, "tan": true, "cot": true, "sec": true, "csc": true, "arcsin": true,
	"arccos": true, "arctan": true, "sinh": true, "cosh": true, "tanh": true, "coth": true,
	"ln": true, "log": true, "lg": true, "exp": true, "arg": true, "deg": true, "dim": true,
	"hom": true, "ker": true,
}

var limitNames = map[string]bool{
	"lim": true, "max": true, "min": true, "sup": true, "inf": true, "det": true, "gcd": true,
	"Pr": true, "limsup": true, "liminf": true,
}

// styleCommands set m:sty; scriptCommands set m:scr.
var styleCommands = map[string]string{
	"mathrm": "p", "mathit": "i", "mathbf": "b", "boldsymbol": "bi", "bm": "bi", "mathup": "p",
}

var scriptCommands = map[string]string{
	"mathbb": "double-struck", "mathcal": "script", "mathscr": "script", "mathfrak": "fraktur",
	"mathsf": "sans-serif", "mathtt": "monospace",
}

var styleNames = map[string]string{"p": "mathrm", "i": "mathit", "b": "mathbf", "bi": "boldsymbol"}

var scriptNames = map[string]string{
	"double-struck": "mathbb", "script": "mathcal", "fraktur": "mathfrak", "sans-serif": "mathsf",
	"monospace": "mathtt",
}

// delimiterNames are the delimiters \left and \right write by name.
var delimiterNames = map[string]string{
	"{": `\{`, "}": `\}`, "\u2016": `\|`, "\u27e8": `\langle`, "\u27e9": `\rangle`,
	"\u2308": `\lceil`, "\u2309": `\rceil`, "\u230a": `\lfloor`, "\u230b": `\rfloor`,
	"\u27e6": `\llbracket`, "\u27e7": `\rrbracket`, "\\": `\backslash`, "": ".",
}
