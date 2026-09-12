package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	toml "github.com/BurntSushi/toml"
)

type Config struct {
	Godot         string       `toml:"godot"`
	DisplayDriver string       `toml:"display_driver"`
	BaselineDir   string       `toml:"baseline_dir"`
	OutputDir     string       `toml:"output_dir"`
	Render        RenderConfig `toml:"render"`
	Defaults      Defaults     `toml:"defaults"`
	Shots         []ShotConfig `toml:"shot"`
}

type RenderConfig struct {
	Width     int    `toml:"width"`
	Height    int    `toml:"height"`
	FPS       int    `toml:"fps"`
	MaxFrames int    `toml:"max_frames"`
	Method    string `toml:"method"`
}

type Defaults struct {
	Record     bool     `toml:"record"`
	Threshold  float64  `toml:"threshold"`
	MaxChanged float64  `toml:"max_changed"`
	Args       []string `toml:"args"`
	Env        []string `toml:"env"`
	Timeout    int      `toml:"timeout"`
	Parallel   int      `toml:"parallel"`
}

type ShotConfig struct {
	Name      string   `toml:"name"`
	Scene     string   `toml:"scene"`
	Size      string   `toml:"size"`
	Width     int      `toml:"width"`
	Height    int      `toml:"height"`
	Paths     []string `toml:"paths"`
	Args      []string `toml:"args"`
	Env       []string `toml:"env"`
	Record    *bool    `toml:"record"`
	Threshold *float64 `toml:"threshold"`
	MaxChanged *float64 `toml:"max_changed"`
	QuitAfter int      `toml:"quit_after"`
	Timeout   int      `toml:"timeout"`
	Serial    bool     `toml:"serial"`
}

type Job struct {
	ID        string
	Key       string
	Multi     bool
	Scene     string
	Width     int
	Height    int
	Record    bool
	Threshold float64
	MaxChanged float64
	Args      []string
	Env       []string
	QuitAfter int
	Timeout   time.Duration
	Paths     []string
	Serial    bool
}

func DefaultConfig() *Config {
	return &Config{
		DisplayDriver: "x11",
		BaselineDir:   "tests/visual/baselines",
		OutputDir:     "tmp/omniviz",
		Render:        RenderConfig{Width: 1280, Height: 720, FPS: 30, MaxFrames: 240},
		Defaults:      Defaults{Record: true, Threshold: 0.1, MaxChanged: 0.01, Timeout: 600},
	}
}

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
		fmt.Fprintf(os.Stderr, "omniviz: warning: unknown config key %q in %s\n", key.String(), path)
	}
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
		if s.Scene == "" {
			return fmt.Errorf("shot %d: missing scene", i+1)
		}
		if len(s.Paths) == 0 && s.Name == "" {
			return fmt.Errorf("shot %d: multi mode (no name) requires paths", i+1)
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

func (c *Config) Jobs() []Job {
	jobs := make([]Job, 0, len(c.Shots))
	for i := range c.Shots {
		s := &c.Shots[i]
		j := Job{
			Scene: s.Scene,
			Paths: s.Paths,
			Args:  s.Args,
			Env:   s.Env,
		}
		j.Multi = s.Name == ""
		if j.Multi {
			base := filepath.Base(filepath.FromSlash(s.Scene))
			j.ID = strings.TrimSuffix(base, filepath.Ext(base))
		} else {
			j.Key, _ = SanitizeKey(s.Name)
			j.ID = j.Key
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
		j.Serial = s.Serial
		jobs = append(jobs, j)
	}
	return jobs
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
