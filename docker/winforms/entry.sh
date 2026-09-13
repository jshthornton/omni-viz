#!/usr/bin/env bash
# seed-if-missing → test → check; test rc is allowed to be 1 (by-design
# failures need attention), check.sh enforces the expected statuses.
set -u
cd "$(dirname "$0")"
export PATH="/repo/bin:$PATH"
bash seed.sh
omniviz test; test_rc=$?
echo "── check (test rc=$test_rc)…"
bash check.sh
