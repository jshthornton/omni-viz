package omniviz

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// RunContext is the per-run state shared by the core and drivers: the
// project root, the loaded config, the sandbox directories and the
// driver registry.
type RunContext struct {
	Project   string // absolute project root
	Config    *Config
	Output    string // abs output_dir (currents, diffs, frames, logs, report)
	Baselines string // abs baseline_dir

	mu       sync.Mutex
	versions map[string]string // driver name → version label (Check fills this)
	keys     map[string]bool   // shot keys claimed this run
}

// LoadContext resolves the project root, loads omniviz.toml and creates the
// sandbox directory layout. It does not check any driver environment — that
// is CheckDrivers' job, so a project can mix drivers freely.
func LoadContext(projectFlag string) (*RunContext, error) {
	project, err := filepath.Abs(projectFlag)
	if err != nil {
		return nil, err
	}
	cfg, err := LoadConfig(filepath.Join(project, "omniviz.toml"))
	if err != nil {
		return nil, err
	}
	output := filepath.Join(project, filepath.FromSlash(cfg.OutputDir))
	baselines := filepath.Join(project, filepath.FromSlash(cfg.BaselineDir))
	for _, d := range []string{
		output,
		filepath.Join(output, "current"),
		filepath.Join(output, "diff"),
		filepath.Join(output, "logs"),
		filepath.Join(output, "scratch"),
		baselines,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &RunContext{
		Project:   project,
		Config:    cfg,
		Output:    output,
		Baselines: baselines,
		versions:  map[string]string{},
		keys:      map[string]bool{},
	}, nil
}

// CheckDrivers runs Driver.Check once for every driver referenced by the
// given jobs. VersionReporter implementations contribute labels shown in
// the report and review header.
func (c *RunContext) CheckDrivers(jobs []Job) error {
	seen := map[string]bool{}
	for _, j := range jobs {
		if seen[j.Driver] {
			continue
		}
		seen[j.Driver] = true
		drv, err := GetDriver(j.Driver)
		if err != nil {
			return err
		}
		if err := drv.Check(c); err != nil {
			return fmt.Errorf("driver %s: %w", drv.Name(), err)
		}
		if vr, ok := drv.(VersionReporter); ok {
			c.mu.Lock()
			c.versions[drv.Name()] = vr.VersionLabel()
			c.mu.Unlock()
		}
	}
	return nil
}

// Versions returns collected driver version labels by driver name.
func (c *RunContext) Versions() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.versions))
	for name, label := range c.versions {
		out[name] = label
	}
	return out
}

// VersionLabels returns collected driver version labels ("godot 4.4", ...).
func (c *RunContext) VersionLabels() []string {
	versions := c.Versions()
	out := make([]string, 0, len(versions))
	for name, label := range versions {
		if label != "" {
			out = append(out, label)
		} else {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// claimKey registers a shot key for this run; false means it was already
// taken (duplicate shot).
func (c *RunContext) claimKey(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys[key] {
		return false
	}
	c.keys[key] = true
	return true
}
