---
name: omniviz
description: >
  Use when setting up, running, debugging or extending visual regression
  testing (VRT) with omniviz — capture/diff/review/approve for Godot, web,
  Unity, Unreal, .NET, Delphi, Android, terminals or any process that can
  save a PNG. Covers omniviz.toml, drivers, CI integration, baselines,
  approvals and determinism troubleshooting.
---

# omniviz — visual regression testing for anything with a pixel

Workflow: **capture → pixel-diff vs committed baselines → review in a local UI →
approve** to promote new baselines. Web VRT concepts (Chromatic/Percy/Argos)
map 1:1; the difference is that baselines live in **git**, so PRs automatically
diff against the base branch's baselines and approvals are baseline updates in
the same PR.

## Commands

| command | purpose |
|---|---|
| `omniviz test --project DIR [--fail-on-new] [--parallel N]` | capture + diff + report; **exit 1** on fail/size/error/missing (and new with --fail-on-new). Writes `report.json` + `junit.xml` into output_dir |
| `omniviz capture` | capture only, no diffing (used for seeding) |
| `omniviz compare` | re-diff existing currents vs baselines without capturing |
| `omniviz approve KEY... / --all` | promote `output_dir/current/<key>.png` to `baseline_dir/` and refresh the report |
| `omniviz review [--port N] [--no-open]` | local UI: shot list, overlay slider, diff heatmaps, recording scrubber, approve buttons |
| `omniviz shots` | list resolved jobs (validates omniviz.toml without capturing) |
| `omniviz run [--driver NAME] TARGET` | adhoc windowed launch, no diffing (godot only for now) |
| `omniviz summary [--markdown]` | print last report; markdown form is for PR comments / `$GITHUB_STEP_SUMMARY` |
| `omniviz drivers` | list registered drivers |

Run `omniviz test` from the project root containing `omniviz.toml`, or pass
`--project DIR`. Binary: `bin/omniviz` (built via `mise run build` or
`go build -o bin/omniviz ./cmd/omniviz`).

## omniviz.toml

```toml
baseline_dir = "tests/visual/baselines"   # committed to git
output_dir   = "tmp/omniviz"              # gitignored

[render]
width = 1280
height = 720
fps = 30                # recording fps
max_frames = 240        # recordings decimated to this

[defaults]
driver = "godot"        # default driver; per-shot override with driver =
record = false
threshold = 0.1         # per-pixel pixelmatch threshold 0..1 (smaller = stricter)
max_changed = 0.01      # changed-area budget: max FRACTION of pixels that may differ at all
max_diff_ratio = 0      # tolerated FRACTION of pixels beyond threshold (0 = strict)
retries = 0             # re-run errored captures N times
serial = false          # all jobs exclusive (heavy engines: unity/unreal)
parallel = 1
timeout = 600

[driver.godot]          # per-driver tables; drivers decode their own options
binary = ""             # else $OMNIVIZ_GODOT, then PATH
display_driver = "x11"  # linux only

[driver.web]
browser = ""            # else $OMNIVIZ_BROWSER, then auto-detect
full_page = false
wait_ms = 250
wait_ready = ""         # JS predicate polled before the shot
freeze = true           # animations/carets frozen (determinism)
selector = ""           # capture one element instead of the page

[driver.command]
# command = ["argv", ...]  OR  script = "..." (runs <shell> -e -c)
shell = "sh"
dir = ""                # cwd relative to project

[[shot]]
name = "unique-key"         # omit for multi mode (one shot per matched file)
target = "res://scenes/x.tscn"  # scene / URL / file path; command shots may omit
driver = "godot"            # per-shot driver override
driver_options = { wait_ms = 500 }   # merged over [driver.<name>]
viewports = ["1280x720", "375x812"]  # fan out to one job per size (id/key suffixed)
ignore_regions = [[0, 0, 1080, 90]]  # [x y w h] rects excluded from the diff
paths = ["tmp/shots/*.png"]          # glob collection; omit = driver default source
args = []                   # appended to the driver's process argv
env = []                    # appended to the process env
record = false
threshold = 0.1
max_changed = 0.01
max_diff_ratio = 0
retries = 0
quit_after = 240            # godot: frames; web: recording frames
serial = false
timeout = 600
```

Multi mode (no `name`) → one shot per matched file, named by file stem; the
job id falls back to the first glob's directory name.

## Drivers

| driver | target | capture |
|---|---|---|
| `godot` | Godot 4 scenes | generic: movie-writer, last frame IS the shot (`quit_after`) · cooperative: scene saves PNGs, `paths` collect |
| `command` | **any process** | runs argv/script, collects PNGs from `$OMNIVIZ_OUTPUT` (auto) or `paths` globs |
| `web` | URLs / local files | one warm Chrome for the whole run, tab per shot, waits readyState+fonts, freezes animations, screenshots viewport/full page/element |

Command-driver process contract: env `OMNIVIZ_OUTPUT` (scratch dir,
auto-collected), `OMNIVIZ_PROJECT`, `OMNIVIZ_JOB`, `OMNIVIZ_WIDTH`,
`OMNIVIZ_HEIGHT`, plus `args`/`env` from config. Process must save PNG(s) and
exit; non-zero exit = job error.

## Statuses and exit codes

Statuses: `pass`, `fail` (per-pixel threshold OR changed-area budget), `new`
(no baseline), `size` (resolution changed), `error` (capture failed),
`captured` (non-compared capture run).

- `omniviz test` exit 0 = clean; exit 1 = shots need attention; exit 2 = usage.
- Report: `output_dir/report.json` (statuses, pixel counts, ratios, logs);
  `output_dir/junit.xml` (GitLab `reports:junit`, GH test reporters).

## Standard workflows

**First run**: `omniviz test` → everything `new` → `omniviz review` →
`omniviz approve --all` (or per key) → commit `baseline_dir/`, never
`output_dir/`. The next `test` diffs against them.

**Intentional change**: make the change → `test` shows `fail` → inspect in
`review` → `approve <key>` → commit the updated baseline with the change.

**CI (PR)**: `omniviz test --fail-on-new` → exit code gates the merge;
publish `summary --markdown` to the PR and upload `output_dir/` as artifacts.
On the default branch, optionally auto-accept: `omniviz test || true;
omniviz approve --all; git commit baseline_dir && git push` (bot commit).
Full recipes: `docs/ci.md` in the omni-viz repo; GitHub composite action at
`.github/actions/omniviz`.

**Debugging a capture**: `output_dir/logs/<job>.log` holds the process output
tail (also embedded in report.json). `OMNIVIZ_DEBUG=1` prints full argv.

## Determinism checklist (the #1 source of failures)

- Fix seeds AND iteration orders (Go map iteration, particle time, shader
  nondeterminism). Real example: a demo gauge stacked rings from Go map
  iteration — flagged on the first run.
- Pin the rendering API/graphics backend per project; switching between runs
  changes every pixel.
- Kill motion: the web driver freezes animations/carets automatically; for
  Android, `capture.sh` hides clock/battery via `secure icon_blacklist` +
  SystemUI restart (demo-mode broadcasts don't engage on AOSP 14
  google_apis); for godot use the overlay (already automatic).
- Absorb environment noise: `max_diff_ratio` (small AA fraction),
  `ignore_regions` (dynamic strips), `max_changed` (global drift). Take
  baselines in the same environment CI uses.
- `size` failures mean the resolution changed — usually what you want to see.

## Per-engine recipes (examples/ in the repo)

| engine | capture glue | notes |
|---|---|---|
| Godot | native driver | deterministic via movie writer; display_driver x11 (linux) |
| Unity | `examples/unity-capture`: `unity-editor -batchmode -executeMethod VisualTestCapture.Capture`, no `-nographics` (ScreenCapture needs GL), pin `-force-vulkan/-force-glcore` | needs GPU runner + Unity license |
| Unreal 5 | `examples/unreal-capture`: test map does delay → `HighResShot WxH` → `quit`; glob `Saved/Screenshots/*/HighResShot*.png` | `-deterministic -seed=N` |
| .NET WinForms | `examples/winforms-capture`: GDI+ `DrawToBitmap` → PNG (Windows/.NET 8); container-verified via Mono | pin DPI/theme |
| Delphi | `examples/delphi-capture`: VCL `GetFormImage` → `TPNGImage` (container-verified via Lazarus/LCL) | Delphi 11+ |
| Android | `examples/android-capture`: `capture.sh` — force-stop, disable animation scales, `icon_blacklist`, launch by deeplink/intent, `adb exec-out screencap -p` | `ANDROID_SERIAL` for multi-device |
| Web | native driver | Chromatic-style: warm browser, tab per shot, fonts+readyState waits, `selector`/`full_page`, `wait_ready` |
| anything else | command driver | if it can save a PNG and exit |

Container verification for winforms/delphi/android (no engines needed on the
host): `docker/verify.sh` in the omni-viz repo.

## Troubleshooting map

| symptom | cause | fix |
|---|---|---|
| everything fails after environment change | baselines from another renderer | re-seed baselines in that environment, or `max_diff_ratio`/`ignore_regions` |
| 0.0% changed but px beyond threshold | status-bar clock / tiny dynamic icon | hide it (icon_blacklist, freeze) or `ignore_regions`; `max_diff_ratio` as last resort |
| shot fails only in CI | font/GPU differences dev vs CI | take baselines from CI (download `current/` artifacts → approve locally → commit) |
| all shots `error: executable file not found` | driver binary missing | driver `[driver.x] binary=`, `$OMNIVIZ_<NAME>` env, or PATH |
| `duplicate shot key` | two shots resolve to same key (name collision or viewport fan-out) | rename shot or dedupe viewports |
| stale failures that "should pass" | baseline updated by an earlier approve | check `baselines/` mtimes; re-seed if polluted |
| godot capture slow first time | Vulkan shader compilation | normal; cached after first run |
| web captures differ run-to-run | dynamic content | `freeze` on (default), add `wait_ready`, `ignore_regions` for clocks/ads |

## Extending

New target kind: implement `omniviz.Driver` (Name, NewOptions, Check,
Capture) in Go, `omniviz.RegisterDriver` from `init()`, blank-import it from
a binary. Most targets don't need this — a command-driver config is
equivalent. Reference implementations live in `drivers/` (`command` is ~200
lines). Optional extensions: `VersionReporter` (report header label),
`AdhocRunner` (powers `omniviz run`).
