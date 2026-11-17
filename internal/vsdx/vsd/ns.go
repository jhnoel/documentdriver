package vsd

// The Visio main namespace has two spellings: NSMain2012 is what Visio
// writes; NSCore2011 is the MS-VSDX schema's targetNamespace. A part in
// either is read the same way (fixture V11); new content is written in
// the namespace the part already uses (contract section 10).
const (
	NSMain2012 = "http://schemas.microsoft.com/office/visio/2012/main"
	NSCore2011 = "http://schemas.microsoft.com/office/visio/2011/1/core"
	// NSRel is the officeDocument relationships namespace of r:id.
	NSRel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

// IsMain reports whether uri is a Visio main namespace (either
// spelling). Chunk V1.
func IsMain(uri string) bool {
	return uri == NSMain2012 || uri == NSCore2011
}
