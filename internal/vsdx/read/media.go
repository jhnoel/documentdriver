package read

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// MediaResult is what vsdx media prints (docs/contract.md §1).
type MediaResult struct {
	Base   string `json:"base"`
	Saved  string `json:"saved"`
	Image  string `json:"image"`
	Type   string `json:"type"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Px     []int  `json:"px,omitempty"`
}

// mediaPrefix is the folder of a drawing's media parts.
const mediaPrefix = "/visio/media/"

// Media writes the bytes of one image of input to w and returns its
// facts: address is a picture (page0/s5, master2/s5) or a media part
// as read shows it in image (/visio/media/image1.png). Anything else
// is not_found; a picture whose media part is missing is
// unreadable_target. The part is streamed, never held whole.
func Media(input []byte, base, address string, w io.Writer) (*MediaResult, error) {
	part := ""
	if strings.HasPrefix(address, "/") {
		if !strings.HasPrefix(strings.ToLower(address), mediaPrefix) {
			return nil, cli.Errorf(cli.CodeNotFound, "", "%s is not a media part of the drawing", address)
		}
		part = address
	} else {
		a, err := vsd.Parse(address)
		if err != nil || a.Kind != vsd.KindShape {
			return nil, cli.Errorf(cli.CodeNotFound, "", "%s is not a picture or a media part", address)
		}
		container := vsd.PageID(a.Container, a.PageDup)
		if a.InMaster {
			container = vsd.MasterID(a.Container)
		}
		p, err := PageModel(input, container, Options{IDs: []string{address}})
		if err != nil {
			return nil, err
		}
		var found bool
		for _, o := range p.Objects {
			if o.ID != address {
				continue
			}
			found = true
			if o.Picture == nil {
				if string(o.Kind) == "picture" {
					return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Message: address + " cannot be read", Detail: "its image relationship or part is missing"}
				}
				return nil, cli.Errorf(cli.CodeNotFound, "", "%s is not a picture", address)
			}
			part = o.Picture.Image
		}
		if !found {
			return nil, cli.Errorf(cli.CodeNotFound, "", "no %s: not found", address)
		}
	}
	pkg, _, _, err := openDoc(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	if !pkg.Has(part) {
		return nil, cli.Errorf(cli.CodeNotFound, "", "no media part %s", part)
	}
	pt, err := pkg.Part(part)
	if err != nil {
		return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Message: part + " cannot be read", Detail: err.Error(), Cause: err}
	}
	typ, _ := pt.ContentType()
	h := sha256.New()
	head := &model.HeadWriter{Max: model.HeaderPrefix}
	n, err := pt.WriteTo(io.MultiWriter(w, h, head))
	if err != nil {
		return nil, &cli.Error{Code: cli.CodeUnreadableTarget, Message: part + " cannot be read", Detail: err.Error(), Cause: err}
	}
	res := &MediaResult{Base: base, Image: part, Type: typ, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	if px, ok := model.HeaderPx(typ, head.B); ok {
		res.Px = []int{px[0], px[1]}
	}
	return res, nil
}
