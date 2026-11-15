package view

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

// manifestEntry is the part of an $PPTX_CORPUS/manifest.jsonl row this
// test uses.
type manifestEntry struct {
	Source string   `json:"source"`
	Path   string   `json:"path"`
	Native bool     `json:"powerpoint_native"`
	Hard   []string `json:"hard_cases"`
}

func nativeDecks(t *testing.T) []string {
	dir := os.Getenv("PPTX_CORPUS")
	if dir == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	f, err := os.Open(filepath.Join(dir, "manifest.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e manifestEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.Native {
			out = append(out, filepath.Join(dir, "repos", e.Source, e.Path))
		}
	}
	sort.Strings(out)
	return out
}

var (
	reDigits = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)
	rePreset = regexp.MustCompile(`preset [A-Za-z0-9]+`)
	reQuoted = regexp.MustCompile(`"[^"]*"`)
	reID     = regexp.MustCompile(`\bs[0-9N]+(~[0-9N]+)?\b`)
)

// reasonClass groups omission reasons by template.
func reasonClass(o model.Omission) string {
	r := rePreset.ReplaceAllString(o.Reason, "preset P")
	r = reQuoted.ReplaceAllString(r, `"…"`)
	r = reDigits.ReplaceAllString(r, "N")
	r = reID.ReplaceAllString(r, "sN")
	p := reDigits.ReplaceAllString(o.Prop, "N")
	return p + ": " + r
}

// TestCorpus reads every slide of every PowerPoint-native deck in the
// corpus manifest: no panics, and JSON/SVG equivalence under every option
// set. It logs counts of omissions by reason, hazards, glue distances and
// derivation failures.
func TestCorpus(t *testing.T) {
	decks := nativeDecks(t)
	// Deck by deck in parallel, each into tallies of its own, summed in
	// deck order.
	type tallies struct {
		stats, omissions, hazards map[string]int
		glue                      []float64
		fails                     []string
	}
	stats := map[string]int{}
	omissions := map[string]int{}
	hazards := map[string]int{}
	var glue []float64
	var fails []string
	files := corpustest.Files(decks, os.Getenv("PPTX_CORPUS"))
	for _, r := range corpustest.Run(t, files, func(t *testing.T, file corpustest.File, tl *tallies) {
		path := file.Path
		stats, omissions, hazards := map[string]int{}, map[string]int{}, map[string]int{}
		tl.stats, tl.omissions, tl.hazards = stats, omissions, hazards
		defer func() {
			if r := recover(); r != nil {
				tl.fails = append(tl.fails, fmt.Sprintf("%s: PANIC %v", path, r))
				stats["panics"]++
			}
		}()
		if _, err := os.Stat(path); err != nil {
			stats["missing decks"]++
			return
		}
		slides, f := checkDeck(t, path)
		stats["decks"]++
		for _, x := range f {
			if strings.Contains(x, ": read: opc: ") {
				// Damaged zip members (fuzzer and crash cases): the
				// error is reported, which is the wanted behaviour.
				stats["unreadable slides"]++
				t.Logf("unreadable: %s: %s", path, x)
				continue
			}
			tl.fails = append(tl.fails, path+": "+x)
			if strings.Contains(x, "options ") {
				stats["equivalence failures"]++
			} else {
				stats["read errors"]++
			}
		}
		for _, s := range slides {
			stats["slides"]++
			for _, o := range s.Omitted {
				omissions["(slide) "+reasonClass(o)]++
			}
			for _, h := range s.Hazards {
				hazards[h.Kind]++
				t.Logf("hazard: %s %s: %s %s: %s", filepath.Base(path), s.Name, h.Kind, h.ID, h.Detail)
			}
			byID := map[string]*model.Object{}
			for _, o := range s.Objects {
				byID[o.ID] = o
			}
			for _, o := range s.Objects {
				stats["objects"]++
				stats["objects."+string(o.Kind)]++
				if o.Box == nil {
					stats["objects without box"]++
				}
				for _, om := range o.Omitted {
					omissions[reasonClass(om)]++
				}
				if c := o.Conn; c != nil && c.First != "?" && o.Kind == model.Connector {
					for i, g := range []*model.Glue{c.Start, c.End} {
						if g == nil || g.ID == "" {
							continue
						}
						stats["glued ends"]++
						t := byID[g.ID]
						if t == nil || g.Site >= len(t.Sites) {
							continue
						}
						end := c.From
						if i == 1 {
							end = c.To
						}
						s := t.Sites[g.Site]
						tl.glue = append(tl.glue, math.Hypot(end.X-s.X, end.Y-s.Y)/model.EMUPerPoint)
					}
				}
			}
		}
	}) {
		for k, v := range r.Value.stats {
			stats[k] += v
		}
		for k, v := range r.Value.omissions {
			omissions[k] += v
		}
		for k, v := range r.Value.hazards {
			hazards[k] += v
		}
		glue = append(glue, r.Value.glue...)
		fails = append(fails, r.Value.fails...)
	}
	for _, f := range fails {
		t.Error(f)
	}
	t.Logf("corpus: %s", fmtCounts(stats))
	t.Logf("hazards: %s", fmtCounts(hazards))
	t.Logf("glue distance to site (pt) over %d checked ends: %s", len(glue), histogram(glue))
	var keys []string
	for k := range omissions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.SliceStable(keys, func(i, j int) bool { return omissions[keys[i]] > omissions[keys[j]] })
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  %7d  %s", omissions[k], k)
	}
	t.Logf("omissions by reason:%s", b.String())
}

func fmtCounts(m map[string]int) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func histogram(ds []float64) string {
	bounds := []float64{0.01, 0.1, 0.5, 1, 5, math.Inf(1)}
	counts := make([]int, len(bounds))
	for _, d := range ds {
		for i, b := range bounds {
			if d < b {
				counts[i]++
				break
			}
		}
	}
	var parts []string
	lo := 0.0
	for i, b := range bounds {
		parts = append(parts, fmt.Sprintf("[%g,%g)=%d", lo, b, counts[i]))
		lo = b
	}
	return strings.Join(parts, " ")
}
