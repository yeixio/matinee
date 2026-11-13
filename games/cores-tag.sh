#!/bin/sh
# Prints the release the built emulators are stored in: game-cores-<12 hex
# of a hash of what they're built from>. The "Game emulators" workflow
# (.github/workflows/game-cores.yml) publishes it; the Dockerfile and the
# packages download it. Any change to the list, the build script or the
# runner names a new release, so stale emulators are never reused.
set -eu
dir=${1:-$(dirname "$0")}
h=$(cat "$dir/cores.txt" "$dir/build.sh" "$dir/runner/runner.c" "$dir/runner/libretro.h" "$dir/testcore/testcore.c" | sha256sum | cut -c1-12)
echo "game-cores-$h"
