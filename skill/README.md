# omniviz skill

An importable skill so AI coding agents know how to drive omniviz — commands,
config schema, drivers, per-engine capture recipes, CI, and a troubleshooting
map drawn from real failure modes.

## Import

**pi** (project):

```bash
mkdir -p .pi/skills && cp -r /path/to/omni-viz/skill/omniviz .pi/skills/
```

**pi** (all projects): copy to `~/.pi/agent/skills/omniviz/` instead.

**Claude Code**: copy to `.claude/skills/omniviz/` (same SKILL.md format).

**Any agent with an AGENTS.md**: append a pointer line instead:

```
For visual regression testing with omniviz, read skill/omniviz/SKILL.md and follow it.
```

The skill is self-contained and references only files that ship in the
omni-viz repo (`docs/ci.md`, `examples/`, `docker/`) — point those paths at
your checkout if it lives elsewhere.
