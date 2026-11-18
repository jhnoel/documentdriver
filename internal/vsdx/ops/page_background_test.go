package ops_test

import (
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// bgSummary returns each page of path as id, background,
// background_page in stored order.
func bgSummary(t *testing.T, path string) [][3]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := read.Document(b, "sha256:x")
	if err != nil {
		t.Fatal(err)
	}
	var out [][3]string
	for _, p := range sum.Pages {
		bg := ""
		if p.Background {
			bg = "bg"
		}
		out = append(out, [3]string{p.ID, bg, p.BackPage})
	}
	return out
}

func sameSummary(a, b [][3]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPageAddBeforeBackgroundPages checks the impact row: on V8
// (page0, page1, background page3) page.add with neither at nor
// position puts the new foreground page after page1, before page3;
// a background page is appended.
func TestPageAddBeforeBackgroundPages(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Fixture(t, "v8-pages"), `{"ops":[{"op":"page.add","name":"New"},{"op":"page.add","name":"BG2","background":true}]}`)
	got := bgSummary(t, out)
	want := [][3]string{{"page0", "", "page3"}, {"page1", "", ""}, {"page4", "", ""}, {"page3", "bg", ""}, {"page5", "bg", ""}}
	if !sameSummary(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

// TestBackgroundPagesWrite checks the Background pages rows on a
// created drawing: page.add background and page.set background_page
// read back; identity writes nothing; "none" removes the use; making
// a page foreground moves it after the last foreground page.
func TestBackgroundPagesWrite(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"page.add","name":"BG","background":true,"as":"bg"},{"op":"page.add","name":"Two"},{"op":"page.set","at":"page0","background_page":"$bg"}]}`)
	want := [][3]string{{"page0", "", "page1"}, {"page2", "", ""}, {"page1", "bg", ""}}
	if got := bgSummary(t, out); !sameSummary(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
	if _, changed := refusalOut(t, out, `{"ops":[{"op":"page.set","at":"page0","background_page":"page1"},{"op":"page.set","at":"page1","background":true}]}`); len(changed) != 0 {
		t.Errorf("identity: changed = %v", changed)
	}
	back, changed := refusalOut(t, out, `{"ops":[{"op":"page.set","at":"page0","background_page":"none"},{"op":"page.set","at":"page1","background":false}]}`)
	if len(changed) != 2 {
		t.Errorf("changed = %v, want page0 and page1", changed)
	}
	want = [][3]string{{"page0", "", ""}, {"page2", "", ""}, {"page1", "", ""}}
	if got := bgSummary(t, back); !sameSummary(got, want) {
		t.Errorf("after none and foreground: pages = %v, want %v", got, want)
	}
}

// TestBackgroundPagesRefusals checks the refusals, each at its field:
// itself, a foreground page, a missing page, a page in use made
// foreground, the last foreground page made a background, page.add
// at a place that breaks the order, and a three-page cycle.
func TestBackgroundPagesRefusals(t *testing.T) {
	out, _ := refusalOut(t, vsdxtest.Blank(t), `{"ops":[{"op":"page.add","name":"BG","background":true},{"op":"page.add","name":"Two"},{"op":"page.set","at":"page0","background_page":"page1"}]}`)
	expectRefusal(t, out, `{"ops":[{"op":"page.set","at":"page0","background_page":"page0"}]}`, "background_page", "cannot use itself")
	expectRefusal(t, out, `{"ops":[{"op":"page.set","at":"page0","background_page":"page2"}]}`, "background_page", "is a foreground page")
	expectRefusalCode(t, out, `{"ops":[{"op":"page.set","at":"page0","background_page":"page9"}]}`, "not_found", "background_page")
	expectRefusal(t, out, `{"ops":[{"op":"page.set","at":"page1","background":false}]}`, "background", "uses page1 as its background")
	expectRefusal(t, vsdxtest.Blank(t), `{"ops":[{"op":"page.set","at":"page0","background":true}]}`, "background", "is the last foreground page")
	expectRefusal(t, out, `{"ops":[{"op":"page.add","name":"X","at":"page1","position":"after"}]}`, "position", "foreground pages come before background pages")
	_, err := apply.Run(out, parsePlan(t, `{"ops":[{"op":"page.set","at":"page2","background":true},{"op":"page.set","at":"page2","background_page":"page1"},{"op":"page.add","name":"BG3","background":true,"background_page":"page2"},{"op":"page.set","at":"page1","background_page":"page3"}]}`), apply.Options{DryRun: true})
	if err == nil || !contains(err.Error(), "cycle") {
		t.Errorf("three-page cycle: err %v, want a cycle refusal", err)
	}
}
