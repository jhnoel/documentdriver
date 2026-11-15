package recalc

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// ErrUnavailable is returned by Compute when LibreOffice cannot be run:
// no soffice binary, or a build without process support (wasip1). The
// CLI refuses `--recalc` with it (operation_failed, path --recalc, a
// hint naming DD_SOFFICE).
var ErrUnavailable = errors.New("recalc: LibreOffice (soffice) is not available")

// Options control Compute.
type Options struct {
	// Soffice is the soffice binary; "" looks at $DD_SOFFICE, then PATH.
	Soffice string
	// Timeout bounds the conversion (0: two minutes); a run past it is
	// killed and is an error.
	Timeout time.Duration
}

// Value is one formula cell's value as LibreOffice computed it.
type Value struct {
	// Sheet is the sheet's name; Row and Col the cell.
	Sheet    string
	Row, Col int
	// T is the cell's t as LibreOffice wrote it ("" for a number, "str",
	// "b", "e"); V its <v> text.
	T, V string
}

// Values returns the formula cells' values of xlsx, LibreOffice's
// converted workbook (chunk K10): every worksheet, every cell with an
// <f> and a <v>, in sheet then row-major order. Shared strings are
// resolved (a t="s" value becomes t="str" with its text), so the values
// can be spliced into a workbook with other shared strings.
func Values(xlsx []byte) ([]Value, error) {
	pkg, err := opc.OpenBytes(xlsx, opc.Limits{})
	if err != nil {
		return nil, fmt.Errorf("recalc: open workbook: %w", err)
	}
	defer pkg.Close() //nolint:errcheck // an in-memory package has no file to close
	w, err := edit.New(pkg)
	if err != nil {
		return nil, fmt.Errorf("recalc: read workbook: %w", err)
	}
	var values []Value
	for _, ref := range w.Sheets() {
		if ref.Kind == edit.KindMissing {
			return nil, fmt.Errorf("recalc: sheet %q is missing", ref.Name)
		}
		if ref.Kind != edit.KindWorksheet {
			continue
		}
		part, err := pkg.Part(ref.Part)
		if err != nil {
			return nil, fmt.Errorf("recalc: sheet %q: %w", ref.Name, err)
		}
		src, ok, err := part.OriginalString()
		if err != nil {
			return nil, fmt.Errorf("recalc: sheet %q: %w", ref.Name, err)
		}
		if !ok {
			return nil, fmt.Errorf("recalc: sheet %q has no content", ref.Name)
		}
		var problem error
		_, err = sheet.Scan(src, sheet.ScanOptions{From: 1, To: formula.MaxRows,
			Problem: func(_ sheet.Problem, _, _ int, detail string) { problem = errors.New(detail) },
			Row: func(row *sheet.Row) error {
				for _, c := range row.Cells {
					if !c.F.Present || !c.HasV {
						continue
					}
					v := Value{Sheet: ref.Name, Row: c.Row, Col: c.Col, T: c.T, V: c.V}
					if c.T == "s" {
						idx, e := strconv.Atoi(c.V)
						if e != nil || idx < 0 {
							return fmt.Errorf("invalid shared string index %q", c.V)
						}
						sst, e := w.SST()
						if e != nil {
							return e
						}
						if sst == nil {
							return errors.New("missing shared strings")
						}
						v.V, e = sst.Text(idx)
						if e != nil {
							return e
						}
						v.T = "str"
					}
					values = append(values, v)
				}
				return nil
			}})
		if err != nil {
			return nil, fmt.Errorf("recalc: sheet %q: %w", ref.Name, err)
		}
		if problem != nil {
			return nil, fmt.Errorf("recalc: sheet %q: %w", ref.Name, problem)
		}
		part.Forget()
	}
	return values, nil
}
