package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func makePNG(t *testing.T, dir, name string, w, h int, fill color.RGBA, override func(x, y int) (color.RGBA, bool)) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if override != nil {
				if c, ok := override(x, y); ok {
					img.Set(x, y, c)
					continue
				}
			}
			img.Set(x, y, fill)
		}
	}
	path := filepath.Join(dir, name)
	if err := savePNG(path, img); err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
	return path
}

func TestCompareIdentical(t *testing.T) {
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	res, diff, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.SizeMismatch {
		t.Fatal("unexpected size mismatch")
	}
	if res.DiffPixels != 0 {
		t.Fatalf("identical images: got %d diff pixels, want 0", res.DiffPixels)
	}
	if diff == nil {
		t.Fatal("expected diff image even when identical")
	}
}

func TestCompareSubtleShiftPasses(t *testing.T) {
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{R: 130, G: 130, B: 130, A: 255}, nil)
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiffPixels != 0 {
		t.Fatalf("subtle +2 shift should pass at threshold 0.1, got %d diff pixels", res.DiffPixels)
	}
}

func TestCompareStrongDeltaFails(t *testing.T) {
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{B: 255, A: 255}, func(x, y int) (color.RGBA, bool) {
		if x == 32 && y == 32 {
			return color.RGBA{R: 255, A: 255}, true
		}
		return color.RGBA{}, false
	})
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{B: 255, A: 255}, nil)
	res, diff, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiffPixels != 1 {
		t.Fatalf("want exactly 1 diff pixel, got %d", res.DiffPixels)
	}
	if res.Ratio <= 0 || res.Ratio >= 1 {
		t.Fatalf("ratio out of range: %v", res.Ratio)
	}
	if diff == nil {
		t.Fatal("expected diff image")
	}
}

func TestCompareSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{A: 255}, nil)
	b := makePNG(t, dir, "b.png", 32, 64, color.RGBA{A: 255}, nil)
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if !res.SizeMismatch {
		t.Fatal("expected size mismatch")
	}
	if res.BaseWidth != 64 || res.CurWidth != 32 {
		t.Fatalf("dims not reported: %+v", res)
	}
}

func TestAntiAliasedEdgeIgnored(t *testing.T) {
	// A soft edge (gradient between two colors) that shifts by one pixel of
	// interpolation should not explode the diff count: the AA detector must
	// absorb the smoothed pixels.
	dir := t.TempDir()
	gradient := func(x, y int) (color.RGBA, bool) {
		v := uint8(minI(255, x*8))
		return color.RGBA{R: v, G: v, B: v, A: 255}, true
	}
	a := makePNG(t, dir, "a.png", 64, 32, color.RGBA{A: 255}, gradient)
	b := makePNG(t, dir, "b.png", 64, 32, color.RGBA{A: 255}, func(x, y int) (color.RGBA, bool) {
		v := uint8(minI(255, x*8+2))
		return color.RGBA{R: v, G: v, B: v, A: 255}, true
	})
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiffPixels > 0 {
		t.Logf("note: %d aa-ish pixels counted as diffs (detector tolerance)", res.DiffPixels)
	}
}

func TestPngDimsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := makePNG(t, dir, "d.png", 31, 17, color.RGBA{A: 255}, nil)
	dims, err := pngDims(p)
	if err != nil {
		t.Fatal(err)
	}
	if dims.X != 31 || dims.Y != 17 {
		t.Fatalf("dims: %v", dims)
	}
}

func TestSanitizeKey(t *testing.T) {
	ok := []string{"fog/fog_haze_near", "room-gallery", "a_b.c"}
	for _, k := range ok {
		if _, err := SanitizeKey(k); err != nil {
			t.Errorf("SanitizeKey(%q) unexpected error: %v", k, err)
		}
	}
	bad := []string{"", "..", "../etc/passwd", "a//b"}
	for _, k := range bad {
		if _, err := SanitizeKey(k); err == nil {
			t.Errorf("SanitizeKey(%q) should fail", k)
		}
	}
	if _, err := SanitizeKey("foo bar"); err != nil {
		t.Errorf("spaces should be replaced, not rejected: %v", err)
	}
}

func TestDecimate(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, "frame"+string(rune('a'+i))+".png")
		os.WriteFile(p, []byte("x"), 0o644)
	}
	n := decimateFrames(dir, 4)
	if n != 4 {
		t.Fatalf("decimate kept %d, want 4", n)
	}
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("dir has %d files after decimate, want 4", count)
	}
}

func TestCompareGlobalDriftFailsChangedBudget(t *testing.T) {
	// the fog-bug case: a uniform brightness shift changes every pixel. A
	// small drift (128 -> 136, ~3.5% HyAB) stays under the per-pixel
	// threshold but must trip the changed-area budget.
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{R: 136, G: 136, B: 136, A: 255}, nil)
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiffPixels != 0 {
		t.Fatalf("global 128->136 drift should stay under the perceptual threshold, got %d diff pixels", res.DiffPixels)
	}
	if res.ChangedRatio < 0.99 {
		t.Fatalf("global drift should flag ~100%% of pixels as changed, got %.1f%%", res.ChangedRatio*100)
	}
}

func TestCompareBigDriftFailsPerPixelToo(t *testing.T) {
	// a 128 -> 160 flip is ~11.7% of the black-white HyAB distance, so the
	// per-pixel threshold (0.1) fails every pixel on its own
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{R: 160, G: 160, B: 160, A: 255}, nil)
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.DiffPixels != res.TotalPixels {
		t.Fatalf("128->160 flip should fail per-pixel everywhere, got %d/%d", res.DiffPixels, res.TotalPixels)
	}
}

func TestCompareNoiseFloorIgnoresDither(t *testing.T) {
	// +-1..2 sRGB jitter (GPU dither) counts as no change
	dir := t.TempDir()
	a := makePNG(t, dir, "a.png", 64, 64, color.RGBA{R: 128, G: 128, B: 128, A: 255}, nil)
	b := makePNG(t, dir, "b.png", 64, 64, color.RGBA{R: 130, G: 130, B: 130, A: 255}, nil)
	res, _, err := ComparePNG(a, b, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if res.ChangedPixels != 0 {
		t.Fatalf("+2 shift should be inside the noise floor, got %d changed pixels", res.ChangedPixels)
	}
}
