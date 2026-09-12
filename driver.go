package omniviz

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Driver turns jobs into capture images. One driver per capture target kind
// (a game engine, a browser, a bare process, ...). The core — config, jobs,
// scheduling, diffing, baselines, approvals, review UI — is driver-agnostic.
//
// Register implementations with RegisterDriver (typically from an init() in
// the driver's own package) and reference them from omniviz.toml:
//
//	[defaults]
//	driver = "godot"             # default for shots that don't override
//
//	[[shot]]
//	name = "main-menu"
//	driver = "web"               # per-shot driver override
//
// Implementations must be safe for concurrent use: the runner may call
// Capture from several goroutines at once. Check is always called once,
// before any Capture.
type Driver interface {
	// Name is the driver id used in config (driver = "<name>").
	Name() string

	// NewOptions returns a fresh zero value that the core TOML-decodes
	// [driver.<name>] and per-shot driver_options tables into, or nil if
	// the driver takes no options. The merged value arrives on
	// Job.DriverOptions; type-assert it back to your own type.
	NewOptions() any

	// Check validates the environment for this run: engine binary present,
	// project layout sane, license valid, whatever the driver needs. It is
	// called once per run for every driver the job set references, before
	// any Capture. Returning an error aborts the run.
	Check(rc *RunContext) error

	// Capture performs one job's capture: launch whatever must be launched,
	// wait for it, and report the produced shot images. Frames for the
	// recording scrubber (if job.Record) go into env.FramesDir; the core
	// decimates them to render.max_frames afterwards.
	Capture(ctx context.Context, rc *RunContext, job Job, env CaptureEnv) (CaptureResult, error)
}

// VersionReporter is an optional Driver extension: drivers that implement it
// contribute a version label (e.g. "godot 4.4.stable") to the report and the
// review UI header.
type VersionReporter interface {
	VersionLabel() string
}

// AdhocRunner is an optional Driver extension: drivers that can launch a
// single target interactively (no baselines, no diffing — a dev/authoring
// convenience) implement it and get the `omniviz run` command.
type AdhocRunner interface {
	RunAdhoc(ctx context.Context, rc *RunContext, req AdhocRequest) error
}

// CaptureEnv is the sandbox a driver captures into.
type CaptureEnv struct {
	Project string   // absolute project root; the natural cwd for drivers
	Env     []string // full process environment: os.Environ() + defaults.env + shot env
	LogPath string   // write process output here (the core tails it into the report)

	FramesDir  string // recording frame destination; exists when Record
	ScratchDir string // per-job scratch space (output/scratch/<job>); always exists

	Width, Height int
	Record        bool     // recording frames requested (already merged with --record/--no-record)
	Slot          int      // parallel slot index — window cascade offsets and the like
	SetOverrides  []string // --set SECTION/KEY=VALUE run-level overrides, for drivers that understand them
}

// CaptureResult reports what a capture produced.
type CaptureResult struct {
	// ShotFiles are the capture images to treat as shots (absolute paths).
	// One file for a named shot; several only when the job is multi mode
	// (no name — files are stem-named into shot keys).
	ShotFiles []string

	// Error is a job-level failure message: the capture ran but produced
	// nothing usable ("no files matched ...", "engine exited with error").
	// Leave empty on success.
	Error string

	// Log is a tail of the capture process output for the report. If empty,
	// the core tails env.LogPath.
	Log string
}

// AdhocRequest is an `omniviz run` invocation.
type AdhocRequest struct {
	Target       string
	Width        int
	Height       int
	Record       bool
	SetOverrides []string
	UserArgs     []string
}

// ---------------------------------------------------------------- registry

var (
	regMu    sync.RWMutex
	registry = map[string]Driver{}
)

// RegisterDriver adds a driver to the global registry. Call it from an
// init() in the driver package; binaries enable drivers by blank-importing
// them. Panics on duplicate names — that is a programming error.
func RegisterDriver(d Driver) {
	if d == nil || d.Name() == "" {
		panic("omniviz: RegisterDriver(nil) or driver with empty name")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[d.Name()]; dup {
		panic(fmt.Sprintf("omniviz: driver %q registered twice", d.Name()))
	}
	registry[d.Name()] = d
}

// GetDriver looks a driver up by config name.
func GetDriver(name string) (Driver, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	d, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown driver %q (registered: %v)", name, DriverNames())
	}
	return d, nil
}

// DriverNames lists registered driver ids, sorted.
func DriverNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
