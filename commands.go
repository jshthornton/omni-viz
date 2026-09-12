package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func cmdTest(argv []string) error    { return runTest(argv, "test", true) }
func cmdCapture(argv []string) error { return runTest(argv, "capture", false) }

// sliceFlags collects repeatable string flags.
type sliceFlags []string

func (s *sliceFlags) String() string { return strings.Join(*s, ",") }
func (s *sliceFlags) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func runTest(argv []string, mode string, compare bool) error {
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	godot := fs.String("godot", "", "godot binary path (else $OMNIVIZ_GODOT, config, or PATH)")
	var only, skip, set sliceFlags
	fs.Var(&only, "only", "run jobs whose name/scene contains this substring (repeatable)")
	fs.Var(&skip, "skip", "skip jobs whose name/scene contains this substring (repeatable)")
	fs.Var(&set, "set", "extra project setting override SECTION/KEY=VALUE (repeatable)")
	noRecord := fs.Bool("no-record", false, "disable recordings for this run")
	record := fs.Bool("record", false, "force recordings on for this run")
	failOnNew := fs.Bool("fail-on-new", false, "exit non-zero when new shots are unapproved")
	parallel := fs.Int("parallel", 0, "max jobs running concurrently (0 = defaults.parallel in omniviz.toml)")
	fs.Parse(argv)
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	if err := c.ensureGodot(*godot); err != nil {
		return err
	}
	jobs := filterJobs(c.cfg.Jobs(), only, skip)
	if len(jobs) == 0 {
		return fmt.Errorf("no [[shot]] entries match the filters")
	}
	workers := *parallel
	if workers == 0 {
		workers = c.cfg.Defaults.Parallel
	}
	if workers < 1 {
		workers = 1
	}

	fmt.Printf("omniviz %s: %d job(s) · godot %s · workers %d · %s\n", mode, len(jobs), c.godotVer, workers, c.project)
	recordMode := 0
	if *noRecord {
		recordMode = 2
	} else if *record {
		recordMode = 1
	}

	report := &Report{Project: c.project, GodotVersion: c.godotVer, Commit: gitCommit(c.project)}
	keys := newKeySet()
	var mu sync.Mutex
	appendShots := func(results []ShotResult) {
		mu.Lock()
		defer mu.Unlock()
		report.Shots = append(report.Shots, results...)
	}
	printResults := func(idx, total int, job Job, results []ShotResult) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("[%d/%d] %s", idx, total, job.ID)
		for _, r := range results {
			fmt.Printf("  %s → %s", displayKey(r), r.Status)
		}
		fmt.Println()
	}

	total := len(jobs)
	runJobAt := func(idx int, job Job, slot int) {
		fmt.Printf("[%d/%d] %s · running\n", idx, total, job.ID)
		results := runJob(c, job, runOpts{set: set, recordMode: recordMode, compare: compare, slot: slot}, keys)
		printResults(idx, total, job, results)
		appendShots(results)
	}

	i := 0
	for i < total {
		job := jobs[i]
		idx := i + 1
		if job.Serial || workers <= 1 {
			runJobAt(idx, job, 0)
			i++
			continue
		}
		batchEnd := i
		for batchEnd < total && batchEnd-i < workers && !jobs[batchEnd].Serial {
			batchEnd++
		}
		var wg sync.WaitGroup
		for slot, j := range jobs[i:batchEnd] {
			wg.Add(1)
			go func(slot int, j Job, idx int) {
				defer wg.Done()
				runJobAt(idx, j, slot)
			}(slot, j, i+slot+1)
		}
		wg.Wait()
		i = batchEnd
	}

	report.Finish()
	if err := saveReport(filepath.Join(c.output, "report.json"), report); err != nil {
		return err
	}
	printSummary(report)
	return exitError(report, *failOnNew)
}

func displayKey(r ShotResult) string {
	if r.Key != "" {
		return r.Key
	}
	return r.Job
}

func cmdCompare(argv []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	fs.Parse(argv)
	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	var shots []ShotResult
	if r, err := loadReport(filepath.Join(c.output, "report.json")); err == nil && len(r.Shots) > 0 {
		shots = r.Shots
		// normalize settings written by older runs
		for i := range shots {
			if shots[i].Threshold <= 0 {
				shots[i].Threshold = c.cfg.Defaults.Threshold
			}
			if shots[i].MaxChanged <= 0 {
				shots[i].MaxChanged = c.cfg.Defaults.MaxChanged
			}
		}
	} else {
		entries, err := os.ReadDir(filepath.Join(c.output, "current"))
		if err != nil {
			return fmt.Errorf("no current captures — run `omniviz capture` first")
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
				continue
			}
			shots = append(shots, ShotResult{
				Key:        strings.TrimSuffix(e.Name(), ".png"),
				Threshold:  c.cfg.Defaults.Threshold,
				MaxChanged: c.cfg.Defaults.MaxChanged,
			})
		}
	}
	if len(shots) == 0 {
		return fmt.Errorf("no current captures — run `omniviz capture` first")
	}
	report := &Report{Project: c.project, GodotVersion: c.godotVer, Commit: gitCommit(c.project)}
	for i := range shots {
		compareShot(c, &shots[i])
	}
	report.Shots = shots
	report.Finish()
	if err := saveReport(filepath.Join(c.output, "report.json"), report); err != nil {
		return err
	}
	printSummary(report)
	return exitError(report, false)
}

func cmdShots(argv []string) error {
	fs := flag.NewFlagSet("shots", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	fs.Parse(argv)
	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	for _, job := range c.cfg.Jobs() {
		fmt.Printf("%-26s %-52s %4dx%-4d record=%-5v threshold=%.2f", job.ID, job.Scene, job.Width, job.Height, job.Record, job.Threshold)
		if job.Serial {
			fmt.Print("  serial")
		}
		if job.QuitAfter > 0 {
			fmt.Printf("  quit_after=%d", job.QuitAfter)
		}
		fmt.Println()
		for _, p := range job.Paths {
			fmt.Printf("    paths: %s\n", p)
		}
	}
	count := 0
	filepath.Walk(c.baselines, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".png") {
			count++
		}
		return nil
	})
	rel, err := filepath.Rel(c.project, c.baselines)
	if err != nil {
		rel = c.baselines
	}
	fmt.Printf("%d baseline image(s) in %s\n", count, rel)
	return nil
}
