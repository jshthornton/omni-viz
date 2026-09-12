#!/usr/bin/env bash
# capture.sh — omniviz command-driver wrapper for Android captures.
#
# Per shot: force-stop the app, pin animation/night-mode/rotation settings
# off for determinism, launch one screen (deeplink preferred), wait for
# settle, then `adb exec-out screencap -p` the display into $OMNIVIZ_OUTPUT.
#
# usage: capture.sh --app=com.example.app --screen=main [--device=SERIAL]
#        [--settle=2] [--deeplink-scheme=app]
#
# Multi-device CI: export ANDROID_SERIAL (adb honors it) or pass --device.
set -eu

APP="" SCREEN="main" SETTLE=2 SCHEME="app" DEVICE_ARGS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --app=*) APP="${1#*=}" ;;
    --screen=*) SCREEN="${1#*=}" ;;
    --settle=*) SETTLE="${1#*=}" ;;
    --scheme=*) SCHEME="${1#*=}" ;;
    --device=*) DEVICE_ARGS=(-s "${1#*=}") ;;
    *) echo "capture: unknown arg $1" >&2; exit 2 ;;
  esac
  shift
done
[ -n "$APP" ] || { echo "capture: --app is required" >&2; exit 2; }

OUT="${OMNIVIZ_OUTPUT:?capture: OMNIVIZ_OUTPUT is not set}"

adb "${DEVICE_ARGS[@]}" wait-for-device

# determinism: animations off, one rotation, light mode, fixed brightness —
# every setting a flaky screenshot has ever blamed
adb "${DEVICE_ARGS[@]}" shell settings put global window_animation_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put global transition_animation_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put global animator_duration_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put system accelerometer_rotation 0
adb "${DEVICE_ARGS[@]}" shell cmd uimode night no

# cold start each time: no state leaks between shots
adb "${DEVICE_ARGS[@]}" shell am force-stop "$APP"
sleep 1

# one screen per shot: deeplink if the app exports them, else activity class
if [ "$SCREEN" = "main" ]; then
  adb "${DEVICE_ARGS[@]}" shell monkey -p "$APP" -c android.intent.category.LAUNCHER 1 >/dev/null
else
  adb "${DEVICE_ARGS[@]}" shell am start -a android.intent.action.VIEW \
    -d "${SCHEME}://${SCREEN}" >/dev/null
fi

# settle: let images decode and layouts finish (animations are already dead)
sleep "$SETTLE"

mkdir -p "$OUT"
adb "${DEVICE_ARGS[@]}" exec-out screencap -p > "$OUT/$SCREEN.png"
