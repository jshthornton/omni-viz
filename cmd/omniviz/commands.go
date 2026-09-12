package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	omniviz "github.com/jshthornton/omni-viz"
	"github.com/jshthornton/omni-viz/review"
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
	var only, skip, set sliceFlags
	fs.Var(&only, "only", "run jobs whose name/target contains this substring (repeatable)")
	fs.Var(&skip, "skip", "skip jobs whose name/target contains this substring (repeatable)")
	fs.Var(&set, "set", "driver-specific override, e.g. godot SECTION/KEY=VALUE (repeatable)")
	noRecord := fs.Bool("no-record", false, "disable recordings for this run")
	record := fs.Bool("record", false, "force recordings on for this run")
	failOnNew := fs.Bool("fail-on-new", false, "exit non-zero when new shots are unapproved")
	parallel := fs.Int("parallel", 0, "max jobs running concurrently (0 = defaults.parallel in omniviz.toml)")
	fs.Parse(argv)
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	jobs := omniviz.FilterJobs(c.Config.Jobs(), only, skip)
	if len(jobs) == 0 {
		return fmt.Errorf("no [[shot]] entries match the filters")
	}
	if err := c.CheckDrivers(jobs); err != nil {
		return err
	}
	workers := *parallel
	if workers == 0 {
		workers = c.Config.Defaults.Parallel
	}
	if workers < 1 {
		workers = 1
	}

	fmt.Printf("omniviz %s: %d job(s) · %s · workers %d · %s\n",
		mode, len(jobs), strings.Join(c.VersionLabels(), " · "), workers, c.Project)
	recordMode := 0
	if *noRecord {
		recordMode = 2
	} else if *record {
		recordMode = 1
	}

	report := &omniviz.Report{Project: c.Project, Versions: c.Versions(), Commit: omniviz.GitCommit(c.Project)}
	var mu sync.Mutex
	appendShots := func(results []omniviz.ShotResult) {
		mu.Lock()
		defer mu.Unlock()
		report.Shots = append(report.Shots, results...)
	}
	printResults := func(idx, total int, job omniviz.Job, results []omniviz.ShotResult) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("[%d/%d] %s", idx, total, job.ID)
		for _, r := range results {
			key := r.Key
			if key == "" {
				key = r.Job
			}
			fmt.Printf("  %s → %s", key, r.Status)
		}
		fmt.Println()
	}

	total := len(jobs)
	runJobAt := func(idx int, job omniviz.Job, slot int) {
		fmt.Printf("[%d/%d] %s · running\n", idx, total, job.ID)
		results := c.RunJob(job, omniviz.RunOptions{Set: set, RecordMode: recordMode, Compare: compare}, slot)
		printResults(idx, total, job, results)
		appendShots(results)
	}

	// Scheduling: batches of up to `workers` parallel jobs; a serial job
	// runs exclusively (nothing else alongside).
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
			go func(slot int, j omniviz.Job, idx int) {
				defer wg.Done()
				runJobAt(idx, j, slot)
			}(slot, j, i+slot+1)
		}
		wg.Wait()
		i = batchEnd
	}

	report.Finish()
	if err := omniviz.SaveReport(filepath.Join(c.Output, "report.json"), report); err != nil {
		return err
	}
	omniviz.PrintSummary(report)
	return omniviz.ExitError(report, *failOnNew)
}

func cmdCompare(argv []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	fs.Parse(argv)
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	var shots []omniviz.ShotResult
	if r, err := omniviz.LoadReport(filepath.Join(c.Output, "report.json")); err == nil && len(r.Shots) > 0 {
		shots = r.Shots
	} else {
		entries, err := os.ReadDir(filepath.Join(c.Output, "current"))
		if err != nil {
			return fmt.Errorf("no current captures — run `omniviz capture` first")
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
				continue
			}
			shots = append(shots, omniviz.ShotResult{
				Key: strings.TrimSuffix(e.Name(), ".png"),
			})
		}
	}
	if len(shots) == 0 {
		return fmt.Errorf("no current captures — run `omniviz capture` first")
	}
	report := &omniviz.Report{Project: c.Project, Versions: c.Versions(), Commit: omniviz.GitCommit(c.Project)}
	c.Rediff(shots)
	report.Shots = shots
	report.Finish()
	if err := omniviz.SaveReport(filepath.Join(c.Output, "report.json"), report); err != nil {
		return err
	}
	omniviz.PrintSummary(report)
	return omniviz.ExitError(report, false)
}

func cmdShots(argv []string) error {
	fs := flag.NewFlagSet("shots", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	fs.Parse(argv)
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	for _, job := range c.Config.Jobs() {
		fmt.Printf("%-26s %-52s %-8s %4dx%-4d record=%-5v threshold=%.2f", job.ID, job.Target, job.Driver, job.Width, job.Height, job.Record, job.Threshold)
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
	filepath.Walk(c.Baselines, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".png") {
			count++
		}
		return nil
	})
	rel, err := filepath.Rel(c.Project, c.Baselines)
	if err != nil {
		rel = c.Baselines
	}
	fmt.Printf("%d baseline image(s) in %s\n", count, rel)
	return nil
}

func cmdReview(argv []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	port := fs.Int("port", 8420, "port to listen on")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	fs.Parse(argv)
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	return review.Serve(c, *port, *noOpen)
}
