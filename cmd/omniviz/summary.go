package main

import (
	"flag"
	"fmt"
	"path/filepath"

	omniviz "github.com/jshthornton/omni-viz"
)

// cmdSummary prints the last run's report — plain table, or markdown for
// PR comments and $GITHUB_STEP_SUMMARY.
//
//	omniviz summary --markdown >> "$GITHUB_STEP_SUMMARY"
func cmdSummary(argv []string) error {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	markdown := fs.Bool("markdown", false, "render as markdown (PR comments, step summaries)")
	fs.Parse(argv)
	c, err := omniviz.LoadContext(*project)
	if err != nil {
		return err
	}
	r, err := omniviz.LoadReport(filepath.Join(c.Output, "report.json"))
	if err != nil {
		return fmt.Errorf("no report at %s — run `omniviz test` first", filepath.Join(c.Output, "report.json"))
	}
	if *markdown {
		fmt.Print(omniviz.MarkdownSummary(r))
		return nil
	}
	omniviz.PrintSummary(r)
	return nil
}
