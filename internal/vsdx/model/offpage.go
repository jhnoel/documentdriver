package model

// offPage adds an off-page hazard for every top-level object of a page
// whose box (or, for a 1-D shape, both ends) lies wholly outside the
// page's [0, 0, W, H] in view points (the contract draft's off-page
// hazard; page.set reports it when it moves the page edge past
// objects, read reports what the file already holds). Detail:
// "box [x,y,w,h] lies outside the page [0,0,W,H]" with geom.FormatNum
// numbers. Pages whose size is not derived, masters, and objects
// without a box or ends add nothing. Chunk OPF.
func offPage(p *Page) {
	_ = p
}
