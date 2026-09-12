#!/usr/bin/env bash
# run-unity.sh — omniviz command-driver wrapper for Unity batchmode captures.
#
# Resolves the editor ($UNITY_EDITOR, then common install paths) and launches
# it with the VisualTestCapture glue. Job args (--scene=..., --seed=...) pass
# through untouched; the command driver appends them after this script's argv.
set -eu

PROJECT="$(cd "$(dirname "$0")" && pwd)"

if [ -n "${UNITY_EDITOR:-}" ]; then
  UNITY="$UNITY_EDITOR"
elif [ -x "/Applications/Unity/Hub/Editor/Current/Unity.app/Contents/MacOS/Unity" ]; then
  UNITY="/Applications/Unity/Hub/Editor/Current/Unity.app/Contents/MacOS/Unity"
elif command -v unity-editor >/dev/null 2>&1; then
  UNITY="unity-editor"
elif [ -x "/opt/unity-editor/Editor/Unity" ]; then
  UNITY="/opt/unity-editor/Editor/Unity"
else
  echo "run-unity: no Unity editor found (set UNITY_EDITOR)" >&2
  exit 127
fi

exec "$UNITY" \
  -batchmode \
  -projectPath "$PROJECT" \
  -executeMethod VisualTestCapture.Capture \
  -logFile "$PROJECT/tmp/omniviz-unity.log" \
  "$@"
