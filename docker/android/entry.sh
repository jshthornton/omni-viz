#!/usr/bin/env bash
# boot emulator → seed-if-missing → test → check.
# NOTE: run this via `podman exec` on a detached container (see verify.sh) —
# as a rootless-podman PID 1 alongside the emulator, the host-side adb
# polling wedges; via exec it behaves normally.
set -u
cd "$(dirname "$0")"
export PATH="/repo/bin:/opt/android-sdk-linux/platform-tools:/opt/android-sdk-linux/emulator:$PATH"

SEEDED=1
[ -n "$(ls -A baselines 2>/dev/null)" ] || SEEDED=0

if [ "${1:-}" != "--no-boot" ]; then
  echo "booting emulator…"
  nohup emulator -avd test -no-window -gpu swiftshader_indirect -no-snapshot \
    -no-audio -no-boot-anim -accel on -no-metrics > /tmp/emu.log 2>&1 &
  timeout 900 adb wait-for-device || { echo "device never appeared" >&2; exit 1; }
  until [ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do
    sleep 5
  done
  echo "emulator booted"
fi

if [ "$SEEDED" = 0 ]; then
  echo "── seeding baselines (first run)…"
  omniviz capture >/dev/null
  mkdir -p baselines
  for k in sound display about; do
    cp "tmp/current/$k.png" "baselines/$k.png"
  done
  cp tmp/current/display.png baselines/security.png   # wrong ref on purpose
  echo "seeded: $(ls baselines | tr '\n' ' ')"
fi

echo "── omniviz test…"
omniviz test; test_rc=$?
echo "── check (test rc=$test_rc)…"
bash check.sh
