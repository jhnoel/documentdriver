package render

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func blank(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	return img
}

func TestDrawIDs(t *testing.T) {
	img := blank(960, 540) // 1 px per pt
	marks := []Mark{{ID: "s2", Box: [4]float64{100, 100, 200, 100}}}
	if key := Draw(img, 960, 540, marks, Overlay{IDs: true}); key != nil {
		t.Errorf("key = %v for a sparse slide", key)
	}
	if got := img.RGBAAt(200, 100); got != colBox {
		t.Errorf("top edge = %v, want box colour", got)
	}
	if got := img.RGBAAt(150, 150); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("interior painted: %v", got)
	}
	if got := img.RGBAAt(102, 104); got != colTagBG {
		t.Errorf("tag background = %v", got)
	}
}

func TestDrawScalesToImage(t *testing.T) {
	img := blank(1440, 810) // 1.5 px per pt
	Draw(img, 960, 540, []Mark{{ID: "a", Box: [4]float64{100, 100, 200, 100}}}, Overlay{IDs: true})
	if got := img.RGBAAt(150+150, 150); got != colBox { // x=100+100pt, y=100pt
		t.Errorf("scaled top edge = %v", got)
	}
}

func TestDrawNumberedKey(t *testing.T) {
	img := blank(960, 540)
	var marks []Mark
	for i := 0; i < denseThreshold+1; i++ {
		marks = append(marks, Mark{ID: "s" + string(rune('A'+i)), Box: [4]float64{float64(i * 30), 50, 20, 20}})
	}
	key := Draw(img, 960, 540, marks, Overlay{IDs: true})
	if len(key) != len(marks) || key[0] != (KeyEntry{1, "sA"}) || key[3].ID != "sD" || key[3].N != 4 {
		t.Errorf("key = %v", key)
	}
}

func TestDrawNumberedOnOverlap(t *testing.T) {
	img := blank(960, 540)
	marks := []Mark{
		{ID: "s10", Box: [4]float64{100, 100, 50, 50}},
		{ID: "s11", Box: [4]float64{102, 102, 50, 50}},
	}
	if key := Draw(img, 960, 540, marks, Overlay{IDs: true}); len(key) != 2 {
		t.Errorf("key = %v, want numbered", key)
	}
}

func TestDrawSitesAndGlue(t *testing.T) {
	img := blank(960, 540)
	m := Mark{ID: "s1", Box: [4]float64{100, 100, 100, 100},
		Sites: [][2]float64{{150, 100}}, Ends: [][2]float64{{300, 300}}}
	Draw(img, 960, 540, []Mark{m}, Overlay{Sites: true, Glue: true})
	if got := img.RGBAAt(150, 100); got != colSite {
		t.Errorf("site dot = %v", got)
	}
	if got := img.RGBAAt(300+7, 300); got != colGlue {
		t.Errorf("glue ring = %v", got)
	}
	if got := img.RGBAAt(150, 150); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("ids drawn without the ids overlay: %v", got)
	}
}

func TestDrawRotated(t *testing.T) {
	img := blank(400, 400)
	// 90 degrees about the centre (150,150): a 100x40 box becomes 40x100.
	Draw(img, 400, 400, []Mark{{ID: "r", Box: [4]float64{100, 130, 100, 40}, Rot: 90}}, Overlay{IDs: true})
	if got := img.RGBAAt(130, 150); got != colBox { // left edge of the rotated box
		t.Errorf("rotated edge = %v", got)
	}
	if got := img.RGBAAt(105, 131); got == colBox {
		t.Errorf("unrotated edge drawn")
	}
}
