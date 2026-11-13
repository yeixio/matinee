#!/bin/sh
# Builds matinee-game-runner, the lag test and the emulators in cores.txt
# into OUT:
#
#   OUT/matinee-game-runner
#   OUT/cores/<name>_libretro.so, OUT/cores/lagtest.so
#   OUT/licenses/<name>.txt
#
# HOST cross-compiles, e.g. HOST=aarch64-linux-gnu with that gcc installed.
# On a Mac it builds for that Mac's CPU with its own compiler; the cores
# keep the .so name there too, so the server finds them the same way.
# KEEP_GOING=1 leaves out an emulator that doesn't build instead of failing.
set -eu
mkdir -p "${1:?usage: build.sh OUT}"
out=$(cd "$1" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
host=${HOST:-}
os=$(uname -s)
if [ "$os" = Darwin ]; then
	cc=clang cxx=clang++ strip=strip platform=osx
	# Older zlibs bundled with some emulators define fdopen away on a Mac,
	# which breaks the SDK's stdio.h unless it comes first. From the
	# environment, so a makefile's own CFLAGS add to it.
	export CFLAGS="${CFLAGS:-} -include stdio.h"
	jobs=$(sysctl -n hw.ncpu)
	runner_libs="-ldl -lm"
else
	cc=${host:+$host-}gcc cxx=${host:+$host-}g++ strip=${host:+$host-}strip platform=unix
	jobs=$(nproc 2>/dev/null || echo 4)
	# libm for every emulator, loaded into the runner: some forget to link it.
	runner_libs="-ldl -Wl,--no-as-needed -lm"
fi
mkdir -p "$out/cores" "$out/licenses"

# shellcheck disable=SC2086 # separate flags
"$cc" -O2 -Wall -o "$out/matinee-game-runner" "$here/runner/runner.c" $runner_libs
"$cc" -O2 -Wall -shared -fPIC -o "$out/cores/lagtest.so" "$here/testcore/testcore.c"
failed=""

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# The CPU being built for, in the names cores' makefiles use.
arch=$("$cc" -dumpmachine | cut -d- -f1)
case $arch in arm64) arch=aarch64 ;; arm*) arch=arm ;; esac

# broke NAME LINES: shows the end of NAME's build log, then stops, or with
# KEEP_GOING notes it and goes on to the next emulator.
broke() {
	tail -"$2" "$work/$1.log" 2>/dev/null || true
	if [ -z "${KEEP_GOING:-}" ]; then
		exit 1
	fi
	echo "== $1 left out: it didn't build or load"
	failed="$failed $1"
	rm -f "$out/cores/$1_libretro.so" "$out/licenses/$1.txt"
}

grep -v '^[[:space:]]*#' "$here/cores.txt" | grep -v '^[[:space:]]*$' > "$work/cores.txt"
exec 3< "$work/cores.txt"
while read -r name repo commit how vars <&3; do
	# only=x86_64,aarch64 builds it for those CPUs alone.
	only=$(echo "$vars" | tr ' ' '\n' | sed -n 's/^only=//p')
	# dir=sub/folder is where its libretro makefile is, when not at the top.
	dir=$(echo "$vars" | tr ' ' '\n' | sed -n 's/^dir=//p')
	vars=$(echo "$vars" | tr ' ' '\n' | grep -v '^only=' | grep -v '^dir=' | tr '\n' ' ')
	if [ -n "$only" ] && ! echo ",$only," | grep -q ",$arch,"; then
		echo "== $name: not for $arch"
		continue
	fi
	echo "== $name"
	src="$work/$name"
	git init -q "$src"
	git -C "$src" fetch -q --depth 1 "$repo" "$commit"
	git -C "$src" checkout -q FETCH_HEAD
	if [ -f "$src/.gitmodules" ]; then
		git -C "$src" submodule -q update --init --recursive --depth 1 ||
			git -C "$src" submodule -q update --init --recursive
	fi
	case $how in
	make)
		mk="$src${dir:+/$dir}"
		makefile=Makefile
		[ -f "$mk/Makefile.libretro" ] && makefile=Makefile.libretro
		# shellcheck disable=SC2086 # vars are separate settings
		if ! make -C "$mk" -f $makefile -j"$jobs" platform=$platform CC="$cc" CXX="$cxx" STRIP="$strip" $(echo "$vars" | sed "s/@ARCH@/$arch/g") \
			>"$work/$name.log" 2>&1; then
			broke "$name" 80
			continue
		fi
		lib="$mk/${name}_libretro.so"
		[ -f "$lib" ] || lib="$mk/${name}_libretro.dylib"
		cp "$lib" "$out/cores/${name}_libretro.so"
		;;
	cmake)
		set -- -DBUILD_LIBRETRO=ON -DBUILD_QT=OFF -DBUILD_SDL=OFF -DBUILD_GL=OFF -DBUILD_GLES2=OFF -DBUILD_GLES3=OFF \
			-DBUILD_SHARED=OFF -DBUILD_STATIC=OFF -DSKIP_LIBRARY=ON -DUSE_FFMPEG=OFF -DUSE_ZLIB=OFF -DUSE_PNG=OFF \
			-DUSE_LIBZIP=OFF -DUSE_MINIZIP=OFF -DUSE_SQLITE3=OFF -DUSE_ELF=OFF -DUSE_LZMA=OFF -DUSE_EDITLINE=OFF \
			-DUSE_EPOXY=OFF -DUSE_DISCORD_RPC=OFF -DUSE_LUA=OFF -DUSE_JSON_C=OFF -DENABLE_SCRIPTING=OFF -DENABLE_DEBUGGERS=OFF \
			-DCMAKE_BUILD_TYPE=Release -DCMAKE_C_COMPILER="$cc" -DCMAKE_CXX_COMPILER="$cxx" \
			-DCMAKE_POLICY_VERSION_MINIMUM=3.5
		if [ -n "$host" ]; then
			set -- "$@" -DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR="${host%%-*}"
		fi
		if ! { cmake -S "$src" -B "$src/build" "$@" >"$work/$name.log" 2>&1 &&
			cmake --build "$src/build" -j"$jobs" >>"$work/$name.log" 2>&1; }; then
			broke "$name" 50
			continue
		fi
		lib="$src/build/${name}_libretro.so"
		[ -f "$lib" ] || lib="$src/build/${name}_libretro.dylib"
		cp "$lib" "$out/cores/${name}_libretro.so"
		;;
	esac
	for l in COPYING Copying LICENSE LICENSE.txt License.txt LICENCE.txt GPLv3.txt mupen64plus-core/LICENSES stella/license.txt; do
		if [ -f "$src/$l" ]; then
			{ echo "$name, built from $repo at $commit"; echo; cat "$src/$l"; } >"$out/licenses/$name.txt"
			break
		fi
	done
	if [ "$os" = Darwin ]; then
		strip -x "$out/cores/${name}_libretro.so"
	else
		"$strip" --strip-unneeded "$out/cores/${name}_libretro.so"
	fi
	# Built for this computer, the runner can check it loads and says
	# which games it plays.
	if [ -z "$host" ] && ! "$out/matinee-game-runner" --info "$out/cores/${name}_libretro.so" >"$work/$name.log" 2>&1; then
		broke "$name" 20
		continue
	fi
	tail -1 "$work/$name.log"
done
exec 3<&-
if [ -n "$failed" ]; then
	echo "left out:$failed"
fi
