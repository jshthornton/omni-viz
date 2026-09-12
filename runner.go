package omniviz

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunOptions are the per-run knobs the CLI flags control.
type RunOptions struct {
	Set        []string // --set overrides, passed through to drivers
	RecordMode int      // 0 default · 1 force on (--record) · 2 force off (--no-record)
	Compare    bool     // diff against baselines after capture (test vs capture)
}

// RunJob performs one job: capture via the job's driver, collect shot
// images, claim keys and (when opts.Compare) diff against baselines.
// Errors travel as error-status ShotResults; RunJob itself never fails.
func (c *RunContext) RunJob(job Job, opts RunOptions, slot int) []ShotResult {
	drv, err := GetDriver(job.Driver)
	if err != nil {
		return jobErrorResult(job, err.Error(), job.Record, 0, 0)
	}

	record := job.Record
	if opts.RecordMode == 1 {
		record = true
	} else if opts.RecordMode == 2 {
		record = false
	}
	framesKept := record

	if len(job.Paths) > 0 {
		// cooperative mode: clear the previous run's captures so stale
		// files can't masquerade as fresh ones, and pre-create dirs
		if previous, err := GlobFiles(c.Project, job.Paths); err == nil {
			for _, f := range previous {
				os.Remove(f)
			}
		}
		EnsureGlobDirs(c.Project, job.Paths)
	}
	framesDir := filepath.Join(c.Output, "frames", job.ID)
	os.RemoveAll(framesDir)
	if record {
		os.MkdirAll(framesDir, 0o755)
	}
	scratchDir := filepath.Join(c.Output, "scratch", job.ID)
	os.RemoveAll(scratchDir)
	os.MkdirAll(scratchDir, 0o755)

	env := CaptureEnv{
		Project:      c.Project,
		Env:          jobEnviron(c, job),
		LogPath:      filepath.Join(c.Output, "logs", job.ID+".log"),
		FramesDir:    framesDir,
		ScratchDir:   scratchDir,
		Width:        job.Width,
		Height:       job.Height,
		Record:       record,
		Slot:         slot,
		SetOverrides: opts.Set,
	}

	start := time.Now()
	res, capErr := drv.Capture(context.Background(), c, job, env)
	duration := time.Since(start)
	if capErr != nil {
		return jobErrorResult(job, capErr.Error(), framesKept, 0, duration.Milliseconds())
	}
	if os.Getenv("OMNIVIZ_DEBUG") != "" {
		fmt.Printf("[omniviz-debug] job=%s driver=%s record=%v framesKept=%v files=%d\n",
			job.ID, job.Driver, record, framesKept, len(res.ShotFiles))
	}

	frames := 0
	if record {
		frames = DecimateFrames(framesDir, c.Config.Render.MaxFrames)
	}

	files := res.ShotFiles
	if res.Error != "" {
		return jobErrorResult(job, res.Error, framesKept, frames, duration.Milliseconds())
	}
	if len(files) == 0 {
		return jobErrorResult(job, "capture produced no images", framesKept, frames, duration.Milliseconds())
	}
	if !job.Multi && len(files) > 1 {
		return jobErrorResult(job, fmt.Sprintf("capture produced %d files; give the shot a unique paths glob or drop name= for multi mode", len(files)), framesKept, frames, duration.Milliseconds())
	}

	var results []ShotResult
	for _, f := range files {
		key := job.Key
		if job.Multi {
			stem := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			k, err := SanitizeKey(stem)
			if err != nil {
				results = append(results, ShotResult{Job: job.ID, Target: job.Target, Status: "error", Error: fmt.Sprintf("shot file %s: %v", filepath.Base(f), err), Recording: framesKept, Frames: frames})
				continue
			}
			key = k
		}
		if !c.claimKey(key) {
			results = append(results, ShotResult{Key: key, Job: job.ID, Target: job.Target, Status: "error", Error: "duplicate shot key", Recording: framesKept, Frames: frames})
			continue
		}
		cur := filepath.Join(c.Output, "current", key+".png")
		if err := CopyFile(f, cur); err != nil {
			results = append(results, ShotResult{Key: key, Job: job.ID, Target: job.Target, Status: "error", Error: fmt.Sprintf("copy capture: %v", err), Recording: framesKept, Frames: frames})
			continue
		}
		dims, derr := PNGDims(cur)
		if derr != nil {
			results = append(results, ShotResult{Key: key, Job: job.ID, Target: job.Target, Status: "error", Error: fmt.Sprintf("invalid png: %v", derr), Recording: framesKept, Frames: frames})
			continue
		}
		logTail := res.Log
		if logTail == "" {
			logTail = TailFile(env.LogPath, 4096)
		}
		r := ShotResult{
			Key:        key,
			Job:        job.ID,
			Target:     job.Target,
			Status:     "captured",
			Threshold:  job.Threshold,
			MaxChanged: job.MaxChanged,
			Width:      dims.X,
			Height:     dims.Y,
			Recording:  framesKept,
			Frames:     frames,
			DurationMs: duration.Milliseconds(),
			Log:        logTail,
		}
		if opts.Compare {
			c.CompareShot(&r)
		}
		results = append(results, r)
	}
	if !framesKept {
		// recordings are opt-in; drivers that must record to produce a shot
		// (godot generic mode) had their frames discarded here, like gdviz
		os.RemoveAll(framesDir)
	}
	os.RemoveAll(scratchDir)
	return results
}

func jobErrorResult(job Job, msg string, record bool, frames int, durationMs int64) []ShotResult {
	return []ShotResult{{
		Key:        job.Key,
		Job:        job.ID,
		Target:     job.Target,
		Status:     "error",
		Error:      msg,
		Threshold:  job.Threshold,
		MaxChanged: job.MaxChanged,
		Recording:  record,
		Frames:     frames,
		DurationMs: durationMs,
	}}
}

// jobEnviron is the process environment a capture runs with.
func jobEnviron(c *RunContext, job Job) []string {
	env := os.Environ()
	env = append(env, c.Config.Defaults.Env...)
	env = append(env, job.Env...)
	return env
}

// FilterJobs applies --only/--skip substring filters over job id + target.
func FilterJobs(jobs []Job, only, skip []string) []Job {
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
		hay := j.ID + " " + j.Target
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
