#!/usr/bin/env bash
# expected outcome: the three real Settings screens pass; security fails by
# design (its baseline is a different screen — the "layout changed" story).
set -eu
cd "$(dirname "$0")"
fails=$(grep -c '"status": "fail"' tmp/report.json || true)
passes=$(grep -c '"status": "pass"' tmp/report.json || true)
echo "passes=$passes fails=$fails"
[ "$passes" = 3 ] && [ "$fails" = 1 ]
