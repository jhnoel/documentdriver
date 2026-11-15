package main

import (
	"fmt"
	"regexp"
)

// releaseVersion matches the versions a release may carry: MAJOR.MINOR.PATCH
// with no leading zeros, optionally followed by one pre-release label,
// -devN, -aN, -bN or -rcN (N optional, default 0). The set is small on
// purpose: each one has exactly one PEP 440 spelling for the wheel.
var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(dev|a|b|rc)(0|[1-9][0-9]*)?)?$`)

// pep440 returns the PEP 440 spelling of a release version, the form the
// wheel's file name and METADATA use: 0.1.0 stays 0.1.0, 0.1.0-dev is
// 0.1.0.dev0, 0.1.0-rc2 is 0.1.0rc2.
func pep440(v string) (string, error) {
	m := releaseVersion.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("version %q: want MAJOR.MINOR.PATCH, optionally with -devN, -aN, -bN or -rcN", v)
	}
	base := m[1] + "." + m[2] + "." + m[3]
	if m[4] == "" {
		return base, nil
	}
	n := m[5]
	if n == "" {
		n = "0"
	}
	if m[4] == "dev" {
		return base + ".dev" + n, nil
	}
	return base + m[4] + n, nil
}
