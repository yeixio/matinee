#!/bin/bash
# Builds universal (arm64 + x86_64) ffmpeg and ffprobe for Matinee Server.app
# from source: FFmpeg with x264 for software encoding and VideoToolbox for the
# Mac's hardware encoder, linked statically so they need nothing but macOS.
# Usage: scripts/build-mac-ffmpeg.sh <out dir>   (on a Mac, with nasm and
# pkg-config from Homebrew)
set -euo pipefail

FFMPEG_VERSION=7.1.2
FFMPEG_SHA256=${FFMPEG_SHA256:-}
# x264's stable branch, pinned to a commit.
X264_COMMIT=${X264_COMMIT:-}
MACOS_MIN=13.0
# Assembly files too, not only C, are built for this macOS.
export MACOSX_DEPLOYMENT_TARGET=$MACOS_MIN

out="$(mkdir -p "$1" && cd "$1" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
jobs="$(sysctl -n hw.ncpu)"

curl -fsSL --retry 3 -o "$work/ffmpeg.tar.xz" "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"
sum="$(shasum -a 256 "$work/ffmpeg.tar.xz" | cut -d' ' -f1)"
if [ -z "$FFMPEG_SHA256" ]; then
	echo "::warning::FFmpeg ${FFMPEG_VERSION} isn't pinned; its SHA-256 is $sum"
elif [ "$sum" != "$FFMPEG_SHA256" ]; then
	echo "::error::FFmpeg ${FFMPEG_VERSION} download has SHA-256 $sum, expected $FFMPEG_SHA256"
	exit 1
fi
tar -C "$work" -xf "$work/ffmpeg.tar.xz"

git clone -q --branch stable https://code.videolan.org/videolan/x264.git "$work/x264"
if [ -n "$X264_COMMIT" ]; then
	git -C "$work/x264" checkout -q "$X264_COMMIT"
else
	echo "::warning::x264 isn't pinned; stable is at $(git -C "$work/x264" rev-parse HEAD)"
fi

for arch in arm64 x86_64; do
	prefix="$work/prefix-$arch"
	flags="-arch $arch -mmacosx-version-min=$MACOS_MIN"
	host=aarch64-apple-darwin ffarch=aarch64
	[ "$arch" = x86_64 ] && host=x86_64-apple-darwin ffarch=x86_64
	cross=()
	[ "$arch" != "$(uname -m)" ] && cross=(--enable-cross-compile)

	echo "::group::x264 ($arch)"
	(cd "$work/x264" && git clean -qfdx &&
		CC=clang ./configure --prefix="$prefix" --host="$host" --enable-static --disable-cli --enable-pic \
			--extra-cflags="$flags" --extra-ldflags="$flags" &&
		make -j"$jobs" && make install)
	echo "::endgroup::"

	echo "::group::FFmpeg ($arch)"
	src="$work/ffmpeg-$arch"
	cp -R "$work/ffmpeg-${FFMPEG_VERSION}" "$src"
	# Only what's named here: nothing from Homebrew sneaks in as a library
	# the app wouldn't have.
	(cd "$src" && PKG_CONFIG_PATH="$prefix/lib/pkgconfig" ./configure \
		--prefix="$prefix" --cc=clang --arch="$ffarch" --target-os=darwin ${cross[@]+"${cross[@]}"} \
		--extra-cflags="$flags -I$prefix/include" --extra-ldflags="$flags -L$prefix/lib" \
		--pkg-config-flags=--static --extra-libs=-liconv \
		--enable-gpl --enable-libx264 \
		--disable-autodetect --enable-videotoolbox --enable-audiotoolbox \
		--enable-zlib --enable-bzlib --enable-iconv --enable-securetransport \
		--disable-shared --enable-static --disable-doc --disable-ffplay --disable-debug &&
		make -j"$jobs" ffmpeg ffprobe)
	cp "$src/ffmpeg" "$work/ffmpeg-$arch.bin"
	cp "$src/ffprobe" "$work/ffprobe-$arch.bin"
	echo "::endgroup::"
done

for tool in ffmpeg ffprobe; do
	lipo -create -output "$out/$tool" "$work/$tool-arm64.bin" "$work/$tool-x86_64.bin"
	strip -x "$out/$tool"
	# Only system libraries, so it runs on any Mac.
	if otool -L "$out/$tool" | tail -n +2 | grep -Ev '^\s*(/usr/lib/|/System/)'; then
		echo "::error::$tool links a library outside macOS"
		exit 1
	fi
done
"$out/ffmpeg" -hide_banner -encoders | grep -q libx264
"$out/ffmpeg" -hide_banner -encoders | grep -q h264_videotoolbox

cat > "$out/FFmpeg.txt" <<NOTICE
Matinee Server includes ffmpeg and ffprobe from FFmpeg ${FFMPEG_VERSION}
(https://ffmpeg.org), built with x264 ($(git -C "$work/x264" rev-parse HEAD),
https://code.videolan.org/videolan/x264). Both are under the GNU General Public
License, version 2 or later. Their source is at the addresses above, and
scripts/build-mac-ffmpeg.sh in https://github.com/yeixio/matinee builds them.

Configuration: $("$out/ffmpeg" -hide_banner -buildconf 2>&1 | tr -s ' \n' ' ')
NOTICE
ls -l "$out"
