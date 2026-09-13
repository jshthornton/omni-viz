#!/usr/bin/env bash
# expected outcome: dashboard+reports pass; drift+mismatch fail by design.
set -eu
cd "$(dirname "$0")"
fails=$(grep -c '"status": "fail"' tmp/report.json || true)
passes=$(grep -c '"status": "pass"' tmp/report.json || true)
echo "passes=$passes fails=$fails"
[ "$passes" = 2 ] && [ "$fails" = 2 ]
