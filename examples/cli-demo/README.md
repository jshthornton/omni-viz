# omniviz example — command driver (visual testing *any* process)

The command driver runs any executable per shot and collects the PNGs it
writes. This example uses a tiny Go renderer as the stand-in for "whatever
you want to screenshot":

- a Unity editor in batchmode (`-batchmode -executeMethod CaptureShot`)
- an Unreal automation commandlet
- a .NET Forms/WPF app that saves its own screenshot
- a TUI captured in a headless terminal

…anything that can save an image and exit. Runs anywhere Go runs — no GPU
or engine needed:

```bash
omniviz test        # PASS 4 · FAIL 2 — two failures ship by design
omniviz review      # inspect the failures: budget vs per-pixel
omniviz approve --all
```

## What's in the demo

| shot | collected via | what it shows |
|---|---|---|
| `report` | auto (scratch dir) | a dashboard image — **passes** |
| `bar` `line` `gauge` | `paths` glob, multi mode | three panels, one shot each — **pass** |
| `drift` | auto | global brightness shift — **fails the changed-area budget** (0 px beyond the per-pixel threshold) |
| `mismatch` | auto | a collapsed bar + swung gauge — **fails the per-pixel threshold** |

## The capture contract

The driver gives your process a sandbox and expects images:

```toml
[[shot]]
name = "report"
driver_options = { command = ["go", "run", "./render", "--set=report"] }
# or: driver_options = { script = "python render.py", shell = "bash" }
```

Environment your process runs with:

| var | meaning |
|---|---|
| `OMNIVIZ_OUTPUT` | per-job scratch dir — PNGs written here are auto-collected when the shot has no `paths` |
| `OMNIVIZ_PROJECT` | project root (write anywhere in it; collect with `paths`) |
| `OMNIVIZ_JOB` / `OMNIVIZ_WIDTH` / `OMNIVIZ_HEIGHT` | job id and target canvas size |

Plus the usual `[defaults]`/shot-level `env` and `args`, `serial`, `timeout`,
per-job `dir`, and multi mode (no `name` → one shot per matched file, named
by file stem).

History note: this demo's first `gauge` implementation stacked its rings by
iterating a Go **map** — random order — and omniviz flagged it immediately.
That's the tool working: fix the seed/order, not the threshold.
