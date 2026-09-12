package main

import (
	"context"
	"flag"
	"fmt"

	omniviz "github.com/jshthornton/omni-viz"
)

// cmdRun launches one target adhoc through a driver that supports it (a
// windowed, unfocused dev capture — no baselines, no diffing).
//
//	omniviz run [--driver NAME] TARGET [-- USER_ARGS...]
func cmdRun(argv []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	driver := fs.String("driver", "", "driver to launch with (default: defaults.driver in omniviz.toml, else godot)")
	width := fs.Int("width", 0, "window width (default: render.width in omniviz.toml, else 1280)")
	height := fs.Int("height", 0, "window height (default: render.height in omniviz.toml, else 720)")
	record := fs.Bool("record", false, "record the run as frames under tmp/omniviz/frames/")
	var set sliceFlags
	fs.Var(&set, "set", "driver-specific override SECTION/KEY=VALUE (repeatable)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("usage: omniviz run [flags] TARGET [-- USER_ARGS...]")
	}
	target := rest[0]
	userArgs := rest[1:]
	if len(userArgs) > 0 && userArgs[0] == "--" {
		userArgs = userArgs[1:]
	}

	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	name := *driver
	if name == "" {
		name = c.Config.Defaults.Driver
	}
	if name == "" {
		name = "godot"
	}
	drv, err := omniviz.GetDriver(name)
	if err != nil {
		return err
	}
	runner, ok := drv.(omniviz.AdhocRunner)
	if !ok {
		return fmt.Errorf("driver %q does not support `omniviz run`", name)
	}
	if err := drv.Check(c); err != nil {
		return fmt.Errorf("driver %s: %w", name, err)
	}

	w, h := *width, *height
	if w <= 0 {
		w = c.Config.Render.Width
	}
	if w <= 0 {
		w = 1280
	}
	if h <= 0 {
		h = c.Config.Render.Height
	}
	if h <= 0 {
		h = 720
	}

	req := omniviz.AdhocRequest{
		Target:       target,
		Width:        w,
		Height:       h,
		Record:       *record,
		SetOverrides: set,
		UserArgs:     userArgs,
	}
	return runner.RunAdhoc(context.Background(), c, req)
}
