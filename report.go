package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ShotResult struct {
	Key            string  `json:"key"`
	Job            string  `json:"job"`
	Scene          string  `json:"scene"`
	Status         string  `json:"status"`
	Threshold      float64 `json:"threshold"`
	MaxChanged     float64 `json:"max_changed"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	MismatchPixels int     `json:"mismatch_pixels"`
	MismatchRatio  float64 `json:"mismatch_ratio"`
	ChangedPixels  int     `json:"changed_pixels"`
	ChangedRatio   float64 `json:"changed_ratio"`
	Recording      bool    `json:"recording"`
	Frames         int     `json:"frames"`
	DurationMs     int64   `json:"duration_ms"`
	BaselineExists bool    `json:"baseline_exists"`
	Error          string  `json:"error,omitempty"`
	Log            string  `json:"log,omitempty"`
}

type Report struct {
	GeneratedAt  string         `json:"generated_at"`
	Project      string         `json:"project"`
	GodotVersion string         `json:"godot_version"`
	Commit       string         `json:"commit"`
	Summary      map[string]int `json:"summary"`
	Shots        []ShotResult   `json:"shots"`
}

var statusOrder = []string{"pass", "fail", "new", "size", "missing", "error", "captured"}

func (r *Report) Finish() {
	sort.SliceStable(r.Shots, func(a, b int) bool {
		if r.Shots[a].Job != r.Shots[b].Job {
			return r.Shots[a].Job < r.Shots[b].Job
		}
		return r.Shots[a].Key < r.Shots[b].Key
	})
	r.GeneratedAt = time.Now().Format(time.RFC3339)
	r.Summary = map[string]int{}
	for _, s := range r.Shots {
		r.Summary[s.Status]++
	}
	r.Summary["total"] = len(r.Shots)
}

func saveReport(path string, r *Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func loadReport(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func gitCommit(project string) string {
	out, err := exec.Command("git", "-C", project, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func compareShot(c *runCtx, r *ShotResult) {
	base := filepath.Join(c.baselines, filepath.FromSlash(r.Key)+".png")
	cur := filepath.Join(c.output, "current", r.Key+".png")
	if _, err := os.Stat(base); err != nil {
		r.Status = "new"
		return
	}
	r.BaselineExists = true
	res, diffImg, err := ComparePNG(base, cur, r.Threshold)
	if err != nil {
		r.Status = "error"
		r.Error = err.Error()
		return
	}
	if res.SizeMismatch {
		r.Status = "size"
		r.Error = fmt.Sprintf("baseline is %dx%d, current is %dx%d", res.BaseWidth, res.BaseHeight, res.CurWidth, res.CurHeight)
		return
	}
	r.MismatchPixels = res.DiffPixels
	r.MismatchRatio = res.Ratio
	r.ChangedPixels = res.ChangedPixels
	r.ChangedRatio = res.ChangedRatio
	if res.DiffPixels == 0 && res.ChangedRatio <= r.MaxChanged {
		r.Status = "pass"
		return
	}
	r.Status = "fail"
	if diffImg != nil {
		if err := savePNG(filepath.Join(c.output, "diff", r.Key+".png"), diffImg); err != nil {
			fmt.Fprintf(os.Stderr, "omniviz: warning: save diff %s: %v\n", r.Key, err)
		}
	}
}

func printSummary(r *Report) {
	var parts []string
	for _, st := range statusOrder {
		if n := r.Summary[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", strings.ToUpper(st), n))
		}
	}
	fmt.Printf("omniviz: %s (%d shots)\n", strings.Join(parts, " · "), r.Summary["total"])
	for _, s := range r.Shots {
		switch s.Status {
		case "fail":
			fmt.Printf("  FAIL  %-40s %.1f%% of pixels changed (budget %.1f%%) · %d px beyond threshold\n", s.Key, s.ChangedRatio*100, s.MaxChanged*100, s.MismatchPixels)
		case "new":
			fmt.Printf("  NEW   %-40s no baseline yet — `omniviz approve %s` adopts it\n", s.Key, s.Key)
		case "size":
			fmt.Printf("  SIZE  %-40s %s\n", s.Key, s.Error)
		case "error":
			fmt.Printf("  ERROR %-40s %s\n", s.Key, s.Error)
		}
	}
}

func exitError(r *Report, failOnNew bool) error {
	bad := r.Summary["fail"] + r.Summary["size"] + r.Summary["error"] + r.Summary["missing"]
	if bad > 0 {
		return fmt.Errorf("%d shot(s) need attention — run `omniviz review` to inspect and approve", bad)
	}
	if failOnNew && r.Summary["new"] > 0 {
		return fmt.Errorf("%d new shot(s) not yet approved (--fail-on-new)", r.Summary["new"])
	}
	return nil
}
