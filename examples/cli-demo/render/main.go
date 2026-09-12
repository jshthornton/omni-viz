// Command render draws deterministic demo images for the cli-demo example.
//
// It is a stand-in for "any program that can save a PNG": a game engine
// capture scene, a Unity batchmode script, a .NET form, a terminal UI —
// the omniviz command driver only asks for a process that writes images
// and exits.
//
//	flags:
//	  --set=report|panels   which images to draw
//	  --variant=clean|drift|broken   drift: global brightness shift (changed-area
//	                         budget failure) · broken: structural change
//	                         (per-pixel threshold failure)
//
// Images go to $OMNIVIZ_OUTPUT (the per-job scratch dir, auto-collected)
// or, for panels, to $OMNIVIZ_PROJECT/tmp/shots (collected via paths globs).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
)

var (
	bg     = color.RGBA{0x12, 0x14, 0x1c, 255}
	panel  = color.RGBA{0x1c, 0x21, 0x2b, 255}
	edge   = color.RGBA{0x30, 0x36, 0x3d, 255}
	accent = color.RGBA{0xe3, 0xb3, 0x41, 255}
	good   = color.RGBA{0x3f, 0xb9, 0x50, 255}
	bad    = color.RGBA{0xf8, 0x51, 0x49, 255}
	blue   = color.RGBA{0x58, 0xa6, 0xff, 255}
	pink   = color.RGBA{0xdb, 0x61, 0xa2, 255}
)

func main() {
	set := flag.String("set", "report", "report|panels")
	variant := flag.String("variant", "clean", "clean|drift|broken")
	flag.Parse()

	out := getenv("OMNIVIZ_OUTPUT", ".")
	project := getenv("OMNIVIZ_PROJECT", ".")

	switch *set {
	case "report":
		write(filepath.Join(out, "report.png"), dashboard(*variant))
	case "panels":
		dir := filepath.Join(project, "tmp", "shots")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
		}
		write(filepath.Join(dir, "bar.png"), barChart(*variant))
		write(filepath.Join(dir, "line.png"), lineChart(*variant))
		write(filepath.Join(dir, "gauge.png"), gauge(*variant))
	default:
		fail(fmt.Errorf("unknown --set %q", *set))
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "render:", err)
	os.Exit(1)
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fail(err)
	}
}

// applyVariant simulates the two failure classes VRT suites catch.
func applyVariant(img *image.RGBA, variant string) *image.RGBA {
	switch variant {
	case "drift":
		// every pixel a little brighter — a fog/exposure shift. The
		// per-pixel threshold tolerates this; the changed-area budget
		// (correctly) does not.
		b := img.Bounds()
		out := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				out.SetRGBA(x, y, color.RGBA{
					R: scale(c.R, 1.05), G: scale(c.G, 1.05), B: scale(c.B, 1.05), A: c.A,
				})
			}
		}
		return out
	default:
		return img
	}
}

func scale(v uint8, m float64) uint8 {
	x := float64(v) * m
	if x > 255 {
		x = 255
	}
	return uint8(x)
}

const (
	W = 480
	H = 320
)

func canvas() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	return img
}

func rect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

func frame(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	rect(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), c)
	rect(img, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), c)
	rect(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y), c)
	rect(img, image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y), c)
}

func line(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func disc(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---------------------------------------------------------------- scenes

// dashboard is the single-shot "report": header, sparkline, bars, gauge.
func dashboard(variant string) image.Image {
	img := canvas()
	rng := rand.New(rand.NewSource(7))

	rect(img, image.Rect(0, 0, W, 40), panel)
	rect(img, image.Rect(0, 38, W, 40), edge)
	disc(img, 24, 20, 8, accent)
	for i, w := range []int{90, 60, 110, 40} {
		rect(img, image.Rect(60+sum(w, i), 16, 60+sum(w, i)+w-8, 24), edge)
	}

	p1 := image.Rect(16, 56, 312, 180)
	rect(img, p1, panel)
	frame(img, p1, edge)
	vals := []int{60, 90, 45, 120, 75, 100}
	if variant == "broken" {
		vals = []int{60, 90, 45, 20, 75, 100} // one bar collapses
	}
	for i, v := range vals {
		h := v
		x0 := p1.Min.X + 16 + i*46
		rect(img, image.Rect(x0, p1.Max.Y-16-h, x0+30, p1.Max.Y-16), cycle(i, blue, good, pink, accent))
	}

	p2 := image.Rect(328, 56, W-16, 180)
	rect(img, p2, panel)
	frame(img, p2, edge)
	cx, cy := (p2.Min.X+p2.Max.X)/2, (p2.Min.Y+p2.Max.Y)/2
	disc(img, cx, cy, 44, bg)
	disc(img, cx, cy, 44, color.RGBA{})
	frame(img, image.Rect(cx-44, cy-44, cx+44, cy+44), edge)
	disc(img, cx, cy, 6, accent)
	needle := 42
	if variant == "broken" {
		needle = -35 // gauge swings the other way
	}
	line(img, cx, cy, cx+needle, cy-28, bad)

	p3 := image.Rect(16, 196, W-16, H-16)
	rect(img, p3, panel)
	frame(img, p3, edge)
	prev := 0
	for x := 0; x <= 400; x += 8 {
		y := 30 + rng.Intn(50) + x/8
		if prev != 0 {
			line(img, p3.Min.X+16+prev*1, p3.Max.Y-16-prev%90, p3.Min.X+16+x, p3.Max.Y-16-y%90, good)
		}
		prev = x
	}
	return applyVariant(img, variant)
}

func sum(w, i int) int {
	offsets := []int{0, 98, 166, 284}
	return offsets[i]
}

func pick(rng *rand.Rand, cs ...color.RGBA) color.RGBA {
	return cs[rng.Intn(len(cs))]
}

// cycle picks a deterministic color by index (no rand).
func cycle(i int, cs ...color.RGBA) color.RGBA { return cs[i%len(cs)] }

func barChart(variant string) image.Image {
	img := canvas()
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 7; i++ {
		h := 40 + rng.Intn(180)
		if variant == "broken" && i == 3 {
			h = 12
		}
		x0 := 30 + i*62
		rect(img, image.Rect(x0, H-30-h, x0+40, H-30), cycle(i, blue, good, pink, accent))
	}
	rect(img, image.Rect(20, H-30, W-20, H-28), edge)
	return applyVariant(img, variant)
}

func lineChart(variant string) image.Image {
	img := canvas()
	rng := rand.New(rand.NewSource(23))
	prevX, prevY := 0, 0
	for i := 0; i <= 8; i++ {
		x := 30 + i*55
		y := H - 40 - rng.Intn(200)
		if variant == "broken" && i == 5 {
			y = 40
		}
		if i > 0 {
			line(img, prevX, prevY, x, y, blue)
			disc(img, x, y, 4, accent)
		}
		prevX, prevY = x, y
	}
	return applyVariant(img, variant)
}

func gauge(variant string) image.Image {
	img := canvas()
	cx, cy := W/2, H/2
	// ordered slice — map iteration order is random, and randomly stacked
	// rings are exactly the nondeterminism a VRT suite exists to catch
	rings := []struct {
		r int
		c color.RGBA
	}{{120, panel}, {100, bg}, {80, panel}}
	for _, ring := range rings {
		disc(img, cx, cy, ring.r, ring.c)
	}
	if variant == "broken" {
		disc(img, cx+30, cy+20, 40, bad)
	}
	disc(img, cx, cy, 10, accent)
	for a := 0; a < 12; a++ {
		line(img, cx+78*icos(a)/1, cy+78*isin(a)/1, cx+90*icos(a)/1, cy+90*isin(a)/1, edge)
	}
	return applyVariant(img, variant)
}

// fixed-point unit circle (deterministic, no float drift across platforms)
func icos(a int) int { return []int{100, 87, 50, 0, -50, -87, -100, -87, -50, 0, 50, 87}[a%12] / 1 }
func isin(a int) int { return []int{0, 50, 87, 100, 87, 50, 0, -50, -87, -100, -87, -50}[a%12] / 1 }
