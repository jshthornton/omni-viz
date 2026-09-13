// Package web implements the omniviz driver for browser targets.
//
// The capture model follows Chromatic's: one warm browser process serves
// the whole run — each shot opens an isolated tab in it, waits for the page
// to settle, freezes animations/carets so captures are deterministic, and
// screenshots the viewport or the full page. No browser is spawned per shot;
// tabs cost milliseconds, processes cost seconds.
package web

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	omniviz "github.com/jshthornton/omni-viz"
)

func init() { omniviz.RegisterDriver(&Driver{}) }

// freezeCSS makes captures deterministic the way capture farms do:
// animations pause in place, transitions and caret blinking off, smooth
// scrolling off. Injected right after navigation so nothing animates
// during the settle window.
const freezeCSS = `
*, *::before, *::after {
  animation-play-state: paused !important;
  transition: none !important;
  caret-color: transparent !important;
  scroll-behavior: auto !important;
}
`

// Driver captures web pages in a shared headless browser.
type Driver struct {
	mu sync.Mutex
	// one warm process per browser binary + headless + sandbox mode
	pools map[string]*pool
	// browserKey → true once a run discovered the sandbox is unusable
	// (AppArmor-blocked user namespaces, root containers) and the pool
	// was rebuilt with --no-sandbox
	learnedNoSandbox map[string]bool
}

type pool struct {
	alloc  context.Context
	cancel context.CancelFunc
}

// Options come from [driver.web] and per-shot driver_options.
type Options struct {
	// Browser is the Chrome/Chromium/Edge executable. Empty resolves
	// $OMNIVIZ_BROWSER, then chromedp's auto-detection.
	Browser string `toml:"browser"`
	// Headless defaults to true.
	Headless *bool `toml:"headless"`
	// FullPage captures the whole scrollable page instead of the viewport.
	FullPage bool `toml:"full_page"`
	// WaitMS is the settle delay after the page is ready (default 250).
	WaitMS int `toml:"wait_ms"`
	// WaitReady is a JS expression polled until truthy before the shot
	// (e.g. "window.__chartsRendered === true").
	WaitReady string `toml:"wait_ready"`
	// Selector captures a single element (CSS selector) instead of the
	// page — component-level shots the way Chromatic stories work.
	Selector string `toml:"selector"`
	// Freeze disables animation/caret freezing for flaky-by-design pages.
	// Default freezes (recommended).
	Freeze *bool `toml:"freeze"`
	// Sandbox forces the chrome sandbox on/off. Default auto: sandbox on,
	// falling back to --no-sandbox when the browser cannot start otherwise
	// (hardened distros, root containers) and remembering it for the run.
	Sandbox *bool `toml:"sandbox"`
}

func (d *Driver) Name() string    { return "web" }
func (d *Driver) NewOptions() any { return &Options{} }

func (d *Driver) Check(rc *omniviz.RunContext) error {
	// The browser binary resolves lazily on first capture; a missing
	// browser surfaces as a per-shot error with its own message.
	return nil
}

// browserKey identifies an allocator: one warm process per binary+mode.
func browserKey(opts *Options) string {
	headless := true
	if opts.Headless != nil {
		headless = *opts.Headless
	}
	bin := opts.Browser
	if bin == "" {
		bin = os.Getenv("OMNIVIZ_BROWSER")
	}
	return bin + "|" + fmt.Sprint(headless)
}

// getPool returns the warm-browser pool for this binary/mode, honoring an
// explicit sandbox preference, a per-run learned no-sandbox fallback, or a
// force (the retry path).
func (d *Driver) getPool(opts *Options, forceNoSandbox bool) *pool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pools == nil {
		d.pools = map[string]*pool{}
	}
	base := browserKey(opts)
	noSandbox := forceNoSandbox
	if !noSandbox && opts.Sandbox != nil {
		noSandbox = !*opts.Sandbox
	}
	if !noSandbox && d.learnedNoSandbox[base] {
		noSandbox = true
	}
	key := fmt.Sprintf("%s|sandbox=%v", base, !noSandbox)
	p, ok := d.pools[key]
	if !ok {
		headless := true
		if opts.Headless != nil {
			headless = *opts.Headless
		}
		allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", headless),
			chromedp.Flag("hide-scrollbars", true),
			chromedp.Flag("force-device-scale-factor", "1"),
			chromedp.Flag("force-prefers-reduced-motion", true),
		)
		if noSandbox {
			allocOpts = append(allocOpts, chromedp.NoSandbox)
		}
		bin := opts.Browser
		if bin == "" {
			bin = os.Getenv("OMNIVIZ_BROWSER")
		}
		if bin != "" {
			allocOpts = append(allocOpts, chromedp.ExecPath(omniviz.ExpandTilde(bin)))
		}
		ctx, cancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
		p = &pool{alloc: ctx, cancel: cancel}
		d.pools[key] = p
	}
	return p
}

// noteStartFailure remembers that this browser cannot run sandboxed, so the
// next capture goes straight to the working pool.
func (d *Driver) noteStartFailure(opts *Options) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.learnedNoSandbox == nil {
		d.learnedNoSandbox = map[string]bool{}
	}
	d.learnedNoSandbox[browserKey(opts)] = true
}

// dropPool forgets an allocator (browser died); the next capture respawns.
func (d *Driver) dropPool(opts *Options) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := browserKey(opts)
	if p, ok := d.pools[key]; ok {
		p.cancel()
		delete(d.pools, key)
	}
}

func (d *Driver) Capture(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) (res omniviz.CaptureResult, err error) {
	opts, _ := job.DriverOptions.(*Options)
	if opts == nil {
		opts = &Options{}
	}
	freeze := true
	if opts.Freeze != nil {
		freeze = *opts.Freeze
	}
	waitMS := opts.WaitMS
	if waitMS == 0 {
		waitMS = 250
	}

	pageURL, err := pageURL(env.Project, job.Target)
	if err != nil {
		return omniviz.CaptureResult{}, err
	}

	out := filepath.Join(env.ScratchDir, "shot.png")
	capture := func(p *pool) error {
		// A new tab in the warm browser — isolated, cheap.
		tabCtx, cancelTab := chromedp.NewContext(p.alloc)
		defer cancelTab()
		runCtx, cancelRun := context.WithTimeout(tabCtx, job.Timeout)
		defer cancelRun()

		var buf []byte
		actions := []chromedp.Action{
			chromedp.EmulateViewport(int64(job.Width), int64(job.Height)),
			chromedp.Navigate(pageURL),
		}
		if freeze {
			actions = append(actions, injectCSS(freezeCSS))
		}
		actions = append(actions,
			waitLoaded(),
			waitFonts(),
		)
		if opts.WaitReady != "" {
			var ready bool
			actions = append(actions, chromedp.Poll(opts.WaitReady, &ready))
		}
		actions = append(actions, chromedp.Sleep(time.Duration(waitMS)*time.Millisecond))
		switch {
		case opts.Selector != "":
			actions = append(actions, chromedp.Screenshot(opts.Selector, &buf, chromedp.NodeVisible))
		case opts.FullPage:
			actions = append(actions, chromedp.FullScreenshot(&buf, 100))
		default:
			actions = append(actions, chromedp.CaptureScreenshot(&buf))
		}
		if err := chromedp.Run(runCtx, actions...); err != nil {
			return err
		}
		return os.WriteFile(out, buf, 0o644)
	}

	if err := capture(d.getPool(opts, false)); err != nil {
		if !isStartFailure(err) {
			return omniviz.CaptureResult{}, fmt.Errorf("browser: %w", err)
		}
		// Sandbox blocked (hardened distros, root containers) or the
		// browser died: rebuild once without the sandbox and remember it.
		d.dropPool(opts)
		d.noteStartFailure(opts)
		if err := capture(d.getPool(opts, true)); err != nil {
			return omniviz.CaptureResult{}, fmt.Errorf("browser: %w", err)
		}
	}

	// Recording: best-effort timed viewport screenshots at render.fps.
	// quit_after frames when set, else render.max_frames.
	if env.Record {
		if err := d.record(opts, rc, job, env); err != nil {
			fmt.Fprintf(os.Stderr, "omniviz: warning: web recording %s: %v\n", job.ID, err)
		}
	}
	return omniviz.CaptureResult{ShotFiles: []string{out}}, nil
}

// isStartFailure reports whether a capture error means the browser process
// never came up (as opposed to a page-level failure).
func isStartFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "failed to start") ||
		strings.Contains(msg, "No usable sandbox") ||
		strings.Contains(msg, "browser has exited") ||
		strings.Contains(msg, "target crashed")
}

// injectCSS appends a stylesheet to the document head.
func injectCSS(css string) chromedp.Action {
	js := fmt.Sprintf(`(() => {
		const s = document.createElement('style');
		s.textContent = %q;
		document.head.appendChild(s);
	})()`, css)
	return chromedp.Evaluate(js, nil)
}

// waitLoaded polls document.readyState to complete.
func waitLoaded() chromedp.Action {
	var complete bool
	return chromedp.Poll(`document.readyState === 'complete'`, &complete)
}

// waitFonts blocks until web fonts finish loading — the classic source of
// "wrong font in the screenshot" flakes that capture farms must handle.
func waitFonts() chromedp.Action {
	var loaded bool
	return chromedp.Poll(`document.fonts.status === 'loaded'`, &loaded,
		chromedp.WithPollingTimeout(10*time.Second))
}

func (d *Driver) record(opts *Options, rc *omniviz.RunContext, job omniviz.Job, env omniviz.CaptureEnv) error {
	fps := rc.Config.Render.FPS
	frames := job.QuitAfter
	if frames <= 0 {
		frames = rc.Config.Render.MaxFrames
	}
	if frames > rc.Config.Render.MaxFrames {
		frames = rc.Config.Render.MaxFrames
	}
	pageTarget, err := pageURL(env.Project, job.Target)
	if err != nil {
		return err
	}
	interval := time.Second / time.Duration(fps)

	tabCtx, cancelTab := chromedp.NewContext(d.getPool(opts, false).alloc)
	defer cancelTab()
	runCtx, cancelRun := context.WithTimeout(tabCtx, job.Timeout)
	defer cancelRun()

	if err := chromedp.Run(runCtx,
		chromedp.EmulateViewport(int64(job.Width), int64(job.Height)),
		chromedp.Navigate(pageTarget),
		waitLoaded(),
	); err != nil {
		return err
	}
	for i := 0; i < frames; i++ {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-time.After(interval):
		}
		var buf []byte
		if err := chromedp.Run(runCtx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return err
		}
		out := filepath.Join(env.FramesDir, fmt.Sprintf("frame_%04d.png", i))
		if err := os.WriteFile(out, buf, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// pageURL resolves targets: full URLs pass through; anything else is a
// project-relative file path turned into a file:// URL.
func pageURL(project, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("web shot needs a target (a URL or a path relative to the project)")
	}
	if strings.Contains(target, "://") {
		return target, nil
	}
	abs, err := filepath.Abs(filepath.Join(project, filepath.FromSlash(target)))
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("target %q: %w (use a URL or a path relative to the project)", target, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("target %q is a directory (want an HTML file or URL)", target)
	}
	u := &url.URL{Scheme: "file", Path: abs}
	return u.String(), nil
}

func derefEnv(envName, val string) string {
	if val != "" {
		return val
	}
	return os.Getenv(envName)
}
