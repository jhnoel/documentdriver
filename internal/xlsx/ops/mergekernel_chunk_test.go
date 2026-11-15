package ops

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

func TestMergeKernelChunk(t *testing.T) {
	src := `<x:mergeCells count='9'><x:mergeCell ref="$B$2:$A$1"/> <x:mergeCell ref="D4:E5"/></x:mergeCells>`
	got, err := AddMerge(src, "x:", "F1:G2")
	if err != nil || got != `<x:mergeCells count='3'><x:mergeCell ref="$B$2:$A$1"/> <x:mergeCell ref="D4:E5"/><x:mergeCell ref="F1:G2"/></x:mergeCells>` {
		t.Fatalf("add: %q %v", got, err)
	}
	got, err = RemoveMerge(got, "a1:b2")
	if err != nil || got != `<x:mergeCells count='2'> <x:mergeCell ref="D4:E5"/><x:mergeCell ref="F1:G2"/></x:mergeCells>` {
		t.Fatalf("remove: %q %v", got, err)
	}
	var r *Rejection
	_, err = AddMerge(src, "x:", "B2:C3")
	if !errors.As(err, &r) || r.Code != cli.CodeOperationFailed || r.Detail != "$B$2:$A$1" {
		t.Fatalf("overlap: %v", err)
	}
	got, err = AddMerge(`<mergeCells/>`, "", "A1:B1")
	if err != nil || got != `<mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells>` {
		t.Fatalf("empty: %q %v", got, err)
	}
	for _, s := range []string{`<mergeCells><mergeCell/></mergeCells>`, `<mergeCells><mergeCell ref="bogus"/></mergeCells>`, `<mergeCells>`} {
		if _, err = AddMerge(s, "", "A1:B1"); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
