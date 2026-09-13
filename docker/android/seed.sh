#!/usr/bin/env bash
# Boot the emulator, wait for full boot, then seed baselines: real captures
# of Settings main/display/about; the security shot gets the DISPLAY screen
# as its (wrong) reference so it fails by design.
set -eu
cd "$(dirname "$0")"
export PATH="/repo/bin:/opt/android-sdk/platform-tools:/opt/android-sdk/emulator:$PATH"

if [ -n "$(ls -A baselines 2>/dev/null)" ]; then
  echo "baselines present — skipping seed (and skipping boot)"
  exit 0
fi

echo "booting emulator (KVM: ${KVM:-on})…"
emulator -avd test -no-window -gpu swiftshader_indirect -no-snapshot \
  -no-audio -no-boot-anim -accel "${KVM:-auto}" &
EMUPID=$!
trap 'kill $EMUPID 2>/dev/null || true' EXIT

adb wait-for-device
for i in $(seq 1 120); do
  [ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ] && break
  sleep 2
done
[ "$(adb shell getprop sys.boot_completed | tr -d '\r')" = "1" ] || { echo "emulator never finished booting" >&2; exit 1; }
echo "emulator booted"

omniviz capture >/dev/null
mkdir -p baselines
for k in settings-main display about; do
  cp "tmp/current/$k.png" "baselines/$k.png"
done
# deliberately wrong reference → the security shot fails by design
cp tmp/current/display.png baselines/security.png
echo "seeded: $(ls baselines | tr '\n' ' ')"
