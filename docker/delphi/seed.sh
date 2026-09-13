#!/usr/bin/env bash
# seed baselines on first run: the clean LCL form render as the reference
# for the clean shot — and, deliberately wrong, for drift/mismatch too.
set -eu
cd "$(dirname "$0")"
export PATH="/repo/bin:$PATH"

if [ -n "$(ls -A baselines 2>/dev/null)" ]; then
  echo "baselines present — skipping seed"
  exit 0
fi
omniviz capture >/dev/null
mkdir -p baselines
cp tmp/current/delphi.png baselines/delphi.png
cp tmp/current/delphi.png baselines/drift.png
cp tmp/current/delphi.png baselines/mismatch.png
echo "seeded: $(ls baselines | tr '\n' ' ')"
