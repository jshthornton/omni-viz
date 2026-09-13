package omniviz

import (
	"fmt"
	"image"
	"math"
)

// DiffResult carries the outcome of comparing two PNGs.
type DiffResult struct {
	BaseWidth, BaseHeight int
	CurWidth, CurHeight   int
	Width, Height         int
	DiffPixels            int // pixels failing the perceptual threshold
	ChangedPixels         int // pixels differing at all above the noise floor
	TotalPixels           int
	Ratio                 float64 // DiffPixels / TotalPixels
	ChangedRatio          float64 // ChangedPixels / TotalPixels
	SizeMismatch          bool
}

// Pixel-diff modelled on pixelmatch (Mapbox): perceptual OKLab HyAB distance
// with a 0..1 black-to-white threshold scale, plus anti-aliased-pixel
// detection so edge softening does not count as a regression.
//
// On top of the per-pixel threshold omniviz adds a changed-AREA budget
// (MaxChanged): the fraction of pixels that may differ at all (above a small
// absolute noise floor) before the shot fails regardless of per-pixel
// magnitude. That is what catches global drift — a fog-density or exposure
// shift changes every pixel slightly, which a per-pixel threshold rightly
// tolerates but a visual regression suite must not.

const (
	noiseFloor = 2.0 // per-channel sRGB delta considered "no change"
	dimAlpha   = 0.1 // dimming of unchanged pixels in the diff image
)

var (
	diffColor    = [4]uint8{255, 0, 0, 255}
	diffColorAlt = [4]uint8{0, 255, 0, 255}
	aaColor      = [4]uint8{255, 255, 0, 255}
)

// ComparePNG diffs two PNG files and produces a diff visualization.
// threshold follows pixelmatch semantics (0..1, default 0.1; smaller is more
// sensitive). Optional ignore regions are excluded from the diff entirely
// (clocks, ads, any dynamic strip). The caller applies the changed-area
// budget to res.ChangedRatio.
func ComparePNG(pathA, pathB string, threshold float64, ignore ...image.Rectangle) (DiffResult, *image.RGBA, error) {
	a, err := LoadRGBA(pathA)
	if err != nil {
		return DiffResult{}, nil, fmt.Errorf("baseline: %w", err)
	}
	b, err := LoadRGBA(pathB)
	if err != nil {
		return DiffResult{}, nil, fmt.Errorf("current: %w", err)
	}
	res := DiffResult{
		BaseWidth:  a.Bounds().Dx(),
		BaseHeight: a.Bounds().Dy(),
		CurWidth:   b.Bounds().Dx(),
		CurHeight:  b.Bounds().Dy(),
	}
	if res.BaseWidth != res.CurWidth || res.BaseHeight != res.CurHeight {
		res.SizeMismatch = true
		return res, nil, nil
	}
	diffImg, diffCount, changedCount := diffRGBA(a, b, threshold, ignore)
	res.Width = res.BaseWidth
	res.Height = res.BaseHeight
	res.TotalPixels = res.Width * res.Height
	res.DiffPixels = diffCount
	res.ChangedPixels = changedCount
	res.Ratio = float64(diffCount) / float64(res.TotalPixels)
	res.ChangedRatio = float64(changedCount) / float64(res.TotalPixels)
	return res, diffImg, nil
}

// ---------------------------------------------------------------- OKLab ----

// sRGB byte -> linear, plus one entry so fractional interpolation can peek +1.
var linLUT [257]float64

// cbrt over [0..1] with linear interpolation (4096-entry table).
const cbrtN = 4096

var cbrtLUT [cbrtN + 2]float64

// toe-corrected OKLab lightness constants (Ottosson).
const (
	toeK1 = 0.206
	toeK2 = 0.03
	toeK3 = (1 + toeK1) / (1 + toeK2)
)

func init() {
	for i := 0; i < 256; i++ {
		c := float64(i) / 255
		if c <= 0.04045 {
			linLUT[i] = c / 12.92
		} else {
			linLUT[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	linLUT[256] = linLUT[255]
	for i := range cbrtLUT {
		cbrtLUT[i] = math.Cbrt(float64(i) / cbrtN)
	}
}

func linInterp(x float64) float64 {
	if x < 0 {
		x = 0
	}
	if x > 255 {
		x = 255
	}
	i := int(x)
	return linLUT[i] + (linLUT[i+1]-linLUT[i])*(x-float64(i))
}

func cbrtInterp(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	t := x * cbrtN
	i := int(t)
	return cbrtLUT[i] + (cbrtLUT[i+1]-cbrtLUT[i])*(t-float64(i))
}

func toe(L float64) float64 {
	x := toeK3*L - toeK1
	return 0.5 * (x + math.Sqrt(x*x+4*toeK2*toeK3*L))
}

// oklabOpaque computes the toe-corrected lightness and LMS for an opaque
// sRGB color.
func oklabOpaque(r, g, b uint8) (lr, l, m, s float64) {
	lr8, lg8, lb8 := linLUT[r], linLUT[g], linLUT[b]
	l = cbrtInterp(0.4122214708*lr8 + 0.5363325363*lg8 + 0.0514459929*lb8)
	m = cbrtInterp(0.2119034982*lr8 + 0.6806995451*lg8 + 0.1073969566*lb8)
	s = cbrtInterp(0.0883024619*lr8 + 0.2817188376*lg8 + 0.6299787005*lb8)
	lr = toe(0.2104542553*l + 0.7936177850*m - 0.0040720468*s)
	return lr, l, m, s
}

// oklabHyabDelta compares two OKLab colors with the HyAB metric folded into
// the threshold test. Returns 0 within threshold, else -1/+1 by lightness
// direction (negative when pixel 1 is lighter).
func oklabHyabDelta(dLr, dl, dm, ds, maxDelta float64) int {
	rest := maxDelta - math.Abs(dLr)
	if rest > 0 {
		da := 1.9779984951*dl - 2.4285922050*dm + 0.4505937099*ds
		db := 0.0259040371*dl + 0.7827717662*dm - 0.8086757660*ds
		if da*da+db*db <= rest*rest {
			return 0
		}
	}
	if dLr > 0 {
		return -1
	}
	return 1
}

// colorDelta returns 0 when the pixels match within maxDelta, else ±1
// (negative when pixel 1 is lighter). p is the byte offset of pixel 1; the
// checkerboard blend background follows pixel 1's index parity, like
// pixelmatch.
func colorDelta(img1, img2 []uint8, p1, p2 int, checkerboard bool, maxDelta float64) int {
	r1, g1, b1, a1 := img1[p1], img1[p1+1], img1[p1+2], img1[p1+3]
	r2, g2, b2, a2 := img2[p2], img2[p2+1], img2[p2+2], img2[p2+3]
	if a1 == 255 && a2 == 255 {
		lr1, l1, m1, s1 := oklabOpaque(r1, g1, b1)
		lr2, l2, m2, s2 := oklabOpaque(r2, g2, b2)
		return oklabHyabDelta(lr1-lr2, l1-l2, m1-m2, s1-s2, maxDelta)
	}
	rb, gb, bb := 255.0, 255.0, 255.0
	if checkerboard {
		k := p1 / 4
		rb = 48 + 159*float64(k%2)
		gb = 48 + 159*float64(int(float64(k)/1.618033988749895)%2)
		bb = 48 + 159*float64(int(float64(k)/2.618033988749895)%2)
	}
	fr1 := linInterp((float64(r1)*float64(a1) + rb*(255-float64(a1))) / 255)
	fg1 := linInterp((float64(g1)*float64(a1) + gb*(255-float64(a1))) / 255)
	fb1 := linInterp((float64(b1)*float64(a1) + bb*(255-float64(a1))) / 255)
	fr2 := linInterp((float64(r2)*float64(a2) + rb*(255-float64(a2))) / 255)
	fg2 := linInterp((float64(g2)*float64(a2) + gb*(255-float64(a2))) / 255)
	fb2 := linInterp((float64(b2)*float64(a2) + bb*(255-float64(a2))) / 255)
	l1 := cbrtInterp(0.4122214708*fr1 + 0.5363325363*fg1 + 0.0514459929*fb1)
	m1 := cbrtInterp(0.2119034982*fr1 + 0.6806995451*fg1 + 0.1073969566*fb1)
	s1 := cbrtInterp(0.0883024619*fr1 + 0.2817188376*fg1 + 0.6299787005*fb1)
	l2 := cbrtInterp(0.4122214708*fr2 + 0.5363325363*fg2 + 0.0514459929*fb2)
	m2 := cbrtInterp(0.2119034982*fr2 + 0.6806995451*fg2 + 0.1073969566*fb2)
	s2 := cbrtInterp(0.0883024619*fr2 + 0.2817188376*fg2 + 0.6299787005*fb2)
	lr1 := toe(0.2104542553*l1 + 0.7936177850*m1 - 0.0040720468*s1)
	lr2 := toe(0.2104542553*l2 + 0.7936177850*m2 - 0.0040720468*s2)
	return oklabHyabDelta(lr1-lr2, l1-l2, m1-m2, s1-s2, maxDelta)
}

// brightnessDelta is the cheap gamma-space Rec.601 luma delta used by the
// anti-aliasing detector (monotonic intensity ramp direction only).
func brightnessDelta(img []uint8, m int, r1, g1, b1, a1 uint8) float64 {
	r2, g2, b2, a2 := img[m], img[m+1], img[m+2], img[m+3]
	dr := float64(r1) - float64(r2)
	dg := float64(g1) - float64(g2)
	db := float64(b1) - float64(b2)
	da := float64(a1) - float64(a2)
	if dr == 0 && dg == 0 && db == 0 && da == 0 {
		return 0
	}
	if a1 < 255 || a2 < 255 {
		dr = (float64(r1)*float64(a1) - float64(r2)*float64(a2) - 255*da) / 255
		dg = (float64(g1)*float64(a1) - float64(g2)*float64(a2) - 255*da) / 255
		db = (float64(b1)*float64(a1) - float64(b2)*float64(a2) - 255*da) / 255
		d := dr*0.29889531 + dg*0.58662247 + db*0.11448223
		if d == 0 && da != 0 {
			return da / 2
		}
		return d
	}
	return dr*0.29889531 + dg*0.58662247 + db*0.11448223
}

// ---------------------------------------------------------------- diff -----

func diffRGBA(a, b *image.RGBA, threshold float64, ignore []image.Rectangle) (*image.RGBA, int, int) {
	w := a.Bounds().Dx()
	h := a.Bounds().Dy()
	maxDelta := threshold
	ab := a.Pix
	bb := b.Pix
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	ob := out.Pix
	diffCount := 0
	changedCount := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pos := (y*w + x) * 4
			if inRegions(ignore, x, y) {
				// excluded: dim like unchanged, never counted
				luma := float64(ab[pos])*0.29889531 + float64(ab[pos+1])*0.58662247 + float64(ab[pos+2])*0.11448223
				val := uint8(255 + (luma-255)*dimAlpha*float64(ab[pos+3])/255)
				ob[pos] = val
				ob[pos+1] = val
				ob[pos+2] = val
				ob[pos+3] = 255
				continue
			}
			delta := 0
			if !pixelsEqual(ab, bb, pos) {
				delta = colorDelta(ab, bb, pos, pos, true, maxDelta)
				if changedPixel(ab, bb, pos) {
					changedCount++
				}
			}
			switch {
			case delta != 0:
				if antialiased(ab, bb, w, h, x, y) || antialiased(bb, ab, w, h, x, y) {
					copy(ob[pos:pos+4], aaColor[:])
				} else {
					diffCount++
					if delta < 0 {
						copy(ob[pos:pos+4], diffColorAlt[:])
					} else {
						copy(ob[pos:pos+4], diffColor[:])
					}
				}
			default:
				// dim unchanged pixels so differences stand out
				luma := float64(ab[pos])*0.29889531 + float64(ab[pos+1])*0.58662247 + float64(ab[pos+2])*0.11448223
				val := uint8(255 + (luma-255)*dimAlpha*float64(ab[pos+3])/255)
				ob[pos] = val
				ob[pos+1] = val
				ob[pos+2] = val
				ob[pos+3] = 255
			}
		}
	}
	return out, diffCount, changedCount
}

func pixelsEqual(a, b []uint8, pos int) bool {
	return a[pos] == b[pos] && a[pos+1] == b[pos+1] && a[pos+2] == b[pos+2] && a[pos+3] == b[pos+3]
}

// inRegions reports whether (x, y) falls inside any ignore region.
func inRegions(regions []image.Rectangle, x, y int) bool {
	for _, r := range regions {
		if x >= r.Min.X && x < r.Max.X && y >= r.Min.Y && y < r.Max.Y {
			return true
		}
	}
	return false
}

// changedPixel reports whether the pixel differs at all above the noise
// floor (cheap sRGB check; the perceptual pass handles semantics).
func changedPixel(a, b []uint8, pos int) bool {
	d := int(a[pos]) - int(b[pos])
	if d < 0 {
		d = -d
	}
	if d > noiseFloor {
		return true
	}
	d = int(a[pos+1]) - int(b[pos+1])
	if d < 0 {
		d = -d
	}
	if d > noiseFloor {
		return true
	}
	d = int(a[pos+2]) - int(b[pos+2])
	if d < 0 {
		d = -d
	}
	return d > noiseFloor
}

func antialiased(img, other []uint8, w, h, x1, y1 int) bool {
	x0 := maxI(x1-1, 0)
	y0 := maxI(y1-1, 0)
	x2 := minI(x1+1, w-1)
	y2 := minI(y1+1, h-1)
	pos := (y1*w + x1) * 4
	cr, cg, cb, ca := img[pos], img[pos+1], img[pos+2], img[pos+3]
	zeroes := 0
	if x1 == x0 || x1 == x2 || y1 == y0 || y1 == y2 {
		zeroes = 1
	}
	minDelta, maxDelta := 0.0, 0.0
	minX, minY, maxX, maxY := 0, 0, 0, 0
	for y := y0; y <= y2; y++ {
		for x := x0; x <= x2; x++ {
			if x == x1 && y == y1 {
				continue
			}
			m := (y*w + x) * 4
			delta := brightnessDelta(img, m, cr, cg, cb, ca)
			if delta == 0 {
				zeroes++
				if zeroes > 2 {
					return false
				}
			} else if delta < minDelta {
				minDelta = delta
				minX, minY = x, y
			} else if delta > maxDelta {
				maxDelta = delta
				maxX, maxY = x, y
			}
		}
	}
	if minDelta == 0 || maxDelta == 0 {
		return false
	}
	return (hasManySiblings(img, w, h, minX, minY) && hasManySiblings(other, w, h, minX, minY)) ||
		(hasManySiblings(img, w, h, maxX, maxY) && hasManySiblings(other, w, h, maxX, maxY))
}

func hasManySiblings(img []uint8, w, h, x1, y1 int) bool {
	pos := (y1*w + x1) * 4
	if x1 > 0 && x1 < w-1 && y1 > 0 && y1 < h-1 {
		count := 0
		for y := y1 - 1; y <= y1+1; y++ {
			for x := x1 - 1; x <= x1+1; x++ {
				if x == x1 && y == y1 {
					continue
				}
				p := (y*w + x) * 4
				if pixelsEqualAt(img, pos, p) {
					count++
				}
			}
		}
		return count > 2
	}
	x0 := maxI(x1-1, 0)
	y0 := maxI(y1-1, 0)
	x2 := minI(x1+1, w-1)
	y2 := minI(y1+1, h-1)
	zeroes := 0
	if x1 == x0 || x1 == x2 || y1 == y0 || y1 == y2 {
		zeroes = 1
	}
	for y := y0; y <= y2; y++ {
		for x := x0; x <= x2; x++ {
			if x == x1 && y == y1 {
				continue
			}
			p := (y*w + x) * 4
			if pixelsEqualAt(img, pos, p) {
				zeroes++
				if zeroes > 2 {
					return true
				}
			}
		}
	}
	return false
}

func pixelsEqualAt(img []uint8, p, q int) bool {
	return img[p] == img[q] && img[p+1] == img[q+1] && img[p+2] == img[q+2] && img[p+3] == img[q+3]
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}
