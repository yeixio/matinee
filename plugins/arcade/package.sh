#!/usr/bin/env bash
# Puts the arcade plugin's package together:
#
#   plugins/arcade/package.sh <games.json.gz> <MAME builds folder> <MAME source> <out.matinee-plugin>
#
# The MAME builds folder holds <driver>.js and <driver>.wasm for each
# driver in drivers.txt, as the arcade workflow builds them; the MAME
# source is the checkout they were built from, for its license.
set -euo pipefail

games=$(realpath "$1")
builds=$(realpath "$2")
mame=$(realpath "$3")
out=$(realpath -m "$4")
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
version=$(cat "$here/mame-version")

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cp "$here/plugin.json" "$here/icon.png" "$work/"
cp "$games" "$work/games.json.gz"
cp -r "$here/player" "$work/player"
cp "$repo/pluginsdk/player/matinee-player.js" "$work/player/"
mkdir -p "$work/player/mame"
cp "$builds"/*.js "$builds"/*.wasm "$work/player/mame/"
(cd "$here" && GOOS=wasip1 GOARCH=wasm go build -trimpath -buildmode=c-shared -o "$work/server.wasm" .)

mkdir -p "$work/licenses"
cp "$repo/LICENSE" "$work/licenses/matinee-arcade.txt"
cp "$mame/COPYING" "$work/licenses/mame.txt"
cat > "$work/licenses/README.md" <<NOTICE
# Licenses

The arcade plugin (plugin.json, server.wasm and the player's own files) is
part of Matinee, under the GNU Affero General Public License, version 3 or
later (matinee-arcade.txt). Its source is at
https://github.com/yeixio/matinee/tree/main/plugins/arcade.

The player runs MAME ${version:0:1}.${version:1}, built to WebAssembly
(player/mame), under the GNU General Public License, version 2 or later
(mame.txt). Its source is at https://github.com/mamedev/mame/tree/mame$version;
the arcade workflow in Matinee's repository builds it.
NOTICE

rm -f "$out"
(cd "$work" && zip -qr -9 "$out" .)
echo "$out: $(du -h "$out" | cut -f1)"
