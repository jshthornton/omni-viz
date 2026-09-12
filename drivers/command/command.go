// Package command implements the universal omniviz driver: it runs any
// executable (or inline shell script) and collects the PNGs it produces.
//
// Almost every capture target can be expressed this way — a Unity editor in
// batchmode, an Unreal automation commandlet, a .NET Forms/WPF app that
// saves a screenshot, a TUI rendered in a headless terminal — which makes
// `command` the default driver and the escape hatch when no native driver
// exists. Native drivers (godot, web) exist for targets that need more than
// "run a process, collect files".
package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	omniviz "github.com/jshthornton/omni-viz"
)

func init() { omniviz.RegisterDriver(&Driver{}) }

// Driver runs a process per shot and collects its images.
type Driver struct{}

// Options come from [driver.command] and per-shot driver_options.
type Options struct {
	// Command is the argv to run (either this or Script is required).
	Command []string `toml:"command"`
	// Script is an inline shell script alternative to Command.
	Script string `toml:"script"`
	// Shell runs Script (default "sh"; invoked as <shell> -e -c <script>).
	Shell string `toml:"shell"`
	// Dir is the working directory for the process, relative to the
	// project root (default: project root).
	Dir string `toml:"dir"`
}

func (d *Driver) Name() string    { return "command" }
func (d *Driver) NewOptions() any { return &Options{} }

func (d *Driver) Check(rc *omniviz.RunContext) error {
	// per-shot validation happens in Capture; nothing global to check
	return nil
}

func (d *Driver) Capture(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) (omniviz.CaptureResult, error) {
	opts, _ := job.DriverOptions.(*Options)
	if opts == nil {
		opts = &Options{}
	}
	if len(opts.Command) == 0 && opts.Script == "" {
		return omniviz.CaptureResult{}, fmt.Errorf("command driver needs driver_options: command = [ ... ] (argv) or script = \"...\" on shot %q", job.ID)
	}

	dir := env.Project
	if opts.Dir != "" {
		dir = filepath.Join(env.Project, filepath.FromSlash(opts.Dir))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return omniviz.CaptureResult{}, fmt.Errorf("dir: %w", err)
		}
	}

	// The process environment carries the sandbox contract: write images
	// into $OMNIVIZ_OUTPUT (auto-collected when the shot has no paths) and
	// render at $OMNIVIZ_WIDTH x $OMNIVIZ_HEIGHT.
	procEnv := append([]string{}, env.Env...)
	procEnv = append(procEnv,
		"OMNIVIZ_JOB="+job.ID,
		"OMNIVIZ_OUTPUT="+env.ScratchDir,
		"OMNIVIZ_WIDTH="+fmt.Sprint(job.Width),
		"OMNIVIZ_HEIGHT="+fmt.Sprint(job.Height),
	)

	var bin string
	var args []string
	if len(opts.Command) > 0 {
		bin = opts.Command[0]
		args = append(append([]string{}, opts.Command[1:]...), job.Args...)
	} else {
		shell := opts.Shell
		if shell == "" {
			shell = "sh"
		}
		bin = shell
		// script gets the shot args positionally ($1, $2, ...)
		args = append([]string{"-e", "-c", opts.Script}, job.Args...)
	}

	tail, runErr := omniviz.RunLogged(ctx, bin, args, procEnv, job.Timeout, env.LogPath)
	return collect(job, env, tail, runErr)
}

func collect(job omniviz.Job, env omniviz.CaptureEnv, tail string, runErr error) (omniviz.CaptureResult, error) {
	var files []string
	var gerr error
	if len(job.Paths) > 0 {
		files, gerr = omniviz.GlobFiles(env.Project, job.Paths)
	} else {
		files, gerr = omniviz.GlobFiles(env.ScratchDir, []string{"*.png"})
	}
	if len(files) == 0 {
		msg := fmt.Sprintf("command produced no images (looked in %s)", env.ScratchDir)
		if len(job.Paths) > 0 {
			msg = fmt.Sprintf("no files matched [%s]", strings.Join(job.Paths, ", "))
		}
		switch {
		case gerr != nil:
			msg = gerr.Error()
		case runErr != nil:
			msg = fmt.Sprintf("command exited with error: %v", runErr)
		}
		return omniviz.CaptureResult{Error: msg, Log: tail}, nil
	}
	return omniviz.CaptureResult{ShotFiles: files, Log: tail}, nil
}
