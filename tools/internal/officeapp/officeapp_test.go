package officeapp

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeApp scripts the app's answers; it never starts anything.
type fakeApp struct {
	// openErrs are the errors the first opens return.
	openErrs []error
	running  bool
	opened   []string
	docs     string // what "name of workbooks" returns once opened
	texts    string // window texts
	listErr  error  // error for the document listing
	quits    int
	log      []string // scripts run, first line
}

// Running reports whether the fake app is running.
func (f *fakeApp) Running() (bool, error) { return f.running, nil }

// Open records the path and returns the next scripted error, if any.
func (f *fakeApp) Open(path string) error {
	f.opened = append(f.opened, path)
	if len(f.openErrs) > 0 {
		err := f.openErrs[0]
		f.openErrs = f.openErrs[1:]
		return err
	}
	f.running = true
	return nil
}

// ForceQuit records the quit and stops the fake app.
func (f *fakeApp) ForceQuit() error { f.quits++; f.running = false; return nil }

// Run returns the scripted listing or window texts for the script.
func (f *fakeApp) Run(lines []string, _ int) (string, error) {
	f.log = append(f.log, lines[len(lines)-1])
	if strings.Contains(strings.Join(lines, "\n"), "name of workbooks") {
		return f.docs, f.listErr
	}
	return f.texts, nil
}

// fakeTime replaces the clock and sleep with a clock sleep advances.
func fakeTime(t *testing.T) {
	t.Helper()
	now := time.Unix(0, 0)
	clock = func() time.Time { return now }
	sleep = func(d time.Duration) { now = now.Add(d) }
	t.Cleanup(func() { clock, sleep = time.Now, time.Sleep })
}

// testClassify is the test smoke's alert classifier, standing in for a
// tool's classify over the phrases the tests use.
func testClassify(texts []string) string {
	for _, text := range texts {
		l := strings.ToLower(text)
		switch {
		case strings.Contains(l, "grant file access"):
			return StatusAccessDialog
		case strings.Contains(l, "problem with some content") || strings.Contains(l, "unreadable content") || strings.Contains(l, "repair"):
			return StatusRepair
		case strings.Contains(l, "cannot open"):
			return StatusOpenError
		}
	}
	return ""
}

// testSmoke returns the excel-shaped smoke the tests drive.
func testSmoke() Smoke {
	return Smoke{
		Tool:     "excel-smoke",
		Noun:     "workbooks",
		Exts:     []string{".xlsx", ".xlsm"},
		Classify: testClassify,
		App:      Spec{AppName: "Microsoft Excel", ProcName: "Microsoft Excel", AppPath: "/Applications/Microsoft Excel.app", QuitSettle: time.Second},
		Usage:    "usage: excel-smoke test\n",
	}
}

// TestOpenAndWaitClean checks a file that is listed opens clean.
func TestOpenAndWaitClean(t *testing.T) {
	fakeTime(t)
	a := &fakeApp{docs: "other.xlsx\n1--in.xlsx"}
	res, err := testSmoke().openAndWait(a, "/w/1--in.xlsx", 10*time.Second)
	if err != nil || res.Status != StatusClean {
		t.Fatalf("got %+v, %v", res, err)
	}
}

// TestOpenAndWaitRepair checks a repair prompt is classified and its
// file name blanked.
func TestOpenAndWaitRepair(t *testing.T) {
	fakeTime(t)
	a := &fakeApp{texts: "We found a problem with some content in '1--out.xlsx'. Do you want us to try to recover as much as we can?\n1--out.xlsx"}
	res, err := testSmoke().openAndWait(a, "/w/1--out.xlsx", 10*time.Second)
	if err != nil || res.Status != StatusRepair {
		t.Fatalf("got %+v, %v", res, err)
	}
	if len(res.Alerts) != 1 || strings.Contains(res.Alerts[0], "1--out") {
		t.Fatalf("alerts %q: want one text with the file name blanked", res.Alerts)
	}
}

// TestOpenAndWaitDialog checks repeated listing timeouts become a
// dialog.
func TestOpenAndWaitDialog(t *testing.T) {
	fakeTime(t)
	a := &fakeApp{listErr: &OSAError{Num: ErrAETimeout, Msg: "timeout (-1712)"}}
	res, _ := testSmoke().openAndWait(a, "/w/x.xlsx", 5*time.Second)
	if res.Status != StatusDialog {
		t.Fatalf("got %s, want dialog", res.Status)
	}
}

// TestOpenAndWaitNotAuthorised checks a -1743 listing aborts the run.
func TestOpenAndWaitNotAuthorised(t *testing.T) {
	fakeTime(t)
	a := &fakeApp{listErr: ParseOSAError("1:2: execution error: Not authorised to send Apple events to Microsoft Excel. (-1743)")}
	if _, err := testSmoke().openAndWait(a, "/w/x.xlsx", 5*time.Second); err == nil {
		t.Fatal("want an abort for -1743")
	}
}

// TestPassesRelative is the input-relative rule: a dirty source's
// prompt does not fail its outputs, a new prompt does.
func TestPassesRelative(t *testing.T) {
	clean := openResult{Status: StatusClean}
	repair := openResult{Status: StatusRepair, Alerts: []string{"We found a problem with some content in <file> Do you want us to try to recover as much as we can?"}}
	other := openResult{Status: StatusRepair, Alerts: []string{"We found a problem with some content in <file> Do you want us to try to recover as much as we can?", "a new alert"}}
	cases := []struct {
		out  openResult
		src  *openResult
		want bool
	}{
		{clean, nil, true},
		{repair, nil, false},
		{repair, &clean, false},
		{repair, &repair, true},
		{clean, &repair, true},
		{other, &repair, false},
		{openResult{Status: StatusOpenError}, &repair, false},
	}
	for i, c := range cases {
		if got := passes(c.out, c.src); got != c.want {
			t.Errorf("case %d: passes = %v, want %v", i, got, c.want)
		}
	}
}

// TestRefusesWhileAppRuns checks preflight refuses while the app runs,
// without opening or quitting it.
func TestRefusesWhileAppRuns(t *testing.T) {
	var errb bytes.Buffer
	s := testSmoke()
	s.App.AppPath = t.TempDir()
	a := &fakeApp{running: true}
	if code := s.preflight(a, t.TempDir(), &errb); code != 2 {
		t.Fatalf("preflight = %d, want 2", code)
	}
	if len(a.opened) != 0 || a.quits != 0 {
		t.Fatal("preflight must not open or quit the app")
	}
	if !strings.Contains(errb.String(), "Microsoft Excel is running") {
		t.Fatalf("stderr %q: want the running refusal", errb.String())
	}
}

// TestAppleScriptQuote checks Quote escapes quotes and backslashes.
func TestAppleScriptQuote(t *testing.T) {
	if got := Quote(`a"b\c`); got != `"a\"b\\c"` {
		t.Fatalf("Quote = %s", got)
	}
}

// TestOpenRetriesLaunchRace: an open that fails with LaunchServices error
// -600 right after a force quit is retried; one that keeps failing, or
// fails otherwise, is an error.
func TestOpenRetriesLaunchRace(t *testing.T) {
	race := errors.New("open: exit status 1: LSOpenURLsWithRole() failed with error -600 for the file /w/1--in.xlsx")
	for _, c := range []struct {
		name   string
		errs   []error
		status string
		opens  int
	}{
		{"once", []error{race}, StatusClean, 2},
		{"twice", []error{race, race}, StatusClean, 3},
		{"always", []error{race, race, race}, StatusError, 3},
		{"other", []error{errors.New("open: exit status 1: no such file")}, StatusError, 1},
	} {
		fakeTime(t)
		a := &fakeApp{openErrs: c.errs, docs: "1--in.xlsx"}
		res, err := testSmoke().openAndWait(a, "/w/1--in.xlsx", 10*time.Second)
		if err != nil || res.Status != c.status || len(a.opened) != c.opens {
			t.Errorf("%s: %+v, %v after %d opens; want %s after %d", c.name, res, err, len(a.opened), c.status, c.opens)
		}
	}
}

// TestExts checks extension matching and file-name blanking.
func TestExts(t *testing.T) {
	s := testSmoke()
	for name, want := range map[string]bool{"a.xlsx": true, "A.XLSM": true, "t.xltx": false, "b.xlsb": false, "d.docx": false} {
		if got := s.hasExt(name); got != want {
			t.Errorf("hasExt(%q) = %v", name, got)
		}
	}
	if got := s.blankFileName("We found a problem with some content in '1--out.xlsm'. Do you"); got != "We found a problem with some content in <file> Do you" {
		t.Errorf("blankFileName = %q", got)
	}
}

// TestParseOSAErrorAndIsOSA checks error-number parsing and matching.
func TestParseOSAErrorAndIsOSA(t *testing.T) {
	err := ParseOSAError("1:2: execution error: Not authorised to send Apple events to Microsoft Excel. (-1743)")
	if err.Num != ErrNotAuthorised {
		t.Fatalf("ParseOSAError Num = %d, want %d", err.Num, ErrNotAuthorised)
	}
	if !IsOSA(err, ErrNotAuthorised) || IsOSA(err, ErrAETimeout) {
		t.Fatal("IsOSA does not match the parsed number")
	}
	plain := ParseOSAError("no number here")
	if plain.Num != 0 || plain.Error() != "no number here" {
		t.Fatalf("ParseOSAError without a number = %+v", plain)
	}
	if IsOSA(errors.New("boom"), ErrAETimeout) {
		t.Fatal("IsOSA matches a non-OSA error")
	}
}

// TestCopyFile checks CopyFile creates a new dst and refuses to
// overwrite one.
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src.xlsx"
	if err := os.WriteFile(src, []byte("PK fake src.xlsx"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := dir + "/dst.xlsx"
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "PK fake src.xlsx" {
		t.Fatalf("copied %q, %v", b, err)
	}
	if err := CopyFile(src, dst); err == nil {
		t.Fatal("CopyFile over an existing dst: want an error")
	}
}

// TestDefaultWorkdirEnv checks the environment override.
func TestDefaultWorkdirEnv(t *testing.T) {
	t.Setenv("PPTORACLE_WORKDIR", "/tmp/work")
	if got := DefaultWorkdir(); got != "/tmp/work" {
		t.Fatalf("DefaultWorkdir = %q, want /tmp/work", got)
	}
}
