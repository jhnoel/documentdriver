package html

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// The committed golden set: for each input, the class of each aspect
// (match or an intended difference, see diff_test.go) and a SHA-256
// prefix of the equation-masked result in the canonical JSON form of
// normalise: docxdriver's for a match (equal to Go's), Go's for an
// intended difference, so either kind of change fails the test. Regenerate it from a full oracle run with
//
//	DOCX_HTMLDIFF=<results.json> DOCX_HTMLDIFF_WRITE=1 go test ./internal/docx/html -run TestDifferential
//
// (see testdata/README.md for how the oracle harness is built).

type goldenAspect struct {
	Class string `json:"class"`
	Hash  string `json:"hash,omitempty"`
}

type goldenCase struct {
	Input   string                  `json:"input"`
	Source  string                  `json:"source"`
	Aspects map[string]goldenAspect `json:"aspects"`
}

func hashJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// goldenWorthy picks the committed subset: every input from docxdriver's
// tests and docs and the hand-written cases, up to 300 bytes (any length for the
// hand-written ones), and one
// mutated input per tokenizer error code.
func goldenWorthy(c oracleCase, codes map[string]bool) bool {
	if !strings.HasPrefix(c.Source, "mutation") {
		return len(c.Input) <= 300 || c.Source == "hand"
	}
	var tree map[string]any
	if err := json.Unmarshal(c.Tree, &tree); err != nil {
		return false
	}
	msg, _ := tree["err"].(string)
	_, code, ok := strings.Cut(msg, `message: "`)
	if !ok {
		return false
	}
	code, _, _ = strings.Cut(code, `"`)
	if codes[code] {
		return false
	}
	codes[code] = true
	return true
}

func writeGolden(t *testing.T, cases []goldenCase) {
	var buf strings.Builder
	buf.WriteString("[\n")
	for i, c := range cases {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(b)
		if i < len(cases)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("]\n")
	if err := os.WriteFile(filepath.Join("testdata", "golden.json"), []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// oracleNamesHash is the hash of a result with the contract's attribute
// names put back into docxdriver's (data-x is data-docx-x), which is how
// the recorded hashes of matching results name them.
func oracleNamesHash(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(b), "data-", "data-docx-")), &back); err != nil {
		t.Fatal(err)
	}
	return hashJSON(t, back)
}

func TestGolden(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	// DOCX_HTML_GOLDEN_UPDATE=1 re-records the aspects whose result moved
	// for a reason this test knows (without an oracle run): an input that
	// strict input now refuses takes the class strict-input, one with an
	// attribute the input does not know takes unknown-attribute, one whose
	// old attribute names were rewritten takes attribute-names, and a
	// reworded duplicate-attribute refusal keeps its class. Any other
	// change still fails.
	update := os.Getenv("DOCX_HTML_GOLDEN_UPDATE") == "1"
	counts := map[string]int{}
	inputs, unknown, renamed := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, c := range cases {
		for name, want := range c.Aspects {
			// The inputs are docxdriver's: its attribute names are
			// rewritten to the contract's before the Go side reads them.
			got := maskEquations(name, normalise(t, aspectGo(name, view.FromOracleMarkup(c.Input))))
			h := hashJSON(t, got)
			if h != want.Hash && oracleNamesHash(t, got) == want.Hash {
				h = want.Hash // the same result under docxdriver's attribute names
			}
			if h != want.Hash && update {
				msg, isErr := hasErr(got)
				switch {
				case isErr && strictInput.MatchString(msg):
					want = goldenAspect{Class: "strict-input", Hash: h}
					inputs[c.Input] = true
				case isErr && strings.HasPrefix(msg, "unknown HTML attribute "):
					want = goldenAspect{Class: "unknown-attribute", Hash: h}
					unknown[c.Input] = true
				case isErr && strings.HasPrefix(msg, "duplicate HTML attribute ") && want.Class == "duplicate-attribute":
					want.Hash = h
				case isErr && name == "doc" && (strings.Contains(msg, "is read's address of an image") || strings.Contains(msg, "create fetches nothing")):
					want = goldenAspect{Class: "document-image-source", Hash: h}
				case !isErr && name == "doc" && documentTableStyle.MatchString(c.Input):
					want = goldenAspect{Class: "document-table-style", Hash: h}
				case strings.Contains(c.Input, "data-docx-"):
					want = goldenAspect{Class: "attribute-names", Hash: h}
					renamed[c.Input] = true
				}
				cases[i].Aspects[name] = want
			}
			counts[want.Class]++
			if h != want.Hash {
				t.Errorf("%s %q (%s): result changed (class %s): %s", name, c.Input, c.Source, want.Class, short(fmtJSON2(got)))
			}
		}
	}
	if update && !t.Failed() {
		writeGolden(t, cases)
		t.Logf("golden.json rewritten: %d inputs moved to strict-input, %d to unknown-attribute", len(inputs), len(unknown))
		t.Logf("%d inputs moved to attribute-names", len(renamed))
	}
	t.Logf("golden: %d inputs, aspects by class %v", len(cases), counts)
}

// documentTableStyle finds a style on a table, row or cell.
var documentTableStyle = regexp.MustCompile(`<(table|tr|td|th)\b[^>]*\bstyle=`)

func fmtJSON2(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
