package opc

import (
	"errors"
	"sync"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// A hook scoped to one target fails that target's saves only, and sees
// none of the stages of saves to other files running at the same time.
func TestFaultHookForScopesToTarget(t *testing.T) {
	failing := testdeck.Minimal(t)
	ageFile(t, failing)
	before := snap(t, failing)
	var mu sync.Mutex
	seen := map[string]int{}
	defer SetFaultHookFor(failing, func(st, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		seen[st]++
		if st == StageRename {
			return errors.New("injected at " + st)
		}
		return nil
	})()
	var wg sync.WaitGroup
	others := make([]string, 8)
	for i := range others {
		others[i] = testdeck.Minimal(t)
	}
	errs := make([]error, len(others))
	for i, path := range others {
		wg.Go(func() {
			p, err := Open(path)
			if err != nil {
				errs[i] = err
				return
			}
			setOff(t, p, slide1, "43")
			errs[i] = p.SaveAndClose()
		})
	}
	p := open(t, failing)
	setOff(t, p, slide1, "42")
	err := p.Save()
	wg.Wait()
	if err == nil {
		t.Fatal("the scoped save succeeded")
	}
	assertUntouched(t, failing, before)
	for i, err := range errs {
		if err != nil {
			t.Errorf("save %d of another file: %v", i, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if seen[StageCreate] != 1 || seen[StageRename] != 1 {
		t.Errorf("the hook saw %v, want the one save's stages", seen)
	}
}

// A scoped hook also sees a save of the document's package to another
// file (a plan's snapshot), as a process-wide hook does.
func TestFaultHookForSeesSnapshots(t *testing.T) {
	src := testdeck.Minimal(t)
	snap := t.TempDir() + "/snapshot.pptx"
	defer SetFaultHookFor(src, func(st, _ string) error {
		if st == StageCreate {
			return errors.New("injected at " + st)
		}
		return nil
	})()
	p := open(t, src)
	setOff(t, p, slide1, "42")
	if err := p.SaveAs(snap); err == nil {
		t.Fatal("a snapshot of the scoped document saved")
	}
	other := open(t, testdeck.Minimal(t))
	setOff(t, other, slide1, "42")
	if err := other.SaveAs(t.TempDir() + "/other.pptx"); err != nil {
		t.Fatalf("another document's snapshot: %v", err)
	}
}
