// Package apply runs a vsdx plan on a drawing: the transaction through
// the edit layer, each op in order, the integrity gate on what the plan
// changed, the recalculation policy of decision (c), and the edit
// result with changed objects read back through internal/vsdx/read.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/integrity"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options control a run, as xlsx apply's.
type Options struct {
	DryRun     bool
	Out        string
	ChangedIDs bool
	IDs        ids.Source
}

// Run runs plan p (from ops.ParsePlan) against the drawing at path and
// returns the edit result. Errors are *cli.Error with the contract's
// codes.
func Run(path string, p *ops.Plan, opt Options) (*plan.Result, error) {
	if p == nil {
		return nil, cli.Errorf(cli.CodeInvalidPlan, "", "the plan did not decode")
	}
	input, err := os.ReadFile(path) //nolint:gosec // G304: the caller's own argument
	if err != nil {
		return nil, cli.Errorf(cli.CodeIO, "", "read the drawing: %v", err)
	}
	if e := cli.CheckBase(p.Base, input); e != nil {
		return nil, e
	}
	inputRecalc, inputFindings, err := inputFacts(input)
	if err != nil {
		return nil, err
	}
	tx, ws, err := begin(path, opt)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res := plan.NewResult()
	res.Base = cli.Hash(input)
	res.DryRun = opt.DryRun
	firstOp := map[string]int{}
	seenPart := map[string]bool{}
	changed := make([]string, 0)
	changedSeen := map[string]bool{}
	deleted := make([]string, 0)
	staleDetail := ""
	staleOp := -1
	for i, spec := range p.Ops {
		def, ok := opDefOf(spec)
		var name string
		if ok {
			name = def.Name
			if !def.Implemented() {
				return nil, cli.Errorf(cli.CodeOperationFailed, cli.Index("ops", i), "%s is not implemented", name)
			}
		} else {
			name = "test"
		}
		alias := opAs(spec)
		if alias != "" {
			if _, dup := res.Aliases[alias]; dup {
				return nil, cli.Errorf(cli.CodeOperationFailed, cli.Join(cli.Index("ops", i), "as"), "the alias $%s is already defined by an earlier operation", alias)
			}
		}
		runner, ok := spec.(ops.Runner)
		if !ok {
			return nil, cli.Errorf(cli.CodeInternal, cli.Index("ops", i), "%s does not run", name)
		}
		var asAddr string
		var ch, del []string
		var stale string
		if err := tx.Run(name, func() error {
			a, c, d, s, e := runner.Run(ws, res.Aliases, p.Units, tx.IDs())
			asAddr, ch, del, stale = a, c, d, s
			return e
		}); err != nil {
			return nil, opRefusal(i, unwrapRun(err))
		}
		if alias != "" {
			if asAddr == "" {
				return nil, cli.Errorf(cli.CodeInternal, cli.Index("ops", i), "%s created nothing for alias $%s", name, alias)
			}
			res.Aliases[alias] = asAddr
		}
		// Collect changed addresses once each, in first-change order:
		// an op repeating an earlier address neither duplicates it
		// in the result nor moves it from its first change.
		for _, a := range ch {
			if !changedSeen[a] {
				changedSeen[a] = true
				changed = append(changed, a)
			}
		}
		deleted = append(deleted, del...)
		if stale != "" && staleDetail == "" {
			staleDetail = stale
			staleOp = i
		}
		if err := noteParts(ws.Package(), seenPart, firstOp, i); err != nil {
			return nil, err
		}
	}
	if staleDetail != "" {
		if _, err := ws.SetRecalcDocument(); err != nil {
			if !errors.Is(err, edit.ErrCustomUnreadable) {
				return nil, opRefusal(staleOp, err)
			}
			staleDetail = "RecalcDocument not set: docProps/custom.xml cannot be read"
		}
		if err := noteParts(ws.Package(), seenPart, firstOp, staleOp); err != nil {
			return nil, err
		}
	}
	if len(seenPart) == 0 {
		tx.Rollback()
		if !opt.DryRun {
			res.NewBase = res.Base
		}
		res.Changed = []json.RawMessage{}
		res.Deleted = []string{}
		res.Hazards = []plan.Hazard{}
		res.Parts = []string{}
		return &res, nil
	}
	parts := slices.Sorted(maps.Keys(seenPart))
	res.Parts = parts
	res.Deleted = deleted

	var sealedBytes []byte
	var gateErr error
	_, err = tx.FinishSealed(plan.Options{DryRun: opt.DryRun, Out: opt.Out}, func(sealed []byte) (*plan.Result, error) {
		introduced, gateErrInner := gateFindings(inputFindings, sealed, parts)
		if gateErrInner != nil {
			gateErr = gateErrInner
			return nil, gateErrInner
		}
		if len(introduced) != 0 {
			gateErr = &gateFailure{introduced: introduced}
			return nil, gateErr
		}
		sealedBytes = sealed
		if err := finishUnits(&res, input, sealed, p.Units, res.Base, changed, opt.ChangedIDs); err != nil {
			return nil, err
		}
		return &res, nil
	})
	if gateErr != nil {
		var gf *gateFailure
		if errors.As(gateErr, &gf) {
			return nil, invalidOutput(gf.introduced, firstOp)
		}
		return nil, invalidOutput(nil, firstOp)
	}
	if err != nil {
		return nil, commitError(err)
	}
	tx.Rollback()
	if !opt.DryRun {
		res.NewBase = cli.Hash(sealedBytes)
		if res.Saved == "" {
			res.Saved = path
			if opt.Out != "" {
				res.Saved = opt.Out
			}
		}
	}
	if staleDetail != "" {
		res.Hazards = append([]plan.Hazard{{Kind: "stale-values", ID: "document", Detail: staleDetail, New: !inputRecalc}}, res.Hazards...)
	}
	return &res, nil
}

// inputFacts returns whether the input asks Visio to recalculate on
// open and every integrity finding in it, opening input with the safety
// limits.
func inputFacts(input []byte) (bool, []integrity.Finding, error) {
	pkg, err := opc.OpenBytes(input, opc.DefaultLimits())
	if err != nil {
		return false, nil, openRefusal(err)
	}
	defer pkg.Close()
	d, err := doc.Open(pkg)
	if err != nil {
		return false, nil, openRefusal(err)
	}
	findings, err := integrity.Check(pkg)
	if err != nil {
		return false, nil, cli.Errorf(cli.CodeInvalidDocument, "", "the file cannot be opened as a drawing: %v", err)
	}
	return d.Recalc, findings, nil
}

// begin opens path as a plan transaction with the edit layer as its
// workspace and the transaction id source from opt.
func begin(path string, opt Options) (*plan.Tx, *edit.Workspace, error) {
	var txOpts []plan.Option
	if opt.IDs != nil {
		txOpts = append(txOpts, plan.WithIDs(opt.IDs))
	}
	ws, err := edit.Begin(path, txOpts...)
	if err != nil {
		return nil, nil, openRefusal(err)
	}
	return ws.Tx(), ws, nil
}

// openRefusal maps a failure to open the package or its drawing.
func openRefusal(err error) error {
	var ce *cli.Error
	if errors.As(err, &ce) {
		return ce
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return cli.Errorf(cli.CodeIO, "", "read the drawing: %v", err)
	}
	if errors.Is(err, opc.ErrLimit) {
		return cli.Errorf(cli.CodeInvalidDocument, "", "the drawing exceeds a safety limit: %v", err)
	}
	return cli.Errorf(cli.CodeInvalidDocument, "", "the file cannot be opened as a drawing: %v", err)
}

// unwrapRun takes the op's own error back out of plan.Tx.Run's wrapping.
func unwrapRun(err error) error { return err }

// opRefusal maps op i's error: a *cli.Error is the contract error at
// ops[i] or ops[i].<field>; an edit not-found or unreadable error is
// not_found or unreadable_target at ops[i]; anything else is internal.
func opRefusal(i int, err error) error {
	path := cli.Index("ops", i)
	var ce *cli.Error
	if errors.As(err, &ce) {
		full := path
		if ce.Path != "" {
			full = cli.Join(path, ce.Path)
		}
		e := cli.Errorf(ce.Code, full, "%s", ce.Message)
		e.Detail = ce.Detail
		e.Hint = ce.Hint
		e.Matches = ce.Matches
		e.Cause = ce.Cause
		return e
	}
	if errors.Is(err, opc.ErrLimit) {
		return cli.Errorf(cli.CodeInvalidDocument, "", "the drawing exceeds a safety limit: %v", err)
	}
	if errors.Is(err, edit.ErrNotFound) {
		e := cli.Errorf(cli.CodeNotFound, path, "%v", err)
		e.Hint = "addresses are as `vsdx read` shows them, such as page0/s1"
		return e
	}
	var u *edit.Unreadable
	if errors.As(err, &u) {
		e := cli.Errorf(cli.CodeUnreadableTarget, path, "%s cannot be edited safely", u.Target)
		e.Detail = u.Detail
		return e
	}
	return cli.Errorf(cli.CodeInternal, path, "%v", err)
}

// commitError maps a failure to write the sealed bytes.
func commitError(err error) error {
	if errors.Is(err, opc.ErrChangedOnDisk) {
		return cli.Errorf(cli.CodeIO, "", "the drawing changed on disk while the plan ran; nothing was written")
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return cli.Errorf(cli.CodeIO, "", "write the drawing: %v", err)
	}
	return err
}

// opDefOf returns the op table entry of spec, by its Spec type.
func opDefOf(spec ops.Spec) (*ops.OpDef, bool) {
	t := reflect.TypeOf(spec)
	if t == nil {
		return nil, false
	}
	for i := range ops.Ops {
		def := &ops.Ops[i]
		if def.New == nil {
			continue
		}
		if reflect.TypeOf(def.New()) == t {
			return def, true
		}
	}
	return nil, false
}

// opAs returns the `as` alias of spec, or "" when it has none.
func opAs(spec ops.Spec) string {
	v := reflect.ValueOf(spec)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName("As")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

// noteParts records the first op that changed each new part of pkg,
// for invalid_output's path.
func noteParts(pkg *opc.Package, seen map[string]bool, firstOp map[string]int, i int) error {
	newParts, err := pkg.ChangedPartsExcept(func(name string) bool { return seen[opc.PartKey(name)] })
	if err != nil {
		return fmt.Errorf("apply: changed parts: %w", err)
	}
	for _, part := range newParts {
		key := opc.PartKey(part)
		if !seen[key] {
			seen[key] = true
			firstOp[key] = i
		}
	}
	return nil
}

// gateFailure is the refusal of a plan whose output has a finding its
// input lacks.
type gateFailure struct {
	introduced []integrity.Finding
}

// Error returns the refusal sentence.
func (g *gateFailure) Error() string { return "the edit would damage the drawing" }

// gateFindings returns the findings of sealed on changed parts that the
// input lacks, scoped to the changed parts.
func gateFindings(inputFindings []integrity.Finding, sealed []byte, changed []string) ([]integrity.Finding, error) {
	pkg, err := opc.OpenBytes(sealed, opc.DefaultLimits())
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	output, err := integrity.Check(pkg)
	if err != nil {
		return nil, err
	}
	inChanged := map[string]bool{}
	for _, part := range changed {
		inChanged[opc.PartKey(part)] = true
	}
	known := map[integrity.Finding]bool{}
	for _, f := range inputFindings {
		known[f] = true
	}
	var introduced []integrity.Finding
	for _, f := range output {
		if !inChanged[opc.PartKey(f.Part)] {
			continue
		}
		if !known[f] {
			introduced = append(introduced, f)
		}
	}
	return introduced, nil
}

// invalidOutput is the refusal of a plan whose output has a finding its
// input lacks: path the op that first changed the part, detail the finding.
func invalidOutput(introduced []integrity.Finding, firstOp map[string]int) error {
	op := -1
	if len(introduced) == 0 {
		path := "ops"
		return cli.Errorf(cli.CodeInvalidOutput, path, "the plan would damage the drawing")
	}
	for _, f := range introduced {
		i, ok := firstOp[opc.PartKey(f.Part)]
		if !ok || (op >= 0 && i != op) {
			op = -1
			break
		}
		op = i
	}
	path := "ops"
	if op >= 0 {
		path = cli.Index("ops", op)
	}
	e := cli.Errorf(cli.CodeInvalidOutput, path, "the plan would damage the drawing")
	f := introduced[0]
	e.Detail = fmt.Sprintf("%s in %s: %s", f.Kind, f.Part, f.Detail)
	return e
}

// hazardJSON is one page hazard as read shows it.
type hazardJSON struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

// pageForChanged is the part of a page JSON the edit result reads: its
// objects and hazards, as raw messages so each keeps read's key order.
type pageForChanged struct {
	Objects []json.RawMessage `json:"objects"`
	Hazards []json.RawMessage `json:"hazards"`
}

// objectID reads the id of one object JSON.
type objectID struct {
	ID string `json:"id"`
}

// objectProps reads the props of one object JSON.
type objectProps struct {
	Props []json.RawMessage `json:"props"`
}

// finishUnits fills res.Changed from changed, read back from sealed
// (the package as the plan leaves it), and res.Hazards from the hazards
// of the changed pages in input and sealed, marked new with plan.MarkNew.
func finishUnits(res *plan.Result, input, sealed []byte, units, base string, changed []string, changedIDs bool) error {
	res.Changed = make([]json.RawMessage, 0, len(changed))
	if len(changed) == 0 {
		res.Hazards = []plan.Hazard{}
		return nil
	}
	summary, err := read.Document(sealed, base)
	if err != nil {
		return err
	}
	pageEntries := map[string]read.PageEntry{}
	for _, e := range summary.Pages {
		pageEntries[e.ID] = e
	}
	pages := map[string]bool{}
	for _, a := range changed {
		if addr, err := vsd.Parse(a); err == nil {
			pages[containerOf(addr)] = true
		}
	}
	outObjects := map[string]map[string]json.RawMessage{}
	outProps := map[string]json.RawMessage{}
	var outHazards, inHazards []plan.Hazard
	for _, pageAddr := range slices.Sorted(maps.Keys(pages)) {
		out, outErr := readPageParts(sealed, base, pageAddr, units)
		if outErr == nil {
			outObjects[pageAddr] = out.objects
			for id, raw := range out.props {
				outProps[id] = raw
			}
			outHazards = append(outHazards, out.hazards...)
		}
		in, inErr := readPageParts(input, base, pageAddr, units)
		if inErr == nil {
			inHazards = append(inHazards, in.hazards...)
		}
	}
	res.Hazards = plan.MarkNew(inHazards, outHazards)
	for _, a := range changed {
		addr, err := vsd.Parse(a)
		if err != nil {
			continue
		}
		switch addr.Kind {
		case vsd.KindPage:
			entry, ok := pageEntries[a]
			if !ok {
				continue
			}
			if changedIDs {
				b, err := cli.Marshal(a)
				if err != nil {
					return err
				}
				res.Changed = append(res.Changed, b)
				continue
			}
			b, err := cli.Marshal(entry)
			if err != nil {
				return err
			}
			res.Changed = append(res.Changed, b)
		case vsd.KindShape:
			raw, ok := outObjects[containerOf(addr)][a]
			if !ok {
				continue
			}
			if changedIDs {
				b, err := cli.Marshal(a)
				if err != nil {
					return err
				}
				res.Changed = append(res.Changed, b)
				continue
			}
			res.Changed = append(res.Changed, raw)
		case vsd.KindProp:
			raw, ok := outProps[a]
			if !ok {
				continue
			}
			if changedIDs {
				b, err := cli.Marshal(a)
				if err != nil {
					return err
				}
				res.Changed = append(res.Changed, b)
				continue
			}
			res.Changed = append(res.Changed, raw)
		}
	}
	if res.Hazards == nil {
		res.Hazards = []plan.Hazard{}
	}
	return nil
}

// containerOf returns the page or master address holding addr.
func containerOf(addr vsd.Address) string {
	if addr.Kind == vsd.KindMaster {
		return vsd.MasterID(addr.Container)
	}
	if addr.Kind == vsd.KindPage {
		return vsd.PageID(addr.Container, addr.PageDup)
	}
	if addr.InMaster {
		return vsd.MasterID(addr.Container)
	}
	return vsd.PageID(addr.Container, addr.PageDup)
}

// pageParts is what finishUnits reads of one page: its objects by id,
// its props by id, and its hazards.
type pageParts struct {
	objects map[string]json.RawMessage
	props   map[string]json.RawMessage
	hazards []plan.Hazard
}

// readPageParts reads one page of data and returns its objects, props
// and hazards, keeping each object's raw bytes.
func readPageParts(data []byte, base, address, units string) (*pageParts, error) {
	b, err := read.Page(data, base, address, read.Options{Units: units})
	if err != nil {
		return nil, err
	}
	var page pageForChanged
	if err := json.Unmarshal(b, &page); err != nil {
		return nil, err
	}
	out := &pageParts{objects: map[string]json.RawMessage{}, props: map[string]json.RawMessage{}}
	for _, raw := range page.Objects {
		var id objectID
		if err := json.Unmarshal(raw, &id); err != nil {
			continue
		}
		out.objects[id.ID] = raw
		var withProps objectProps
		if err := json.Unmarshal(raw, &withProps); err != nil {
			continue
		}
		for _, praw := range withProps.Props {
			var pid objectID
			if err := json.Unmarshal(praw, &pid); err == nil {
				out.props[pid.ID] = praw
			}
		}
	}
	for _, raw := range page.Hazards {
		var h hazardJSON
		if err := json.Unmarshal(raw, &h); err != nil {
			continue
		}
		out.hazards = append(out.hazards, plan.Hazard{Kind: h.Kind, ID: h.ID, Detail: h.Detail})
	}
	return out, nil
}
