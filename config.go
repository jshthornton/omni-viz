package omniviz

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	toml "github.com/BurntSushi/toml"
)

// Config is the omniviz.toml schema. Everything except the driver tables is
// engine-agnostic; drivers declare their own options via Driver.NewOptions.
type Config struct {
	BaselineDir string                    `toml:"baseline_dir"`
	OutputDir   string                    `toml:"output_dir"`
	Render      RenderConfig              `toml:"render"`
	Defaults    Defaults                  `toml:"defaults"`
	Shots       []ShotConfig              `toml:"shot"`
	Driver      map[string]toml.Primitive `toml:"driver"`

	meta toml.MetaData
}

type RenderConfig struct {
	Width     int    `toml:"width"`
	Height    int    `toml:"height"`
	FPS       int    `toml:"fps"`
	MaxFrames int    `toml:"max_frames"`
	Method    string `toml:"method"` // optional driver passthrough (godot: --rendering-method)
}

type Defaults struct {
	Driver     string   `toml:"driver"` // default driver for shots without one
	Record     bool     `toml:"record"`
	Threshold  float64  `toml:"threshold"`
	MaxChanged float64  `toml:"max_changed"`
	Args       []string `toml:"args"`
	Env        []string `toml:"env"`
	Timeout    int      `toml:"timeout"`
	Parallel   int      `toml:"parallel"`
	Serial     bool     `toml:"serial"` // run every job exclusively (heavy engines)
}

// ShotConfig is one [[shot]] entry. The canonical target key is `target`
// (a scene path, a URL, a command label — whatever the driver captures);
// `scene` is accepted as an alias for Godot-flavored configs.
type ShotConfig struct {
	Name   string `toml:"name"`
	Target string `toml:"target"`
	Scene  string `toml:"scene"` // alias for target (godot vocabulary)

	Driver        string         `toml:"driver"`
	DriverOptions toml.Primitive `toml:"driver_options"`

	Size       string   `toml:"size"`
	Width      int      `toml:"width"`
	Height     int      `toml:"height"`
	Paths      []string `toml:"paths"`
	Args       []string `toml:"args"`
	Env        []string `toml:"env"`
	Record     *bool    `toml:"record"`
	Threshold  *float64 `toml:"threshold"`
	MaxChanged *float64 `toml:"max_changed"`
	QuitAfter  int      `toml:"quit_after"`
	Timeout    int      `toml:"timeout"`
	Serial     bool     `toml:"serial"`
}

// effectiveTarget returns target, falling back to the scene alias.
func (s *ShotConfig) effectiveTarget() string {
	if s.Target != "" {
		return s.Target
	}
	return s.Scene
}

// Job is a fully resolved shot: defaults applied, driver options decoded.
type Job struct {
	ID    string
	Key   string
	Multi bool

	Driver        string // resolved driver id
	DriverOptions any    // merged [driver.<name>] + shot driver_options, nil if optionless

	Target     string
	Width      int
	Height     int
	Record     bool
	Threshold  float64
	MaxChanged float64
	Args       []string
	Env        []string
	QuitAfter  int
	Timeout    time.Duration
	Paths      []string
	Serial     bool
}

func DefaultConfig() *Config {
	return &Config{
		BaselineDir: "tests/visual/baselines",
		OutputDir:   "tmp/omniviz",
		Render:      RenderConfig{Width: 1280, Height: 720, FPS: 30, MaxFrames: 240},
		Defaults:    Defaults{Record: true, Threshold: 0.1, MaxChanged: 0.01, Timeout: 600},
	}
}

// LoadConfig reads and validates an omniviz.toml.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, key := range meta.Undecoded() {
		// [driver.<name>] and shot.driver_options belong to drivers, which
		// decode them lazily (BurntSushi also lists primitive children as
		// undecoded) — never warn about driver-owned keys.
		if key[0] == "driver" || slices.Contains(key, "driver_options") {
			continue
		}
		fmt.Fprintf(os.Stderr, "omniviz: warning: unknown config key %q in %s\n", key.String(), path)
	}
	cfg.meta = meta
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Render.Width <= 0 || c.Render.Height <= 0 {
		return fmt.Errorf("render.width/height must be positive")
	}
	if c.Render.FPS <= 0 {
		c.Render.FPS = 30
	}
	if c.Render.MaxFrames <= 0 {
		c.Render.MaxFrames = 240
	}
	if c.Defaults.Threshold <= 0 || c.Defaults.Threshold > 1 {
		c.Defaults.Threshold = 0.1
	}
	if c.Defaults.MaxChanged <= 0 || c.Defaults.MaxChanged > 1 {
		c.Defaults.MaxChanged = 0.01
	}
	if c.Defaults.Timeout <= 0 {
		c.Defaults.Timeout = 600
	}
	if c.Defaults.Parallel < 0 {
		c.Defaults.Parallel = 0
	}
	if len(c.Shots) == 0 {
		return fmt.Errorf("no [[shot]] entries defined")
	}
	seen := map[string]bool{}
	for i := range c.Shots {
		s := &c.Shots[i]
		// target is driver-owned: godot/web require one and validate it
		// themselves; command shots don't need it (the command is the target)
		if len(s.Paths) == 0 && s.Name == "" {
			return fmt.Errorf("shot %d: multi mode (no name) requires paths", i+1)
		}
		name := s.Driver
		if name == "" {
			name = c.Defaults.Driver
		}
		if name == "" {
			name = "command"
		}
		if _, err := GetDriver(name); err != nil {
			return fmt.Errorf("shot %d: %w", i+1, err)
		}
		if s.Name == "" {
			continue
		}
		key, err := SanitizeKey(s.Name)
		if err != nil {
			return fmt.Errorf("shot %d: %w", i+1, err)
		}
		if seen[key] {
			return fmt.Errorf("duplicate shot name %q", key)
		}
		seen[key] = true
	}
	return nil
}

func SanitizeKey(name string) (string, error) {
	k := strings.TrimSpace(filepath.ToSlash(name))
	if k == "" {
		return "", fmt.Errorf("empty shot key")
	}
	k = strings.Trim(k, "/")
	var b strings.Builder
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '/', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	k = b.String()
	if k == "" || k == "." || strings.HasPrefix(k, "..") || strings.Contains(k, "//") {
		return "", fmt.Errorf("invalid shot key %q", name)
	}
	return k, nil
}

// GlobalDriverOptions decodes the [driver.<name>] table into a fresh value
// from the driver's NewOptions (nil when the driver is unknown or
// optionless). Drivers use it in Check; Jobs uses the per-job merged view.
func (c *Config) GlobalDriverOptions(name string) any {
	drv, err := GetDriver(name)
	if err != nil {
		return nil
	}
	vo := drv.NewOptions()
	if vo == nil {
		return nil
	}
	if prim, ok := c.Driver[name]; ok {
		if err := c.meta.PrimitiveDecode(prim, vo); err != nil {
			fmt.Fprintf(os.Stderr, "omniviz: warning: [driver.%s]: %v\n", name, err)
		}
	}
	return vo
}

// Jobs resolves every shot into a runnable job: defaults applied, driver
// picked, driver options decoded (global [driver.<name>] first, then the
// per-shot driver_options table on top — present keys win).
func (c *Config) Jobs() []Job {
	jobs := make([]Job, 0, len(c.Shots))
	for i := range c.Shots {
		s := &c.Shots[i]
		j := Job{
			Target: s.effectiveTarget(),
			Paths:  s.Paths,
			Args:   s.Args,
			Env:    s.Env,
		}
		j.Multi = s.Name == ""
		if j.Multi {
			// id from the target stem; command-style multi shots without a
			// target fall back to their first glob's directory name
			base := filepath.Base(filepath.FromSlash(s.Target))
			j.ID = strings.TrimSuffix(base, filepath.Ext(base))
			if j.ID == "" || j.ID == "." || j.ID == "/" {
				if label := multiLabel(s.Paths); label != "" {
					j.ID = label
				} else {
					j.ID = fmt.Sprintf("shot-%d", i+1)
				}
			}
		} else {
			j.Key, _ = SanitizeKey(s.Name)
			j.ID = j.Key
		}
		j.Driver = s.Driver
		if j.Driver == "" {
			j.Driver = c.Defaults.Driver
		}
		if j.Driver == "" {
			j.Driver = "command"
		}
		if drv, err := GetDriver(j.Driver); err == nil {
			if vo := drv.NewOptions(); vo != nil {
				if prim, ok := c.Driver[j.Driver]; ok {
					if err := c.meta.PrimitiveDecode(prim, vo); err != nil {
						fmt.Fprintf(os.Stderr, "omniviz: warning: [driver.%s]: %v\n", j.Driver, err)
					}
				}
				if !reflect.DeepEqual(s.DriverOptions, toml.Primitive{}) {
					if err := c.meta.PrimitiveDecode(s.DriverOptions, vo); err != nil {
						fmt.Fprintf(os.Stderr, "omniviz: warning: shot %q driver_options: %v\n", j.ID, err)
					}
				}
				j.DriverOptions = vo
			}
		}
		w, h := s.Width, s.Height
		if w <= 0 || h <= 0 {
			if s.Size != "" {
				if pw, ph, err := parseSize(s.Size); err == nil {
					w, h = pw, ph
				} else {
					fmt.Fprintf(os.Stderr, "omniviz: warning: shot %q: %v (using defaults)\n", j.ID, err)
				}
			}
		}
		if w <= 0 {
			w = c.Render.Width
		}
		if h <= 0 {
			h = c.Render.Height
		}
		j.Width, j.Height = w, h
		j.Record = c.Defaults.Record
		if s.Record != nil {
			j.Record = *s.Record
		}
		j.Threshold = c.Defaults.Threshold
		if s.Threshold != nil {
			j.Threshold = *s.Threshold
		}
		j.MaxChanged = c.Defaults.MaxChanged
		if s.MaxChanged != nil {
			j.MaxChanged = *s.MaxChanged
		}
		timeout := c.Defaults.Timeout
		if s.Timeout > 0 {
			timeout = s.Timeout
		}
		j.Timeout = time.Duration(timeout) * time.Second
		j.QuitAfter = s.QuitAfter
		j.Serial = c.Defaults.Serial || s.Serial
		jobs = append(jobs, j)
	}
	return jobs
}

// multiLabel derives a job label from a paths glob: the literal directory
// name the files land in ("tmp/shots/*.png" → "shots").
func multiLabel(patterns []string) string {
	for _, p := range patterns {
		dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(p)))
		base := path.Base(dir)
		if base != "" && base != "." && base != "/" && !strings.ContainsAny(base, "*?[") {
			return base
		}
	}
	return ""
}

func parseSize(s string) (int, int, error) {
	wStr, hStr, ok := strings.Cut(strings.ToLower(s), "x")
	if !ok {
		return 0, 0, fmt.Errorf("bad size %q (want WxH)", s)
	}
	w, err := strconv.Atoi(wStr)
	if err != nil {
		return 0, 0, fmt.Errorf("bad size %q (width)", s)
	}
	h, err := strconv.Atoi(hStr)
	if err != nil {
		return 0, 0, fmt.Errorf("bad size %q (height)", s)
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad size %q", s)
	}
	return w, h, nil
}
