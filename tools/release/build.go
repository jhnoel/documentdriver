package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// target is one platform a release builds for, with the wheel platform
// tags its binaries are packaged under, in fixed order. A wheel file
// name joins them with '.' (a PEP 425 compressed tag set), so one file
// per platform covers every tag in the set. The binaries are static
// (no cgo), so they need nothing from the system and the oldest tags
// fit: the linux wheels carry both the glibc (manylinux) and the musl
// (musllinux) tag for their architecture, the same binaries serving
// both.
type target struct {
	GOOS, GOARCH string
	WheelTags    []string
}

// targets are the platforms of a release, in output order.
var targets = []target{
	{"linux", "amd64", []string{"manylinux2014_x86_64", "musllinux_1_1_x86_64"}},
	{"linux", "arm64", []string{"manylinux2014_aarch64", "musllinux_1_1_aarch64"}},
	{"darwin", "arm64", []string{"macosx_11_0_arm64"}},
}

// wheelCommands are the CLIs the Python wheel carries, sorted: every CLI
// with real commands.
var wheelCommands = []string{"docx", "pptx", "xlsx"}

// binaryName is the release file name of a CLI for a target, e.g.
// docx-0.1.0-linux-amd64.
func binaryName(cmd, version string, t target) string {
	return cmd + "-" + version + "-" + t.GOOS + "-" + t.GOARCH
}

// commands lists the CLIs under cmd/ of the repository, sorted.
func commands(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		return nil, fmt.Errorf("list the commands: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "cmd", e.Name(), "main.go")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// buildEnv is the environment of a release build: the caller's, with
// everything that changes the output pinned, so two builds of one commit
// give the same bytes whatever the shell has set.
func buildEnv(t target) []string {
	pinned := map[string]string{
		"CGO_ENABLED":  "0",
		"GOOS":         t.GOOS,
		"GOARCH":       t.GOARCH,
		"GOAMD64":      "v1",
		"GOARM64":      "v8.0",
		"GOFLAGS":      "",
		"GOEXPERIMENT": "",
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := pinned[k]; !ok {
			env = append(env, kv)
		}
	}
	for _, k := range []string{"CGO_ENABLED", "GOOS", "GOARCH", "GOAMD64", "GOARM64", "GOFLAGS", "GOEXPERIMENT"} {
		env = append(env, k+"="+pinned[k])
	}
	return env
}

// build compiles ./cmd/<cmd> of the repository at root for t into out, with
// the version stamped into main.version. -trimpath, -buildvcs=false and an
// empty build id keep the binary a function of the source, the version and
// the Go toolchain alone: a checkout and a `git archive` of the same commit
// build the same bytes.
func build(root, cmd, version string, t target, out string) error {
	ldflags := "-s -w -buildid= -X main.version=" + version
	c := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", out, "./cmd/"+cmd) //nolint:gosec // G204: go with arguments built from the fixed command list and a validated version
	c.Dir = root
	c.Env = buildEnv(t)
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("build %s for %s/%s: %w", cmd, t.GOOS, t.GOARCH, err)
	}
	return nil
}
