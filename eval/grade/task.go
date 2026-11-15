// Package grade grades one trial of an evaluation task: it reads the
// task's input deck and the trial's output deck through the read model
// (internal/pptx/model), runs the task's machine-checkable expectations, and
// counts every change the task did not ask for.
//
// Graders do not assume the output was written by pptxdriver. Objects are
// matched between input and output by shape id and name, then by a
// signature of kind, name and text (see match.go), and untargeted objects
// are compared first byte for byte and then as canonical XML, so a tool
// that re-serialises the slide (python-pptx, a hand edit) is not charged
// for formatting it did not change.
package grade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Task types, as in docs/design.md "Evaluation".
const (
	Identify = "identify"
	Restyle  = "restyle"
	Move     = "move"
	Connect  = "connect"
	Insert   = "insert"
	Author   = "author"
	Freeform = "freeform"
)

// Types lists the task types in the design's order.
var Types = []string{Identify, Restyle, Move, Connect, Insert, Author, Freeform}

// Task is one evaluation task: eval/tasks/<id>/task.json.
type Task struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Category is the diagram kind: architecture, flowchart, org-chart,
	// timeline, process, icon-grid.
	Category string  `json:"category"`
	Deck     DeckRef `json:"deck"`
	// Slide is the slide part name, e.g. /ppt/slides/slide7.xml.
	Slide string `json:"slide"`
	// Instruction is what the agent is told, written as a user would.
	Instruction string `json:"instruction"`
	// Targets are input objects (short ids, e.g. s12) the task may change
	// or delete. Every other input object must be unchanged.
	Targets []string `json:"targets,omitempty"`
	// MaxNew is the number of new objects allowed; -1 means any number.
	// New objects beyond it count as unintended changes.
	MaxNew int `json:"max_new"`
	// Answer is the labelled answer of an Identify task.
	Answer *AnswerSpec `json:"answer,omitempty"`
	// Checks are the machine-checkable expectations.
	Checks []Check `json:"checks,omitempty"`
	// Rubric names a render rubric file beside task.json for a later
	// judge. It is not automated: grade.json marks it pending.
	Rubric string `json:"rubric,omitempty"`
	// Notes are for task authors and are not shown to the agent.
	Notes string `json:"notes,omitempty"`
}

// DeckRef locates a corpus deck. Decks are never committed; materialise
// copies them from the corpus and checks the hash.
type DeckRef struct {
	// Path is relative to the corpus root: web/<slug>.pptx or
	// repos/<source>/<path>.
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Licence string `json:"licence"`
}

// AnswerSpec is the expected answer of an Identify task.
type AnswerSpec struct {
	// IDs are the expected objects as short ids (s12). The answer must
	// name exactly these objects, in any order.
	IDs []string `json:"ids"`
}

// Check is one expectation. Fields apply by Type; see checks.go.
//
// Selectors name objects: "s12" is input object s12 as found in the
// output (see match.go), "$name" is an alias bound by an earlier check,
// and "new" is every object the output added.
type Check struct {
	Type string `json:"type"`
	// Target and Targets are selectors.
	Target  string   `json:"target,omitempty"`
	Targets []string `json:"targets,omitempty"`
	// Alias binds the objects a check found, for later checks.
	Alias string `json:"alias,omitempty"`

	// fill, line: colour by scheme slot or hex (RRGGBB); line width.
	Scheme  string   `json:"scheme,omitempty"`
	Hex     string   `json:"hex,omitempty"`
	WidthPt *float64 `json:"width_pt,omitempty"`

	// box: expected slide-space values in points; Keep lists box fields
	// (x, y, w, h) that must equal the input's.
	X    *float64 `json:"x,omitempty"`
	Y    *float64 `json:"y,omitempty"`
	W    *float64 `json:"w,omitempty"`
	H    *float64 `json:"h,omitempty"`
	Keep []string `json:"keep,omitempty"`
	// Tol is the tolerance in points (default 0.5).
	Tol float64 `json:"tol,omitempty"`

	// align, distribute, order: Edge is left, right, top, bottom,
	// centerx or centery; Value is the expected edge in points; Ref is a
	// selector whose edge (or, for a line, whose y or x) gives the value.
	Edge  string   `json:"edge,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	// Axis is x or y.
	Axis string `json:"axis,omitempty"`

	// connector: glue from From to To. Sides are top, right, bottom,
	// left; a site index pins one site. An empty To with ToPoint means
	// the far end is free and must lie at that point.
	From     string    `json:"from,omitempty"`
	To       string    `json:"to,omitempty"`
	FromSide string    `json:"from_side,omitempty"`
	ToSide   string    `json:"to_side,omitempty"`
	FromSite *int      `json:"from_site,omitempty"`
	ToSite   *int      `json:"to_site,omitempty"`
	ToPoint  []float64 `json:"to_point,omitempty"`
	ArrowAt  string    `json:"arrow_at,omitempty"` // "to": an arrowhead at the To end
	Geoms    []string  `json:"geoms,omitempty"`    // accepted geometry names or prefixes ending in *
	Texts    []string  `json:"texts,omitempty"`    // sequence: texts in order
	Text     string    `json:"text,omitempty"`     // new_shape, text
	Custom   bool      `json:"custom,omitempty"`   // new_shape: custGeom required
	Count    int       `json:"count,omitempty"`    // new_objects
	Kind     string    `json:"kind,omitempty"`     // new_shape, new_objects: object kind
	Region   []float64 `json:"region,omitempty"`   // x, y, w, h in points
	MinLen   float64   `json:"min_len,omitempty"`  // new_line
	MinPts   int       `json:"min_points,omitempty"`
	Orient   string    `json:"orient,omitempty"` // new_line: h or v
}

// LoadTask reads a task.json.
func LoadTask(path string) (*Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Task
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &t, nil
}

// LoadTasks reads every <dir>/*/task.json, sorted by id.
func LoadTasks(dir string) ([]*Task, error) {
	m, err := filepath.Glob(filepath.Join(dir, "*", "task.json"))
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, p := range m {
		t, err := LoadTask(p)
		if err != nil {
			return nil, err
		}
		if want := filepath.Base(filepath.Dir(p)); t.ID != want {
			return nil, fmt.Errorf("%s: id %q does not match its directory", p, t.ID)
		}
		out = append(out, t)
	}
	return out, nil
}

var checkTypes = map[string]bool{
	"fill": true, "line": true, "box": true, "align": true, "distribute": true,
	"order": true, "same_size": true, "connector": true, "no_connector": true,
	"deleted": true, "resolved": true, "new_shape": true, "new_objects": true,
	"new_line": true, "sequence": true, "path": true, "region": true, "text": true,
}

// Validate checks the spec's own consistency.
func (t *Task) Validate() error {
	ok := false
	for _, ty := range Types {
		ok = ok || ty == t.Type
	}
	if !ok {
		return fmt.Errorf("unknown task type %q", t.Type)
	}
	if t.Deck.Path == "" || len(t.Deck.SHA256) != 64 || t.Slide == "" || t.Instruction == "" {
		return fmt.Errorf("deck path, sha256, slide and instruction are required")
	}
	if t.Type == Identify {
		if t.Answer == nil || len(t.Answer.IDs) == 0 {
			return fmt.Errorf("identify task without answer ids")
		}
	} else if len(t.Checks) == 0 {
		return fmt.Errorf("task has no checks")
	}
	if (t.Type == Author || t.Type == Freeform) && t.Rubric == "" {
		return fmt.Errorf("%s task needs a rubric", t.Type)
	}
	for i, c := range t.Checks {
		if !checkTypes[c.Type] {
			return fmt.Errorf("check %d: unknown type %q", i, c.Type)
		}
	}
	return nil
}

// AnswerContract is the answer format every condition is told for
// Identify tasks. It is appended to the instruction in task.md.
const AnswerContract = `Answer by writing the object(s) to answer.txt, one per line. Name each
object by its address as pptx read shows it (for example slide3/s17; the
shape id alone, 17 or s17, is accepted too). An object's exact
name (cNvPr name) is accepted only when no other object on the slide has
that name. Do not change the deck.`

// EditContract is told for every task that edits the deck.
const EditContract = `Leave input.pptx as it is and write the edited deck to output.pptx.
Change only what the request needs: every other object, part and
relationship must stay as it is.`

// ToolAnswerContract and ToolEditContract are the contracts for the Pi
// runner, whose agent has tools instead of a shell and no files to name.
const ToolAnswerContract = `Answer by calling submit_answer with the object(s), each named by its address
as pptx_read shows it (for example slide3/s17; the shape id alone, 17 or
s17, is accepted too). Do not change the deck.`

// ToolEditContract is the contract for the Pi edit tool, appended to an edit
// task's prompt.
const ToolEditContract = `Leave input.pptx as it is: pptx_apply writes the edited deck to output.pptx.
Change only what the request needs: every other object, part and
relationship must stay as it is.`

// ToolMarkdown renders the task text for the Pi runner.
func (t *Task) ToolMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", t.ID)
	short := strings.TrimSuffix(t.Slide[strings.LastIndex(t.Slide, "/")+1:], ".xml")
	fmt.Fprintf(&b, "Deck: slide `%s` (part `%s`).\n\n", short, t.Slide)
	fmt.Fprintf(&b, "%s\n\n", t.Instruction)
	if t.Type == Identify {
		b.WriteString(ToolAnswerContract)
	} else {
		b.WriteString(ToolEditContract)
	}
	b.WriteString("\n")
	return b.String()
}

// Markdown renders the agent-facing task.md.
func (t *Task) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", t.ID)
	short := strings.TrimSuffix(t.Slide[strings.LastIndex(t.Slide, "/")+1:], ".xml")
	fmt.Fprintf(&b, "Deck: `input.pptx`, slide `%s` (part `%s`).\n\n", short, t.Slide)
	fmt.Fprintf(&b, "%s\n\n", t.Instruction)
	if t.Type == Identify {
		b.WriteString(AnswerContract)
	} else {
		b.WriteString(EditContract)
	}
	b.WriteString("\n")
	return b.String()
}
