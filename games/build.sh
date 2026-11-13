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
strip=${host:+$host-}strip
jobs=$(nproc 2>/dev/null || echo 4)
mkdir -p "$out/cores" "$out/licenses"

# libm for every emulator, loaded into the runner: some forget to link it.
"$cc" -O2 -Wall -o "$out/matinee-game-runner" "$here/runner/runner.c" -ldl -Wl,--no-as-needed -lm
"$cc" -O2 -Wall -shared -fPIC -o "$out/cores/lagtest.so" "$here/testcore/testcore.c"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# The CPU being built for, in the names cores' makefiles use.
arch=$(${host:+$host-}gcc -dumpmachine | cut -d- -f1)
case $arch in arm*) arch=arm ;; esac

grep -v '^\s*#' "$here/cores.txt" | grep -v '^\s*$' | while read -r name repo commit how vars; do
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
		make -C "$mk" -f $makefile -j"$jobs" platform=unix CC="$cc" CXX="$cxx" STRIP="$strip" $(echo "$vars" | sed "s/@ARCH@/$arch/g") \
			>"$work/$name.log" 2>&1 ||
			{ tail -80 "$work/$name.log"; exit 1; }
		cp "$mk/${name}_libretro.so" "$out/cores/"
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
	for l in COPYING Copying LICENSE LICENSE.txt License.txt LICENCE.txt GPLv3.txt mupen64plus-core/LICENSES stella/license.txt; do
		if [ -f "$src/$l" ]; then
			{ echo "$name, built from $repo at $commit"; echo; cat "$src/$l"; } >"$out/licenses/$name.txt"
			break
		fi
	done
	"${host:+$host-}strip" --strip-unneeded "$out/cores/${name}_libretro.so"
	# Built for this computer, the runner can check it loads and says
	# which games it plays.
	if [ -z "$host" ]; then
		"$out/matinee-game-runner" --info "$out/cores/${name}_libretro.so" || exit 1
	fi
done
