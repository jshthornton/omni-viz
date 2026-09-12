// Package godot implements the omniviz driver for Godot 4.x projects:
// deterministic captures via the movie writer, a settings overlay so
// capture runs never fight the desktop, and windowed adhoc runs.
package godot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	omniviz "github.com/jshthornton/omni-viz"
)

func init() { omniviz.RegisterDriver(&Driver{}) }

// Driver captures Godot scenes.
//
// Two capture modes, like gdviz:
//   - generic (no shot paths): the scene runs under the movie writer and
//     the final recorded frame IS the screenshot — zero project code.
//   - cooperative (shot paths): the scene saves its own PNGs wherever it
//     likes and quits; the globs collect them.
type Driver struct {
	bin string // resolved in Check
	ver string
}

// Options come from [driver.godot] and per-shot driver_options.
type Options struct {
	Binary        string `toml:"binary"`         // godot executable (else $OMNIVIZ_GODOT, then PATH)
	DisplayDriver string `toml:"display_driver"` // linux only (default x11)
	Method        string `toml:"method"`         // --rendering-method passthrough
}

func (d *Driver) Name() string    { return "godot" }
func (d *Driver) NewOptions() any { return &Options{} }

func (d *Driver) VersionLabel() string {
	if d.ver == "" {
		return "godot (unprobed)"
	}
	return "godot " + d.ver
}

// Check resolves the Godot binary (per-driver options > $OMNIVIZ_GODOT >
// PATH) and probes its version. It also sanity-checks that the project
// looks like a Godot project.
func (d *Driver) Check(rc *omniviz.RunContext) error {
	opts := d.globalOptions(rc)
	bin := resolveBinary(opts.Binary)
	if bin == "" {
		return fmt.Errorf("godot binary not found (use [driver.godot] binary=, $OMNIVIZ_GODOT, or PATH)")
	}
	d.bin = bin
	d.ver = probeVersion(bin)
	if _, err := os.Stat(filepath.Join(rc.Project, "project.godot")); err != nil {
		return fmt.Errorf("%s is not a Godot project (no project.godot)", rc.Project)
	}
	return nil
}

func (d *Driver) Capture(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) (omniviz.CaptureResult, error) {
	opts, _ := job.DriverOptions.(*Options)
	if opts == nil {
		opts = &Options{}
	}
	if job.Target == "" {
		return omniviz.CaptureResult{}, fmt.Errorf("godot shot %q needs a target (a res:// scene path)", job.ID)
	}
	generic := len(job.Paths) == 0
	record := env.Record
	if generic {
		// generic mode: the screenshot IS the final recorded frame, so the
		// movie writer must run even when the recording is not kept
		record = true
	}
	framesDir := env.FramesDir
	if record {
		os.MkdirAll(framesDir, 0o755)
	}

	overlay, err := BuildOverlay(env.Project, job.Width, job.Height, env.SetOverrides)
	if err != nil {
		return omniviz.CaptureResult{}, fmt.Errorf("build overlay: %w", err)
	}
	defer os.RemoveAll(overlay)

	args := d.args(rc, job, opts, record, framesDir, overlay, env.Slot)
	tail, runErr := omniviz.RunLogged(ctx, d.bin, args, env.Env, job.Timeout, env.LogPath)

	var files []string
	var gerr error
	if generic {
		if list := omniviz.ListFrames(framesDir); len(list) > 0 {
			files = list[len(list)-1:]
		}
	} else {
		files, gerr = omniviz.GlobFiles(env.Project, job.Paths)
	}
	if len(files) == 0 {
		msg := fmt.Sprintf("no files matched [%s]", strings.Join(job.Paths, ", "))
		if generic {
			msg = "generic shot produced no recorded frames (did the scene render?)"
		}
		switch {
		case gerr != nil:
			msg = gerr.Error()
		case runErr != nil:
			msg = fmt.Sprintf("godot exited with error: %v", runErr)
		}
		return omniviz.CaptureResult{Error: msg, Log: tail}, nil
	}
	return omniviz.CaptureResult{ShotFiles: files, Log: tail}, nil
}

func (d *Driver) args(rc *omniviz.RunContext, job omniviz.Job, opts *Options, record bool, framesDir, overlay string, slot int) []string {
	args := []string{"--path", overlay}
	if runtime.GOOS == "linux" {
		display := opts.DisplayDriver
		if display == "" {
			display = "x11"
		}
		args = append(args, "--display-driver", display)
	}
	// Off-screen: captures must never overlay the desktop or steal focus.
	// no_focus=true comes in via the overlay's override.cfg; vsync is
	// disabled by the visual-test window path so an unmapped/occluded
	// window cannot presentation-stall. Cascade windows by slot so
	// parallel jobs don't stack perfectly.
	args = append(args,
		"--audio-driver", "Dummy",
		"--windowed",
		"--resolution", fmt.Sprintf("%dx%d", job.Width, job.Height),
		"--position", fmt.Sprintf("%d,%d", -30000+slot*24, -30000+slot*24),
		"--single-window",
	)
	if opts.Method != "" {
		args = append(args, "--rendering-method", opts.Method)
	}
	if record {
		args = append(args,
			"--write-movie", filepath.Join(framesDir, "frame.png"),
			"--fixed-fps", strconv.Itoa(rc.Config.Render.FPS),
		)
	}
	if job.QuitAfter > 0 {
		args = append(args, "--quit-after", strconv.Itoa(job.QuitAfter))
	}
	args = append(args, "--scene", job.Target, "--", "--visual-test-window")
	args = append(args, job.Args...)
	return args
}

// RunAdhoc launches one scene windowed and unfocused — the authoring
// convenience behind `omniviz run`.
func (d *Driver) RunAdhoc(ctx context.Context, rc *omniviz.RunContext, req omniviz.AdhocRequest) error {
	opts := d.globalOptions(rc)
	overlay, err := BuildOverlay(rc.Project, req.Width, req.Height, req.SetOverrides)
	if err != nil {
		return fmt.Errorf("build overlay: %w", err)
	}
	defer os.RemoveAll(overlay)

	framesDir := ""
	var args []string
	if req.Record {
		framesDir = filepath.Join(rc.Output, "frames", fmt.Sprintf("run-%d", time.Now().Unix()))
		if err := os.MkdirAll(framesDir, 0o755); err != nil {
			return err
		}
		args = append(
			args,
			"--write-movie", filepath.Join(framesDir, "frame.png"),
			"--fixed-fps", strconv.Itoa(rc.Config.Render.FPS),
		)
	}
	display := opts.DisplayDriver
	if display == "" && runtime.GOOS == "linux" {
		display = "x11"
	}
	if display != "" {
		args = append(args, "--display-driver", display)
	}
	args = append(args,
		"--audio-driver", "Dummy",
		"--windowed",
		"--resolution", fmt.Sprintf("%dx%d", req.Width, req.Height),
		"--position", "24,24",
		"--single-window",
		"--scene", req.Target,
		"--",
		"--visual-test-window",
	)
	args = append(args, req.UserArgs...)

	cmd := exec.Command(d.bin, args...)
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
		frames := omniviz.DecimateFrames(framesDir, rc.Config.Render.MaxFrames)
		fmt.Printf("omniviz run: %d frames -> %s\n", frames, framesDir)
	}
	return nil
}

func (d *Driver) globalOptions(rc *omniviz.RunContext) *Options {
	if o, ok := rc.Config.GlobalDriverOptions(d.Name()).(*Options); ok && o != nil {
		return o
	}
	return &Options{}
}
func resolveBinary(cfgVal string) string {
	if cfgVal != "" {
		return omniviz.ExpandTilde(cfgVal)
	}
	if v := os.Getenv("OMNIVIZ_GODOT"); v != "" {
		return v
	}
	if p, err := exec.LookPath("godot"); err == nil {
		return p
	}
	return ""
}

func probeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
