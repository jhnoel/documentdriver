// Package apply runs a plan against a deck and builds the edit result:
// the objects the plan changed, re-projected exactly as `pptx read --json`
// shows them, the ids its aliases were bound to, the hazards the read
// model finds on the result, and the parts it touched. Writing goes
// through internal/pptx/ops only; this package reads the result back through
// the read model, which internal/pptx/ops never sees. The transaction,
// dry run and result envelope are internal/plan's; this package adds the
// slide, the units and the read-model hazards (stale-connector,
// dangling-glue, group-frame) and inset-overflow on the objects it changed.
package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
)

// Options control a run: dry run, the id source and the output file
// (plan.Options), and how much of each changed object the result shows.
type Options struct {
	// DryRun builds the result without saving anything.
	DryRun bool
	// IDs supplies fresh GUIDs and the clock; nil means ids.Default().
	IDs ids.Source
	// Out is the file to write; "" writes the deck in place.
	Out string
	// ChangedIDs shows each changed object as its address only.
	ChangedIDs bool
}

// Hazard is a contradiction in the result, as the read model reports it
// (plan.Hazard). New is set when the input did not have it (same kind
// and object).
type Hazard = plan.Hazard

// Result is the edit result of a plan: the units it was read in, then
// the engine's envelope (plan.Result). Its aliases map to addresses
// (slide3/s1001), Changed holds the new and changed objects of every
// slide the plan addressed, in slide then paint order, as the JSON view
// shows them, and Deleted the removed objects' input addresses.
type Result struct {
	Units string
	plan.Result
}

// Run applies the plan text to the deck at path. Any failure leaves
// every file untouched. The error is a *cli.Error or cli.Errors with
// the contract's codes: every fault of the plan itself (found without
// the deck) together, then the first operation that does not apply.
func Run(path string, planText []byte, opt Options) (*Result, error) {
	p, err := ops.ParsePlan(planText)
	if err != nil {
		return nil, err
	}
	steps, err := p.Compile()
	if err != nil {
		return nil, err
	}
	input, err := os.ReadFile(path) //nolint:gosec // G304: the deck is the file the caller named
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the deck: %v", err)
	}
	if e := cli.CheckBase(p.Base, input); e != nil {
		return nil, e
	}
	var txOpts []ops.Option
	if opt.IDs != nil {
		txOpts = append(txOpts, ops.WithIDs(opt.IDs))
	}
	tx, err := ops.Begin(path, txOpts...)
	if err != nil {
		return nil, cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	defer tx.Rollback()
	for i, op := range steps {
		if err := tx.Run(op); err != nil {
			return nil, opError(i, errors.Unwrap(err))
		}
	}
	var res *Result
	if _, err := tx.Finish(plan.Options{DryRun: opt.DryRun, IDs: opt.IDs, Out: opt.Out}, func(snap string) (*plan.Result, error) {
		res, err = result(path, snap, p, tx, opt)
		if err != nil {
			return nil, err
		}
		return &res.Result, nil
	}); err != nil {
		if errors.Is(err, plan.ErrUndeclared) {
			return nil, &cli.Error{Code: cli.CodeInternal, Message: err.Error(), Cause: err}
		}
		return nil, cli.Wrap(cli.CodeIO, "", err)
	}
	res.Base = cli.Hash(input)
	if !res.DryRun {
		if len(res.Parts) == 0 {
			res.Saved, res.NewBase = "", res.Base // a no-op writes nothing
		} else {
			out, err := os.ReadFile(res.Saved)
			if err != nil {
				return nil, cli.Errorf(cli.CodeIO, "", "read the saved deck: %v", err)
			}
			res.NewBase = cli.Hash(out)
		}
	}
	return res, nil
}

// opError classifies the failure of operation i under the contract's
// codes.
func opError(i int, err error) *cli.Error {
	e := &cli.Error{Code: cli.CodeOperationFailed, Path: cli.Index("ops", i), Message: err.Error(), Cause: err}
	var many *text.ManyError
	switch {
	case errors.As(err, &many):
		e.Code, e.Matches, e.Path = cli.CodeAmbiguous, many.Matches, e.Path+".select"
		e.Hint = "give occurrence (1-based) or all"
	case errors.Is(err, text.ErrNoMatch):
		e.Code, e.Path = cli.CodeNotFound, e.Path+".select"
	case errors.Is(err, prop.ErrNotDerived):
		// The operation needs a value of its own target that the file
		// stores unreadably: the one refusal a pre-existing defect causes.
		e.Code, e.Path, e.Detail = cli.CodeUnreadableTarget, e.Path+".at", err.Error()
		e.Message = "the target stores a value this operation needs in a form that cannot be read"
	case errors.Is(err, edit.ErrNoObject), errors.Is(err, edit.ErrBadAddress):
		e.Code = cli.CodeNotFound
		e.Hint = "addresses are as `pptx read` shows them, such as slide3/s17"
	}
	return e
}

// readSlide reads one slide of a deck file through the read model.
func readSlide(path, slide string) (*model.Slide, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, err
	}
	part, err := d.ResolveSlide(slide)
	if err != nil {
		return nil, err
	}
	return d.ReadSlide(part)
}

// result builds the edit result comparing input and output decks.
func result(in, out string, p *ops.Plan, tx *ops.Tx, opt Options) (*Result, error) {
	res := &Result{Units: p.Units, Result: plan.NewResult()}
	res.DryRun = true
	for a, addr := range tx.Aliases() {
		slide, id, _ := strings.Cut(addr, "#")
		res.Aliases[a] = slide + "/s" + id
	}
	var hzPre, hzPost []Hazard
	for _, slide := range p.Slides() {
		pre, err := readSlide(in, slide)
		if err != nil {
			return nil, err
		}
		post, err := readSlide(out, slide)
		if err != nil {
			return nil, err
		}
		before := map[string]*model.Object{}
		for _, o := range pre.Objects {
			before[o.ID] = o
		}
		after := map[string]bool{}
		for _, o := range post.Objects {
			after[o.ID] = true
		}
		moved := reordered(pre, post)
		vo := view.Options{Units: p.Units, Slide: post.Name}
		for _, o := range post.Objects {
			b := before[o.ID]
			if b != nil && b.Kind == o.Kind && bytes.Equal(ownXML(pre, b), ownXML(post, o)) && !moved[o.ID] {
				continue
			}
			if b != nil {
				hzPre = append(hzPre, insetHazards(pre.Name, b)...)
			}
			hzPost = append(hzPost, insetHazards(post.Name, o)...)
			var j []byte
			if opt.ChangedIDs {
				j, err = json.Marshal(post.Name + "/" + o.ID)
			} else {
				j, err = view.ObjectJSON(o, vo)
			}
			if err != nil {
				return nil, err
			}
			res.Changed = append(res.Changed, j)
		}
		for _, o := range pre.Objects {
			if !after[o.ID] {
				res.Deleted = append(res.Deleted, pre.Name+"/"+o.ID)
			}
		}
		hzPre = append(hzPre, hazards(pre)...)
		hzPost = append(hzPost, hazards(post)...)
	}
	res.Hazards = plan.MarkNew(hzPre, hzPost)
	return res, nil
}

// ownXML returns an object's own bytes: its element (or mc:AlternateContent)
// with the bytes of its child objects cut out, so a group changes only
// when the group itself does.
func ownXML(s *model.Slide, o *model.Object) []byte {
	b := o.XML()
	if o.Kind != model.Group {
		return b
	}
	for _, c := range s.Objects {
		if c.Parent == o.ID {
			b = bytes.Replace(b, c.XML(), []byte{0}, 1)
		}
	}
	return b
}

// reordered returns the objects (present before and after) whose paint
// order relative to the others changed: those outside a longest run kept
// in order.
func reordered(pre, post *model.Slide) map[string]bool {
	pos := map[string]int{}
	for i, o := range pre.Objects {
		pos[o.ID] = i
	}
	var ids []string
	var seq []int
	for _, o := range post.Objects {
		if p, ok := pos[o.ID]; ok {
			ids = append(ids, o.ID)
			seq = append(seq, p)
		}
	}
	keep := lis(seq)
	out := map[string]bool{}
	for i, id := range ids {
		if !keep[i] {
			out[id] = true
		}
	}
	return out
}

// lis marks the indices of one longest increasing subsequence of seq.
func lis(seq []int) map[int]bool {
	n := len(seq)
	length := make([]int, n)
	prev := make([]int, n)
	best := -1
	for i := range seq {
		length[i], prev[i] = 1, -1
		for j := range i {
			if seq[j] < seq[i] && length[j]+1 > length[i] {
				length[i], prev[i] = length[j]+1, j
			}
		}
		if best < 0 || length[i] > length[best] {
			best = i
		}
	}
	out := map[int]bool{}
	for i := best; i >= 0; i = prev[i] {
		out[i] = true
	}
	return out
}

// insetHazards returns the inset-overflow hazard of an object whose text
// insets leave no room for text: left + right at least the box width, or
// top + bottom at least its height (both in slide space). It is checked
// only on objects a plan changed, so it is reported at apply time, never
// by read; an object whose box or any inset is not derived has none.
func insetHazards(slide string, o *model.Object) []Hazard {
	t := o.Text
	if t == nil || o.Box == nil {
		return nil
	}
	for _, v := range t.Insets {
		if v == nil {
			return nil
		}
	}
	pt := func(e float64) string { return derive.FormatNum(e / model.EMUPerPoint) }
	l, tp, r, b := *t.Insets[0], *t.Insets[1], *t.Insets[2], *t.Insets[3]
	var why []string
	if l+r >= o.Box.W {
		why = append(why, fmt.Sprintf("left + right insets %s + %s pt >= width %s pt", pt(l), pt(r), pt(o.Box.W)))
	}
	if tp+b >= o.Box.H {
		why = append(why, fmt.Sprintf("top + bottom insets %s + %s pt >= height %s pt", pt(tp), pt(b), pt(o.Box.H)))
	}
	if why == nil {
		return nil
	}
	return []Hazard{{Kind: "inset-overflow", ID: slide + "/" + o.ID, Detail: strings.Join(why, "; ") + ": no room for text (insets are [left, top, right, bottom], not CSS order)"}}
}

// hazards returns the read model's hazards on a slide.
func hazards(s *model.Slide) []Hazard {
	out := make([]Hazard, 0, len(s.Hazards))
	for _, h := range s.Hazards {
		out = append(out, Hazard{Kind: h.Kind, ID: s.Name + "/" + h.ID, Detail: h.Detail})
	}
	return out
}

// JSON writes the result with one key per line, base and units first,
// and each changed object and hazard on its own line, like the JSON view.
func (r *Result) JSON() ([]byte, error) {
	return r.Result.JSON(plan.Field{Key: "units", Value: r.Units})
}
