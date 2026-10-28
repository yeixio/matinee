#!/usr/bin/env bash
# Builds MAME to WebAssembly for one driver, as the arcade player loads it:
#
#   plugins/arcade/build-mame.sh <MAME source> <drivers.txt line> <out folder>
#
# A drivers.txt line is a driver's source file under src/mame, such as
# pacman/pacman.cpp, and any more files it needs after a comma. The build
# lands in <out folder>/pacman_pacman.js and .wasm. Needs Emscripten
# (emsdk-version) on the PATH; mame-version is the source it's built from.
set -euo pipefail

src=$1
line=$2
out=$(realpath -m "$3")

driver=${line%%,*}
name=$(echo "$driver" | sed 's#/#_#g; s#\.cpp$##')
sources=$(echo "$line" | tr ',' '\n' | sed 's#^#src/mame/#' | paste -sd, -)

cd "$src"
# The player writes ROM sets into Emscripten's file system and reads saved
# states back out, so the build exports it along with cwrap.
if ! grep -q "\['cwrap','FS'\]" scripts/genie.lua; then
  perl -pi -e "s/\['cwrap'\]/['cwrap','FS']/" scripts/genie.lua
  grep -q "\['cwrap','FS'\]" scripts/genie.lua
fi

emmake make -j"$(nproc)" SUBTARGET="$name" SOURCES="$sources" NOWERROR=1 REGENIE=1

mkdir -p "$out"
cp "mame$name.js" "$out/$name.js"
cp "mame$name.wasm" "$out/$name.wasm"
ls -la "$out/$name".*
