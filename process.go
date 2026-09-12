package omniviz

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file holds the process- and file-level helpers the core and the
// process-launching drivers share.

// RunLogged runs a capture process, teeing its output to logPath, and kills
// it after timeout. Returns a tail of the log for the report.
func RunLogged(ctx context.Context, bin string, args []string, env []string, timeout time.Duration, logPath string) (string, error) {
	if os.Getenv("OMNIVIZ_DEBUG") != "" {
		fmt.Println("[omniviz-debug] " + bin + " " + strings.Join(args, " "))
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, bin, args...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		runErr = fmt.Errorf("timed out after %s", timeout)
		<-done
	}
	return TailFile(logPath, 4096), runErr
}

// TailFile returns the last max bytes of a text file ("" when unreadable).
func TailFile(path string, max int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > max {
		data = data[len(data)-max:]
	}
	return string(data)
}

// DecimateFrames keeps at most max evenly spaced PNG frames in dir (last
// frame always kept) and returns how many remain. Recordings cost disk only;
// this caps them.
func DecimateFrames(dir string, max int) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var frames []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			frames = append(frames, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(frames)
	if len(frames) <= max {
		return len(frames)
	}
	keep := map[int]bool{}
	for i := 0; i < max; i++ {
		keep[i*(len(frames)-1)/(max-1)] = true
	}
	kept := 0
	for i, f := range frames {
		if keep[i] {
			kept++
		} else {
			os.Remove(f)
		}
	}
	return kept
}

// ListFrames returns the sorted PNG files in dir (empty when absent).
func ListFrames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// GlobFiles collects the files matched by patterns (slash paths relative to
// root), sorted and deduplicated.
func GlobFiles(root string, patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range patterns {
		// shell-habit negation: Go character classes negate with ^, not !
		p = strings.ReplaceAll(p, "[!", "[^")
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, fmt.Errorf("bad pattern %q: %w", p, err)
		}
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && st.Mode().IsRegular() && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// EnsureGlobDirs pre-creates the directories each shot glob writes into —
// many engines' image savers do not create missing parent directories.
func EnsureGlobDirs(root string, patterns []string) {
	for _, p := range patterns {
		dir := filepath.Dir(filepath.FromSlash(p))
		if strings.ContainsAny(dir, "*?[") {
			continue
		}
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
}

// CopyFile copies src to dst, syncing dst to disk.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// ExpandTilde resolves a leading ~ in a path (binary paths in config).
func ExpandTilde(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	if len(p) > 1 && p[1] != '/' && p[1] != filepath.Separator {
		return p // ~user paths left alone
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// ---------------------------------------------------------------- png io

// LoadRGBA decodes a PNG file into an RGBA image.
func LoadRGBA(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return ToRGBA(img), nil
}

// ToRGBA converts any image to a zero-origin RGBA.
func ToRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == image.Pt(0, 0) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// PNGDims reads just the dimensions of a PNG file.
func PNGDims(path string) (image.Point, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Point{}, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return image.Point{}, err
	}
	return image.Pt(cfg.Width, cfg.Height), nil
}

// SavePNG encodes img to path as PNG.
func SavePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// ParseSize parses a "WxH" string.
func ParseSize(s string) (int, int, error) { return parseSize(s) }
