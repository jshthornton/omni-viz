package omniviz

import (
	"encoding/json"
	"fmt"
	"image"
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
	Target         string  `json:"target"`
	Status         string  `json:"status"`
	Threshold      float64 `json:"threshold"`
	MaxChanged     float64 `json:"max_changed"`
	MaxDiffRatio   float64 `json:"max_diff_ratio,omitempty"`
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

	ignore []image.Rectangle `json:"-"` // set by the runner, applied by CompareShot
}

type Report struct {
	GeneratedAt string            `json:"generated_at"`
	Project     string            `json:"project"`
	Versions    map[string]string `json:"versions,omitempty"` // driver name → version label
	Commit      string            `json:"commit"`
	Summary     map[string]int    `json:"summary"`
	Shots       []ShotResult      `json:"shots"`
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

func SaveReport(path string, r *Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func LoadReport(path string) (*Report, error) {
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

func GitCommit(project string) string {
	out, err := exec.Command("git", "-C", project, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CompareShot diffs a captured shot against its baseline and updates the
// result in place: pass / new / size / fail (+ diff heatmap on disk).
func (c *RunContext) CompareShot(r *ShotResult) {
	base := filepath.Join(c.Baselines, filepath.FromSlash(r.Key)+".png")
	cur := filepath.Join(c.Output, "current", r.Key+".png")
	if _, err := os.Stat(base); err != nil {
		r.Status = "new"
		return
	}
	r.BaselineExists = true
	res, diffImg, err := ComparePNG(base, cur, r.Threshold, r.ignore...)
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
	// pass: no perceptual misses at all, or a tolerated small ratio of them
	// (max_diff_ratio, default 0 = strict) — plus the changed-area budget
	if res.DiffPixels == 0 && res.ChangedRatio <= r.MaxChanged {
		r.Status = "pass"
		return
	}
	if r.MaxDiffRatio > 0 && res.Ratio <= r.MaxDiffRatio && res.ChangedRatio <= r.MaxChanged {
		r.Status = "pass"
		return
	}
	r.Status = "fail"
	if diffImg != nil {
		if err := SavePNG(filepath.Join(c.Output, "diff", r.Key+".png"), diffImg); err != nil {
			fmt.Fprintf(os.Stderr, "omniviz: warning: save diff %s: %v\n", r.Key, err)
		}
	}
}

// Rediff normalizes settings from an older report and re-compares every
// shot (the `compare` command). Ignore regions come from the current config,
// matched by shot key.
func (c *RunContext) Rediff(shots []ShotResult) {
	regions := map[string][]image.Rectangle{}
	for _, job := range c.Config.Jobs() {
		if job.Key != "" {
			regions[job.Key] = job.IgnoreRegions
		}
	}
	for i := range shots {
		if shots[i].Threshold <= 0 {
			shots[i].Threshold = c.Config.Defaults.Threshold
		}
		if shots[i].MaxChanged <= 0 {
			shots[i].MaxChanged = c.Config.Defaults.MaxChanged
		}
		if shots[i].MaxDiffRatio <= 0 {
			shots[i].MaxDiffRatio = c.Config.Defaults.MaxDiffRatio
		}
		shots[i].ignore = regions[shots[i].Key]
		c.CompareShot(&shots[i])
	}
}

func PrintSummary(r *Report) {
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

func ExitError(r *Report, failOnNew bool) error {
	bad := r.Summary["fail"] + r.Summary["size"] + r.Summary["error"] + r.Summary["missing"]
	if bad > 0 {
		return fmt.Errorf("%d shot(s) need attention — run `omniviz review` to inspect and approve", bad)
	}
	if failOnNew && r.Summary["new"] > 0 {
		return fmt.Errorf("%d new shot(s) not yet approved (--fail-on-new)", r.Summary["new"])
	}
	return nil
}
