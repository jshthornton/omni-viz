# omniviz examples

Every example ships failing shots **on purpose** so the review UI has
something to show immediately. Baselines are committed; `tmp/` is not.

| example | driver | capture mechanism | status |
|---|---|---|---|
| [`godot-minimal`](godot-minimal) | `godot` | engine movie-writer / cooperative scenes | ✅ engine-verified |
| [`cli-demo`](cli-demo) | `command` | any process — Go renderer stand-in | ✅ engine-verified (runs anywhere) |
| [`web-demo`](web-demo) | `web` | warm Chrome, tab per shot, animations frozen | ✅ engine-verified |
| [`unity-capture`](unity-capture) | `command` | `unity-editor -batchmode -executeMethod`, `ScreenCapture.CaptureScreenshot` | 📋 recipe |
| [`unreal-capture`](unreal-capture) | `command` | test maps + `HighResShot` console command + `quit` | 📋 recipe |
| [`winforms-capture`](winforms-capture) | `command` | GDI+ paint + `DrawToBitmap` (Windows, .NET 8) — verified via Mono in a container ([`docker/winforms`](../docker)) | ✅ container-verified |
| [`delphi-capture`](delphi-capture) | `command` | VCL paint + `GetFormImage` → `TPNGImage` (Delphi 11+) — verified via Lazarus/LCL in a container ([`docker/delphi`](../docker)) | ✅ container-verified |
| [`android-capture`](android-capture) | `command` | adb: force-stop → launch screen → `screencap` — verified against a real emulator ([`docker/android`](../docker)) | ✅ container-verified |

✅ = captured, diffed, reviewed and approved end-to-end in CI of this repo.
📋 = config shape validated (`omniviz shots`); the capture glue is the
engine's standard idiom, but you need the engine installed to run it.

## The pattern behind every recipe

The command driver gives your process a sandbox and expects images:

1. **one process per shot** — launch, save PNG(s), exit (exit code ≠ 0 = job error)
2. **write to `$OMNIVIZ_OUTPUT`** (per-job scratch, auto-collected) **or**
   anywhere in the project + a `paths` glob
3. receive `OMNIVIZ_PROJECT`, `OMNIVIZ_JOB`, `OMNIVIZ_WIDTH`, `OMNIVIZ_HEIGHT`
   and any `env`/`args` you declare

Everything else — scheduling, parallelism, the OKLab diff, changed-area
budget, recordings, review UI, approvals, CI exit codes — is the core, and
identical across all targets.

## Determinism checklist (applies to every engine)

- fix seeds **and iteration orders** (Go map iteration, shader nondeterminism,
  particle time — the usual suspects)
- pin the rendering API/quality level per project; switching between runs
  changes every pixel
- kill motion: animations, blink carets, clocks, status bars (the web driver
  freezes automatically; elsewhere, script it — see android's settings-put
  block or the godot overlay)
- capture baselines on the same OS/theme/DPI as CI
