// Package web implements the omniviz driver for browser targets: it loads a
// URL in a headless Chrome-family browser, waits for the page to settle and
// screenshots the viewport (or the full scrollable page).
package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"

	omniviz "github.com/jshthornton/omni-viz"
)

func init() { omniviz.RegisterDriver(&Driver{}) }

// Driver captures web pages.
type Driver struct{}

// Options come from [driver.web] and per-shot driver_options.
type Options struct {
	// Browser is the Chrome/Chromium/Edge executable. Empty auto-detects.
	Browser string `toml:"browser"`
	// Headless defaults to true.
	Headless *bool `toml:"headless"`
	// FullPage captures the whole scrollable page instead of the viewport.
	FullPage bool `toml:"full_page"`
	// WaitMS is the settle delay after load (default 250).
	WaitMS int `toml:"wait_ms"`
	// WaitReady is a JS expression polled until truthy before the shot
	// (e.g. "window.__chartsRendered === true").
	WaitReady string `toml:"wait_ready"`
}

func (d *Driver) Name() string    { return "web" }
func (d *Driver) NewOptions() any { return &Options{} }
func (d *Driver) Check(rc *omniviz.RunContext) error {
	// The browser binary is resolved by chromedp at first use; a missing
	// browser surfaces as a per-shot error with its own message.
	return nil
}

func (d *Driver) Capture(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) (omniviz.CaptureResult, error) {
	opts, _ := job.DriverOptions.(*Options)
	if opts == nil {
		opts = &Options{}
	}
	headless := true
	if opts.Headless != nil {
		headless = *opts.Headless
	}
	waitMS := opts.WaitMS
	if waitMS == 0 {
		waitMS = 250
	}

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", headless),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("force-device-scale-factor", "1"),
		chromedp.Flag("window-size", fmt.Sprintf("%d,%d", job.Width, job.Height)),
	)
	if opts.Browser != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(omniviz.ExpandTilde(opts.Browser)))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	// timeout belt-and-braces around the whole browser session
	runCtx, cancelRun := context.WithTimeout(browserCtx, job.Timeout)
	defer cancelRun()

	out := filepath.Join(env.ScratchDir, "shot.png")
	var buf []byte
	actions := []chromedp.Action{
		chromedp.EmulateViewport(int64(job.Width), int64(job.Height)),
		chromedp.Navigate(job.Target),
		chromedp.WaitReady("body"),
		chromedp.Sleep(time.Duration(waitMS) * time.Millisecond),
	}
	if opts.WaitReady != "" {
		var ready bool
		actions = append(actions, chromedp.Poll(opts.WaitReady, &ready))
	}
	if opts.FullPage {
		actions = append(actions, chromedp.FullScreenshot(&buf, 100))
	} else {
		actions = append(actions, chromedp.CaptureScreenshot(&buf))
	}
	if err := chromedp.Run(runCtx, actions...); err != nil {
		return omniviz.CaptureResult{}, fmt.Errorf("browser: %w", err)
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		return omniviz.CaptureResult{}, err
	}

	// Recording: best-effort timed screenshots at render.fps (no movie
	// writer equivalent in a browser). quit_after frames when set, else
	// render.max_frames.
	if env.Record {
		if err := d.record(runCtx, rc, job, env); err != nil {
			fmt.Fprintf(os.Stderr, "omniviz: warning: web recording %s: %v\n", job.ID, err)
		}
	}
	return omniviz.CaptureResult{ShotFiles: []string{out}}, nil
}

func (d *Driver) record(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) error {
	fps := rc.Config.Render.FPS
	frames := job.QuitAfter
	if frames <= 0 {
		frames = rc.Config.Render.MaxFrames
	}
	if frames > rc.Config.Render.MaxFrames {
		frames = rc.Config.Render.MaxFrames
	}
	interval := time.Second / time.Duration(fps)
	for i := 0; i < frames; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var buf []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return err
		}
		out := filepath.Join(env.FramesDir, fmt.Sprintf("frame_%04d.png", i))
		if err := os.WriteFile(out, buf, 0o644); err != nil {
			return err
		}
	}
	return nil
}
