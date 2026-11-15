package grade

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

// Result is grade.json.
type Result struct {
	Task string `json:"task"`
	Type string `json:"type"`
	// Pass: every check passed, no unintended change, no hazard left,
	// and (when run) PowerPoint opened the output cleanly. For Author and
	// Freeform tasks it covers the model checks only; RubricPending says
	// the render rubric still needs a judge.
	Pass          bool          `json:"pass"`
	Checks        []CheckResult `json:"checks"`
	GeomErrorPt   *float64      `json:"geom_error_pt,omitempty"`
	WrongObject   bool          `json:"wrong_object"`
	Unintended    int           `json:"unintended_changes"`
	Changes       []string      `json:"unintended_detail,omitempty"`
	Reserialised  int           `json:"reserialised,omitempty"`
	Allowed       []string      `json:"allowed_changes,omitempty"`
	HazardsLeft   []string      `json:"hazards_left,omitempty"`
	Match         MatchSummary  `json:"match"`
	Answer        *AnswerResult `json:"answer,omitempty"`
	PowerPoint    *PPResult     `json:"powerpoint,omitempty"`
	RubricPending bool          `json:"rubric_pending,omitempty"`
	Error         string        `json:"error,omitempty"`
}

// MatchSummary reports how objects were paired.
type MatchSummary struct {
	ByID        int      `json:"by_id"`
	BySignature int      `json:"by_signature"`
	New         []string `json:"new,omitempty"`
	Deleted     []string `json:"deleted,omitempty"`
}

// AnswerResult grades an Identify answer.
type AnswerResult struct {
	Given      []string `json:"given"`
	Resolved   []string `json:"resolved"`
	Expected   []string `json:"expected"`
	Unresolved []string `json:"unresolved,omitempty"`
	Pass       bool     `json:"pass"`
}

// PPResult is the optional PowerPoint smoke step.
type PPResult struct {
	Clean  bool   `json:"clean"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
}

// Options configures Grade.
type Options struct {
	// Answer is the Identify answer text (answer.txt).
	Answer *string
	// PowerPoint runs `go run ./tools/pptoracle smoke` in RepoDir.
	PowerPoint bool
	RepoDir    string
}

// AllowedParts may change without counting as unintended: tools that
// re-save a deck rewrite their timestamps and statistics. Changes are
// recorded in allowed_changes.
var AllowedParts = map[string]bool{"/docProps/app.xml": true, "/docProps/core.xml": true}

// Grade grades output against task t with input deck input.
func Grade(t *Task, input, output string, opt Options) (*Result, error) {
	res := &Result{Task: t.ID, Type: t.Type, Checks: []CheckResult{}}
	if t.Rubric != "" {
		res.RubricPending = true
	}
	inPkg, err := opc.Open(input)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	defer inPkg.Close()
	inSl, err := readSlide(inPkg, t.Slide)
	if err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	outPkg, err := opc.Open(output)
	if err != nil {
		res.Error = fmt.Sprintf("output does not open as a package: %v", err)
		return res, nil
	}
	defer outPkg.Close()
	outSl, err := readSlide(outPkg, t.Slide)
	if err != nil {
		res.Error = fmt.Sprintf("output: %v", err)
		return res, nil
	}

	targets := map[string]bool{}
	for _, id := range t.Targets {
		targets[id] = true
	}
	inDoc, _ := partTree(inPkg, t.Slide)
	outDoc, _ := partTree(outPkg, t.Slide)
	inNS, outNS := ScopeOf(inDoc), ScopeOf(outDoc)
	m := Match(inSl, outSl, inNS, outNS, targets)
	res.Match = MatchSummary{ByID: m.ByID, BySignature: m.BySignature}
	for _, o := range m.New {
		res.Match.New = append(res.Match.New, o.ID)
	}
	for _, o := range m.Deleted {
		res.Match.Deleted = append(res.Match.Deleted, o.ID)
	}

	// Checks.
	e := &env{m: m, in: inSl, out: outSl, aliases: map[string][]*model.Object{}, targets: targets}
	maxErr := -1.0
	targetMiss := false
	for _, c := range t.Checks {
		r := e.run(c)
		if r.ErrPt != nil && *r.ErrPt > maxErr {
			maxErr = *r.ErrPt
		}
		targetMiss = targetMiss || (!r.Pass && r.TargetMiss)
		res.Checks = append(res.Checks, r)
	}
	if maxErr >= 0 {
		res.GeomErrorPt = errp(maxErr)
	}

	// Unintended changes on the slide.
	objChanged := 0
	for _, in := range inSl.Objects {
		if targets[in.ID] {
			continue
		}
		out := m.InToOut[in.ID]
		if out == nil {
			if parentDeleted(in, inSl, m, targets) {
				continue
			}
			res.Changes = append(res.Changes, in.ID+": deleted")
			objChanged++
			continue
		}
		same, reser := ownSame(in, out, inNS, outNS)
		switch {
		case !same:
			res.Changes = append(res.Changes, in.ID+": changed")
			objChanged++
		case reser:
			res.Reserialised++
		}
		if pin, pout := in.Parent, out.Parent; pin != m.InID(m.OutByID(pout)) && !(pin == "" && pout == "") {
			res.Changes = append(res.Changes, in.ID+": moved to another group")
			objChanged++
		}
	}
	for _, id := range reordered(m, targets) {
		res.Changes = append(res.Changes, id+": paint order changed")
		objChanged++
	}
	if t.MaxNew >= 0 && len(m.New) > t.MaxNew {
		res.Changes = append(res.Changes, fmt.Sprintf("%d new objects, %d allowed", len(m.New), t.MaxNew))
	}
	if inDoc != nil && outDoc != nil && !bytes.Equal(Canonical(inDoc.Node(), true), Canonical(outDoc.Node(), true)) {
		res.Changes = append(res.Changes, t.Slide+": slide outside its objects changed")
	}

	// Unintended changes in the rest of the package.
	pc, err := diffPackages(inPkg, outPkg, t.Slide)
	if err != nil {
		return nil, err
	}
	res.Changes = append(res.Changes, pc.changed...)
	res.Allowed = pc.allowed
	res.Reserialised += pc.reserialised
	res.Unintended = len(res.Changes)
	if t.MaxNew >= 0 && len(m.New) > t.MaxNew {
		res.Unintended += len(m.New) - t.MaxNew - 1
	}

	// Hazards the output has that the input did not.
	had := map[string]bool{}
	for _, h := range inSl.Hazards {
		had[h.Kind+" "+h.ID] = true
	}
	for _, h := range outSl.Hazards {
		id := h.ID
		if o := m.OutByID(h.ID); o != nil {
			id = m.Label(o)
		}
		if !had[h.Kind+" "+id] {
			res.HazardsLeft = append(res.HazardsLeft, h.Kind+" "+id+": "+h.Detail)
		}
	}

	pass := res.Unintended == 0 && len(res.HazardsLeft) == 0
	for _, c := range res.Checks {
		pass = pass && c.Pass
	}
	if t.Type == Identify {
		ans := ""
		if opt.Answer != nil {
			ans = *opt.Answer
		}
		res.Answer = gradeAnswer(t, inSl, ans)
		pass = pass && res.Answer.Pass
		res.WrongObject = len(res.Answer.Resolved) > 0 && !subset(res.Answer.Resolved, t.Answer.IDs) || len(res.Answer.Unresolved) > 0
	} else {
		res.WrongObject = targetMiss && objChanged > 0
	}
	if opt.PowerPoint {
		res.PowerPoint = smoke(opt.RepoDir, input, output)
		pass = pass && res.PowerPoint.Clean
	}
	res.Pass = pass
	return res, nil
}

// parentDeleted reports whether an object went with a deleted targeted
// ancestor group.
func parentDeleted(o *model.Object, sl *model.Slide, m *Matching, targets map[string]bool) bool {
	for p := o.Parent; p != ""; {
		if targets[p] && m.InToOut[p] == nil {
			return true
		}
		var next string
		for _, x := range sl.Objects {
			if x.ID == p {
				next = x.Parent
			}
		}
		p = next
	}
	return false
}

// subset reports whether every element of a appears in b.
func subset(a, b []string) bool {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	for _, x := range a {
		if !in[x] {
			return false
		}
	}
	return true
}

// readSlide reads the slide named part from package p.
func readSlide(p *opc.Package, part string) (*model.Slide, error) {
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, err
	}
	name, err := d.ResolveSlide(part)
	if err != nil {
		return nil, err
	}
	return d.ReadSlide(name)
}

// partTree parses the named package part into a tree document.
func partTree(p *opc.Package, name string) (*tree.Document, error) {
	pt, err := p.Part(name)
	if err != nil {
		return nil, err
	}
	return pt.Tree()
}

var idToken = regexp.MustCompile(`^(?:slide\d+[/#])?s?(\d+(?:~\d+)?)$`)

// gradeAnswer resolves an Identify answer: one object per line (commas
// and semicolons also separate), each an address as pptx read shows it
// (slide3/s17) or a shape id (17, s17) or an object name that is unique on the slide.
func gradeAnswer(t *Task, sl *model.Slide, ans string) *AnswerResult {
	r := &AnswerResult{Expected: append([]string(nil), t.Answer.IDs...)}
	byID := map[string]bool{}
	names := map[string][]string{}
	for _, o := range sl.Objects {
		byID[o.ID] = true
		names[o.Name] = append(names[o.Name], o.ID)
	}
	seen := map[string]bool{}
	for _, tok := range strings.FieldsFunc(ans, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		tok = strings.Trim(strings.TrimSpace(tok), "`\"'")
		if tok == "" {
			continue
		}
		r.Given = append(r.Given, tok)
		id := ""
		if mm := idToken.FindStringSubmatch(tok); mm != nil && byID["s"+mm[1]] {
			id = "s" + mm[1]
		} else if v := names[tok]; len(v) == 1 {
			id = v[0]
		}
		if id == "" {
			r.Unresolved = append(r.Unresolved, tok)
			continue
		}
		if !seen[id] {
			seen[id] = true
			r.Resolved = append(r.Resolved, id)
		}
	}
	sort.Strings(r.Resolved)
	sort.Strings(r.Expected)
	r.Pass = len(r.Unresolved) == 0 && strings.Join(r.Resolved, ",") == strings.Join(r.Expected, ",")
	return r
}

// smoke runs the PowerPoint oracle on output, with input as its source.
func smoke(repo, input, output string) *PPResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./tools/pptoracle", "smoke", "--source", input, output)
	cmd.Dir = repo
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	r := &PPResult{Output: strings.TrimSpace(buf.String())}
	switch {
	case err == nil:
		r.Clean, r.Status = true, "clean"
	case cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1:
		r.Status = "not clean"
	default:
		r.Status = "error: " + err.Error()
	}
	return r
}

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
