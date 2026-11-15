package apply

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// Chunk G18 tests for recalcCommit (designer rows; frozen test is
// TestRecalcSplicesValues in recalc_test.go). They need G17 (Compute) and
// skip on its stub panic via xlsxtest.Skip until it lands.

func chunkSoffice(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("DD_SOFFICE"); bin != "" {
		return bin
	}
	bin, err := exec.LookPath("soffice")
	if err != nil {
		t.Skip("no soffice on PATH and DD_SOFFICE is not set")
	}
	return bin
}

func chunkBook(t *testing.T) (input, sealed []byte, path string) {
	t.Helper()
	inBytes, err := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "Budget",
		Rows: `<row r="1"><c r="B1"><v>4</v></c></row>`}}}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	sealedBytes, err := xlsxtest.Book{CalcPr: `<calcPr fullCalcOnLoad="1"/>`,
		Sheets: []xlsxtest.Sheet{{Name: "Budget",
			Rows: `<row r="1"><c r="B1"><v>4</v></c><c r="C1"><f>B1+1</f></c></row>`}}}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "in.xlsx")
	if err := os.WriteFile(path, inBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return inBytes, sealedBytes, path
}

func chunkScope() integrity.Scope {
	return integrity.Scope{Parts: []string{"/xl/worksheets/sheet1.xml"}}
}

func TestRecalcCommitChunkUnavailable(t *testing.T) {
	xlsxtest.Skip(t, func() {
		input, sealed, path := chunkBook(t)
		target := filepath.Join(t.TempDir(), "out.xlsx")
		_, _, err := recalcCommit(input, sealed, path, target, chunkScope(), Options{Out: target, Recalc: &RecalcOptions{Soffice: "/nonexistent/soffice"}})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeOperationFailed || e.Path != "--recalc" || e.Hint != "install LibreOffice or set DD_SOFFICE" {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Error("a refused recalc wrote the target")
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, input) { //nolint:gosec // test input
			t.Error("a refused recalc changed the input file")
		}
	})
}

func TestRecalcCommitChunkSplicesDryRun(t *testing.T) {
	xlsxtest.Skip(t, func() {
		bin := chunkSoffice(t)
		input, sealed, path := chunkBook(t)
		target := filepath.Join(t.TempDir(), "out.xlsx")
		out, _, err := recalcCommit(input, sealed, path, target, chunkScope(), Options{DryRun: true, Out: target, Recalc: &RecalcOptions{Soffice: bin}})
		if err != nil {
			t.Fatal(err)
		}
		parts, err := xlsxtest.Parts(out)
		if err != nil {
			t.Fatal(err)
		}
		if s := string(parts["xl/worksheets/sheet1.xml"]); !strings.Contains(s, `<c r="C1"><f>B1+1</f><v>5</v></c>`) {
			t.Errorf("value not spliced: %s", s)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Error("a dry run wrote the target")
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, input) { //nolint:gosec // test input
			t.Error("a dry run changed the input file")
		}
	})
}

func TestRecalcCommitChunkWrites(t *testing.T) {
	xlsxtest.Skip(t, func() {
		bin := chunkSoffice(t)
		input, sealed, path := chunkBook(t)
		// Out of place: the input file is left alone, the target is written.
		target := filepath.Join(t.TempDir(), "out.xlsx")
		out, _, err := recalcCommit(input, sealed, path, target, chunkScope(), Options{Out: target, Recalc: &RecalcOptions{Soffice: bin}})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(target); !bytes.Equal(got, out) { //nolint:gosec // test output
			t.Error("the written target differs from the returned bytes")
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, input) { //nolint:gosec // test input
			t.Error("an out-of-place recalc changed the input file")
		}
		// In place: the input file is overwritten with the second output.
		out, _, err = recalcCommit(input, sealed, path, path, chunkScope(), Options{Recalc: &RecalcOptions{Soffice: bin}})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, out) { //nolint:gosec // test input
			t.Error("the in-place target was not written")
		}
	})
}

func TestRecalcCommitChunkChangedOnDisk(t *testing.T) {
	xlsxtest.Skip(t, func() {
		bin := chunkSoffice(t)
		input, sealed, path := chunkBook(t)
		other := []byte("changed while the plan ran")
		if err := os.WriteFile(path, other, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := recalcCommit(input, sealed, path, path, chunkScope(), Options{Recalc: &RecalcOptions{Soffice: bin}})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeIO || e.Message != "the workbook changed on disk while the plan ran" {
			t.Fatalf("got %v", err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, other) { //nolint:gosec // test input
			t.Error("a changed-on-disk recalc overwrote the file")
		}
		// A dry run does not check the file on disk and writes nothing.
		if _, _, err := recalcCommit(input, sealed, path, path, chunkScope(), Options{DryRun: true, Recalc: &RecalcOptions{Soffice: bin}}); err != nil {
			t.Fatalf("dry run with a changed disk: %v", err)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, other) { //nolint:gosec // test input
			t.Error("a dry run changed the file")
		}
	})
}
