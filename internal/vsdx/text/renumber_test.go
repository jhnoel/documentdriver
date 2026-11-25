package text

import (
	"reflect"
	"strconv"
	"testing"
)

// renumberRand is a small xorshift generator for the property test.
type renumberRand uint64

// next returns a value in [0, n).
func (r *renumberRand) next(n int) int {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = renumberRand(x)
	return int(x % uint64(n))
}

// randomText returns a body with random row references (including -1
// and rows reused out of order) and formats for some of the rows.
func randomText(r *renumberRand) (Body, Formats) {
	f := Formats{Char: map[int]CharFormat{}, Para: map[int]ParaFormat{}}
	chars := []CharFormat{{}, {Bold: "on"}, {Italic: "on"}, {Size: "20"}, {Bold: "on", Size: "20"}}
	paras := []ParaFormat{{}, {Align: "left"}, {Bullet: true}, {SpaceAfter: "6pt"}}
	for ix := range 4 {
		if r.next(3) > 0 {
			f.Char[ix] = chars[r.next(len(chars))]
		}
		if r.next(3) > 0 {
			f.Para[ix] = paras[r.next(len(paras))]
		}
	}
	var b Body
	for p := range 1 + r.next(4) {
		para := Para{PP: r.next(5) - 1}
		for k := range r.next(5) {
			run := Run{CP: r.next(5) - 1, Text: "r" + strconv.Itoa(p) + strconv.Itoa(k), Field: -1}
			if r.next(6) == 0 {
				run.Br, run.Text = true, ""
			}
			para.Runs = append(para.Runs, run)
		}
		b.Paras = append(b.Paras, para)
	}
	b.Terminated = r.next(2) == 0
	return b, f
}

// TestRenumberProperty checks Renumber on random texts: every run keeps
// its text and the format it showed, every paragraph its format; cp and
// pp values run 0, 1, 2... in text order, a new value exactly where the
// format changes; and Renumber of its own output changes nothing.
func TestRenumberProperty(t *testing.T) {
	r := renumberRand(0x9e3779b97f4a7c15)
	for i := range 2000 {
		b, f := randomText(&r)
		nb, nf := Renumber(b, f)
		lastCP, lastPP := -1, -1
		var lastChar CharFormat
		var lastPara ParaFormat
		for pi, p := range b.Paras {
			np := nb.Paras[pi]
			want := paraFormat(f, p.PP)
			if got := nf.Para[np.PP]; got != want {
				t.Fatalf("case %d para %d: format %+v, want %+v", i, pi, got, want)
			}
			if np.PP != lastPP && (np.PP != lastPP+1 || (pi > 0 && want == lastPara)) || np.PP == lastPP && want != lastPara {
				t.Fatalf("case %d para %d: pp %d after %d (format change %v)", i, pi, np.PP, lastPP, want != lastPara)
			}
			lastPP, lastPara = np.PP, want
			for ri, run := range p.Runs {
				nr := np.Runs[ri]
				if nr.Text != run.Text || nr.Br != run.Br {
					t.Fatalf("case %d run %d.%d: text %q/%v, want %q/%v", i, pi, ri, nr.Text, nr.Br, run.Text, run.Br)
				}
				want := charFormat(f, run.CP)
				if got := nf.Char[nr.CP]; got != want {
					t.Fatalf("case %d run %d.%d: format %+v, want %+v", i, pi, ri, got, want)
				}
				first := lastCP == -1
				if nr.CP != lastCP && (nr.CP != lastCP+1 || (!first && want == lastChar)) || nr.CP == lastCP && want != lastChar {
					t.Fatalf("case %d run %d.%d: cp %d after %d", i, pi, ri, nr.CP, lastCP)
				}
				lastCP, lastChar = nr.CP, want
			}
		}
		if len(nf.Char) != lastCP+1 || len(nf.Para) != lastPP+1 {
			t.Fatalf("case %d: %d char and %d para rows, want %d and %d", i, len(nf.Char), len(nf.Para), lastCP+1, lastPP+1)
		}
		ab, af := Renumber(nb, nf)
		if !reflect.DeepEqual(ab, nb) || !reflect.DeepEqual(af, nf) {
			t.Fatalf("case %d: Renumber is not idempotent", i)
		}
		if render := Render(b, f, "s"); render != Render(nb, nf, "s") {
			t.Fatalf("case %d: html %q, want %q", i, Render(nb, nf, "s"), render)
		}
	}
}
