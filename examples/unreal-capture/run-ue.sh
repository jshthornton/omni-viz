#!/usr/bin/env bash
# run-ue.sh — omniviz command-driver wrapper for Unreal Engine 5 captures.
#
# Resolves the engine ($UNREAL_EDITOR, then common install paths) and runs
# the project's test map in game mode. The map itself is the capture glue:
# its level blueprint should, on BeginPlay:
#     1. Delay 2s (post-processes, streaming, Lumen warm-up)
#     2. Execute Console Command: HighResShot 1280x720
#     3. Delay 1s
#     4. Execute Console Command: quit
# The screenshot lands in <project>/Saved/Screenshots/<Platform>/ which the
# omniviz shot collects via its paths glob.
#
# Per-shot args (from omniviz.toml) arrive as: MAPNAME [engine flags...]
set -eu

PROJECT="$(cd "$(dirname "$0")" && pwd)"
UPROJECT="$(ls "$PROJECT"/*.uproject 2>/dev/null | head -1)"
if [ -z "$UPROJECT" ]; then
  echo "run-ue: no .uproject found in $PROJECT" >&2
  exit 1
fi

if [ -n "${UNREAL_EDITOR:-}" ]; then
  UE="$UNREAL_EDITOR"
elif [ -x "$HOME/UnrealEngine/Engine/Binaries/Linux/UnrealEditor-Cmd" ]; then
  UE="$HOME/UnrealEngine/Engine/Binaries/Linux/UnrealEditor-Cmd"
elif [ -x "/c/Program Files/Epic Games/UE_5.4/Engine/Binaries/Win64/UnrealEditor-Cmd.exe" ]; then
  UE="/c/Program Files/Epic Games/UE_5.4/Engine/Binaries/Win64/UnrealEditor-Cmd.exe"
else
  echo "run-ue: no Unreal editor found (set UNREAL_EDITOR)" >&2
  exit 127
fi

MAP="$1"; shift

exec "$UE" "$UPROJECT" "$MAP" -game \
  -windowed -ResX=1280 -ResY=720 \
  -log "$@"
