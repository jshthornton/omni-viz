package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type runCtx struct {
	project   string
	cfg       *Config
	godotBin  string
	godotVer  string
	output    string
	baselines string
}

func loadCtx(projectFlag string) (*runCtx, error) {
	project, err := filepath.Abs(projectFlag)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(project, "project.godot")); err != nil {
		return nil, fmt.Errorf("%s is not a Godot project (no project.godot)", project)
	}
	cfg, err := LoadConfig(filepath.Join(project, "omniviz.toml"))
	if err != nil {
		return nil, err
	}
	output := filepath.Join(project, filepath.FromSlash(cfg.OutputDir))
	baselines := filepath.Join(project, filepath.FromSlash(cfg.BaselineDir))
	for _, d := range []string{
		output,
		filepath.Join(output, "current"),
		filepath.Join(output, "diff"),
		filepath.Join(output, "logs"),
		baselines,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	c := &runCtx{
		project:   project,
		cfg:       cfg,
		output:    output,
		baselines: baselines,
	}
	if g := resolveGodot("", cfg.Godot); g != "" {
		c.godotBin = g
		c.godotVer = probeGodotVersion(g)
	}
	return c, nil
}

// ensureGodot resolves the Godot binary (flag > $OMNIVIZ_GODOT > config > PATH)
// and errors when a capture command cannot run without one.
func (c *runCtx) ensureGodot(flagVal string) error {
	if g := resolveGodot(flagVal, c.cfg.Godot); g != "" {
		c.godotBin = g
		c.godotVer = probeGodotVersion(g)
		return nil
	}
	return fmt.Errorf("godot binary not found (use --godot, $OMNIVIZ_GODOT, or godot= in omniviz.toml)")
}

func resolveGodot(flagVal, cfgVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("OMNIVIZ_GODOT"); v != "" {
		return v
	}
	if cfgVal != "" {
		return cfgVal
	}
	if p, err := exec.LookPath("godot"); err == nil {
		return p
	}
	return ""
}

func probeGodotVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

type runOpts struct {
	only       []string
	skip       []string
	set        []string
	recordMode int
	compare    bool
	slot       int
}

func filterJobs(jobs []Job, only, skip []string) []Job {
	contains := func(pats []string, hay string) bool {
		for _, p := range pats {
			if strings.Contains(strings.ToLower(hay), strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
	var out []Job
	for _, j := range jobs {
		hay := j.ID + " " + j.Scene
		if len(only) > 0 && !contains(only, hay) {
			continue
		}
		if contains(skip, hay) {
			continue
		}
		out = append(out, j)
	}
	return out
}

// keySet is the thread-safe registry of shot keys already claimed this run.
type keySet struct {
	mu sync.Mutex
	m  map[string]bool
}

func newKeySet() *keySet { return &keySet{m: map[string]bool{}} }

func (k *keySet) claim(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m[key] {
		return false
	}
	k.m[key] = true
	return true
}

func runJob(c *runCtx, job Job, opts runOpts, keys *keySet) []ShotResult {
	record := job.Record
	if opts.recordMode == 1 {
		record = true
	} else if opts.recordMode == 2 {
		record = false
	}
	framesKept := record
	generic := len(job.Paths) == 0
	if generic {
		// generic mode: the screenshot IS the final recorded frame, so the
		// movie writer must run even when the recording is not kept
		record = true
	}

	if len(job.Paths) > 0 {
		if previous, err := globProject(c.project, job.Paths); err == nil {
			for _, f := range previous {
				os.Remove(f)
			}
		}
		ensureShotDirs(c.project, job.Paths)
	}
	framesDir := filepath.Join(c.output, "frames", job.ID)
	os.RemoveAll(framesDir)
	if record {
		os.MkdirAll(framesDir, 0o755)
	}

	overlay, err := buildOverlay(c.project, job.Width, job.Height, opts.set)
	if err != nil {
		return jobErrorResult(job, fmt.Sprintf("build overlay: %v", err), record, 0, 0)
	}
	defer os.RemoveAll(overlay)

	args := godotArgs(c, job, record, framesDir, overlay, opts.slot)
	logPath := filepath.Join(c.output, "logs", job.ID+".log")
	start := time.Now()
	tail, runErr := runGodot(c.godotBin, args, jobEnv(c, job), job.Timeout, logPath)
	duration := time.Since(start)

	frames := 0
	if record {
		frames = decimateFrames(framesDir, c.cfg.Render.MaxFrames)
	}
	if os.Getenv("OMNIVIZ_DEBUG") != "" {
		fmt.Printf("[omniviz-debug] job=%s record=%v framesKept=%v generic=%v frames=%d dirFiles=%d\n",
			job.ID, record, framesKept, generic, frames, len(listFrames(framesDir)))
	}

	var files []string
	var gerr error
	if generic {
		if list := listFrames(framesDir); len(list) > 0 {
			files = list[len(list)-1:]
		}
	} else {
		files, gerr = globProject(c.project, job.Paths)
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
		return jobErrorResult(job, msg, record && framesKept, frames, duration.Milliseconds())
	}
	if !job.Multi && len(files) > 1 {
		return jobErrorResult(job, fmt.Sprintf("patterns matched %d files; give shot a unique paths glob or drop name= for multi mode", len(files)), record, frames, duration.Milliseconds())
	}

	var results []ShotResult
	for _, f := range files {
		key := job.Key
		if job.Multi {
			stem := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			k, err := SanitizeKey(stem)
			if err != nil {
				results = append(results, ShotResult{Job: job.ID, Scene: job.Scene, Status: "error", Error: fmt.Sprintf("shot file %s: %v", filepath.Base(f), err), Recording: record, Frames: frames})
				continue
			}
			key = k
		}
		if !keys.claim(key) {
			results = append(results, ShotResult{Key: key, Job: job.ID, Scene: job.Scene, Status: "error", Error: "duplicate shot key", Recording: record, Frames: frames})
			continue
		}
		cur := filepath.Join(c.output, "current", key+".png")
		if err := copyFile(f, cur); err != nil {
			results = append(results, ShotResult{Key: key, Job: job.ID, Scene: job.Scene, Status: "error", Error: fmt.Sprintf("copy capture: %v", err), Recording: record, Frames: frames})
			continue
		}
		dims, derr := pngDims(cur)
		if derr != nil {
			results = append(results, ShotResult{Key: key, Job: job.ID, Scene: job.Scene, Status: "error", Error: fmt.Sprintf("invalid png: %v", derr), Recording: record, Frames: frames})
			continue
		}
		r := ShotResult{
			Key:        key,
			Job:        job.ID,
			Scene:      job.Scene,
			Status:     "captured",
			Threshold:  job.Threshold,
			MaxChanged: job.MaxChanged,
			Width:      dims.X,
			Height:     dims.Y,
			Recording:  framesKept,
			Frames:     frames,
			DurationMs: duration.Milliseconds(),
			Log:        tail,
		}
		if opts.compare {
			compareShot(c, &r)
		}
		results = append(results, r)
	}
	if !framesKept {
		// recordings are opt-in; generic shots only needed the last frame
		os.RemoveAll(framesDir)
		frames = 0
	}
	return results
}

func jobErrorResult(job Job, msg string, record bool, frames int, durationMs int64) []ShotResult {
	return []ShotResult{{
		Key:        job.Key,
		Job:        job.ID,
		Scene:      job.Scene,
		Status:     "error",
		Error:      msg,
		Threshold:  job.Threshold,
		MaxChanged: job.MaxChanged,
		Recording:  record,
		Frames:     frames,
		DurationMs: durationMs,
	}}
}

func godotArgs(c *runCtx, job Job, record bool, framesDir, overlay string, slot int) []string {
	args := []string{"--path", overlay}
	if runtime.GOOS == "linux" {
		args = append(args, "--display-driver", c.cfg.DisplayDriver)
	}
	// Off-screen: captures must never overlay the desktop or steal focus
	// (the machine doubles as a gaming rig). no_focus=true comes in via the
	// overlay's override.cfg; vsync is disabled by the visual-test window
	// path so an unmapped/occluded window cannot presentation-stall.
	args = append(args,
		"--audio-driver", "Dummy",
		"--windowed",
		"--resolution", fmt.Sprintf("%dx%d", job.Width, job.Height),
		"--position", "-30000,-30000",
		"--single-window",
	)
	if c.cfg.Render.Method != "" {
		args = append(args, "--rendering-method", c.cfg.Render.Method)
	}
	if record {
		args = append(args,
			"--write-movie", filepath.Join(framesDir, "frame.png"),
			"--fixed-fps", strconv.Itoa(c.cfg.Render.FPS),
		)
	}
	if job.QuitAfter > 0 {
		args = append(args, "--quit-after", strconv.Itoa(job.QuitAfter))
	}
	args = append(args, "--scene", job.Scene, "--", "--visual-test-window")
	args = append(args, job.Args...)
	return args
}

func jobEnv(c *runCtx, job Job) []string {
	env := os.Environ()
	env = append(env, c.cfg.Defaults.Env...)
	env = append(env, job.Env...)
	return env
}

func runGodot(bin string, args []string, env []string, timeout time.Duration, logPath string) (string, error) {
	if os.Getenv("OMNIVIZ_DEBUG") != "" {
		fmt.Println("[omniviz-debug] " + bin + " " + strings.Join(args, " "))
	}
	f, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd := exec.Command(bin, args...)
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
	return tailFile(logPath, 4096), runErr
}

func tailFile(path string, max int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > max {
		data = data[len(data)-max:]
	}
	return string(data)
}

func decimateFrames(dir string, max int) int {
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

func globProject(project string, patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range patterns {
		// shell-habit negation: Go character classes negate with ^, not !
		p = strings.ReplaceAll(p, "[!", "[^")
		matches, err := filepath.Glob(filepath.Join(project, filepath.FromSlash(p)))
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

// ensureShotDirs pre-creates the directories each shot glob writes into —
// Godot's Image.save_png does not create missing parent directories.
func ensureShotDirs(project string, patterns []string) {
	for _, p := range patterns {
		dir := filepath.Dir(filepath.FromSlash(p))
		if strings.ContainsAny(dir, "*?[") {
			continue
		}
		os.MkdirAll(filepath.Join(project, dir), 0o755)
	}
}

func copyFile(src, dst string) error {
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
