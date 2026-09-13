package omniviz

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// JUnit XML output — the reporting format GitLab (`reports:junit`) and many
// GitHub test-reporter actions consume natively. One testsuite per job, one
// testcase per shot: fail/size/error/missing become <failure>, new becomes
// <skipped> (nothing to compare against), pass is a plain testcase.

type junitTestsuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit writes the report as JUnit XML at path.
func WriteJUnit(path string, r *Report) error {
	var out junitTestsuites
	out.Name = "omniviz"
	byJob := map[string][]ShotResult{}
	for _, s := range r.Shots {
		byJob[s.Job] = append(byJob[s.Job], s)
	}
	for job, shots := range byJob {
		suite := junitSuite{Name: job, Tests: len(shots)}
		for _, s := range shots {
			sec := fmt.Sprintf("%.3f", float64(s.DurationMs)/1000)
			tc := junitCase{Name: s.Key, ClassName: job, Time: sec}
			switch s.Status {
			case "pass", "captured":
				// plain testcase
			case "new":
				msg := "new shot — no baseline yet; approve to adopt"
				tc.Skipped = &junitSkipped{Message: msg}
			case "fail":
				tc.Failure = &junitFailure{
					Message: fmt.Sprintf("%.1f%% of pixels changed (budget %.1f%%), %d px beyond threshold",
						s.ChangedRatio*100, s.MaxChanged*100, s.MismatchPixels),
					Body: strings.TrimSpace(s.Error),
				}
				suite.Failures++
				out.Failures++
			default: // size, error, missing
				tc.Failure = &junitFailure{Message: s.Status + ": " + s.Error}
				suite.Failures++
				out.Failures++
			}
			suite.Cases = append(suite.Cases, tc)
		}
		out.Suites = append(out.Suites, suite)
		out.Tests += suite.Tests
	}
	data, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// MarkdownSummary renders the report as a GitHub-flavored markdown table —
// for PR comments ($GITHUB_STEP_SUMMARY, gh pr comment) and MR notes.
func MarkdownSummary(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## omniviz — visual regression report\n\n")
	var parts []string
	for _, st := range statusOrder {
		if n := r.Summary[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", strings.ToUpper(st), n))
		}
	}
	if r.Commit != "" {
		fmt.Fprintf(&b, "commit `%s` · ", r.Commit)
	}
	fmt.Fprintf(&b, "%s · %d shot(s)\n\n", strings.Join(parts, " · "), r.Summary["total"])

	b.WriteString("| shot | status | changed | beyond threshold | detail |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, s := range r.Shots {
		detail := ""
		switch s.Status {
		case "fail":
			detail = fmt.Sprintf("budget %.1f%%", s.MaxChanged*100)
		case "new":
			detail = "no baseline yet — approve to adopt"
		case "size", "error", "missing":
			detail = s.Error
		}
		fmt.Fprintf(&b, "| `%s` | %s | %.1f%% | %d px | %s |\n",
			s.Key, s.Status, s.ChangedRatio*100, s.MismatchPixels, detail)
	}
	if r.GeneratedAt != "" {
		fmt.Fprintf(&b, "\n_generated %s_\n", r.GeneratedAt)
	}
	return b.String()
}
