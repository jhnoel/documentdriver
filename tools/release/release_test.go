package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

func TestPEP440(t *testing.T) {
	for in, want := range map[string]string{
		"0.1.0":      "0.1.0",
		"1.20.3":     "1.20.3",
		"0.1.0-dev":  "0.1.0.dev0",
		"0.1.0-dev4": "0.1.0.dev4",
		"0.1.0-a1":   "0.1.0a1",
		"0.1.0-b":    "0.1.0b0",
		"2.0.0-rc2":  "2.0.0rc2",
	} {
		got, err := pep440(in)
		if err != nil || got != want {
			t.Errorf("pep440(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "v1.2.3", "01.2.3", "1.2.3-beta", "1.2.3-dev-1", "1.2.3+x", "1.2.3 ", `1.2.3"`} {
		if got, err := pep440(bad); err == nil {
			t.Errorf("pep440(%q) = %q, want an error", bad, got)
		}
	}
}

func testPackage(t *testing.T) pyPackage {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := readPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) == 0 || pkg.Files[0].Name != distName+"/__init__.py" {
		t.Fatalf("package = %v, want documentdriver/__init__.py first", pkg.Files)
	}
	if !bytes.Contains(pkg.License, []byte("Apache License")) {
		t.Fatalf("license = %.40q, want the Apache License", pkg.License)
	}
	return pkg
}

// TestWheelReproducibleAndVerified builds a wheel from stand-in binaries
// twice, wants identical bytes, checks that verifyWheel accepts it, and
// that it rejects an entry whose content no longer matches RECORD.
func TestWheelReproducibleAndVerified(t *testing.T) {
	pkg := testPackage(t)
	bins := map[string][]byte{"pptx": []byte("pptx binary"), "docx": []byte("docx binary")}
	linux := []string{"manylinux2014_x86_64", "musllinux_1_1_x86_64"}
	if got, want := wheelName("1.2.3rc1", linux), "documentdriver-1.2.3rc1-py3-none-manylinux2014_x86_64.musllinux_1_1_x86_64.whl"; got != want {
		t.Errorf("wheelName = %q, want %q", got, want)
	}
	if got, want := string(wheelMeta(linux)), "Wheel-Version: 1.0\nGenerator: documentdriver tools/release\nRoot-Is-Purelib: false\nTag: py3-none-manylinux2014_x86_64\nTag: py3-none-musllinux_1_1_x86_64\n"; got != want {
		t.Errorf("wheelMeta = %q, want %q", got, want)
	}
	a, err := makeWheel("1.2.3rc1", linux, pkg, bins)
	if err != nil {
		t.Fatal(err)
	}
	b, err := makeWheel("1.2.3rc1", linux, pkg, bins)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of one wheel differ")
	}
	got, err := verifyWheel(a, "1.2.3rc1", linux)
	if err != nil {
		t.Fatal(err)
	}
	if string(got["docx"]) != "docx binary" || len(got) != 2 {
		t.Errorf("binaries = %q", got)
	}
	if _, err := verifyWheel(a, "1.2.3rc1", []string{"macosx_11_0_arm64"}); err == nil {
		t.Error("verifyWheel accepted the wrong platform tag")
	}
	if _, err := verifyWheel(a, "1.2.3rc1", []string{"manylinux2014_x86_64"}); err == nil {
		t.Error("verifyWheel accepted a WHEEL missing the musllinux tag")
	}
	// A wheel whose WHEEL lists only the manylinux tag must not verify
	// against the two-tag set.
	single, err := makeWheel("1.2.3rc1", []string{"manylinux2014_x86_64"}, pkg, bins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWheel(single, "1.2.3rc1", linux); err == nil {
		t.Error("verifyWheel accepted a WHEEL missing one tag")
	}

	// Rewrite the wheel with one binary changed but RECORD kept.
	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
		if f.Name == distName+"/bin/docx" {
			data = []byte("tampered")
		}
		h := f.FileHeader
		w, err := zw.CreateHeader(&h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWheel(buf.Bytes(), "1.2.3rc1", linux); err == nil || !strings.Contains(err.Error(), "bin/docx") {
		t.Errorf("verifyWheel on a tampered binary = %v, want a RECORD mismatch for bin/docx", err)
	}
}

// TestBuiltWheelRunsVersion builds the wheel CLIs for this machine with a
// stamped version, packs them into the wheel for this platform, unpacks
// it, checks RECORD, and runs each binary's --version.
func TestBuiltWheelRunsVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLIs")
	}
	var host *target
	for i := range targets {
		if targets[i].GOOS == runtime.GOOS && targets[i].GOARCH == runtime.GOARCH {
			host = &targets[i]
		}
	}
	if host == nil {
		t.Skipf("%s/%s is not a release platform", runtime.GOOS, runtime.GOARCH)
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	const version = "0.0.1-dev7"
	dir := t.TempDir()
	bins := map[string][]byte{}
	for _, c := range wheelCommands {
		p := filepath.Join(dir, c)
		if err := build(root, c, version, *host, p); err != nil {
			t.Fatal(err)
		}
		if err := runVersion(p, c, version); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		bins[c] = b
	}
	whl, err := makeWheel("0.0.1.dev7", host.WheelTags, testPackage(t), bins)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyWheel(whl, "0.0.1.dev7", host.WheelTags)
	if err != nil {
		t.Fatal(err)
	}
	if err := runWheelBinaries(got, dir, version); err != nil {
		t.Fatal(err)
	}
}
