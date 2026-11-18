package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// expectApplyRefusal runs planText on the fixture path and checks
// it fails with operation_failed at ops[0].field and a message
// containing want, writing no out file.
func expectApplyRefusal(t *testing.T, path, planText, field, want string) {
	t.Helper()
	parsed, errs := ops.ParsePlan([]byte(planText))
	if len(errs) != 0 || parsed == nil {
		t.Fatalf("plan %s: parse errors %v, want a plan", planText, errs)
	}
	out := filepath.Join(t.TempDir(), "out.vsdx")
	_, err := Run(path, parsed, Options{Out: out})
	var ce *cli.Error
	if !errors.As(err, &ce) {
		t.Fatalf("%s: error %v, want *cli.Error", planText, err)
	}
	if ce.Code != cli.CodeOperationFailed || ce.Path != "ops[0]."+field {
		t.Errorf("%s: got %s at %q, want operation_failed at ops[0].%s", planText, ce.Code, ce.Path, field)
	}
	if !strings.Contains(ce.Message, want) {
		t.Errorf("%s: message %q, want it to contain %q", planText, ce.Message, want)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("%s: a refused op wrote its out file", planText)
	}
}

// TestGlueOtherCellRefused checks the CC14 refusal row: V5
// page0/s5 to_glue "s2@Width" (a cell other than a connection
// point) is invalid_operation at ops[0].to_glue, before open:
// ParsePlan alone, with no drawing, reports it.
func TestGlueOtherCellRefused(t *testing.T) {
	_, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s5","to_glue":"s2@Width"}]}`))
	if len(errs) != 1 {
		t.Fatalf("plan: errors %v, want one invalid_operation", errs)
	}
	e := errs[0]
	if e.Code != cli.CodeInvalidOperation || e.Path != "ops[0].to_glue" {
		t.Errorf("got %s at %q, want invalid_operation at ops[0].to_glue", e.Code, e.Path)
	}
}

// TestConnectorEndsCoincide checks the CC14 refusal row:
// object.add of a connector with from and to both [10,10] is
// operation_failed at ops[0].to, "the connector ends coincide".
func TestConnectorEndsCoincide(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	expectApplyRefusal(t, path,
		`{"ops":[{"op":"object.add","at":"page0","kind":"connector","from":[10,10],"to":[10,10]}]}`,
		"to", "the connector ends coincide")
}

// TestShapeTakesNoEnds checks the CC14 refusal row: object.set
// from [1,2] on V1 page0/s1, a 2-D shape, is operation_failed at
// ops[0].at, "page0/s1 is not a 1-D shape".
func TestShapeTakesNoEnds(t *testing.T) {
	path := vsdxtest.Fixture(t, "v1-minimal")
	expectApplyRefusal(t, path,
		`{"ops":[{"op":"object.set","at":"page0/s1","from":[1,2]}]}`,
		"at", "page0/s1 is not a 1-D shape")
}

// TestSubShapeDeleteRefused checks the CC14 refusal row: V2
// page0/s4 object.delete is operation_failed at ops[0].at,
// "page0/s4 is inherited from master3 (master shape 6)".
func TestSubShapeDeleteRefused(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	expectApplyRefusal(t, path,
		`{"ops":[{"op":"object.delete","at":"page0/s4"}]}`,
		"at", "page0/s4 is inherited from master3 (master shape 6)")
}
