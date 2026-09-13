#!/usr/bin/env bash
# docker/verify.sh — spin up engine containers and verify the omniviz
# capture pipelines end-to-end, no engines installed on the host.
#
#   ./docker/verify.sh              # winforms + delphi (fast, no licenses)
#   ./docker/verify.sh android      # boots an emulator (needs /dev/kvm)
#   ./docker/verify.sh all
#
# Baselines are seeded on first run (clean renders; the by-design failure
# shot gets a deliberately wrong reference). After that every run is a true
# regression run, checked by each engine's check.sh.
#
# Requires: podman (or DOCKER=1 for docker). Run from the repo root.
set -eu

cd "$(dirname "$0")/.."
CTR="${CTR:-}"
if [ -z "$CTR" ]; then
  if command -v podman >/dev/null 2>&1; then CTR=podman; else CTR=docker; fi
fi
[ -x bin/omniviz ] || { echo "build omniviz first: go build -o bin/omniviz ./cmd/omniviz" >&2; exit 1; }

verify() {
  local engine="$1" rc=0
  echo "═══ $engine ═══"
  echo "── building image (first time is slow)…"
  "$CTR" build -t "omniviz-$engine" -f "docker/$engine/Containerfile" "docker/$engine"

  if [ "$engine" = android ]; then
    # detached + exec: as a rootless-podman PID 1 alongside the emulator the
    # script's adb wedges; via exec it behaves normally
    "$CTR" run -d --name "ov-$engine" --device /dev/kvm \
      -v "$PWD:/repo" -w "/repo/docker/$engine" "omniviz-$engine" sleep infinity >/dev/null
    echo "── running pipeline inside the container…"
    set +e
    "$CTR" exec "ov-$engine" bash /repo/docker/android/entry.sh
    rc=$?
    "$CTR" rm -f "ov-$engine" >/dev/null
    set -e
  else
    echo "── running pipeline inside the container…"
    set +e
    "$CTR" run --rm -v "$PWD:/repo" -w "/repo/docker/$engine" \
      "omniviz-$engine" bash /repo/docker/"$engine"/entry.sh
    rc=$?
    set -e
  fi
  [ $rc -eq 0 ] && echo "✓ $engine verified" || echo "✗ $engine FAILED (rc=$rc)"
  return $rc
}

case "${1:-winforms delphi}" in
  all) set -- winforms delphi android ;;
esac
rc=0
for engine in "$@"; do verify "$engine" || rc=1; done
exit $rc
