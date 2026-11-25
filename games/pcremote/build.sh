#!/bin/sh
# Builds matinee-pcremote (pcremote.c), the bridge to a gaming PC running
# Sunshine, against moonlight-common-c at the commit in moonlight.txt, into
# $1 (as $1/matinee-pcremote, with moonlight-common-c's licence in
# $1/licenses/).
#
# Built for the machine it runs on, on Debian: the Dockerfile runs it in a
# stage of its own for each platform. Needs git, cmake, gcc, pkg-config and
# OpenSSL's -dev package. OpenSSL is linked in statically, so the program
# runs on the server image without it.
set -eu
out=$(mkdir -p "$1" && cd "$1" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
repo=$(sed -n 's/^repo=//p' "$here/moonlight.txt")
commit=$(sed -n 's/^commit=//p' "$here/moonlight.txt")
work=${WORK:-$(mktemp -d)}

src="$work/moonlight-common-c"
if [ ! -d "$src/.git" ]; then
	git init -q "$src"
	git -C "$src" remote add origin "$repo"
fi
git -C "$src" fetch -q --depth 1 origin "$commit"
git -C "$src" checkout -q FETCH_HEAD
git -C "$src" submodule update -q --init --depth 1

cmake -S "$src" -B "$work/build" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF >/dev/null
cmake --build "$work/build" -j "$(nproc 2>/dev/null || echo 4)" >/dev/null

b="$work/build"
${CC:-gcc} -O2 -Wall -o "$out/matinee-pcremote" "$here/pcremote.c" \
	-I"$src/src" \
	"$b/libmoonlight-common-c.a" "$b/enet/libenet.a" \
	-Wl,-Bstatic -lssl -lcrypto -Wl,-Bdynamic -lpthread -ldl
strip "$out/matinee-pcremote" 2>/dev/null || true

mkdir -p "$out/licenses"
cp "$src/LICENSE.txt" "$out/licenses/moonlight-common-c.txt"
cp "$src/enet/LICENSE" "$out/licenses/enet.txt"
echo "matinee-pcremote built with moonlight-common-c $commit"
