//go:build !wasip1

package recalc

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/jhnoel/documentdriver/internal/soffice"
)

// Compute runs LibreOffice on a copy of xlsx (a sealed workbook) and
// returns its formula values (chunk G17), as the package doc says. The
// process runs with a fresh private profile in a temporary directory
// that is removed afterwards, with no network access asked for and no
// GUI (--headless --norestore --nologo --nodefault --nolockcheck).
func Compute(xlsx []byte, opt Options) (values []Value, err error) {
	configured := opt.Soffice
	if configured == "" {
		configured = os.Getenv("DD_SOFFICE")
	}
	name := configured
	var bin string
	if name == "" {
		name = "soffice"
		bin = soffice.Find("", name, soffice.DefaultPaths...)
	} else {
		bin = soffice.Find("", name)
	}
	if bin == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	dir, err := os.MkdirTemp("", "xlsx-recalc-")
	if err != nil {
		return nil, fmt.Errorf("recalc: create temporary directory: %w", err)
	}
	defer func() {
		if e := os.RemoveAll(dir); e != nil {
			err = errorsJoinCleanup(err, e)
		}
	}()
	profile := filepath.Join(dir, "profile")
	if err = os.MkdirAll(filepath.Join(profile, "user"), 0700); err != nil {
		return nil, fmt.Errorf("recalc: create profile: %w", err)
	}
	const registry = `<?xml version="1.0" encoding="UTF-8"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Calc/Formula/Load"><prop oor:name="OOXMLRecalcMode" oor:op="fuse"><value>0</value></prop></item></oor:items>`
	if err = os.WriteFile(filepath.Join(profile, "user", "registrymodifications.xcu"), []byte(registry), 0600); err != nil {
		return nil, fmt.Errorf("recalc: write profile: %w", err)
	}
	inputDir := filepath.Join(dir, "input")
	outputDir := filepath.Join(dir, "output")
	for _, path := range []string{inputDir, outputDir} {
		if err = os.Mkdir(path, 0700); err != nil {
			return nil, fmt.Errorf("recalc: create conversion directory: %w", err)
		}
	}
	input := filepath.Join(inputDir, "workbook.xlsx")
	if err = os.WriteFile(input, xlsx, 0600); err != nil {
		return nil, fmt.Errorf("recalc: write workbook: %w", err)
	}
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	profileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(profile)}).String()
	// soffice.Run kills the process group on timeout and after the exit,
	// so LibreOffice's soffice.bin child never outlives the call.
	if _, err = soffice.Run(timeout, "", bin, "-env:UserInstallation="+profileURL,
		"--headless", "--norestore", "--nologo", "--nodefault", "--nolockcheck",
		"--convert-to", "xlsx", "--outdir", outputDir, input); err != nil {
		return nil, fmt.Errorf("recalc: conversion: %w", err)
	}
	//nolint:gosec // G304: the output path is inside our private temporary directory.
	converted, err := os.ReadFile(filepath.Join(outputDir, "workbook.xlsx"))
	if err != nil {
		return nil, fmt.Errorf("recalc: read converted workbook: %w", err)
	}
	return Values(converted)
}

// errorsJoinCleanup returns err joined with a temporary-directory cleanup failure.
func errorsJoinCleanup(err, cleanup error) error {
	if err == nil {
		return fmt.Errorf("recalc: remove temporary directory: %w", cleanup)
	}
	return fmt.Errorf("%w; recalc: remove temporary directory: %w", err, cleanup)
}
