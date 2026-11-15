package model

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
)

// ImageMIME returns the media type docxdriver 0.5.2 labels an image part
// with in its asset manifest and asset URLs, by the part name's extension
// (after the last ".", ASCII case-insensitive), and false for a part it
// does not list as an asset.
//
// These labels are docxdriver's output vocabulary, compared byte for byte
// by the parity oracle, not content types read from or written to a
// package; bmp and webp have no Office-authored source in the ctypes table,
// so this file is exempt from TestContentTypesFromTable.
func ImageMIME(part string) (string, bool) {
	switch ImageExt(part) {
	case "png":
		return ctypes.CTImagePng, true
	case "jpg", "jpeg":
		return ctypes.CTImageJpeg, true
	case "gif":
		return ctypes.CTImageGif, true
	case "webp":
		return "image/webp", true
	case "bmp":
		return "image/bmp", true
	case "svg":
		return ctypes.CTImageSvgXml, true
	}
	return "", false
}

// ImageExt is the lowercased text after the last "." of a part name (the
// whole name when it has none), as docxdriver takes an asset's extension.
func ImageExt(part string) string {
	return ascii.Lower(part[strings.LastIndexByte(part, '.')+1:])
}
