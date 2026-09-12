package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func approveKey(c *runCtx, key string) error {
	cur := filepath.Join(c.output, "current", key+".png")
	if _, err := os.Stat(cur); err != nil {
		return fmt.Errorf("no current capture for %q — run `omniviz capture` first", key)
	}
	dst := filepath.Join(c.baselines, filepath.FromSlash(key)+".png")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(cur, dst)
}

func approveKeys(c *runCtx, keys []string) ([]string, error) {
	var approved []string
	for _, k := range keys {
		if err := approveKey(c, k); err != nil {
			return approved, err
		}
		approved = append(approved, k)
	}
	refreshReportAfterApprove(c, approved)
	return approved, nil
}

func refreshReportAfterApprove(c *runCtx, approved []string) {
	rp := filepath.Join(c.output, "report.json")
	r, err := loadReport(rp)
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
		saveReport(rp, r)
	}
}

func pendingKeys(c *runCtx) ([]string, error) {
	r, err := loadReport(filepath.Join(c.output, "report.json"))
	if err != nil {
		return nil, fmt.Errorf("no report at %s — run `omniviz test` first", filepath.Join(c.output, "report.json"))
	}
	var keys []string
	for _, s := range r.Shots {
		if s.Status == "fail" || s.Status == "new" {
			keys = append(keys, s.Key)
		}
	}
	return keys, nil
}

func cmdApprove(argv []string) error {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	all := fs.Bool("all", false, "approve every failed and new shot from the last report")
	fs.Parse(argv)
	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	keys := fs.Args()
	if *all {
		keys, err = pendingKeys(c)
		if err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		fmt.Println("omniviz: nothing to approve")
		return nil
	}
	approved, err := approveKeys(c, keys)
	for _, k := range approved {
		fmt.Printf("  approved %s\n", k)
	}
	if err != nil {
		return err
	}
	fmt.Printf("omniviz: %d baseline(s) updated\n", len(approved))
	return nil
}
