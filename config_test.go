package omniviz

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubDriver struct{}

func (stubDriver) Name() string    { return "stub" }
func (stubDriver) NewOptions() any { return nil }
func (stubDriver) Check(rc *RunContext) error {
	return nil
}
func (stubDriver) Capture(ctx context.Context, rc *RunContext, job Job, env CaptureEnv) (CaptureResult, error) {
	return CaptureResult{}, nil
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "omniviz.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestViewportFanout(t *testing.T) {
	RegisterDriver(stubDriver{})
	p := writeConfig(t, `
baseline_dir = "b"
output_dir = "t"
[defaults]
driver = "stub"
[[shot]]
name = "page"
target = "x"
viewports = ["1280x720", "375x812", "375x812"]
`)
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	jobs := c.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs (duplicate viewport collapsed), got %d", len(jobs))
	}
	want := []struct {
		id, key string
		w, h    int
	}{
		{"page-1280x720", "page-1280x720", 1280, 720},
		{"page-375x812", "page-375x812", 375, 812},
	}
	for i, w := range want {
		if jobs[i].ID != w.id || jobs[i].Key != w.key || jobs[i].Width != w.w || jobs[i].Height != w.h {
			t.Errorf("job %d = %+v, want %v", i, jobs[i], w)
		}
	}
}

func TestIgnoreRegionsAndRetriesInConfig(t *testing.T) {
	p := writeConfig(t, `
baseline_dir = "b"
output_dir = "t"
[defaults]
driver = "stub"
retries = 2
[[shot]]
name = "masked"
target = "x"
ignore_regions = [[100, 0, 200, 60], [0, 100, 50, 50]]
`)
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	j := c.Jobs()[0]
	if j.Retries != 2 || len(j.IgnoreRegions) != 2 {
		t.Fatalf("job = %+v", j)
	}
	if j.IgnoreRegions[0].Dx() != 200 || j.IgnoreRegions[0].Dy() != 60 {
		t.Fatalf("rect = %v", j.IgnoreRegions[0])
	}
}

func TestIgnoreRegionsRejected(t *testing.T) {
	p := writeConfig(t, `
baseline_dir = "b"
output_dir = "t"
[defaults]
driver = "stub"
[[shot]]
name = "bad"
target = "x"
ignore_regions = [[1, 2, 3]]
`)
	_, err := LoadConfig(p)
	if err == nil || !strings.Contains(err.Error(), "ignore_regions") {
		t.Fatalf("want ignore_regions validation error, got %v", err)
	}
}

func TestJUnitOutput(t *testing.T) {
	r := &Report{Shots: []ShotResult{
		{Key: "ok", Job: "j1", Status: "pass"},
		{Key: "broken", Job: "j1", Status: "fail", ChangedRatio: 0.44, MaxChanged: 0.01, MismatchPixels: 224848},
		{Key: "fresh", Job: "j2", Status: "new"},
		{Key: "oops", Job: "j2", Status: "error", Error: "engine exploded"},
	}}
	r.Finish()
	p := filepath.Join(t.TempDir(), "junit.xml")
	if err := WriteJUnit(p, r); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	x := string(data)
	for _, want := range []string{`failures="2"`, `<skipped`, "engine exploded", `name="broken"`} {
		if !strings.Contains(x, want) {
			t.Errorf("junit missing %q:\n%s", want, x)
		}
	}
}

func TestMarkdownSummary(t *testing.T) {
	r := &Report{Shots: []ShotResult{
		{Key: "ok", Job: "j", Status: "pass"},
		{Key: "broken", Job: "j", Status: "fail", ChangedRatio: 0.44, MaxChanged: 0.01, MismatchPixels: 224848},
	}}
	r.Finish()
	md := MarkdownSummary(r)
	for _, want := range []string{"| `broken` | fail | 44.0% | 224848 px |", "FAIL 1"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}
