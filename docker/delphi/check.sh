#!/usr/bin/env bash
# expected outcome: delphi passes; drift (changed-area budget) and mismatch
# (per-pixel threshold) fail by design.
set -eu
cd "$(dirname "$0")"
fails=$(grep -c '"status": "fail"' tmp/report.json || true)
passes=$(grep -c '"status": "pass"' tmp/report.json || true)
echo "passes=$passes fails=$fails"
[ "$passes" = 1 ] && [ "$fails" = 2 ]
