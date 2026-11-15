package ops

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// FormatRow is row.format (docxdriver's format_row).
type FormatRow struct {
	At     string    `json:"at"`
	Format RowFormat `json:"format"`
}

// opRowFormat is row.format's entry in the operation table.
var opRowFormat = OpDef{
	Name: "row.format", Oracle: "format_row", New: func() Spec { return &FormatRow{} },
	Rules: formatRowRules,
}

// rowClears are the row properties clear removes, by field.
var rowClears = map[string]string{"height": "trHeight", "repeating_header": "tblHeader", "allow_split": "cantSplit"}

// Run formats a row (tables.rs format_row): its height and height rule,
// whether it repeats as a header and whether it may break across pages,
// recorded as a w:trPrChange; direct mode accepts it. A row with a
// pending property change is refused, and so is a row another author's
// pending insertion or deletion marks, in both modes (docxdriver refuses
// it only when tracking).
func (o *FormatRow) Run(env *Env) error {
	t, err := env.resolveTable(o.At)
	if err != nil {
		return err
	}
	if t.row == nil || t.cell != nil {
		return reject("not_found", "at", "%s does not name a row", o.At)
	}
	tr := t.row
	trPr := model.ChildW(tr, "trPr")
	if pendingRecord(trPr, "trPrChange") {
		return reject("operation_failed", "at", "%s has a pending property change (w:trPrChange); accept or reject it first", o.At)
	}
	if m := model.RowRevision(tr); m != nil && model.AttrOr(m, "author") != env.Author {
		return reject("operation_failed", "at", "%s has another author's pending row %s; accept or reject it first", o.At, m.Local())
	}
	f := &o.Format
	if trPr == nil && f.Height == nil && f.HeightRule == nil && f.RepeatingHeader == nil && f.AllowSplit == nil {
		return nil
	}
	main, _, err := env.Main()
	if err != nil {
		return err
	}
	h := env.WS.Target(main, o.At, tr)
	trPr, created, err := ensureBag(h, tr, "trPr")
	if err != nil {
		return err
	}
	w := propWriter{h, trPr}
	if _, err := env.Rev.Property(h, trPr, func() error {
		if f.Height != nil {
			n, err := w.ensure(trPr, "trHeight")
			if err != nil {
				return err
			}
			if err := w.set(n, "val", twips(*f.Height)); err != nil {
				return err
			}
		}
		if f.HeightRule != nil {
			n, err := w.ensure(trPr, "trHeight")
			if err != nil {
				return err
			}
			if _, ok := model.Attr(n, "val"); !ok {
				if err := w.set(n, "val", "0"); err != nil {
					return err
				}
			}
			rule := *f.HeightRule
			if rule == "at_least" {
				rule = "atLeast"
			}
			if err := w.set(n, "hRule", rule); err != nil {
				return err
			}
		}
		if f.RepeatingHeader != nil {
			if err := w.onOff("tblHeader", *f.RepeatingHeader); err != nil {
				return err
			}
		}
		if f.AllowSplit != nil {
			if err := w.onOff("cantSplit", !*f.AllowSplit); err != nil {
				return err
			}
		}
		if err := w.clears(f.Clear, rowClears); err != nil {
			return err
		}
		for _, c := range f.Clear {
			if c != "height_rule" {
				continue
			}
			if n := model.ChildW(trPr, "trHeight"); n != nil {
				if err := w.unset(n, "hRule"); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := dropIfEmpty(h, trPr, created); err != nil {
		return err
	}
	return nil
}
