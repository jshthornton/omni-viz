package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// cmdRun launches a single scene adhoc — the interactive/dev-capture path the
// project's legacy windowed runner used to cover. No baselines, no diffing,
// no key claims: Godot's output streams straight to the terminal so sandbox
// scenes remain watchable, and the exit code is Godot's.
//
//	omniviz run [--project DIR] [--width N] [--height N] [--record]
//	          [--set KEY=VAL]... [--godot PATH] SCENE [-- USER_ARGS...]
func cmdRun(argv []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	project := fs.String("project", ".", "Godot project root containing omniviz.toml")
	godot := fs.String("godot", "", "godot binary path (else $OMNIVIZ_GODOT, config, or PATH)")
	width := fs.Int("width", 0, "window width (default: render.width in omniviz.toml, else 1280)")
	height := fs.Int("height", 0, "window height (default: render.height in omniviz.toml, else 720)")
	record := fs.Bool("record", false, "record the run as movie frames under tmp/omniviz/frames/")
	var set sliceFlags
	fs.Var(&set, "set", "extra project-setting override SECTION/KEY=VALUE (repeatable)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("usage: omniviz run [flags] SCENE [-- USER_ARGS...]")
	}
	scene := rest[0]
	userArgs := rest[1:]
	if len(userArgs) > 0 && userArgs[0] == "--" {
		userArgs = userArgs[1:]
	}

	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	if err := c.ensureGodot(*godot); err != nil {
		return err
	}

	w := *width
	h := *height
	if w <= 0 {
		w = c.cfg.Render.Width
	}
	if w <= 0 {
		w = 1280
	}
	if h <= 0 {
		h = c.cfg.Render.Height
	}
	if h <= 0 {
		h = 720
	}

	overlay, err := buildOverlay(c.project, w, h, set)
	if err != nil {
		return fmt.Errorf("build overlay: %w", err)
	}
	defer os.RemoveAll(overlay)

	framesDir := ""
	var args []string
	if *record {
		framesDir = filepath.Join(c.output, "frames", fmt.Sprintf("run-%d", time.Now().Unix()))
		if err := os.MkdirAll(framesDir, 0o755); err != nil {
			return err
		}
		args = append(
			args,
			"--write-movie", filepath.Join(framesDir, "frame.png"),
			"--fixed-fps", strconv.Itoa(c.cfg.Render.FPS),
		)
	}
	args = append(args,
		"--display-driver", c.cfg.DisplayDriver,
		"--audio-driver", "Dummy",
		"--windowed",
		"--resolution", fmt.Sprintf("%dx%d", w, h),
		"--position", "24,24",
		"--single-window",
		"--scene", scene,
		"--",
		"--visual-test-window",
	)
	args = append(args, userArgs...)

	cmd := exec.Command(c.godotBin, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil // Godot's own non-zero exit passes through silently.
		}
		return err
	}
	if framesDir != "" {
		frames := decimateFrames(framesDir, c.cfg.Render.MaxFrames)
		fmt.Printf("omniviz run: %d frames -> %s\n", frames, framesDir)
	}
	return nil
}
