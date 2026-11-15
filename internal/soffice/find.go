package soffice

import (
	"os"
	"os/exec"
)

// DefaultPaths are where LibreOffice's soffice is looked for when it is
// not on PATH: Homebrew, the macOS application bundle, and the usual
// Linux locations.
var DefaultPaths = []string{"/opt/homebrew/bin/soffice", "/Applications/LibreOffice.app/Contents/MacOS/soffice",
	"/usr/bin/soffice", "/usr/local/bin/soffice"}

// Find returns the binary to run: env when it is set (the caller reads
// the environment variable, such as DD_SOFFICE or PPTX_SOFFICE, and
// passes its value), else name found on PATH, else the first of extra
// that is a regular file, else "".
func Find(env, name string, extra ...string) string {
	if env != "" {
		return env
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, p := range extra {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
