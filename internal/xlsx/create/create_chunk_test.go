package create

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestRunBlankAndEmptyPlanChunk(t *testing.T) {
	template, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*ops.Plan{nil, {}} {
		out := filepath.Join(t.TempDir(), "out.xlsx")
		res, err := Run(p, Options{Out: out})
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, template) || res.Base != "" || res.NewBase != cli.Hash(got) || res.Saved != out {
			t.Fatalf("unexpected result: %+v", res)
		}
		if res.DryRun {
			t.Errorf("blank create got dry run: %+v", res)
		}
	}
}

func TestRunDryAndRefusalChunk(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.xlsx")
	res, err := Run(nil, Options{Out: out, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Base != "" || res.NewBase != "" || res.Saved != "" {
		t.Fatalf("unexpected dry result: %+v", res)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run wrote output: %v", err)
	}
	_, err = Run(&ops.Plan{Base: "some base"}, Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != cli.CodeInvalidPlan || ce.Path != "base" {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused run wrote output")
	}
	if _, err := Run(nil, Options{}); err == nil {
		t.Fatal("missing output accepted")
	} else {
		var ce *cli.Error
		if !errors.As(err, &ce) || ce.Code != cli.CodeUsage {
			t.Fatalf("missing output: unexpected error %v", err)
		}
	}
	if _, err := Run(nil, Options{Out: filepath.Join(out, "missing.xlsx")}); err == nil {
		t.Fatal("invalid output accepted")
	}
}
