package read

import (
	"encoding/base64"
	"maps"
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// assetManifest lists the package's image parts by asset URL (sorted):
// media type, size, the parts holding those bytes, and with payloads a
// data: URL. Parts with identical bytes and extension share an entry,
// which takes the media type and size of the last of them.
func assetManifest(pkg *model.Package, payloads bool) []any {
	byURL := map[string]Object{}
	partsOf := map[string][]string{}
	for _, p := range pkg.Parts() {
		mime, ok := model.ImageMIME(p.Name)
		if !ok {
			continue
		}
		data, ok := pkg.Get(p.Name)
		if !ok {
			continue // unreadable: pkg.Err fails the read
		}
		url := model.AssetURL(p.Name, data)
		parts := append(partsOf[url], p.Name)
		slices.Sort(parts)
		partsOf[url] = parts
		v := Object{"url": url, "mime": mime, "size": len(data), "parts": slices.Clone(parts)}
		if payloads {
			v["data_url"] = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
		byURL[url] = v
	}
	urls := slices.Sorted(maps.Keys(byURL))
	out := make([]any, len(urls))
	for i, u := range urls {
		parts := make([]any, len(partsOf[u]))
		for j, p := range partsOf[u] {
			parts[j] = p
		}
		byURL[u]["parts"] = parts
		out[i] = byURL[u]
	}
	return out
}
