package math

// canonicalCases are LaTeX inputs covering the grammar, checked for
// canonical stability.
var canonicalCases = []string{
	`x`, `x^{2}+\frac{a}{b}`, `\frac12`, `\sqrt{x}`, `\sqrt[3]{x}`, `\sqrt[{a]b}]{x}`,
	`\sum_{i=1}^{n} x_{i}+y`, `\sum_{i=1}^{n}{x_{i}}`, `\int_0^1 x\,dx`, `\int\limits_0^1 f`, `\sum\nolimits_{k}{k}`,
	`\prod_{k=1}^{m} (k+1)`, `\mathop{\star}_{i}{x}`, `\left(a\middle|b\right)`, `\left.x\right\rangle`,
	`\binom{n}{k}`, `\genfrac{}{}{0pt}{}{a}{b}`, `\genfrac[]{0pt}{}{a}{b}`, `\sfrac{1}{2}`, `\docxlinfrac{a+b}{2}`,
	`\begin{matrix}a & b \\ c & d\end{matrix}`, `\begin{pmatrix}1 & 0 \\ 0 & 1\end{pmatrix}`,
	`\begin{aligned}a &= b \\ c &= d\end{aligned}`, `\begin{cases}1 & x>0 \\ 0 & \text{otherwise}\end{cases}`,
	`\begin{split}a &= b+c \\\docxalnat{1} &= d\end{split}`, `\hat{x}\check{y}\tilde{z}\vec{v}\bar{w}`,
	`\overline{x}\underline{y}`, `\underbrace{a+b}_{n}\overbrace{c}^{m}`, `\overset{def}{=}`, `\underset{x}{y}`,
	`\lim_{n\to\infty}{a_{n}}`, `\sin{x}`, `\sin^{2}{x}`, `\operatorname{tr}{A}`, `\max_{i}{x_{i}}`, `\sin x`,
	`\phantom{x}\vphantom{y}\hphantom{z}\smash{w}`, `\boxed{x}\cancel{y}\bcancel{z}\xcancel{w}`,
	`\mathbb{R}\mathcal{L}\mathfrak{g}\mathsf{x}\mathtt{y}\mathrm{d}x\mathbf{v}\boldsymbol{\alpha}`,
	`\text{if } x\text{ else}`, `a\ b\,c\:d\;e\quad f\qquad g~h\!i`, `x'`, `f''(x)`, `x^{\prime}`,
	`{}_{a}^{b}X`, `\prescript{14}{6}{C}`, `{x}`, `{{x}}_{i}`, `\unicode{x2061}`, `\alpha\beta\Gamma`,
	`a\&b`, `\{x\}`, `\left\{x\middle|y\right\}`, `x_{a}^{b}`, `\docxfunc{f}{x}`, `\docxaccent{\unicode{x304}}{x}`,
	`\docxovergroup{\to}{x}`, `\docxphantom[show,transp]{x}`, `\docxboxed[hideTop,strikeH]{x}`, `\docxbox[opEmu]{x}`,
	`a \\ b`, `\begin{gathered}a \\ b\end{gathered}`, `\text{a  b}`, `\displaystyle x`,
}
