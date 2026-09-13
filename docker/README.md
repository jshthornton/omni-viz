# Container verification for engine examples

The recipe examples (`examples/*-capture`) teach each engine's capture
idiom. This directory **verifies them for real**, in containers, so nothing
depends on engines being installed on the host.

| engine | container strategy | license needed | status |
|---|---|---|---|
| WinForms | Debian + **Mono** (`System.Windows.Forms` runs on Linux) + xvfb | none | ✅ verified: PASS 2 · FAIL 2 (budget + per-pixel failures by design) |
| Delphi | Ubuntu + **Lazarus/LCL** (`GetFormImage`, the VCL primitive) + xvfb | none | ✅ verified: PASS 1 · FAIL 2 |
| Android | **SDK emulator** (KVM) + system Settings app as target — `adb screencap`, clock/battery icons hidden | none | ✅ verified: PASS 3 · FAIL 1, byte-stable captures |
| Unity | [GameCI](https://game.ci) `unityci/editor` | Unity Personal `.ulf` (free, from your account) | ⚙️ wired, needs license |
| Unreal | Epic's `ghcr.io/epicgames/unreal-engine` | Epic account + GitHub PAT (gated pull) | ⚙️ wired, needs auth |

## Usage

```bash
go build -o bin/omniviz ./cmd/omniviz
./docker/verify.sh                # winforms + delphi (fast, license-free)
./docker/verify.sh android        # boots an emulator (uses /dev/kvm)
./docker/verify.sh all
```

First run builds images (mono ~400MB, lazarus ~1GB, android ~4GB) and seeds
baselines inside each container project (clean renders; the by-design
failure shot gets a deliberately wrong reference). After that, every run is
a true regression run: clean shots must PASS, by-design shots must FAIL.

## How the verified pattern maps back to the recipes

- **WinForms**: mono's WinForms implements `DrawToBitmap` — the container
  proves the exact capture primitive the .NET version uses. Differences are
  text rendering metrics (different font stacks) → each keeps its own
  baselines; take .NET baselines on Windows for the real example.
- **Delphi**: Lazarus/LCL is the free sibling of VCL — `TForm.GetFormImage`
  is the identical primitive. The VCL `.dpr` in `examples/delphi-capture`
  is what Delphi shops compile; the LCL port here is the runnable proof.
- **Android**: the emulator runs real system images; `capture.sh` (same
  script as `examples/android-capture`) drives Settings via intent actions,
  so no APK build is needed to verify the flow end-to-end.
- **Unity/Unreal**: fully wired — supply credentials per the comments in
  their Containerfiles and the same verify loop applies.

## Why the app under test differs per container

VRT cares about two things: the capture mechanism and the determinism of
the pixels. The mechanism is per-engine (DrawToBitmap / GetFormImage /
screencap). The determinism story is per-**environment**: containerized
rendering (mono fonts, llvmpipe, a pinned AVD) differs from a desktop —
which is exactly why baselines live next to the environment that produces
them, and why the examples ship with their own baseline sets.
