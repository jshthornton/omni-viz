# omni-viz

**Visual regression testing for anything with a pixel — engines, browsers, processes, terminals.**

omniviz brings the webdev workflow (Chromatic, Percy, Playwright screenshots) to every
visual target: capture deterministically, pixel-diff against a committed baseline, and
review diffs in a local UI where approving a change promotes it to the new baseline.

Three commands do the work:

```
omniviz test     →  capture every shot, diff vs baselines, non-zero exit on regressions
omniviz review   →  http://127.0.0.1:8420 — diffs, overlay slider, recordings, approvals
omniviz approve  →  promote a changed image to the new baseline
```

One static binary. No pip, no node, no runtime dependencies — the review UI is embedded
(htmx 4).

## Why

Web developers have had visual regression testing for years. Chromatic, Percy, Playwright
screenshots: change the UI, the tool diffs it against a committed baseline, you review
what moved, approve the intentional changes. It catches the stuff unit tests never see —
and it's routine.

Everything outside the browser has nothing comparable. Godot has GUT and gdUnit4 for
game logic, but no answer for a fog density change that quietly shifts every scene.
Unity, Unreal, .NET apps, terminals: same gap. The moment your UI isn't a web page
you're back to eyeballing screenshots and hoping you'd notice.

omniviz started as gdviz, a visual regression tool for Godot. It worked, and it quickly
turned out the Godot part was the smallest piece: launch the engine, get PNGs out.
Everything that made the tool worth running — the perceptual diff, the changed-area
budget, baselines, approvals, the review UI — never touched Godot at all. So the
engine-specific bit became a driver, and the rest became this project.

That's the whole split. The core (config, scheduling, diff, baselines, report, review
UI) doesn't know what a Godot or a Chrome is:

```
┌────────────────────────────────────────────────────────┐
│ core: omniviz.toml · jobs · runner · diff · baselines  │
│       report · approve · review UI                     │
└──────────────┬─────────────┬──────────────┬────────────┘
        driver │godot   driver │command  driver │web
               │              │              │
         Godot 4.x      any process    Chrome/Chromium
         movie writer   (Unity batch,  one warm browser,
         overlay cfg    Unreal cmdlet, tab per shot,
         --write-movie  .NET forms,    animations frozen,
         cooperative    CLIs, TUIs,    fonts settled)
         scenes         anything)
```

A driver is just how a target plugs in: how it's launched and where the images come
from. Most engines don't need native code at all — the command driver runs any process
and collects the PNGs, which covers Unity batchmode, Unreal commandlets, .NET forms,
Delphi apps and Android with a few lines of config. Web gets its own driver because
browser capture is its own craft. Shots can mix drivers in one `omniviz.toml` and land
in one report — game menus and the companion website reviewed side by side.

Games and apps are visual: layout regressions, material drift, generation changes and
authored-scene mistakes are invisible to `assert_eq`. omniviz makes every screenshot a
reviewable, committed artifact — intended changes get approved and become the new
reference, unintended ones fail the run with a pixel heatmap of exactly what moved.

## Drivers

| driver | captures | example |
|---|---|---|
| `godot` | Godot 4 scenes — movie-writer frames (generic) or scene-saved PNGs (cooperative) | [`examples/godot-minimal`](examples/godot-minimal) ✅ |
| `command` | **any process** — runs an argv or shell script, collects the images. The universal escape hatch: Unity batchmode, Unreal commandlets, .NET/WPF apps, Delphi VCL/FMX, Android via adb, CLI/TUI screenshots | [`examples/cli-demo`](examples/cli-demo) ✅ |
| `web` | pages in a Chrome-family browser — Chromatic-style: one warm browser, tab per shot, animations frozen, fonts settled | [`examples/web-demo`](examples/web-demo) ✅ |

Per-engine recipes under [`examples/`](examples): [`unity-capture`](examples/unity-capture) (batchmode + ScreenCapture), [`unreal-capture`](examples/unreal-capture) (test maps + HighResShot), [`winforms-capture`](examples/winforms-capture) (DrawToBitmap), [`delphi-capture`](examples/delphi-capture) (GetFormImage), [`android-capture`](examples/android-capture) (adb screencap). ✅ = captured/diffed/reviewed end-to-end in this repo; 📋 = recipe with the engine's standard capture idiom, config-shape validated.

### The web driver, the Chromatic way

The web driver copies the capture model that makes Chromatic fast and deterministic:
one warm browser process serves the whole run (a shot is a new tab — milliseconds, not
a fresh process), animations/carets freeze before the settle window,
`readyState` + `document.fonts` + an optional per-shot `wait_ready` JS predicate gate the
screenshot. An animated page captures byte-identically, run after run.

### Command driver: the universal contract

```toml
[[shot]]
name = "payment-modal"
driver_options = { command = ["dotnet", "run", "--project", "src/App", "--", "--screenshot-flow"] }
paths = ["tmp/shots/*.png"]        # or write to $OMNIVIZ_OUTPUT for auto-collect
```

The process gets `OMNIVIZ_OUTPUT` (scratch dir, auto-collected), `OMNIVIZ_PROJECT`,
`OMNIVIZ_JOB`, `OMNIVIZ_WIDTH`, `OMNIVIZ_HEIGHT` plus any `env`/`args` from config.
If your target can save a PNG and exit, the command driver tests it.

## A runnable example

Every driver has a self-contained example, each shipping two failing shots **on
purpose** so you can explore the failure UI immediately. [`examples/cli-demo`](examples/cli-demo)
runs anywhere Go runs (no GPU, no browser, no engine):

```bash
git clone https://github.com/jshthornton/omni-viz && cd omni-viz
mise run build
cd examples/cli-demo
../../bin/omniviz test        # PASS 4 · FAIL 2 (by design)
../../bin/omniviz review      # open the UI, click around
../../bin/omniviz approve --all
```

See also [`examples/godot-minimal`](examples/godot-minimal) (needs godot + GPU) and
[`examples/web-demo`](examples/web-demo) (needs any Chrome-family browser).

## Install

```bash
go install github.com/jshthornton/omni-viz/cmd/omniviz@latest
```

or build from a clone (mise users: `mise run build`, others:
`go build -o bin/omniviz ./cmd/omniviz`).

The `godot` driver needs a machine with a GPU/display that can run Godot graphically
(Forward+/Vulkan on Linux; a small X11 window is spawned, unfocused). Headless servers
without a GPU cannot render, so they cannot capture. The `web` driver needs any
Chrome-family browser. The `command` driver needs nothing but your process.

## Quickstart

Add an `omniviz.toml` to your project root:

```toml
baseline_dir = "tests/visual/baselines"   # committed to git
output_dir = "tmp/omniviz"                # gitignored: currents, diffs, recordings, report

[render]
width = 1280
height = 720

[defaults]
driver = "godot"                          # default; per-shot override with driver =

[driver.godot]
# binary = "~/engines/godot4"             # else $OMNIVIZ_GODOT, then PATH

[[shot]]
name = "main-menu"
target = "res://scenes/main_menu.tscn"
quit_after = 120                          # generic mode: render 120 frames, last frame is the shot

[[shot]]
name = "gameplay"
target = "res://tests/visual/gameplay.tscn"
paths = ["tmp/visual_tests/gameplay/*.png"]   # cooperative: globs collected as shots
threshold = 0.05

[[shot]]
name = "landing-page"
driver = "web"                            # mix drivers freely
target = "https://localhost:3000/"
```

```bash
omniviz test               # 1st run: everything is NEW — nothing has a baseline yet
omniviz review             # inspect the shots; click approve to adopt them
omniviz approve --all      # or adopt from the CLI
omniviz test               # now: PASS — and every future run diffs against these
```

Commit `baseline_dir/` (the reference images). Never commit `output_dir/`.

## Commands

| command | what it does |
|---|---|
| `omniviz test` | capture every shot + diff vs baselines + write report; exit 1 on regression |
| `omniviz capture` | capture only (no diffing) |
| `omniviz compare` | re-diff existing currents vs baselines without launching anything |
| `omniviz approve [--all\|KEY...]` | promote current captures to baselines |
| `omniviz review` | serve the review UI |
| `omniviz shots` | list configured jobs and baseline count |
| `omniviz run [--driver NAME] TARGET` | launch one target adhoc windowed (drivers that support it — dev/authoring aid) |
| `omniviz drivers` | list registered drivers |

Useful flags: `--project DIR`, `--only SUBSTR` / `--skip SUBSTR` (filter jobs),
`--no-record` / `--record`, `--set SECTION/KEY=VALUE` (driver-specific overrides),
`--fail-on-new` (treat unapproved new shots as failures — for CI), `--parallel N`.

## Writing a driver

The contract is small — implement it, `omniviz.RegisterDriver` from an `init()`, blank-
import your package from a binary, and the whole core (scheduling, diff, UI, approvals)
applies to your target:

```go
type Driver interface {
    // id used in config: driver = "<name>"
    Name() string
    // zero value for [driver.<name>] + shot driver_options decoding
    // (nil = no options); arrives merged on Job.DriverOptions
    NewOptions() any
    // validate the environment once per run (binary present, project sane)
    Check(rc *omniviz.RunContext) error
    // run one job: launch, wait, report the images
    Capture(ctx context.Context, rc *omniviz.RunContext, job omniviz.Job,
        env omniviz.CaptureEnv) (omniviz.CaptureResult, error)
}
```

`CaptureEnv` hands you the sandbox (frames dir, scratch dir, log path, size, record
flag, `--set` overrides); `CaptureResult` reports the shot files (and a job-level error
message when the capture ran but produced nothing). Optional extensions:
`VersionReporter` (a label for the report header) and `AdhocRunner` (powers
`omniviz run`). The built-in drivers are the reference implementations — `command`
(~200 lines) is the simplest to copy.

## Configuration reference

```toml
baseline_dir = "tests/visual/baselines"
output_dir = "tmp/omniviz"

[render]
width = 1280                          # default window/capture size
height = 720
fps = 30                              # recording fps
max_frames = 240                      # recordings are decimated to at most this many frames
method = ""                           # optional driver passthrough (godot: --rendering-method)

[defaults]
driver = "godot"                      # driver for shots without one (default: command)
record = true                         # record scenarios by default
threshold = 0.1                       # pixelmatch threshold 0..1 (smaller = more sensitive)
max_changed = 0.01                    # max fraction of pixels that may differ at all
max_diff_ratio = 0                    # tolerated fraction of pixels beyond the perceptual
                                      # threshold (0 = strict; 0.01 tolerates ≤1% AA noise —
                                      # the changed-area budget still catches real drift)
args = []                             # extra args appended for every job (driver-dependent)
env = []                              # KEY=VALUE env for every job
timeout = 600                         # per-job seconds before the capture is killed
parallel = 1                          # jobs run concurrently up to this many
serial = false                        # true: every job runs exclusively (heavy engines)

[driver.godot]                        # driver tables — decoded by each driver
binary = ""                           # godot binary (else $OMNIVIZ_GODOT, then PATH)
display_driver = "x11"                # linux only

[driver.web]
browser = ""                          # else $OMNIVIZ_BROWSER, then auto-detect
full_page = false
wait_ms = 250                         # settle delay
# wait_ready = "window.__app === 'ready'"
# freeze = true · sandbox = true · headless = true

[driver.command]
# shell = "sh"                        # for script = mode (runs <shell> -e -c)
# dir = "tools"                       # cwd relative to project

[[shot]]
name = "my-shot"                      # optional; omit for multi mode (file-stem-named shots)
target = "res://scenes/whatever.tscn" # scene / URL / file path; command shots may omit
size = "1280x720"                     # or width/height ints; falls back to [render]
paths = ["tmp/shots/*.png"]           # optional glob collection; omit = driver's default source
driver = "godot"                      # per-shot driver override
driver_options = { wait_ms = 500 }    # per-shot driver options, merged over the global table
args = ["--level=2"]                  # extra args for this job only
env = ["SPOOKY_SEED=1234"]
record = true
threshold = 0.05
max_changed = 0.02
quit_after = 240                      # godot: quit after N frames · web: recording frame count
serial = true                         # heavy job: run exclusively (nothing else alongside)
timeout = 900
```

## How the diff works

omniviz ports the current [pixelmatch](https://github.com/mapbox/pixelmatch)
algorithm: colors are compared as OKLab HyAB distance with a 0..1 black-to-
white scale, and anti-aliased pixels are detected and excluded so edge
softening does not count as a regression. `threshold` (default 0.1) is the
max per-pixel distance as a fraction of black↔white; smaller is stricter.

On top of the per-pixel threshold omniviz adds a **changed-area budget**:
`max_changed` (default 0.01) caps the fraction of pixels that may differ at
all (above a small dither-noise floor). This is what catches *global* drift —
a fog-density or exposure shift changes every pixel slightly, which a
per-pixel threshold rightly tolerates but a visual regression suite must
not. A shot fails if EITHER budget is exceeded.

A size change (different resolution) is its own `size` failure — you almost
always want to notice that explicitly.

## Recordings

When `record` is on, the driver captures the whole scenario as frames
(godot: `--write-movie --fixed-fps`; web: timed screenshots; command: your
process's choice). Frames are decimated to `max_frames` (evenly spaced, last
frame kept) and served by the review UI's scrubber. They live under
`output_dir` (gitignored), never enter the diff, and cost disk only.

## Parallelism

`[defaults] parallel` (or `--parallel`) runs up to N capture jobs at once.
A job with `serial = true` is scheduled **exclusively**: nothing else runs
while it does. Use it for heavy full-game scenarios that want the whole GPU.
The web driver parallelizes naturally within one browser (tab per shot);
godot gives each job its own window (cascade-offset so they don't stack).

## Determinism tips

- Fix your seeds — and your iteration orders. (The cli-demo's first gauge
  drew from Go map iteration: random ring stacking, flagged on the very
  first run. The tool working.)
- For scenes that host a match, pass the engine's seed override as a shot
  arg and pin any time/phase-driven ambience before the capture.
- The godot driver launches Godot with a small unfocused window via a
  project-settings overlay, so capture runs don't fight your desktop or
  settings autoloads. Movie mode decouples simulation from wall-clock.
- For UI shots beware animated clocks/blinking cursors — the web driver
  freezes them for you; elsewhere, mask them or raise the threshold.

## CI/CD

`omniviz test` exits non-zero when any shot is `fail`, `size`, `error` or
`missing`; `--fail-on-new` also fails on unapproved `new` shots. Every run
writes `report.json` and `junit.xml` (GitLab test reports, GitHub test
reporters) into `output_dir`, and `omniviz summary --markdown` renders the
report for PR comments and `$GITHUB_STEP_SUMMARY`.

Baselines live in git, so a PR diffs against the base branch's baselines
automatically — the branching story hosted tools build servers for. Full
recipes (GitHub Actions incl. a composite action in
[`.github/actions/omniviz`](.github/actions/omniviz), GitLab CI with junit
reports, baseline seeding/acceptance flows, release binaries via
GoReleaser): **[docs/ci.md](docs/ci.md)**.

## AI agents

An importable skill teaches coding agents the tool inside and out — commands,
config schema, per-engine capture recipes, determinism troubleshooting:
[`skill/omniviz/SKILL.md`](skill/omniviz/SKILL.md). Import instructions for
pi / Claude Code / AGENTS.md in [`skill/README.md`](skill/README.md).

## Status

v0.1 — Linux is the tested path (X11 + Vulkan/Forward+ for godot; headless
chrome for web; anything for command). Windows/macOS need their display-driver
paths wired (the godot driver's structure allows it). License: MIT; the diff
algorithm is modelled on pixelmatch (ISC). omni-viz descends from
[gdviz](https://github.com/jshthornton/gdviz), the Godot-only ancestor —
its Godot capture machinery lives on as the `godot` driver.
