#!/usr/bin/env bash
# capture.sh — omniviz command-driver wrapper for Android captures.
#
# Per shot: force-stop the app, pin animation/night-mode/rotation settings
# off for determinism, launch one screen (deeplink preferred), wait for
# settle, then `adb exec-out screencap -p` the display into $OMNIVIZ_OUTPUT.
#
# usage: capture.sh --app=com.example.app --screen=main [--device=SERIAL]
#        [--settle=2] [--deeplink-scheme=app] [--action=android.settings.X]
#
# --action launches an intent ACTION instead of a deeplink — this is how
# you drive built-in screens (Settings, Display, About) when verifying
# against a system app with no exported deeplinks.
#
# Multi-device CI: export ANDROID_SERIAL (adb honors it) or pass --device.
set -eu

APP="" SCREEN="main" SETTLE=2 SCHEME="app" ACTION="" DEVICE_ARGS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --app=*) APP="${1#*=}" ;;
    --screen=*) SCREEN="${1#*=}" ;;
    --settle=*) SETTLE="${1#*=}" ;;
    --scheme=*) SCHEME="${1#*=}" ;;
    --action=*) ACTION="${1#*=}" ;;
    --device=*) DEVICE_ARGS=(-s "${1#*=}") ;;
    *) echo "capture: unknown arg $1" >&2; exit 2 ;;
  esac
  shift
done
if [ -z "$APP" ] && [ -z "$ACTION" ]; then
  echo "capture: --app is required (or --action for system screens)" >&2
  exit 2
fi

OUT="${OMNIVIZ_OUTPUT:?capture: OMNIVIZ_OUTPUT is not set}"

adb "${DEVICE_ARGS[@]}" wait-for-device

# determinism: animations off, one rotation, light mode, fixed brightness —
# every setting a flaky screenshot has ever blamed
adb "${DEVICE_ARGS[@]}" shell settings put global window_animation_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put global transition_animation_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put global animator_duration_scale 0
adb "${DEVICE_ARGS[@]}" shell settings put system accelerometer_rotation 0
adb "${DEVICE_ARGS[@]}" shell cmd uimode night no

# status-bar determinism: hide the clock and battery icons (they move as
# time passes and charge drains — AA noise beyond the threshold even at
# 0.0% changed). icon_blacklist is a standard SystemUI secure setting;
# it is only read at SystemUI start, so restart SystemUI after setting it.
# (Alternative on builds that support it: SystemUI demo mode via
#  `com.android.systemui.demo` broadcasts — this image ignores them.)
adb "${DEVICE_ARGS[@]}" shell settings put secure icon_blacklist clock,battery
adb "${DEVICE_ARGS[@]}" shell am force-stop com.android.systemui
sleep 3

# cold start each time: no state leaks between shots
[ -n "$APP" ] && adb "${DEVICE_ARGS[@]}" shell am force-stop "$APP"
sleep 1

# one screen per shot: explicit intent action (system screens), deeplink if
# the app exports them, else the launcher activity
if [ -n "$ACTION" ]; then
  adb "${DEVICE_ARGS[@]}" shell am start -a "$ACTION" >/dev/null
elif [ "$SCREEN" = "main" ]; then
  adb "${DEVICE_ARGS[@]}" shell monkey -p "$APP" -c android.intent.category.LAUNCHER 1 >/dev/null
else
  adb "${DEVICE_ARGS[@]}" shell am start -a android.intent.action.VIEW \
    -d "${SCHEME}://${SCREEN}" >/dev/null
fi

# settle: let images decode and layouts finish (animations are already dead)
sleep "$SETTLE"

mkdir -p "$OUT"
adb "${DEVICE_ARGS[@]}" exec-out screencap -p > "$OUT/$SCREEN.png"
