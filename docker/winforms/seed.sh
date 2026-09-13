#!/usr/bin/env bash
# seed baselines on first run: clean renders for the clean shots, and the
# CLEAN dashboard render as the (wrong) reference for drift/mismatch —
# the standard omniviz example pattern.
set -eu
cd "$(dirname "$0")"
export PATH="/repo/bin:$PATH"

if [ -n "$(ls -A baselines 2>/dev/null)" ]; then
  echo "baselines present — skipping seed"
  exit 0
fi
omniviz capture >/dev/null
mkdir -p baselines
for k in dashboard reports; do
  cp "tmp/current/$k.png" "baselines/$k.png"
done
cp tmp/current/dashboard.png baselines/drift.png
cp tmp/current/dashboard.png baselines/mismatch.png
echo "seeded: $(ls baselines | tr '\n' ' ')"
