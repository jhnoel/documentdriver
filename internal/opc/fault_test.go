package opc

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func TestFaultAtEachStage(t *testing.T) {
	stages := []string{StageCreate, StageWrite, StageChmod, StageSync, StageClose, StageRename}
	for _, stage := range stages {
		for _, closing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/close=%v", stage, closing), func(t *testing.T) {
				path := testdeck.Minimal(t)
				ageFile(t, path)
				s := snap(t, path)
				p := open(t, path)
				setOff(t, p, slide1, "42")
				defer SetFaultHook(func(st, _ string) error {
					if st == stage {
						return errors.New("injected at " + st)
					}
					return nil
				})()
				var err error
				if closing {
					err = p.SaveAndClose()
				} else {
					err = p.Save()
				}
				if err == nil {
					t.Fatal("save succeeded")
				}
				assertUntouched(t, path, s)
			})
		}
	}
}

// A sealed commit fails at each stage before the rename with the file
// untouched, and otherwise writes exactly the sealed bytes, which are
// what Save writes.
func TestSealedCommit(t *testing.T) {
	stages := []string{StageCreate, StageWrite, StageChmod, StageSync, StageClose, StageRename}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			path := testdeck.Minimal(t)
			ageFile(t, path)
			s := snap(t, path)
			p := open(t, path)
			setOff(t, p, slide1, "42")
			sealed, err := p.Seal()
			if err != nil {
				t.Fatal(err)
			}
			defer SetFaultHook(func(st, _ string) error {
				if st == stage {
					return errors.New("injected at " + st)
				}
				return nil
			})()
			if err := p.CommitSealed(sealed, ""); err == nil {
				t.Fatal("commit succeeded")
			}
			assertUntouched(t, path, s)
		})
	}
	path := testdeck.Minimal(t)
	p := open(t, path)
	setOff(t, p, slide1, "42")
	var want bytes.Buffer
	if err := p.WriteZip(&want); err != nil {
		t.Fatal(err)
	}
	sealed, err := p.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if !sealed.Changed() || !bytes.Equal(sealed.Bytes(), want.Bytes()) {
		t.Fatal("the sealed bytes are not what a save writes")
	}
	if err := p.CommitSealed(sealed, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, want.Bytes()) {
		t.Fatal("the commit did not write the sealed bytes")
	}
	// Unchanged: in place writes nothing.
	path = testdeck.Minimal(t)
	ageFile(t, path)
	s := snap(t, path)
	p = open(t, path)
	if sealed, err = p.Seal(); err != nil || sealed.Changed() || !bytes.Equal(sealed.Bytes(), s.data) {
		t.Fatalf("unchanged seal: %v", err)
	}
	if err := p.CommitSealed(sealed, ""); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, s)
}

func TestFaultAfterRenameIsNotAFailure(t *testing.T) {
	path := testdeck.Minimal(t)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	setOff(t, p, slide1, "42")
	defer SetFaultHook(func(st, _ string) error {
		if st == StageDirSync {
			return errors.New("injected at " + st)
		}
		return nil
	})()
	if err := p.SaveAndClose(); err != nil {
		t.Fatalf("SaveAndClose err = %v after the rename", err)
	}
	if b, _ := os.ReadFile(path); bytes.Equal(b, s.data) {
		t.Fatal("not saved")
	}
}

func TestReopenFailureIsDistinct(t *testing.T) {
	path := testdeck.Minimal(t)
	p := open(t, path)
	setOff(t, p, slide1, "42")
	// Truncate the file after the rename so that reopening fails.
	defer SetFaultHook(func(st, file string) error {
		if st == StageDirSync {
			return os.WriteFile(file, []byte("not a zip"), 0o644)
		}
		return nil
	})()
	err := p.Save()
	if !errors.Is(err, ErrReopen) {
		t.Fatalf("Save err = %v, want ErrReopen", err)
	}
}

func TestChangedParts(t *testing.T) {
	p := open(t, testdeck.Minimal(t))
	if got, err := p.ChangedParts(); err != nil || len(got) != 0 {
		t.Fatalf("ChangedParts = %v, %v", got, err)
	}
	setOff(t, p, slide1, "42")
	if _, err := p.AddPart("/ppt/media/x.bin", []byte{1}, ""); err != nil {
		t.Fatal(err)
	}
	got, err := p.ChangedParts()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != slide1+",/ppt/media/x.bin" {
		t.Fatalf("ChangedParts = %v", got)
	}
}
