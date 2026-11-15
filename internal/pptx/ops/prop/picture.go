package prop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/show"
)

// Picture codecs: the image and SVG parts a p:pic shows (a:blip r:embed
// and the asvg:svgBlip extension), its blip fill (crop, fill mode, fill
// rectangle) and its locks. Each reads with the derive call the read
// model makes and shows through internal/pptx/show, as the JSON view does.

var (
	pathBlipFill = edit.Path{edit.P("blipFill")}
	pathBlip     = edit.Path{edit.P("blipFill"), edit.A("blip")}
	pathSrcRect  = edit.Path{edit.P("blipFill"), edit.A("srcRect")}
	pathStretch  = edit.Path{edit.P("blipFill"), edit.A("stretch")}
	pathFillRect = edit.Path{edit.P("blipFill"), edit.A("stretch"), edit.A("fillRect")}
	pathCNvPicPr = edit.Path{edit.P("nvPicPr"), edit.P("cNvPicPr")}
	pathPicLocks = edit.Path{edit.P("nvPicPr"), edit.P("cNvPicPr"), edit.A("picLocks")}
)

// pictures fails unless the object is a picture in every
// mc:AlternateContent branch: a picture whose other branch is a graphic
// frame (a 3D model, say) cannot take the same change there.
func pictures(h *edit.Handle, what string) error {
	if k := h.Kind(); k != "pic" {
		return fmt.Errorf("%w: a %s has no %s", ErrNoProperty, k, what)
	}
	for i := 1; i < h.Branches(); i++ {
		if k := h.Elem(i).Local(); k != "pic" {
			return fmt.Errorf("%w: branch %d is a %s, not a picture", edit.ErrBranch, i, k)
		}
	}
	return nil
}

// notPicture reports a missing value when branch i is not a picture.
func notPicture(h *edit.Handle, i int) (derive.Val[struct{}], bool) {
	if k := h.Elem(i).Local(); k != "pic" {
		return derive.Missing[struct{}]("a " + k + " is not a picture"), true
	}
	return derive.Val[struct{}]{}, false
}

// MediaRefs resolves a picture's relationships on its slide: the part an
// embedding relationship id targets, and the id of a relationship from
// the slide to a media part (an existing one, or one it adds).
type MediaRefs interface {
	Target(rid string) (part string, ok bool)
	Embed(part string) (rid string, err error)
}

// Image returns the codec of a picture's image part: the target of its
// a:blip's r:embed, shown as the part name. Writing a part sets r:embed
// to a relationship to it (refs.Embed); the part must exist.
func Image(refs MediaRefs) Codec[string] {
	return Codec[string]{
		Name: "image",
		Read: func(h *edit.Handle, i int) derive.Val[string] {
			if v, bad := notPicture(h, i); bad {
				return derive.Missing[string](v.Reason())
			}
			blip := h.FindIn(i, pathBlip)
			if blip == nil {
				return derive.Missing[string]("no a:blip")
			}
			rid, ok := derive.AttrNS(blip, derive.REL, "embed")
			if !ok {
				return derive.Missing[string]("the a:blip embeds nothing")
			}
			t, ok := refs.Target(rid)
			if !ok {
				return derive.Missing[string]("relationship " + rid + " not found")
			}
			return derive.Got(t, derive.FromSlide)
		},
		Write: func(h *edit.Handle, part string) error {
			if err := pictures(h, "image; pictures do"); err != nil {
				return err
			}
			for i := range h.Branches() {
				if blip := h.FindIn(i, pathBlip); blip != nil {
					if _, linked := derive.AttrNS(blip, derive.REL, "link"); linked {
						return fmt.Errorf("%w: a linked image (r:link) is not replaced", ErrReadOnly)
					}
				}
			}
			rid, err := refs.Embed(part)
			if err != nil {
				return err
			}
			if err := h.Ensure(nil, edit.P("blipFill")); err != nil {
				return err
			}
			if err := h.Ensure(pathBlipFill, edit.A("blip")); err != nil {
				return err
			}
			return h.SetRelAttr(pathBlip, "embed", rid)
		},
		Show: func(v string) string { return v },
	}
}

// SVG returns the codec of a picture's SVG part: the target of the
// asvg:svgBlip in its a:blip's SVG extension, "" when it has none (shown
// as none). Writing a part sets the svgBlip's r:embed, adding the
// extension as PowerPoint writes it when there is none; writing "" removes
// the extension.
func SVG(refs MediaRefs) Codec[string] {
	return Codec[string]{
		Name: "svg",
		Read: func(h *edit.Handle, i int) derive.Val[string] {
			if v, bad := notPicture(h, i); bad {
				return derive.Missing[string](v.Reason())
			}
			sb := h.ExtChild(i, pathBlip, derive.SVGBlipExt, derive.SVGNS, "svgBlip")
			if sb == nil {
				return derive.Got("", derive.FromSlide)
			}
			rid, ok := derive.AttrNS(sb, derive.REL, "embed")
			if !ok {
				return derive.Missing[string]("asvg:svgBlip without r:embed")
			}
			t, ok := refs.Target(rid)
			if !ok {
				return derive.Missing[string]("relationship " + rid + " not found")
			}
			return derive.Got(t, derive.FromSlide)
		},
		Write: func(h *edit.Handle, part string) error {
			if err := pictures(h, "SVG; pictures do"); err != nil {
				return err
			}
			if part == "" {
				return h.RemoveExt(pathBlip, derive.SVGBlipExt)
			}
			rid, err := refs.Embed(part)
			if err != nil {
				return err
			}
			if h.ExtChild(0, pathBlip, derive.SVGBlipExt, derive.SVGNS, "svgBlip") != nil {
				return h.SetExtRelAttr(pathBlip, derive.SVGBlipExt, derive.SVGNS, "svgBlip", "embed", rid)
			}
			// As PowerPoint 16 writes it (059_crop-images.pptx): the
			// extension declares asvg on the svgBlip itself.
			frag := `<asvg:svgBlip xmlns:asvg="` + derive.SVGNS + `" r:embed="` + rid + `"/>`
			return h.AppendExt(pathBlip, derive.SVGBlipExt, []byte(frag), map[string]string{"r": derive.REL[0]})
		},
		Show: func(v string) string {
			if v == "" {
				return "none"
			}
			return v
		},
	}
}

// ShowPicFill shows a blip fill in one string, the views' values: crop,
// fit and fill rectangle.
func ShowPicFill(f derive.PicFill) string {
	rect := func(r *[4]int64) string {
		if r == nil {
			return "none"
		}
		return string(show.Rect(*r))
	}
	return "crop=" + rect(f.Crop) + " fit=" + f.Fit + " fillRect=" + rect(f.FillRect)
}

// PicFill returns the codec of a picture's blip fill: crop (a:srcRect),
// fill mode (a:stretch or a:tile) and the stretch's fill rectangle
// (a:fillRect). A write sets only what differs from what is shown: an
// attribute whose value is wanted stays as written; a side wanted 0 is
// removed (the schema default); a nil crop or fill rectangle removes the
// element. The fill mode can become stretch, never tile (the tile's
// offset, scale, alignment and flip are not modelled), and cannot be
// unset.
func PicFill() Codec[derive.PicFill] {
	return Codec[derive.PicFill]{
		Name: "crop",
		Read: func(h *edit.Handle, i int) derive.Val[derive.PicFill] {
			if v, bad := notPicture(h, i); bad {
				return derive.Missing[derive.PicFill](v.Reason())
			}
			bf := h.FindIn(i, pathBlipFill)
			if bf == nil {
				return derive.Missing[derive.PicFill]("no p:blipFill")
			}
			f, err := derive.ReadPicFill(bf)
			if err != nil {
				return derive.Missing[derive.PicFill](err.Error())
			}
			return derive.Got(f, derive.FromSlide)
		},
		Write: writePicFill,
		Show:  ShowPicFill,
	}
}

// writePicFill writes the wanted picture crop, fill mode, and fill rectangle.
func writePicFill(h *edit.Handle, want derive.PicFill) error {
	if err := pictures(h, "picture fill"); err != nil {
		return err
	}
	cur, ok := PicFill().Read(h, 0).Get()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotDerived, PicFill().Read(h, 0).Reason())
	}
	for _, r := range []*[4]int64{want.Crop, want.FillRect} {
		if r == nil {
			continue
		}
		for k, side := range derive.RectSides {
			if err := edit.CheckAttr("a:CT_RelativeRect", side, strconv.FormatInt(r[k], 10)); err != nil {
				return fmt.Errorf("%s: %w", side, err)
			}
		}
	}
	switch want.Fit {
	case cur.Fit:
	case "":
		return fmt.Errorf("fit: cannot unset the fill mode (%s)", cur.Fit)
	case "tile":
		return fmt.Errorf("fit: %w: a tiled fill's offset, scale, alignment and flip are not modelled; tile is writable back unchanged only", ErrReadOnly)
	case "stretch":
		if err := h.Ensure(pathBlipFill, edit.A("stretch")); err != nil {
			return err
		}
		cur.FillRect = nil // a new, empty stretch
	default:
		return fmt.Errorf("fit %q: want stretch or tile", want.Fit)
	}
	if want.FillRect != nil && want.Fit != "stretch" {
		return fmt.Errorf("fillRect: the fill rectangle belongs to a stretch fill; give fit stretch")
	}
	if err := writeRelRect(h, pathBlipFill, edit.A("srcRect"), pathSrcRect, cur.Crop, want.Crop); err != nil {
		return fmt.Errorf("crop: %w", err)
	}
	if want.Fit == "stretch" {
		if err := writeRelRect(h, pathStretch, edit.A("fillRect"), pathFillRect, cur.FillRect, want.FillRect); err != nil {
			return fmt.Errorf("fillRect: %w", err)
		}
	}
	return nil
}

// writeRelRect makes the CT_RelativeRect child of parent (at path) hold
// want: removed when want is nil, ensured otherwise, with each side
// written only where it differs from cur.
func writeRelRect(h *edit.Handle, parent edit.Path, child edit.Name, path edit.Path, cur, want *[4]int64) error {
	if want == nil {
		if cur == nil {
			return nil
		}
		return h.Remove(path)
	}
	if cur == nil {
		if err := h.Ensure(parent, child); err != nil {
			return err
		}
		cur = &[4]int64{}
	}
	for k, side := range derive.RectSides {
		if cur[k] == want[k] {
			continue
		}
		var err error
		if want[k] == 0 {
			err = h.RemoveAttr(path, side)
		} else {
			err = h.SetAttr(path, side, strconv.FormatInt(want[k], 10))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Locks returns the codec of a picture's locks: the a:picLocks flags that
// are set, sorted by name. A write sets a flag wanted on to "1" (adding
// a:picLocks when there is none) and removes a flag wanted off (false is
// the schema default); flags whose value is wanted stay as written.
func Locks() Codec[[]string] {
	return Codec[[]string]{
		Name: "locks",
		Read: func(h *edit.Handle, i int) derive.Val[[]string] {
			if v, bad := notPicture(h, i); bad {
				return derive.Missing[[]string](v.Reason())
			}
			m, err := derive.ReadPicLocks(h.FindIn(i, pathCNvPicPr))
			if err != nil {
				return derive.Missing[[]string](err.Error())
			}
			return derive.Got(derive.TrueLocks(m), derive.FromSlide)
		},
		Write: writeLocks,
		Show:  func(v []string) string { return strings.Join(v, " ") },
	}
}

// writeLocks writes the wanted set of picture lock flags.
func writeLocks(h *edit.Handle, want []string) error {
	if err := pictures(h, "picture locks"); err != nil {
		return err
	}
	if h.FindIn(0, pathCNvPicPr) == nil {
		return fmt.Errorf("%w: no p:cNvPicPr", ErrNotDerived)
	}
	cur, err := derive.ReadPicLocks(h.FindIn(0, pathCNvPicPr))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDerived, err)
	}
	on := map[string]bool{}
	for _, n := range want {
		if _, ok := edit.AttrType("a:CT_PictureLocking", n); !ok {
			return fmt.Errorf("locks: %q is not an a:picLocks flag", n)
		}
		on[n] = true
	}
	for _, n := range derive.TrueLocks(on) { // sorted: attributes are added in name order
		if !cur[n] {
			if err := h.Ensure(pathCNvPicPr, edit.A("picLocks")); err != nil {
				return err
			}
			if err := h.SetAttr(pathPicLocks, n, "1"); err != nil {
				return err
			}
		}
	}
	for n, v := range cur {
		if v && !on[n] {
			if err := h.RemoveAttr(pathPicLocks, n); err != nil {
				return err
			}
		}
	}
	return nil
}
