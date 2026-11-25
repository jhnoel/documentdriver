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

// rewriteWheel copies a wheel entry by entry, replacing the content of the
// entry named tamper (if any) with "tampered". streamed writes each entry
// with zip.Writer.CreateHeader, which adds data descriptors; otherwise
// it uses writeEntry, as makeWheel does.
func rewriteWheel(t *testing.T, whl []byte, tamper string, streamed bool) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(whl), int64(len(whl)))
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
		if f.Name == tamper {
			data = []byte("tampered")
		}
		if !streamed {
			if err := writeEntry(zw, wheelFile{Name: f.Name, Data: data, Exec: f.Mode()&0o111 != 0}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		h := zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: zipTime}
		h.SetMode(f.Mode())
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
	return buf.Bytes()
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
	if len(pkg.Licenses) != len(licenseFiles) || !bytes.Contains(pkg.Licenses[0].Data, []byte("Apache License")) ||
		!bytes.Contains(pkg.Licenses[1].Data, []byte("SIL Open Font License")) || !bytes.Contains(pkg.Licenses[2].Data, []byte("Bitstream Vera")) {
		t.Fatalf("licences = %d files, want LICENSE (Apache), fonts/OFL.txt (OFL) and fonts/LICENSE-DejaVu.txt (Bitstream Vera) first", len(pkg.Licenses))
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

	// One binary changed but RECORD kept.
	tampered := rewriteWheel(t, a, distName+"/bin/docx", false)
	if _, err := verifyWheel(tampered, "1.2.3rc1", linux); err == nil || !strings.Contains(err.Error(), "bin/docx") {
		t.Errorf("verifyWheel on a tampered binary = %v, want a RECORD mismatch for bin/docx", err)
	}
	// The same entries written by streaming carry data descriptors.
	streamed := rewriteWheel(t, a, "", true)
	if _, err := verifyWheel(streamed, "1.2.3rc1", linux); err == nil || !strings.Contains(err.Error(), "data descriptor") {
		t.Errorf("verifyWheel on a streamed wheel = %v, want a data descriptor refusal", err)
	}
	if got := rewriteWheel(t, a, "", false); !bytes.Equal(got, a) {
		t.Error("rewriting a wheel entry by entry with writeEntry changed its bytes")
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

// TestWheelLicences checks that a wheel carries the tools' and the
// fonts' licence files under .dist-info/licenses with a License-File
// line each and the combined licence expression, and that verifyWheel
// refuses a wheel missing any one of them.
func TestWheelLicences(t *testing.T) {
	pkg := testPackage(t)
	bins := map[string][]byte{"vsdx": []byte("vsdx binary")}
	tags := []string{"macosx_11_0_arm64"}
	w, err := makeWheel("1.2.3", tags, pkg, bins)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyWheel(w, "1.2.3", tags); err != nil {
		t.Fatalf("verifyWheel: %v", err)
	}
	meta := string(metadata("1.2.3", bins))
	for _, want := range []string{
		"License-Expression: Apache-2.0 AND OFL-1.1 AND Bitstream-Vera AND LicenseRef-Arev\n",
		"License-File: LICENSE\n", "License-File: fonts/OFL.txt\n", "License-File: fonts/LICENSE-DejaVu.txt\n", "License-File: fonts/PROVENANCE.md\n",
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("METADATA lacks %q", want)
		}
	}
	for i := range pkg.Licenses {
		short := pkg
		short.Licenses = append(append([]wheelFile(nil), pkg.Licenses[:i]...), pkg.Licenses[i+1:]...)
		bad, err := makeWheel("1.2.3", tags, short, bins)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifyWheel(bad, "1.2.3", tags); err == nil || !strings.Contains(err.Error(), pkg.Licenses[i].Name) {
			t.Errorf("a wheel without %s: verifyWheel = %v, want an error naming it", pkg.Licenses[i].Name, err)
		}
	}
}
