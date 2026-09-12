package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var overlayExcluded = map[string]bool{
	".git":          true,
	".pi-subagents": true,
	".ralph":        true,
	".test-results": true,
	"build":         true,
	"override.cfg":  true,
	"project.godot": true,
}

type settingOverride struct {
	section string
	option  string
	value   string
}

func buildOverlay(project string, width, height int, extraSets []string) (string, error) {
	overlay, err := os.MkdirTemp("", "omniviz-project-")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		os.RemoveAll(overlay)
		return "", fmt.Errorf("read project: %w", err)
	}
	for _, entry := range entries {
		if overlayExcluded[entry.Name()] {
			continue
		}
		source := filepath.Join(project, entry.Name())
		if err := os.Symlink(source, filepath.Join(overlay, entry.Name())); err != nil {
			os.RemoveAll(overlay)
			return "", fmt.Errorf("symlink %s: %w", entry.Name(), err)
		}
	}
	if err := os.Symlink(filepath.Join(project, "project.godot"), filepath.Join(overlay, "project.godot")); err != nil {
		os.RemoveAll(overlay)
		return "", fmt.Errorf("symlink project.godot: %w", err)
	}
	pairs := []settingOverride{
		{"display/window/size", "mode", "0"},
		{"display/window/size", "no_focus", "true"},
		{"display/window/size", "window_width_override", fmt.Sprint(width)},
		{"display/window/size", "window_height_override", fmt.Sprint(height)},
		{"display/window/size", "borderless", "false"},
		{"display/window/size", "always_on_top", "false"},
	}
	for _, set := range extraSets {
		key, value, ok := strings.Cut(set, "=")
		if !ok {
			os.RemoveAll(overlay)
			return "", fmt.Errorf("--set expects SECTION/KEY=VALUE, got %q", set)
		}
		slash := strings.LastIndex(key, "/")
		if slash < 0 {
			os.RemoveAll(overlay)
			return "", fmt.Errorf("--set expects SECTION/KEY=VALUE, got %q", set)
		}
		pairs = append(pairs, settingOverride{section: key[:slash], option: key[slash+1:], value: value})
	}
	if err := os.WriteFile(filepath.Join(overlay, "override.cfg"), []byte(renderOverrideCFG(pairs)), 0o644); err != nil {
		os.RemoveAll(overlay)
		return "", err
	}
	return overlay, nil
}

func renderOverrideCFG(pairs []settingOverride) string {
	var out strings.Builder
	var current string
	for _, p := range pairs {
		if p.section != current {
			fmt.Fprintf(&out, "[%s]\n\n", p.section)
			current = p.section
		}
		fmt.Fprintf(&out, "%s=%s\n", p.option, p.value)
	}
	out.WriteString("\n")
	return out.String()
}
