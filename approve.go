package omniviz

import (
	"fmt"
	"os"
	"path/filepath"
)

// ApproveKey promotes a current capture to the baseline.
func (c *RunContext) ApproveKey(key string) error {
	cur := filepath.Join(c.Output, "current", key+".png")
	if _, err := os.Stat(cur); err != nil {
		return fmt.Errorf("no current capture for %q — run `omniviz capture` first", key)
	}
	dst := filepath.Join(c.Baselines, filepath.FromSlash(key)+".png")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return CopyFile(cur, dst)
}

// ApproveKeys promotes several captures and refreshes the on-disk report so
// the review UI reflects the new baselines.
func (c *RunContext) ApproveKeys(keys []string) ([]string, error) {
	var approved []string
	for _, k := range keys {
		if err := c.ApproveKey(k); err != nil {
			return approved, err
		}
		approved = append(approved, k)
	}
	c.refreshReportAfterApprove(approved)
	return approved, nil
}

func (c *RunContext) refreshReportAfterApprove(approved []string) {
	rp := filepath.Join(c.Output, "report.json")
	r, err := LoadReport(rp)
	if err != nil {
		return
	}
	set := map[string]bool{}
	for _, k := range approved {
		set[k] = true
	}
	changed := false
	for i := range r.Shots {
		if set[r.Shots[i].Key] && r.Shots[i].Status != "pass" {
			r.Shots[i].Status = "pass"
			r.Shots[i].MismatchPixels = 0
			r.Shots[i].MismatchRatio = 0
			r.Shots[i].BaselineExists = true
			changed = true
		}
	}
	if changed {
		r.Finish()
		SaveReport(rp, r)
	}
}

// PendingKeys lists the fail/new shot keys from the last report.
func (c *RunContext) PendingKeys() ([]string, error) {
	r, err := LoadReport(filepath.Join(c.Output, "report.json"))
	if err != nil {
		return nil, fmt.Errorf("no report at %s — run `omniviz test` first", filepath.Join(c.Output, "report.json"))
	}
	var keys []string
	for _, s := range r.Shots {
		if s.Status == "fail" || s.Status == "new" {
			keys = append(keys, s.Key)
		}
	}
	return keys, nil
}
