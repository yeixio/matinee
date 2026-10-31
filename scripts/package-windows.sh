#!/bin/sh
# Builds the Windows installers into dist/: Matinee-Setup-x64.exe and
# Matinee-Setup-arm64.exe, each with the server, the tray app
# (cmd/matinee-tray) and FFmpeg. Needs Go, makensis (NSIS 3), curl and unzip; run `make web` first.
# Usage: VERSION=1.2.3 scripts/package-windows.sh
# ARCHES="x64" builds only that one. With WINDOWS_SIGNING set up (see
# scripts/sign-windows.sh), the programs and installers are signed.
set -eu
: "${VERSION:?set VERSION, e.g. 1.2.3}"
test -f web/dist/index.html || { echo "build the web player first: make web" >&2; exit 1; }

# FFmpeg's own Windows builds, from the FFmpeg 8.1 release branch, GPL so
# H.264 and HEVC encoding work. The builds are rebuilt daily under the same
# names, so each download is checked against the same day's checksums.
FFMPEG_BASE=https://github.com/BtbN/FFmpeg-Builds/releases/download/latest
FFMPEG_SERIES=n8.1

# Windows version numbers are four numbers, so 0.2.0-beta.1 is 0.2.0.0.
numver="${VERSION%%-*}.0"

root=$(pwd)
mkdir -p dist
cache="${FFMPEG_CACHE:-$root/.cache/ffmpeg}"
mkdir -p "$cache"
curl -fsSL "$FFMPEG_BASE/checksums.sha256" -o "$cache/checksums.sha256"

sign=""
if [ -n "${WINDOWS_SIGNING:-}" ]; then
	sign="$root/scripts/sign-windows.sh"
fi

for arch in ${ARCHES:-x64 arm64}; do
	case $arch in
	x64) goarch=amd64 ffarch=win64 ;;
	arm64) goarch=arm64 ffarch=winarm64 ;;
	*) echo "unknown arch $arch" >&2; exit 1 ;;
	esac
	stage=$(mktemp -d)
	trap 'rm -rf "$stage"' EXIT

	CGO_ENABLED=0 GOOS=windows GOARCH=$goarch go build -trimpath \
		-ldflags "-s -w -X github.com/yeixio/matinee/internal/version.Version=$VERSION -X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=${TMDB_KEY:-}" \
		-o "$stage/matinee.exe" ./cmd/matinee

	# The tray app's icon, version and manifest go in a resource file next
	# to its code, which go build links in.
	go run github.com/tc-hib/go-winres@v0.3.3 simply \
		--arch "$goarch" --out cmd/matinee-tray/rsrc \
		--icon cmd/matinee-tray/icons/app-256.png --manifest gui \
		--product-name Matinee --file-description Matinee \
		--product-version "$numver" --file-version "$numver" \
		--original-filename matinee-tray.exe --copyright "AGPL-3.0-or-later"
	CGO_ENABLED=0 GOOS=windows GOARCH=$goarch go build -trimpath \
		-ldflags "-s -w -H=windowsgui -X github.com/yeixio/matinee/internal/version.Version=$VERSION" \
		-o "$stage/matinee-tray.exe" ./cmd/matinee-tray
	rm -f cmd/matinee-tray/rsrc_windows_*.syso

	zip="ffmpeg-$FFMPEG_SERIES-latest-$ffarch-gpl-shared-${FFMPEG_SERIES#n}.zip"
	want=$(awk -v f="$zip" '$2 == f { print $1 }' "$cache/checksums.sha256")
	test -n "$want" || { echo "no checksum for $zip" >&2; exit 1; }
	if [ ! -f "$cache/$zip" ] || [ "$(sha256sum "$cache/$zip" | cut -d' ' -f1)" != "$want" ]; then
		curl -fsSL "$FFMPEG_BASE/$zip" -o "$cache/$zip"
	fi
	echo "$want  $cache/$zip" | sha256sum -c -
	unzip -q -o "$cache/$zip" -d "$stage/unzip"
	mkdir -p "$stage/ffmpeg"
	src="$stage/unzip/${zip%.zip}"
	cp "$src/bin/ffmpeg.exe" "$src/bin/ffprobe.exe" "$src"/bin/*.dll "$stage/ffmpeg/"
	cp "$src/LICENSE.txt" "$stage/ffmpeg/LICENSE.txt"
	cat > "$stage/ffmpeg/README.txt" <<-TXT
		This is FFmpeg ($FFMPEG_SERIES), built by https://github.com/BtbN/FFmpeg-Builds
		and licensed under the GNU GPL version 3 (LICENSE.txt). Its source code is
		at https://ffmpeg.org/download.html and https://github.com/FFmpeg/FFmpeg.
	TXT
	rm -rf "$stage/unzip"
	cp LICENSE "$stage/LICENSE.txt"

	if [ -n "$sign" ]; then
		"$sign" "$stage/matinee.exe" "$stage/matinee-tray.exe"
	fi
	out="$root/dist/Matinee-Setup-$arch.exe"
	makensis -V2 -INPUTCHARSET UTF8 \
		-DVERSION="$VERSION" -DNUMVER="$numver" -DARCH="$arch" -DSTAGE="$stage" -DOUTFILE="$out" \
		${sign:+-DSIGN="$sign"} packaging/windows/matinee.nsi
	rm -rf "$stage"
	trap - EXIT
done
ls -l dist/Matinee-Setup-*
