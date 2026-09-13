# omniviz example (recipe) — Android via the command driver
#
# Each shot drives an emulator/device with adb: force-stop → pin animation
# and night-mode settings → hide clock/battery status icons → launch one
# screen (deeplink, launcher, or intent action) → settle → `screencap`.
# The PNG lands in $OMNIVIZ_OUTPUT, auto-collected.
#
# Requires: platform-tools (`adb`) on PATH and one booted emulator/device.
# The capture flow is verified end-to-end against a real emulator in
# [`docker/android`](../../docker) — config shape validated here, captures
# byte-stable there.
baseline_dir = "baselines"
output_dir = "tmp"

[render]
width = 1080
height = 2400        # match your emulator's `wm size` — pin it in CI

[defaults]
driver = "command"
record = false
threshold = 0.1
max_changed = 0.01
timeout = 120

[driver.command]
command = ["./capture.sh", "--app=com.example.app"]

[[shot]]
name = "main"
args = ["--screen=main"]

[[shot]]
name = "settings"
args = ["--screen=settings"]        # app://settings deeplink

[[shot]]
name = "profile"
args = ["--screen=profile"]

## Determinism notes
#
# • the script disables the three animation scales and rotation, pins light
#   mode, and hides the clock/battery status icons (secure icon_blacklist +
#   SystemUI restart) — a moving clock is AA-noise beyond the perceptual
#   threshold even when 0.0% of the frame changed
# • alternative on builds that support it: SystemUI demo mode via
#   `com.android.systemui.demo` broadcasts
# • multi-device CI: export ANDROID_SERIAL (adb honors it) or pass --device
# • navigate per shot via exported deeplinks (`am start -a
#   android.intent.action.VIEW -d "app://screen"`); system screens use
#   intent actions instead (--action=android.settings.DISPLAY_SETTINGS)
# • cold-start each shot (force-stop first) so state never leaks
