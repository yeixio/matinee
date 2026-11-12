#!/bin/sh
# Builds matinee-game-runner, the lag test and the emulators in cores.txt
# into OUT:
#
#   OUT/matinee-game-runner
#   OUT/cores/<name>_libretro.so, OUT/cores/lagtest.so
#   OUT/licenses/<name>.txt
#
# HOST cross-compiles, e.g. HOST=aarch64-linux-gnu with that gcc installed.
set -eu
out=$(realpath -m "${1:?usage: build.sh OUT}")
here=$(cd "$(dirname "$0")" && pwd)
host=${HOST:-}
cc=${host:+$host-}gcc
cxx=${host:+$host-}g++
jobs=$(nproc 2>/dev/null || echo 4)
mkdir -p "$out/cores" "$out/licenses"

"$cc" -O2 -Wall -o "$out/matinee-game-runner" "$here/runner/runner.c" -ldl
"$cc" -O2 -Wall -shared -fPIC -o "$out/cores/lagtest.so" "$here/testcore/testcore.c"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
grep -v '^\s*#' "$here/cores.txt" | grep -v '^\s*$' | while read -r name repo commit how; do
	echo "== $name"
	src="$work/$name"
	git init -q "$src"
	git -C "$src" fetch -q --depth 1 "$repo" "$commit"
	git -C "$src" checkout -q FETCH_HEAD
	case $how in
	make)
		make -C "$src" -f Makefile.libretro -j"$jobs" platform=unix CC="$cc" CXX="$cxx" >"$work/$name.log" 2>&1 ||
			{ tail -50 "$work/$name.log"; exit 1; }
		cp "$src/${name}_libretro.so" "$out/cores/"
		;;
	cmake)
		set -- -DBUILD_LIBRETRO=ON -DBUILD_QT=OFF -DBUILD_SDL=OFF -DBUILD_GL=OFF -DBUILD_GLES2=OFF -DBUILD_GLES3=OFF \
			-DBUILD_SHARED=OFF -DBUILD_STATIC=OFF -DSKIP_LIBRARY=ON -DUSE_FFMPEG=OFF -DUSE_ZLIB=OFF -DUSE_PNG=OFF \
			-DUSE_LIBZIP=OFF -DUSE_MINIZIP=OFF -DUSE_SQLITE3=OFF -DUSE_ELF=OFF -DUSE_LZMA=OFF -DUSE_EDITLINE=OFF \
			-DUSE_EPOXY=OFF -DUSE_DISCORD_RPC=OFF -DUSE_LUA=OFF -DUSE_JSON_C=OFF -DENABLE_SCRIPTING=OFF -DENABLE_DEBUGGERS=OFF \
			-DCMAKE_BUILD_TYPE=Release -DCMAKE_C_COMPILER="$cc" -DCMAKE_CXX_COMPILER="$cxx"
		if [ -n "$host" ]; then
			set -- "$@" -DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR="${host%%-*}"
		fi
		cmake -S "$src" -B "$src/build" "$@" >"$work/$name.log" 2>&1 &&
			cmake --build "$src/build" -j"$jobs" >>"$work/$name.log" 2>&1 ||
			{ tail -50 "$work/$name.log"; exit 1; }
		cp "$src/build/${name}_libretro.so" "$out/cores/"
		;;
	esac
	for l in COPYING Copying LICENSE LICENSE.txt; do
		if [ -f "$src/$l" ]; then
			{ echo "$name, built from $repo at $commit"; echo; cat "$src/$l"; } >"$out/licenses/$name.txt"
			break
		fi
	done
	"${host:+$host-}strip" --strip-unneeded "$out/cores/${name}_libretro.so"
done
