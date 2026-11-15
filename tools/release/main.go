// Command release builds a documentdriver release into a directory
// (default dist/): every CLI under cmd/ for linux/amd64, linux/arm64 and
// darwin/arm64 as <cmd>-<version>-<os>-<arch>, one Python wheel per
// platform carrying the docx and pptx binaries (packaging/python) —
// the linux wheels under a compressed manylinux/musllinux tag set, one
// file per architecture — and SHA256SUMS over all of them. Builds are
// CGO_ENABLED=0 -trimpath with the version stamped into main.version,
// and the wheels are written with
// sorted entries and fixed times, so two runs on one commit give
// identical files. After writing, it checks what it wrote: SHA256SUMS,
// each wheel's RECORD, and --version of every binary that runs here.
//
// It publishes nothing. RELEASING.md has the procedure.
//
// Usage:
//
//	go run ./tools/release -version 0.1.0 [-out dist]         build and check
//	go run ./tools/release -version 0.1.0 [-out dist] -check  check only
//	go run ./tools/release -version 0.1.0 [-out dist] -host-wheel
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// sumsName is the checksum file of a release, in sha256sum format.
const sumsName = "SHA256SUMS"

// main parses flags and builds or checks the release.
func main() {
	version := flag.String("version", "", "the release version, e.g. 0.1.0 or 0.1.0-dev (required)")
	out := flag.String("out", "", "the output directory (default dist/ at the repository root)")
	check := flag.Bool("check", false, "only check an existing release in -out, build nothing")
	hostWheel := flag.Bool("host-wheel", false, "only print the path of this machine's wheel in -out, build nothing")
	flag.Parse()
	if *hostWheel {
		p, err := hostWheelPath(*version, *out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "release:", err)
			os.Exit(1)
		}
		fmt.Println(p)
		return
	}
	if err := run(*version, *out, *check); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

// hostWheelPath is the path of the wheel for this machine's platform in out
// (default dist/ at the repository root).
func hostWheelPath(version, out string) (string, error) {
	pyVersion, err := pep440(version)
	if err != nil {
		return "", err
	}
	if out == "" {
		root, err := repo.Root()
		if err != nil {
			return "", err
		}
		out = filepath.Join(root, "dist")
	}
	for _, t := range targets {
		if t.GOOS == runtime.GOOS && t.GOARCH == runtime.GOARCH {
			return filepath.Join(out, wheelName(pyVersion, t.WheelTags)), nil
		}
	}
	return "", fmt.Errorf("%s/%s is not a release platform", runtime.GOOS, runtime.GOARCH)
}

// run builds the release unless checkOnly, then checks it.
func run(version, out string, checkOnly bool) error {
	if version == "" {
		return errors.New("-version is required")
	}
	pyVersion, err := pep440(version)
	if err != nil {
		return err
	}
	root, err := repo.Root()
	if err != nil {
		return err
	}
	if out == "" {
		out = filepath.Join(root, "dist")
	}
	if !checkOnly {
		if err := buildRelease(root, version, pyVersion, out); err != nil {
			return err
		}
	}
	return checkRelease(out, version, pyVersion)
}

// buildRelease writes the binaries, the wheels and SHA256SUMS into out.
func buildRelease(root, version, pyVersion, out string) error {
	cmds, err := commands(root)
	if err != nil {
		return err
	}
	for _, w := range wheelCommands {
		if !slices.Contains(cmds, w) {
			return fmt.Errorf("the wheel carries %s, but there is no cmd/%s", w, w)
		}
	}
	pkg, err := readPackage(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil { //nolint:gosec // G301: a release directory others read
		return fmt.Errorf("create %s: %w", out, err)
	}
	var files []string
	for _, t := range targets {
		bins := map[string][]byte{}
		for _, c := range cmds {
			name := binaryName(c, version, t)
			p := filepath.Join(out, name)
			fmt.Fprintf(os.Stderr, "build %s\n", name)
			if err := build(root, c, version, t, p); err != nil {
				return err
			}
			files = append(files, name)
			if slices.Contains(wheelCommands, c) {
				b, err := os.ReadFile(p) //nolint:gosec // G304: the binary just built
				if err != nil {
					return fmt.Errorf("read %s: %w", name, err)
				}
				bins[c] = b
			}
		}
		whl, err := makeWheel(pyVersion, t.WheelTags, pkg, bins)
		if err != nil {
			return fmt.Errorf("wheel for %v: %w", t.WheelTags, err)
		}
		name := wheelName(pyVersion, t.WheelTags)
		fmt.Fprintf(os.Stderr, "wheel %s\n", name)
		if err := os.WriteFile(filepath.Join(out, name), whl, 0o644); err != nil { //nolint:gosec // G306: a release file others read
			return fmt.Errorf("write %s: %w", name, err)
		}
		files = append(files, name)
	}
	sums, err := checksums(out, files)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, sumsName), sums, 0o644); err != nil { //nolint:gosec // G306: a release file others read
		return fmt.Errorf("write %s: %w", sumsName, err)
	}
	return nil
}

// checksums is SHA256SUMS for the named files of dir, sorted by name, in
// the format sha256sum -c and shasum -a 256 -c read.
func checksums(dir string, names []string) ([]byte, error) {
	names = append([]string(nil), names...)
	sort.Strings(names)
	var b bytes.Buffer
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n)) //nolint:gosec // G304: a release file this tool names
		if err != nil {
			return nil, fmt.Errorf("checksum %s: %w", n, err)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return b.Bytes(), nil
}

// checkRelease checks a release directory: every file SHA256SUMS lists
// has its digest, it lists every binary and wheel of the release, each
// wheel passes verifyWheel, and the binaries for this machine's platform
// (raw and in the wheel) report the version.
func checkRelease(out, version, pyVersion string) error {
	sums, err := os.ReadFile(filepath.Join(out, sumsName)) //nolint:gosec // G304: the release directory the caller named
	if err != nil {
		return fmt.Errorf("read %s: %w", sumsName, err)
	}
	listed := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		hexSum, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			return fmt.Errorf("%s line %q: want <sha256>  <name>", sumsName, sc.Text())
		}
		data, err := os.ReadFile(filepath.Join(out, filepath.Base(name))) //nolint:gosec // G304: a file of the release directory
		if err != nil {
			return fmt.Errorf("%s lists %s: %w", sumsName, name, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hexSum {
			return fmt.Errorf("%s: digest differs from %s", name, sumsName)
		}
		listed[name] = true
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", sumsName, err)
	}
	for _, t := range targets {
		var bins int
		for name := range listed {
			if strings.HasSuffix(name, "-"+version+"-"+t.GOOS+"-"+t.GOARCH) {
				bins++
				if t.GOOS == runtime.GOOS && t.GOARCH == runtime.GOARCH {
					tool := strings.TrimSuffix(name, "-"+version+"-"+t.GOOS+"-"+t.GOARCH)
					if err := runVersion(filepath.Join(out, name), tool, version); err != nil {
						return err
					}
				}
			}
		}
		if bins < len(wheelCommands) {
			return fmt.Errorf("%s lists %d binaries for %s/%s", sumsName, bins, t.GOOS, t.GOARCH)
		}
		name := wheelName(pyVersion, t.WheelTags)
		if !listed[name] {
			return fmt.Errorf("%s does not list %s", sumsName, name)
		}
		whl, err := os.ReadFile(filepath.Join(out, name)) //nolint:gosec // G304: a file of the release directory
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		wbins, err := verifyWheel(whl, pyVersion, t.WheelTags)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if got := sortedKeys(wbins); strings.Join(got, ",") != strings.Join(wheelCommands, ",") {
			return fmt.Errorf("%s carries %v, want %v", name, got, wheelCommands)
		}
		if t.GOOS == runtime.GOOS && t.GOARCH == runtime.GOARCH {
			if err := runWheelBinaries(wbins, "", version); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		fmt.Fprintf(os.Stderr, "ok %s\n", name)
	}
	fmt.Fprintf(os.Stderr, "release %s in %s: %d files, %s ok\n", version, out, len(listed), sumsName)
	return nil
}
