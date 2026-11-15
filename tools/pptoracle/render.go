package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/jhnoel/documentdriver/tools/internal/officeapp"
)

const renderUsage = `usage: pptoracle render deck.pptx (--slide slideN -o out.png | --out-dir dir) [--dpi 144] [--pdf deck.pdf] [--timeout 60] [--workdir dir]

Render slides through PowerPoint itself. PowerPoint saves the deck as PDF
(its own "save as PNG" writes nothing when scripted), and pdftoppm turns
the requested pages into PNG. --slide names a slide part (slide3 =
/ppt/slides/slide3.xml), mapped to its position through presentation.xml's
slide list. --out-dir writes every visible slide as <dir>/slideN.png, named
by part. Hidden slides are not in PowerPoint's PDF and are skipped.
`

const resaveUsage = `usage: pptoracle resave deck.pptx -o out.pptx [--timeout 60] [--workdir dir]

Open a copy of the deck in PowerPoint and save it as a new .pptx, so the
result shows what PowerPoint itself writes (for example recomputed autofit).
`

// sessionFlags are shared by render and resave.
type sessionFlags struct {
	timeout float64
	grace   float64
	workdir string
}

// register adds the shared session flags to the flag set.
func (s *sessionFlags) register(fs *flag.FlagSet) {
	fs.Float64Var(&s.timeout, "timeout", 60, "seconds to wait for opening and for saving")
	fs.Float64Var(&s.grace, "dialog-grace", 0, "when a save hangs, seconds to leave PowerPoint and its dialog up (watching for the file) before force-quitting")
	fs.StringVar(&s.workdir, "workdir", officeapp.DefaultWorkdir(), "folder PowerPoint may read and write (granted once); copies live here")
}

// parseInterleaved parses flags that may follow positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// cmdRender renders slides to PNG through PowerPoint and returns the exit code.
func cmdRender(a app, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, renderUsage) }
	var sf sessionFlags
	sf.register(fs)
	slide := fs.String("slide", "", "slide part to export, such as slide3")
	out := fs.String("o", "", "output PNG for --slide")
	outDir := fs.String("out-dir", "", "folder for every slide's PNG")
	pdfOut := fs.String("pdf", "", "also keep PowerPoint's PDF of the deck here")
	dpi := fs.Int("dpi", 144, "PNG resolution (72 = one pixel per point)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || (*slide == "") == (*outDir == "") || (*slide != "" && *out == "") {
		fs.Usage()
		return 2
	}
	deck := pos[0]
	order, err := slideOrder(deck)
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle render:", err)
		return 2
	}
	want := -1
	if *slide != "" {
		if want, err = slideIndex(deck, *slide); err != nil {
			fmt.Fprintln(stderr, "pptoracle render:", err)
			return 2
		}
	}
	if code := preflight(a, stderr); code != 0 {
		return code
	}
	hidden, err := slidesHidden(deck, order)
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle render:", err)
		return 2
	}
	if want > 0 && hidden[want-1] {
		fmt.Fprintf(stderr, "pptoracle render: %s is hidden, and PowerPoint leaves hidden slides out of its PDF\n", *slide)
		return 2
	}
	raster, rerr := exec.LookPath("pdftoppm")
	if rerr != nil && *pdfOut == "" {
		fmt.Fprintln(stderr, "pptoracle render: needs pdftoppm (poppler) to turn PowerPoint's PDF into PNG; or pass --pdf to keep the PDF only")
		return 2
	}
	err = withSession(a, deck, sf, func(rd *runDir) error {
		pdf := rd.file("render.pdf")
		if err := save(a, pdf, "save as PDF", sf); err != nil {
			return err
		}
		if err := waitFile(pdf, 10*time.Second); err != nil {
			return fmt.Errorf("PowerPoint reported success but wrote no PDF: %v", err)
		}
		data, err := os.ReadFile(pdf)
		if err != nil {
			return err
		}
		visible := 0
		page := make([]int, len(order)) // 1-based PDF page per slide, 0 if hidden
		for i := range order {
			if !hidden[i] {
				visible++
				page[i] = visible
			}
		}
		if n := pdfPageCount(data); n != visible {
			return fmt.Errorf("PowerPoint's PDF has %d pages for %d visible slides", n, visible)
		}
		if *pdfOut != "" {
			_ = os.Remove(*pdfOut) // outputs are replaced, like -o
			if err := officeapp.CopyFile(pdf, *pdfOut); err != nil {
				return err
			}
			if rerr != nil {
				return nil
			}
		}
		rasterize := func(p int, dst string) error {
			// pdftoppm runs as this process, so its files need no grant.
			prefix := rd.file("page" + strconv.Itoa(p))
			cmd := exec.Command(raster, "-png", "-singlefile", "-r", strconv.Itoa(*dpi),
				"-f", strconv.Itoa(p), "-l", strconv.Itoa(p), pdf, prefix)
			if b, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("pdftoppm: %v: %s", err, b)
			}
			return moveFile(prefix+".png", dst)
		}
		if want > 0 {
			return rasterize(page[want-1], *out)
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		for i, part := range order {
			if page[i] == 0 {
				continue
			}
			if err := rasterize(page[i], filepath.Join(*outDir, partBase(part)+".png")); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle render:", err)
		return 1
	}
	if want > 0 {
		fmt.Fprintf(stdout, "%s (slide %d of %d) -> %s\n", *slide, want, len(order), *out)
	} else {
		fmt.Fprintf(stdout, "%d slides -> %s\n", len(order), *outDir)
	}
	return 0
}

// cmdResave saves a deck through PowerPoint to a new file and returns the exit code.
func cmdResave(a app, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("resave", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, resaveUsage) }
	var sf sessionFlags
	sf.register(fs)
	out := fs.String("o", "", "output .pptx")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || *out == "" {
		fs.Usage()
		return 2
	}
	deck := pos[0]
	if code := preflight(a, stderr); code != 0 {
		return code
	}
	err = withSession(a, deck, sf, func(rd *runDir) error {
		dst := rd.file("resaved.pptx")
		if err := save(a, dst, "save as Open XML presentation", sf); err != nil {
			return err
		}
		if err := waitFile(dst, 10*time.Second); err != nil {
			return fmt.Errorf("PowerPoint reported success but wrote nothing: %v", err)
		}
		return moveFile(dst, *out)
	})
	if err != nil {
		fmt.Fprintln(stderr, "pptoracle resave:", err)
		return 1
	}
	fmt.Fprintf(stdout, "resaved -> %s\n", *out)
	return 0
}

// withSession copies deck into a run folder, opens it in a fresh
// PowerPoint, runs fn, and always force-quits PowerPoint and removes the
// run folder.
func withSession(a app, deck string, sf sessionFlags, fn func(*runDir) error) (err error) {
	rd, err := newRunDir(sf.workdir)
	if err != nil {
		return err
	}
	cleanup.add(func() { rd.Close() })
	defer rd.Close()
	defer func() {
		if qerr := a.ForceQuit(); qerr != nil && err == nil {
			err = qerr
		}
	}()
	cp := rd.file(filepath.Base(deck))
	if err := officeapp.CopyFile(deck, cp); err != nil {
		return err
	}
	res, err := openAndWait(a, cp, time.Duration(sf.timeout*float64(time.Second)))
	if err != nil {
		return err
	}
	if res.Status != statusClean {
		return fmt.Errorf("PowerPoint did not open the deck cleanly: %s %s", res.Status, res.Detail)
	}
	return fn(rd)
}

// save runs a save-as and turns a hang into an explained error.
func save(a app, dst, format string, sf sessionFlags) error {
	timeout := sf.timeout
	_, err := a.Run(scriptSaveAs(dst, format), int(timeout))
	if err == nil {
		return nil
	}
	if officeapp.IsOSA(err, errAETimeout) && sf.grace > 0 {
		// Leave the dialog up so a person can read it (or grant access);
		// the file appearing means the save went through after all.
		deadline := clock().Add(time.Duration(sf.grace * float64(time.Second)))
		for clock().Before(deadline) {
			if st, serr := os.Stat(dst); serr == nil && st.Size() > 0 {
				return nil
			}
			sleep(5 * time.Second)
		}
	}
	if officeapp.IsOSA(err, errAETimeout) {
		if texts, werr := windowTexts(a); werr == nil {
			if s := classifyWindows(texts); s == statusAccessDialog {
				return fmt.Errorf("PowerPoint asked for file access to %s (Grant File Access dialog); grant PowerPoint that folder once or use the default workdir", filepath.Dir(dst))
			}
			return fmt.Errorf("save did not finish within %.0fs; PowerPoint windows: %s", timeout, joinTexts(texts))
		}
		return fmt.Errorf("save did not finish within %.0fs; PowerPoint is probably showing a dialog (Grant File Access if PowerPoint has no grant for the workdir)", timeout)
	}
	if officeapp.IsOSA(err, errNotAuthorised) {
		return fmt.Errorf("%s", errNotAuthorisedMsg)
	}
	return err
}

var pdfPageRE = regexp.MustCompile(`/Type\s*/Page[^s]`)

// pdfPageCount counts page objects in an uncompressed-xref PDF such as
// the ones macOS Quartz writes for PowerPoint.
func pdfPageCount(b []byte) int { return len(pdfPageRE.FindAll(b, -1)) }

// waitFile waits until path exists and its size is stable.
func waitFile(path string, d time.Duration) error {
	deadline := clock().Add(d)
	last := int64(-1)
	for {
		st, err := os.Stat(path)
		if err == nil && st.Size() > 0 && st.Size() == last {
			return nil
		}
		if err == nil {
			last = st.Size()
		}
		if !clock().Before(deadline) {
			if err != nil {
				return err
			}
			if st.Size() == 0 {
				return fmt.Errorf("%s is empty", path)
			}
			return nil
		}
		sleep(250 * time.Millisecond)
	}
}
