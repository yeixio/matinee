#!/bin/sh
# Builds matinee-psremote (psremote.c), the PlayStation Remote Play bridge,
# against chiaki-ng's library at the commit in chiaki.txt, into $1 (as
# $1/matinee-psremote, with chiaki-ng's licence in $1/licenses/).
#
# Built for the machine it runs on, on Debian: the Dockerfile runs it in a
# stage of its own for each platform. Needs git, cmake, gcc, python3,
# protobuf-compiler, python3-protobuf, pkg-config, and the -dev packages of
# OpenSSL, json-c, miniupnpc and libevent. They're linked in statically, so
# the program runs on the server image without them.
set -eu
out=$(mkdir -p "$1" && cd "$1" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
repo=$(sed -n 's/^repo=//p' "$here/chiaki.txt")
commit=$(sed -n 's/^commit=//p' "$here/chiaki.txt")
work=${WORK:-$(mktemp -d)}

src="$work/chiaki-ng"
if [ ! -d "$src/.git" ]; then
	git init -q "$src"
	git -C "$src" remote add origin "$repo"
fi
git -C "$src" fetch -q --depth 1 origin "$commit"
git -C "$src" checkout -q FETCH_HEAD
git -C "$src" submodule update -q --init --depth 1 third-party/nanopb third-party/jerasure third-party/gf-complete third-party/curl

# Only the library: no app, no decoders (the players decode), no
# microphone, no Steam shortcuts.
cmake -S "$src" -B "$work/build" -DCMAKE_BUILD_TYPE=Release \
	-DCHIAKI_ENABLE_GUI=OFF -DCHIAKI_ENABLE_CLI=OFF -DCHIAKI_ENABLE_TESTS=OFF \
	-DCHIAKI_ENABLE_STEAM_SHORTCUT=OFF -DCHIAKI_ENABLE_FFMPEG_DECODER=OFF -DCHIAKI_ENABLE_PI_DECODER=OFF \
	-DCHIAKI_ENABLE_SETSU=OFF -DCHIAKI_ENABLE_STEAMDECK_NATIVE=OFF -DCHIAKI_ENABLE_SPEEX=OFF \
	-DCHIAKI_LIB_ENABLE_OPUS=OFF -DCHIAKI_ENABLE_BOREALIS=OFF >/dev/null
cmake --build "$work/build" --target chiaki-lib -j "$(nproc 2>/dev/null || echo 4)" >/dev/null

b="$work/build"
${CC:-gcc} -O2 -Wall -o "$out/matinee-psremote" "$here/psremote.c" \
	-I"$src/lib/include" -I"$b/lib/include" \
	"$b/lib/libchiaki.a" "$b/third-party/nanopb/libprotobuf-nanopb.a" \
	"$b/third-party/libjerasure.a" "$b/third-party/libgf_complete.a" "$b/third-party/curl/lib/libcurl.a" \
	-Wl,-Bstatic -ljson-c -lminiupnpc -levent -lssl -lcrypto -Wl,-Bdynamic -lz -lpthread -ldl
strip "$out/matinee-psremote" 2>/dev/null || true

mkdir -p "$out/licenses"
cp "$src/LICENSES/AGPL-3.0-only-OpenSSL.txt" "$out/licenses/chiaki-ng.txt"
echo "matinee-psremote built with chiaki-ng $commit"
