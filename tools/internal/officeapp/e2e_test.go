package officeapp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bookApp is a fake app that opens files by their original name: a name
// in alerts shows that alert text (a repair prompt), any other opens
// clean. It checks that every file it is given is a flat copy in the
// work folder.
type bookApp struct {
	t       *testing.T
	workdir string
	alerts  map[string]string // original base name -> alert text
	running bool
	current string // base name of the open copy
	opened  []string
	quits   int
}

// Running reports whether the fake app is running.
func (d *bookApp) Running() (bool, error) { return d.running, nil }

// Open records the copy path and marks the fake app running.
func (d *bookApp) Open(path string) error {
	if filepath.Dir(path) != d.workdir {
		d.t.Errorf("app given %s, outside the work folder", path)
	}
	if _, err := os.Stat(path); err != nil {
		d.t.Errorf("app given a missing copy: %v", err)
	}
	d.opened = append(d.opened, path)
	d.running, d.current = true, filepath.Base(path)
	return nil
}

// ForceQuit records the quit and stops the fake app.
func (d *bookApp) ForceQuit() error { d.quits++; d.running = false; return nil }

// original returns the base name before the copy prefix separator.
func (d *bookApp) original() string {
	_, name, _ := strings.Cut(d.current, "--")
	return name
}

// Run returns the listing or the alert text for the open copy.
func (d *bookApp) Run(lines []string, _ int) (string, error) {
	alert, bad := d.alerts[d.original()]
	if strings.Contains(strings.Join(lines, "\n"), "name of workbooks") {
		if bad {
			return "", &OSAError{Num: ErrAETimeout, Msg: "timeout (-1712)"}
		}
		return d.current, nil
	}
	if bad {
		return alert + "\n" + d.current, nil
	}
	return "", nil
}

// writeXlsx writes a fake workbook and returns its path.
func writeXlsx(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("PK fake "+name), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSmokeEndToEnd runs the command over a source and its outputs with
// a fake app: copies, verdicts relative to the source, JSON report,
// exit status, and the work folder left empty.
func TestSmokeEndToEnd(t *testing.T) {
	fakeTime(t)
	s := testSmoke()
	s.App.AppPath = t.TempDir()
	in, wd := t.TempDir(), t.TempDir()
	repairA := "We found a problem with some content in 'src.xlsx'. Do you want us to try to recover as much as we can? If you trust the source of this workbook, click Yes."
	repairB := "Excel cannot open the file 'worse.xlsx' because the file format or file extension is not valid."
	src := writeXlsx(t, in, "src.xlsx")
	same := writeXlsx(t, in, "same.xlsx")
	worse := writeXlsx(t, in, "worse.xlsx")
	good := writeXlsx(t, in, "good.xlsx")
	d := &bookApp{t: t, workdir: wd, alerts: map[string]string{
		"src.xlsx": repairA, "same.xlsx": strings.Replace(repairA, "src.xlsx", "same.xlsx", 1), "worse.xlsx": repairB,
	}}
	var out, errb bytes.Buffer
	code := s.Main(d, []string{"--json", "--timeout", "5", "--workdir", wd, "--source", src, same, worse, good}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (one output worse than its source); stderr %s", code, errb.String())
	}
	var got []record
	dec := json.NewDecoder(&out)
	for dec.More() {
		var r record
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	type verdict struct {
		file, role, status string
		pass               bool
	}
	var verdicts []verdict
	for _, r := range got {
		verdicts = append(verdicts, verdict{filepath.Base(r.File), r.Role, r.Status, r.Pass})
	}
	want := []verdict{
		{"src.xlsx", "source", StatusRepair, true},
		{"same.xlsx", "output", StatusRepair, true},
		{"worse.xlsx", "output", StatusOpenError, false},
		{"good.xlsx", "output", StatusClean, true},
	}
	if !slices.Equal(verdicts, want) {
		t.Fatalf("report %+v, want %+v", verdicts, want)
	}
	if len(d.opened) != 4 || d.running {
		t.Fatalf("opened %d files, app running %v: want 4 and quit", len(d.opened), d.running)
	}
	left, err := os.ReadDir(wd)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("work folder keeps %d files", len(left))
	}

	// Without the worse output the run passes.
	out.Reset()
	if code := s.Main(d, []string{"--workdir", wd, "--source", src, same, good}, &out, &errb); code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out.String())
	}
}

// TestSmokeRefusesBadInputsBeforeApp checks inputs before touching the
// app.
func TestSmokeRefusesBadInputsBeforeApp(t *testing.T) {
	s := testSmoke()
	s.App.AppPath = t.TempDir()
	in := t.TempDir()
	d := &bookApp{t: t, workdir: t.TempDir()}
	notXlsx := filepath.Join(in, "x.xltx")
	if err := os.WriteFile(notXlsx, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--source", filepath.Join(in, "missing.xlsx"), writeXlsx(t, in, "a.xlsx")},
		{notXlsx},
	} {
		var out, errb bytes.Buffer
		if code := s.Main(d, append([]string{"--workdir", d.workdir}, args...), &out, &errb); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	if len(d.opened) != 0 || d.quits != 0 {
		t.Fatal("bad inputs must not open or quit the app")
	}
}

// TestSmokeUsageErrors checks usage errors print the usage text to
// stderr and exit 2.
func TestSmokeUsageErrors(t *testing.T) {
	s := testSmoke()
	s.App.AppPath = t.TempDir()
	d := &bookApp{t: t, workdir: t.TempDir()}
	for _, args := range [][]string{{}, {"--nope"}} {
		var out, errb bytes.Buffer
		if code := s.Main(d, args, &out, &errb); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if !strings.Contains(errb.String(), s.Usage) {
			t.Errorf("%q: stderr %q does not contain the usage text", args, errb.String())
		}
	}
}
