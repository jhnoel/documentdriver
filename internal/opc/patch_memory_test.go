//go:build darwin || linux

package opc

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
)

// TestPatchMemory measures one cell edit on a sheet of 1,000,000 cells
// (100,000 rows by 10) through SetPatch, Seal and CommitSealed, the path
// xlsx apply takes, against the design's budget of 1.3 N live heap (N
// the sheet's XML; this workbook has no shared strings, so T is 0). It
// is a measurement, not part of make test: run it with
// SPLICE_MEMORY=1 go test -run TestPatchMemory -v ./internal/opc.
// The edit runs in a child process so that its maximum RSS is its own,
// not the generator's.
func TestPatchMemory(t *testing.T) {
	if os.Getenv("SPLICE_MEMORY_CHILD") != "" {
		patchMemoryChild(t)
		return
	}
	if os.Getenv("SPLICE_MEMORY") == "" {
		t.Skip("set SPLICE_MEMORY=1 to measure")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPatchMemory$", "-test.v") //nolint:gosec // G204: the test binary itself
	cmd.Env = append(os.Environ(), "SPLICE_MEMORY_CHILD=1")
	out, err := cmd.CombinedOutput()
	t.Logf("child:\n%s", out)
	if err != nil {
		t.Fatal(err)
	}
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		rss := ru.Maxrss // bytes on darwin, KiB on linux
		if runtime.GOOS == "linux" {
			rss *= 1024
		}
		t.Logf("child max RSS %s", mib(uint64(rss))) //nolint:gosec // G115: RSS is positive
	}
}

// streamWorkbook writes the workbook bigSheet(rows) gives, streaming the
// sheet into the zip so that making it holds no copy of it.
func streamWorkbook(t testing.TB, rows int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	put := func(name string, fill func(w io.Writer) error) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err == nil {
			err = fill(w)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	put("[Content_Types].xml", func(w io.Writer) error {
		_, err := io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`)
		return err
	})
	put(strings.TrimPrefix(sheetPart, "/"), func(w io.Writer) error {
		bw := bufio.NewWriter(w)
		bw.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")                           //nolint:errcheck // bufio errors are sticky; Flush returns them
		bw.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`) //nolint:errcheck // bufio errors are sticky; Flush returns them
		for r := 1; r <= rows; r++ {
			fmt.Fprintf(bw, `<row r="%d">`, r) //nolint:errcheck // bufio errors are sticky; Flush returns them
			for c := range 10 {
				fmt.Fprintf(bw, `<c r="%c%d"><v>%d</v></c>`, 'A'+c, r, r*c) //nolint:errcheck // bufio errors are sticky; Flush returns them
			}
			bw.WriteString(`</row>`) //nolint:errcheck // bufio errors are sticky; Flush returns them
		}
		bw.WriteString(`</sheetData></worksheet>`) //nolint:errcheck // bufio errors are sticky; Flush returns them
		return bw.Flush()
	})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func patchMemoryChild(t *testing.T) {
	path := streamWorkbook(t, 100_000)
	// A low GC target keeps the heap close to live data, so the sampled
	// maximum of heap objects bounds the transient peak live heap from
	// above (within the 10% target and the 4 MiB floor).
	defer debug.SetGCPercent(debug.SetGCPercent(10))
	var peak, phase atomic.Uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		tick := time.NewTicker(100 * time.Microsecond)
		defer tick.Stop()
		for {
			metrics.Read(s)
			v := s[0].Value.Uint64()
			if v > peak.Load() {
				peak.Store(v)
			}
			if v > phase.Load() {
				phase.Store(v)
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	base, alloc0 := ms.HeapAlloc, ms.TotalAlloc
	var marks []string
	var n int
	mark := func(name string) {
		runtime.GC()
		runtime.ReadMemStats(&ms)
		live := ms.HeapAlloc - min(base, ms.HeapAlloc)
		top := phase.Swap(0)
		top -= min(base, top)
		marks = append(marks, fmt.Sprintf("%s %s (%.2f N; peak before it %.2f N)", name, mib(live),
			float64(live)/float64(max(n, 1)), float64(top)/float64(max(n, 1))))
	}

	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := p.Part(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	orig, _, err := pt.OriginalString()
	if err != nil {
		t.Fatal(err)
	}
	n = len(orig)
	mark("read")
	cell := `<c r="C50000"><v>100000</v></c>`
	i := strings.Index(orig, cell)
	if i < 0 {
		t.Fatal("no cell C50000")
	}
	patch := splice.New(orig)
	if err := patch.Replace(i, i+len(cell), `<c r="C50000" t="inlineStr"><is><t>edited</t></is></c>`); err != nil {
		t.Fatal(err)
	}
	if err := pt.SetPatch(patch); err != nil {
		t.Fatal(err)
	}
	mark("patched")
	// Check is what Seal runs first; run apart, its scan's garbage
	// (a node per token) is measured on its own.
	if err := patch.Check(); err != nil {
		t.Fatal(err)
	}
	mark("checked")
	sealed, err := p.Seal()
	if err != nil {
		t.Fatal(err)
	}
	mark("sealed")
	if err := p.CommitSealed(sealed, filepath.Join(t.TempDir(), "out.xlsx")); err != nil {
		t.Fatal(err)
	}
	mark("committed")
	close(stop)
	wg.Wait()
	runtime.ReadMemStats(&ms)
	pk := peak.Load()
	peakLive := pk - min(base, pk)
	t.Logf("N = %s (%d cells), sealed zip %s", mib(uint64(n)), 1_000_000, mib(uint64(len(sealed.Bytes()))))
	t.Logf("checkpoints (live after GC): %s", strings.Join(marks, ", "))
	t.Logf("sampled peak heap objects %s = %.2f N (budget 1.3 N = %s); allocated %s = %.2f N",
		mib(peakLive), float64(peakLive)/float64(n), mib(uint64(float64(n)*1.3)),
		mib(ms.TotalAlloc-alloc0), float64(ms.TotalAlloc-alloc0)/float64(n))
}

func mib(b uint64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }
